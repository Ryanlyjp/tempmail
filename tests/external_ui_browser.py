"""UI regression fixtures; serve frontend on loopback :18089, no real API used."""
import json
from urllib.parse import urlparse, parse_qs
from playwright.sync_api import sync_playwright

BASE = 'http://127.0.0.1:18089'
ADMIN = {'id': 'admin', 'username': 'UI test', 'is_admin': True}
ACCOUNT = {'id': 'account-a', 'address': 'parent@gmail.com', 'provider': 'gmail',
           'enabled': True, 'tg_enabled': False, 'last_sync': '2026-09-26T12:00:00Z'}
accounts = [ACCOUNT, {'id': 'account-b', 'address': 'second@outlook.com', 'provider': 'outlook', 'enabled': True, 'tg_enabled': False},
            {'id': 'account-c', 'address': 'delete@yahoo.com', 'provider': 'yahoo', 'enabled': True, 'tg_enabled': False}]
CHILD = {'id': 'child-a', 'address': 'child@example.org', 'count': 3, 'tg_enabled': False}
children = [CHILD] + [{'id': f'child-{i}', 'address': f'child-{i}@example.org', 'count': i, 'tg_enabled': False} for i in range(2, 15)]
SHARE = {'mailbox_id': CHILD['id'], 'address': CHILD['address'], 'api_key': 'child_fixture', 'enabled': True}
IMAGE = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jD1sAAAAASUVORK5CYII='
MAIL = {'id': 'mail-a', 'subject': 'Fixture HTML', 'sender': 'sender@example.net',
        'recipient': CHILD['address'], 'received_at': '2026-09-26T12:00:00Z',
        'body_html': '<p>Mailbox fixture</p><img src="' + IMAGE + '"><a href="https://example.org/" target="_blank">Open link</a><script>parent.fixtureExecuted=true</script>',
        'attachments': [{'id': 1, 'filename': 'fixture.txt', 'size_bytes': 7, 'content_type': 'text/plain'}]}
shares = [SHARE.copy()]
requests = []


def api(route):
    path = urlparse(route.request.url).path
    method = route.request.method
    requests.append((method, path))
    result = {}
    if path == '/api/admin/external/accounts':
        result = {'accounts': accounts}
    elif path.endswith('/accounts/account-a/mailboxes'):
        result = {'mailboxes': children}
    elif '/accounts/' in path and path.endswith('/mailboxes'):
        result = {'mailboxes': []}
    elif path == '/api/admin/external/shares':
        result = {'shares': shares}
    elif path.endswith('/child-a/share'):
        if method == 'DELETE':
            shares.clear()
        elif method == 'PUT':
            values = route.request.post_data_json
            item = shares[0] if shares else SHARE.copy()
            item['enabled'] = values['enabled']
            if values.get('rotate'):
                item['api_key'] = 'child_rotated'
            shares[:] = [item]
    elif path.endswith('/account-a/tg'):
        ACCOUNT['tg_enabled'] = route.request.post_data_json['tg_enabled']
        result = {'message': '提醒设置已更新'}
    elif path.endswith('/account-c') and method == 'DELETE':
        accounts[:] = [a for a in accounts if a['id'] != 'account-c']
        result = {'message': '外部邮箱已删除'}
    elif path.endswith('/mailboxes/child-2') and method == 'DELETE':
        children[:] = [b for b in children if b['id'] != 'child-2']
        result = {'message': '子邮箱记录已删除'}
    elif path.endswith('/attachments/1'):
        assert route.request.headers['authorization'] == 'Bearer ui-test'
        route.fulfill(status=200, body='fixture', content_type='text/plain')
        return
    elif path.endswith('/otp/latest') or path.endswith('/otp'):
        result = {'otp': {'code': 'ABC-DEF'}}
    elif path.endswith('/emails/mail-a'):
        result = {'email': MAIL}
    elif path.endswith('/emails/mail-b'):
        result = {'email': {**MAIL, 'id': 'mail-b', 'subject': 'Fixture text', 'body_html': '', 'body_text': '<plain text>', 'attachments': []}}
    elif path.endswith('/emails') and '/external/' in path:
        page = int(parse_qs(urlparse(route.request.url).query).get('page', ['1'])[0])
        result = {'emails': [MAIL, {**MAIL, 'id': 'mail-b', 'subject': 'Fixture text'}] if page == 1 else [{**MAIL, 'id': 'mail-b', 'subject': 'Fixture text'}], 'size': 2, 'total': 3}
    elif path.endswith('/sync'):
        result = {'message': 'Queued'}
    elif path == '/api/me':
        result = {'account': ADMIN}
    elif path == '/api/mailboxes':
        result = {'data': [], 'total': 0}
    elif path == '/api/domains':
        result = {'domains': []}
    elif path == '/api/favorite-groups':
        result = {'groups': []}
    elif path == '/public/settings':
        result = {'mailbox_page_size': '2'}
    route.fulfill(status=200, json=result)


