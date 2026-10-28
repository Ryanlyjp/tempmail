"""Catch-all settings UI regression; serve frontend on loopback :18089."""
import json
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import sync_playwright


BASE = "http://127.0.0.1:18089"
ADMIN = {"id": "admin", "username": "UI admin", "is_admin": True}
ACCOUNT_IDS = [f"00000000-0000-0000-0000-{index:012d}" for index in range(1, 102)]
updates = []


def api(route):
    parsed = urlparse(route.request.url)
    path = parsed.path
    method = route.request.method
    if path == "/api/admin/accounts":
        page = int(parse_qs(parsed.query).get("page", ["1"])[0])
        start = (page - 1) * 100
        items = [
            {
                "id": account_id,
                "username": f"account-{index + 1}",
                "is_admin": index == 0,
                "is_active": True,
            }
            for index, account_id in enumerate(ACCOUNT_IDS)
        ][start:start + 100]
        route.fulfill(json={"data": items, "total": len(ACCOUNT_IDS), "page": page, "size": 100})
        return
    if path == "/api/admin/settings":
        if method == "PUT":
            updates.append(route.request.post_data_json)
            route.fulfill(json={"message": "settings updated"})
        else:
            route.fulfill(json={
                "catchall_enabled": "true",
                "catchall_account_id": ACCOUNT_IDS[-1],
            })
        return
    if path == "/api/admin/hostnames":
        route.fulfill(json={"hostnames": []})
        return
    if path == "/api/domains":
        route.fulfill(json={"domains": []})
        return
    if path == "/public/settings":
        route.fulfill(json={})
        return
    route.fulfill(json={})


with sync_playwright() as playwright:
    browser = playwright.chromium.launch(headless=True)
    context = browser.new_context(viewport={"width": 1280, "height": 900})
    context.add_init_script(
        "localStorage.setItem('tm_apikey','ui-test');"
        f"localStorage.setItem('tm_account',{json.dumps(json.dumps(ADMIN))});"
    )
    context.route("**/api/**", api)
    context.route("**/public/**", api)
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    page.goto(BASE)
    page.evaluate("navigate('admin-settings')")

    select = page.locator("#input-catchall-account-id")
    select.wait_for()
    assert select.locator("option").count() == 102
    assert select.input_value() == ACCOUNT_IDS[-1]
    assert "account-101" in select.locator("option:checked").inner_text()
    assert select.get_attribute("data-1p-ignore") == "true"

    select.select_option(ACCOUNT_IDS[1])
    select.locator("xpath=following-sibling::button").click()
    page.wait_for_function("() => document.querySelector('#toast-container').textContent.includes('Catch-all 已开启')")
    assert updates[-1] == {
        "catchall_enabled": "true",
        "catchall_account_id": ACCOUNT_IDS[1],
    }
    select.locator("xpath=ancestor::div[contains(@class,'form-group')]").screenshot(
        path="/tmp/catchall-settings-desktop.png"
    )

    page.set_viewport_size({"width": 390, "height": 844})
    select.scroll_into_view_if_needed()
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth + 1")
    select.locator("xpath=ancestor::div[contains(@class,'form-group')]").screenshot(
        path="/tmp/catchall-settings-mobile.png"
    )
    assert not errors, errors
    print("UI_OK: Catch-all uses the paginated account selector and saves the selected owner")
    browser.close()
