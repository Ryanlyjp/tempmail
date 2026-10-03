"""Run only against the isolated local test stack (port 18088)."""
import json
import urllib.request
from playwright.sync_api import sync_playwright

BASE = "http://127.0.0.1:18088"
KEY = "external-test-admin-key"
req = urllib.request.Request(BASE + "/api/me", headers={"Authorization": "Bearer " + KEY})
with urllib.request.urlopen(req) as response:
    me = json.load(response)
account = me.get("account", me)
assert account.get("is_admin")

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    context = browser.new_context(viewport={"width": 1440, "height": 1000}, permissions=["clipboard-read", "clipboard-write"])
    context.add_init_script("localStorage.setItem('tm_apikey', " + json.dumps(KEY) + ");localStorage.setItem('tm_account', " + json.dumps(json.dumps(account)) + ");")
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    page.goto(BASE)
    page.locator('[data-page="admin-external"]').click()
    page.locator('#external-add').click()
    page.locator('#external-address').fill('browser-fixture@gmail.com')
    page.locator('#external-password').fill('fixture-not-a-real-password')
    page.locator('#external-enabled').uncheck()
    page.locator('#modal-confirm-btn').click()
    page.locator('[data-add-child]').wait_for()
    page.locator('[data-add-child]').click()
    page.locator('#external-child-address').fill('alice.test+tag@example.org')
    page.locator('#modal-confirm-btn').click()
    page.locator('[data-mailbox]').filter(has_text='alice.test+tag@example.org').click()
    page.locator('#external-share').click()
    page.locator('#modal-confirm-btn').click()
    key_input = page.locator('#external-copy-0')
    key_input.wait_for()
    share_key = key_input.input_value()
    assert share_key.startswith('alice.test+tag_') and '@' not in share_key
    page.locator('[data-copy="0"]').click()
    assert page.evaluate('navigator.clipboard.readText()') == share_key
    page.locator('[data-copy="2"]').click()
    assert '/public/external-otp/latest?format=text' in page.evaluate('navigator.clipboard.readText()')
    share_url = page.locator('#external-copy-1').input_value()
    shared = context.new_page()
    shared.on('pageerror', lambda error: errors.append(str(error)))
    shared.goto(share_url)
    shared.wait_for_function("document.querySelector('#share-mailbox').textContent === 'alice.test+tag@example.org'")
    page.locator('#external-share-enabled').uncheck()
    page.locator('#modal-confirm-btn').click()
    page.locator('#external-copy-0').wait_for()
    shared.reload()
    shared.wait_for_function("document.querySelector('#share-mailbox').textContent.includes('invalid')")
    page.locator('.modal-close').click()
    page.screenshot(path='/tmp/external-desktop.png', full_page=True)
    page.set_viewport_size({'width': 390, 'height': 844})
    page.screenshot(path='/tmp/external-mobile.png', full_page=True)
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth + 1'), 'mobile horizontal overflow'
    assert not errors, errors
    print('BROWSER_OK: admin navigation, account, child mailbox, key name, clipboard, share page, stop, desktop/mobile')
    browser.close()