with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    context = browser.new_context(viewport={'width': 1440, 'height': 1000}, permissions=['clipboard-read', 'clipboard-write'])
    context.add_init_script("localStorage.setItem('tm_apikey','ui-test');localStorage.setItem('tm_account'," + json.dumps(json.dumps(ADMIN)) + ');')
    context.route('**/api/**', api)
    context.route('**/public/**', api)
    page = context.new_page()
    errors = []
    page.on('pageerror', lambda e: errors.append(str(e)))
    page.goto(BASE)
    page.locator('#pane-mailboxes').wait_for()
    page.locator('[data-page="admin-external"]').click()
    page.locator('#external-account-pagination').get_by_text('1 / 2').wait_for()
    assert page.locator('[data-mailbox="child-a"]').count() == 0, 'children must start collapsed'
    assert 'Gmail' not in page.locator('#external-mailboxes').inner_text()
    assert page.locator('#external-account-otp').is_visible()
    assert page.locator('#external-account-otp').evaluate("el => el.nextElementSibling?.id === 'external-sync'")
    page.locator('#external-account-otp').click()
    page.wait_for_function("navigator.clipboard.readText().then(t => t === 'ABC-DEF')")
    page.locator('[data-expand="account-a"]').click()
    page.locator('[data-mailbox="child-a"]').click()
    page.locator('#external-delete-mailbox').wait_for()
    assert page.locator('#external-account-otp').count() == 0
    page.locator('[data-email="mail-a"]').wait_for()
    assert page.locator('.external-grid > .pane:visible').count() == 3
    page.locator('[data-email="mail-a"]').click()
    frame = page.frame_locator('#external-body iframe')
    frame.get_by_text('Mailbox fixture').wait_for()
    assert frame.locator('img').evaluate('(img) => img.complete && img.naturalWidth > 0')
    assert not page.evaluate('window.fixtureExecuted || false')
    assert page.locator('.attachment-item').count() == 1
    with page.expect_download() as download:
        page.locator('[data-attachment="1"]').click()
    assert download.value.suggested_filename == 'fixture.txt'
    with page.expect_popup() as popup:
        frame.get_by_text('Open link').click()
    popup.value.close()
    page.locator('#external-detail-otp').click()
    page.wait_for_function("navigator.clipboard.readText().then(t => t === 'ABC-DEF')")
    ACCOUNT['last_sync'] = '2026-09-26T13:00:00Z'
    page.wait_for_function("externalState.accounts.find(a => a.id === 'account-a')?.last_sync.includes('13:00')")
    assert frame.get_by_text('Mailbox fixture').is_visible(), 'poll replaced open message'
    page.locator('[data-email="mail-b"]').click()
    page.locator('.email-body-text').wait_for()
    assert page.locator('.email-body-text').inner_text() == '<plain text>'
    page.evaluate("externalSetView('mailboxes')")
    page.locator('[data-expand="account-a"]').click()
    page.locator('[data-expand="account-a"]').click()
    page.locator('[data-mailbox="child-a"]').wait_for()
    assert page.locator('.external-child-list').evaluate('(el) => el.scrollHeight > el.clientHeight')
    page.locator('[data-copy-account="account-a"]').click()
    page.wait_for_function("navigator.clipboard.readText().then(t => t === 'parent@gmail.com')")
    page.locator('[data-account-otp="account-a"]').click()
    page.wait_for_function("navigator.clipboard.readText().then(t => t === 'ABC-DEF')")
    page.locator('[data-account-tg="account-a"]').click()
    page.wait_for_function("document.querySelector('[data-account-tg=\"account-a\"]')?.title.includes('开启')")
    page.locator('[data-mailbox="child-a"]').click()
    page.locator('#external-mailbox-id').click()
    page.wait_for_function("navigator.clipboard.readText().then(t => t === 'child-a')")
    page.evaluate("externalSetView('mailboxes')")
    page.locator('[data-mailbox="child-2"]').click()
    page.locator('#external-delete-mailbox').click()
    assert page.locator('.modal').get_by_text('以后收到发往该地址的新邮件时，子邮箱会被自动重新发现。', exact=False).is_visible()
    assert page.locator('#modal-confirm-btn').inner_text() == '确认删除'
    page.locator('#modal-confirm-btn').click()
    page.wait_for_function("!document.querySelector('[data-mailbox=\"child-2\"]')")
    assert page.locator('#external-list-title').inner_text() == '全部收到的邮件'
    page.evaluate("externalSetView('mailboxes')")
    page.locator('#external-account-page-jump').click()
    page.locator('#external-account-page-input').fill('2')
    page.locator('#modal-confirm-btn').click()
    page.locator('[data-delete-account="account-c"]').wait_for()
    page.locator('[data-delete-account="account-c"]').click()
    assert page.locator('.modal').get_by_text('将永久删除整个母邮箱账号、该母邮箱同步的全部邮件、其下所有子邮箱及所有对应分享/API Key。').is_visible()
    assert page.locator('#modal-confirm-btn').inner_text() == '确认删除'
    page.locator('#modal-confirm-btn').click()
    page.wait_for_function("!document.querySelector('[data-delete-account=\"account-c\"]')")
    assert page.locator('#external-account-pagination').count() == 1
    assert not page.locator('#external-account-pagination').is_visible()
    page.locator('#external-next').click()
    page.wait_for_function("document.querySelector('#external-pagination').textContent.includes('2 / 2')")
    page.locator('#external-prev').click()
    page.locator('#external-manage-shares').click()
    page.locator('[data-share="0"]').click()
    page.locator('[data-copy="2"]').click()
    assert '/public/external-otp/latest?format=text' in page.evaluate('navigator.clipboard.readText()')
    page.locator('#external-share-rotate').check()
    page.locator('#external-share-days').fill('7')
    page.locator('#modal-confirm-btn').click()
    page.wait_for_function("document.querySelector('#external-copy-0')?.value === 'child_rotated'")
    page.locator('#external-share-enabled').uncheck()
    page.locator('#modal-confirm-btn').click()
    page.wait_for_function("document.querySelector('#external-shares').textContent.includes('已停止')")
    page.locator('#external-copy-0').wait_for()
    page.once('dialog', lambda dialog: dialog.accept())
    page.locator('#external-share-revoke').click()
    page.wait_for_function("document.querySelector('#external-shares').textContent.includes('暂无分享')")
    page.get_by_role('button', name='← 返回外部邮箱').click()
    page.locator('#external-add').click()
    assert page.locator('#external-help a[href="https://myaccount.google.com/apppasswords"]').count() == 1
    page.wait_for_function("document.querySelector('#external-password').getAttribute('data-1p-ignore') === 'true'")
    assert page.locator('#external-password').get_attribute('type') == 'password'
    assert page.locator('#external-password').get_attribute('autocomplete') == 'new-password'
    assert page.locator('#external-address').get_attribute('autocomplete') == 'off'
    page.locator('#external-address').fill('typing@gmail.com')
    assert page.locator('#external-address').input_value() == 'typing@gmail.com'
    page.locator('.modal-close').click()
    page.screenshot(path='/tmp/external-ui-desktop.png', full_page=True)
    page.set_viewport_size({'width': 390, 'height': 844})
    page.evaluate("externalSetView('mailboxes')")
    page.locator('[data-expand="account-a"]').click()
    page.locator('[data-mailbox="child-a"]').click()
    page.locator('.pane-emails').wait_for(state='visible')
    assert not page.locator('.pane-mailboxes').is_visible()
    page.locator('[data-email="mail-a"]').click()
    page.locator('#external-detail-otp').wait_for()
    assert page.locator('#external-detail').is_visible()
    page.locator('#external-detail .pane-back-btn').click()
    page.locator('.pane-emails .pane-back-btn').click()
    assert page.locator('.pane-mailboxes').is_visible()
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth + 1')
    page.screenshot(path='/tmp/external-ui-mobile.png', full_page=True)
    assert not errors, errors
    assert all('/external/' in path for method, path in requests if method != 'GET'), 'legacy API mutation'
    login = browser.new_page()
    login.route('**/api/**', api)
    login.route('**/public/**', api)
    login.goto(BASE)
    login.locator('input').first.wait_for()
    login.wait_for_function("[...document.querySelectorAll('input')].every(e => e.dataset.lpignore === 'true')")
    login.goto(BASE + '/otp-share.html')
    login.evaluate("document.body.insertAdjacentHTML('beforeend', '<input id=policy-probe>')")
    login.wait_for_function("document.querySelector('#policy-probe').autocomplete === 'off'")
    print('UI_OK: collapsed parents, scroll, global-size pagination/jump, parent actions/custom deletion, clickable child ID, three panes, mail rendering, shares, input policy, mobile; no legacy API mutations')
    browser.close()
