#!/usr/bin/env python3
"""End-to-end check of the web page's Feishu accounts (#1321, docs/extensions/feishu.md "Multiple accounts"): the "Messages" page's account chips,
a chat of the second account read and named as that account, its inbox item opening in it, and Settings › Connectors listing
the accounts.

Needs a running web page against the fake hub with two Feishu accounts and Playwright for Python:

    MOCK_LARK_ACCOUNTS=1 PORT=18833 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18833 npx vite --port 5333 --strictPort &)
    python3 scripts/test/web_lark_accounts_e2e.py http://127.0.0.1:5333 [--shots <dir>] [--chrome]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves light / dark / narrow screenshots.
"""
import argparse
import os
import sys

from playwright.sync_api import expect, sync_playwright


def ok(msg):
    print("ok  " + msg, flush=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    ap.add_argument("--chrome", action="store_true", help="drive the installed Chrome instead of Playwright's Chromium")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel="chrome") if a.chrome else p.chromium.launch()
        try:
            run(browser, a.base.rstrip("/"), a.token, a.shots)
        finally:
            browser.close()
    print("PASS web Feishu accounts e2e")


def run(browser, base, token, shots):
    ctx = browser.new_context(viewport={"width": 1280, "height": 820}, device_scale_factor=2)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    lark = []
    page.on("request", lambda r: "/api/v1/lark/" in r.url and lark.append(r.url))

    def shot(name):
        if not shots:
            return
        for theme in ("light", "dark"):
            page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")
            page.reload()
            page.wait_for_timeout(900)
            page.screenshot(path=os.path.join(shots, f"{name}-{theme}.png"))
        page.evaluate("localStorage.setItem('persona.theme', 'light')")

    page.goto(base + "/?token=" + token)
    page.evaluate("localStorage.setItem('lark.homeMessages', '1'); localStorage.removeItem('lark.account')")

    # 1. "Messages": the hub's own account first, a chip per signed-in account (not the signed-out one).
    page.goto(base + "/messages")
    accounts = page.get_by_role("group", name="飞书账号")
    expect(accounts.get_by_role("button")).to_have_count(2)
    own, work = accounts.get_by_role("button").nth(0), accounts.get_by_role("button").nth(1)
    expect(own).to_have_attribute("aria-pressed", "true")
    expect(work).to_contain_text("工作号")
    expect(page.locator('[data-chat="f2"]').first).to_be_visible()
    ok("消息 lists the hub's own account's chats, chips for the two signed-in accounts")

    # 2. The work account: its own chats, read as account 2.
    lark.clear()
    work.click()
    expect(work).to_have_attribute("aria-pressed", "true")
    expect(page.locator('[data-chat="w1"]').first).to_be_visible()
    expect(page.locator('[data-chat="f2"]')).to_have_count(0)
    assert any("/lark/conversations?" in u and "ol_account=2" in u for u in lark), lark
    ok("工作号 shows its own chats, read with ol_account=2")
    shot("messages-second")

    # 3. Its chat: messages read as account 2, the header names the account.
    lark.clear()
    page.locator('a[data-chat="w1"]').last.click()
    expect(page.get_by_text("下周一前把排期填一下").first).to_be_visible()
    expect(page.get_by_text("群聊 · 工作号")).to_be_visible()
    assert any("/lark/conversations/w1/messages" in u and "ol_account=2" in u for u in lark), lark
    ok("工作号's chat is read with ol_account=2, its header says 工作号")
    shot("messages-second-chat")

    # 4. The choice is kept: a reload stays on the work account.
    page.goto(base + "/messages")
    expect(page.get_by_role("group", name="飞书账号").get_by_role("button").nth(1)).to_have_attribute("aria-pressed", "true")
    ok("the account choice survives a reload")

    # 5. The inbox item from the work account opens its chat in that account.
    lark.clear()
    page.goto(base + "/inbox/" + "lark%3Aaccount%3A2%3Aw1%3A10")
    expect(page.get_by_text("下周一前把排期填一下").first).to_be_visible()
    assert any("/lark/conversations/w1/messages" in u and "ol_account=2" in u for u in lark), lark
    ok("工作号's inbox item opens its chat with ol_account=2")
    shot("inbox-second")

    # 6. Settings › Connectors: every account, the default one marked, the signed-out one too.
    page.goto(base + "/settings?tab=connectors")
    for i in ("1", "2", "3"):
        expect(page.locator(f'[data-account="{i}"]')).to_be_visible()
    expect(page.locator('[data-account="1"]').get_by_text("默认")).to_be_visible()
    expect(page.locator('[data-account="3"]').get_by_text("未登录")).to_be_visible()
    ok("设置 › 连接器 lists the accounts")
    if shots:
        page.locator('[data-account="1"]').scroll_into_view_if_needed()
    shot("settings-accounts")

    # 7. Narrow: the chips sit above the list.
    narrow = browser.new_context(viewport={"width": 390, "height": 844}, device_scale_factor=3)
    p2 = narrow.new_page()
    p2.goto(base + "/?token=" + token)
    p2.evaluate("localStorage.setItem('lark.homeMessages', '1')")
    p2.goto(base + "/messages")
    expect(p2.get_by_role("group", name="飞书账号").get_by_role("button")).to_have_count(2)
    if shots:
        p2.wait_for_timeout(900)
        p2.screenshot(path=os.path.join(shots, "messages-narrow-light.png"))
    ok("narrow: account chips above the list")
    narrow.close()
    ctx.close()


if __name__ == "__main__":
    try:
        main()
    except AssertionError as e:
        print(f"FAIL {e}", file=sys.stderr)
        sys.exit(1)
