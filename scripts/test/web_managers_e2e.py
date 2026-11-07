#!/usr/bin/env python3
"""Check the web's multi-manager pages (docs/managers.md "Clients") against the mock hub's three managers.

Run with a dedicated fake hub and the web dev server pointing to it (the default roster: 助手 default, 工作 online,
家里 offline; MOCK_EXTENSIONS=1 for the extensions page, MOCK_MANAGER_NAS=1 gives 家里 the NAS, offline with it, which the
machines checks need; MOCK_MANAGER_OUTDATED=1 marks 工作 needs_update, for --outdated):
    PORT=<port> MOCK_EXTENSIONS=1 MOCK_MANAGER_NAS=1 MOCK_MANAGER_OUTDATED=1 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:<port> npx vite --port <web port> --strictPort &)
    python3 scripts/test/web_managers_e2e.py http://127.0.0.1:<web port> --outdated [--shots <directory>]
and for one manager (MOCK_MANAGERS=1 MOCK_EXTENSIONS=1 on another mock): web_managers_e2e.py <its web address> --single.
It moves the NAS to 工作 and back to 家里, so every round (390 / 1200, light / dark) starts alike.

Checks the assistant page's switcher (header sent, offline greyed with the composer off), grouping on the machines,
extensions and inbox pages, another manager's inbox item (its address names the manager, and so do its requests, the
Trail's too), the Muted dialog's groups, and Settings › Managers' Notifications switch; --outdated its Needs update
badge; --single checks none of it shows with one manager and that no request names a manager (no X-Persona-Manager
header, no manager= in the address). Screenshots (390 and 1200 wide, light and dark) contain only mock data.
"""
import argparse
import os
import re
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import Error as PlaywrightError, expect, sync_playwright


def go(page, url):
    """page.goto, once more when a navigation the page started itself (a dialog closed with Back) cut it short."""
    try:
        page.goto(url)
    except PlaywrightError as e:
        if "ERR_ABORTED" not in str(e):
            raise
        page.wait_for_timeout(500)
        page.goto(url)


def check(browser, base, width, theme, shots, single, outdated):
    context = browser.new_context(viewport={"width": width, "height": 900}, color_scheme=theme, locale="zh-CN")
    context.add_init_script("localStorage.setItem('persona.theme', " + repr(theme) + ")")
    page = context.new_page()
    page.set_default_timeout(15000)
    errors, assistant_headers, api_requests = [], [], []
    page.on("pageerror", lambda error: errors.append(str(error)))
    page.on("request", lambda r: "/api/v1/assistant" in r.url and assistant_headers.append(r.headers.get("x-persona-manager")))
    # every request to the hub, with the manager it names (header or query), for the checks below
    page.on("request", lambda r: "/api/" in r.url and api_requests.append((r.method, r.url, r.headers.get("x-persona-manager"), (parse_qs(urlparse(r.url).query).get("manager") or [None])[0])))
    name = lambda what: os.path.join(shots, f"managers-{what}-{width}-{theme}.png")
    try:
        page.goto(base + "/?token=mock")
        main = page.locator("main")
        switcher = page.get_by_role("radiogroup", name="管理者")
        if single:
            # signed in and the assistant page drawn (its box): only then does no switcher mean none
            page.wait_for_function("localStorage.getItem('persona.token')")
            expect(main.get_by_role("textbox").last).to_be_visible()
            expect(switcher).to_have_count(0)
            page.goto(base + "/machines")
            expect(main.get_by_text("MacBook", exact=True).first).to_be_visible()
            expect(main.get_by_text("工作", exact=True)).to_have_count(0)
            # the pages that name a manager with several: with one, none of their requests may name it
            page.goto(base + "/inbox")
            row = main.locator("[data-item]").first
            expect(row).to_be_visible()
            row.locator("a").first.click()
            page.wait_for_url(re.compile(r"/inbox/[^/?]+"))
            assert "manager=" not in page.url, page.url
            page.wait_for_timeout(1200)
            page.goto(base + "/inbox")  # a phone shows the list again only without an item open
            main.get_by_role("button", name="更多操作", exact=True).first.click()
            page.get_by_role("menuitem", name="已静音").click()
            expect(page.get_by_role("dialog")).to_be_visible()
            page.wait_for_timeout(400)
            page.keyboard.press("Escape")
            expect(page.get_by_role("dialog")).to_have_count(0)  # closing it goes back in history: let it land
            go(page, base + "/settings?tab=extensions")
            main.locator("button").filter(has=page.locator("span.truncate")).first.click()
            page.wait_for_timeout(1200)
            page.goto(base + "/health")
            page.wait_for_timeout(1000)
            page.goto(base + "/machines/macbook")
            page.wait_for_timeout(1000)
            named = [r for r in api_requests if r[2] or r[3]]
            assert not named, named[:5]
            assert len(api_requests) > 20, len(api_requests)
            assert not errors, errors
            return
        expect(switcher).to_be_visible()
        expect(switcher.get_by_role("radio")).to_have_count(3)  # the one still installing is left out
        expect(switcher.get_by_role("radio", name="助手")).to_have_attribute("aria-checked", "true")
        if shots:
            page.wait_for_timeout(500)
            page.screenshot(path=name("assistant"))
        assert all(h is None for h in assistant_headers), assistant_headers
        switcher.get_by_role("radio", name="工作").click()
        expect(switcher.get_by_role("radio", name="工作")).to_have_attribute("aria-checked", "true")
        expect(main.get_by_role("textbox").last).to_be_enabled()
        page.wait_for_timeout(500)
        assert "m-3fa21c" in assistant_headers, assistant_headers
        switcher.get_by_role("radio", name="家里").click()
        box = main.get_by_role("textbox").last
        expect(box).to_be_disabled()
        assert "最后同步于" in (box.get_attribute("placeholder") or ""), box.get_attribute("placeholder")
        expect(main.get_by_text("家里离线，能看历史，暂时不能发消息。", exact=True)).to_be_visible()
        if shots:
            page.wait_for_timeout(500)
            page.screenshot(path=name("assistant-offline"))
        switcher.get_by_role("radio", name="助手").click()
        expect(main.get_by_role("textbox").last).to_be_enabled()

        page.goto(base + "/machines")
        for heading in ("助手", "工作", "家里"):
            expect(main.get_by_text(heading, exact=True).first).to_be_visible()
        expect(main.get_by_text("最后同步于", exact=False).first).to_be_visible()
        # 家里's NAS: offline with its manager, said in our words (not the hub's), and no Reconnect through it
        # the computer's card (the token usage section names the NAS too)
        nas_card = main.locator("section:not([data-testid])").filter(has=page.get_by_text("NAS", exact=True))
        expect(nas_card.locator('[data-error-code="manager_offline"]')).to_have_text("它的管理者「家里」离线，管理者回来后这台电脑会恢复在线。")
        expect(nas_card.get_by_role("button", name="重新连接")).to_have_count(0)
        assert "助手暂时连不上" not in (nas_card.inner_text() or ""), nas_card.inner_text()
        if shots:
            page.wait_for_timeout(600)
            page.screenshot(path=name("machines"))

        page.goto(base + "/settings?tab=extensions")
        expect(main.get_by_text("工作", exact=True).first).to_be_visible()
        if shots:
            page.wait_for_timeout(600)
            page.screenshot(path=name("extensions"))

        page.goto(base + "/inbox")
        expect(main.get_by_text("工作", exact=True).first).to_be_visible()
        if shots:
            page.wait_for_timeout(600)
            page.screenshot(path=name("inbox"))
        # 工作's item: its address names 工作, and so does every request about it (the item, its Trail, its Lark chat)
        before = len(api_requests)
        main.locator('[data-item="lark:f3:300"] a').first.click()
        expect(page).to_have_url(re.compile(r"/inbox/lark(:|%3A)f3(:|%3A)300\?manager=m-3fa21c$"))
        page.wait_for_timeout(1500)
        about = [r for r in api_requests[before:] if "/inbox/tree/inbox/lark:f3:300" in r[1] or "/lark/conversations/f3" in r[1]]
        assert about, api_requests[before:]
        assert all("m-3fa21c" in (r[2], r[3]) for r in about), about
        if shots:
            page.wait_for_timeout(400)
            page.screenshot(path=name("inbox-item"))
        # the Muted dialog: each manager's under its name
        page.goto(base + "/inbox")
        main.get_by_role("button", name="更多操作", exact=True).first.click()
        page.get_by_role("menuitem", name="已静音").click()
        dialog = page.get_by_role("dialog")
        expect(dialog.locator("[data-mutes-manager]").first).to_be_visible()
        if shots:
            page.wait_for_timeout(400)
            page.screenshot(path=name("muted"))
        page.keyboard.press("Escape")
        expect(page.get_by_role("dialog")).to_have_count(0)  # closing it goes back in history: let it land

        go(page, base + "/settings?tab=managers")
        work = main.get_by_role("button").filter(has=page.get_by_text("工作", exact=True)).first
        expect(work).to_be_visible()
        if outdated:
            expect(work.get_by_text("需要更新", exact=True)).to_be_visible()
        if shots:
            page.wait_for_timeout(400)
            page.screenshot(path=name("list"))
        work.click()
        dialog = page.get_by_role("dialog")
        switch = dialog.get_by_role("switch", name="推送通知")
        expect(switch).to_have_attribute("aria-checked", "true")
        if outdated:
            expect(dialog.get_by_text("请在它所在的电脑上更新 personad", exact=False)).to_be_visible()
            if shots:
                page.wait_for_timeout(400)
                page.screenshot(path=name("detail"))
        switch.click()
        expect(switch).to_have_attribute("aria-checked", "false")
        if shots:
            page.wait_for_timeout(400)
            page.screenshot(path=name("push"))
        switch.click()
        expect(switch).to_have_attribute("aria-checked", "true")
        # 删除 says what goes with it: its computers come back, its sources don't
        dialog.get_by_role("button", name="删除", exact=True).click()
        expect(dialog.get_by_text("它的 1 台电脑会回到 hub 自己的管理者。", exact=True)).to_be_visible()
        expect(dialog.get_by_text("不先移到别的管理者", exact=False)).to_be_visible()
        if shots:
            page.wait_for_timeout(400)
            page.screenshot(path=name("delete"))
        dialog.get_by_role("button", name="保留", exact=True).click()
        dialog.get_by_role("button", name="完成", exact=True).click()
        expect(dialog).to_have_count(0)

        # 移到… on a computer's page: the NAS (家里's, offline with it) goes to 工作, then back
        page.goto(base + "/machines/nas")
        expect(main.locator('[data-error-code="manager_offline"]')).to_be_visible()
        expect(main.get_by_role("button", name="重新连接")).to_have_count(0)
        if shots:
            page.wait_for_timeout(400)
            page.screenshot(path=name("machine-offline"))
        main.get_by_role("button", name="移到…", exact=True).click()
        menu = page.get_by_role("menu")
        expect(menu.get_by_role("menuitem").filter(has_text="家里")).to_contain_text("离线")
        if shots:
            page.wait_for_timeout(400)
            page.screenshot(path=name("move"))
        menu.get_by_role("menuitem").filter(has_text="工作").click()
        expect(page.get_by_text("已移到「工作」", exact=True)).to_be_visible()
        main.get_by_role("button", name="移到…", exact=True).click()
        page.get_by_role("menu").get_by_role("menuitem").filter(has_text="家里").click()
        expect(page.get_by_text("已移到「家里」", exact=True)).to_be_visible()
        assert not errors, errors
    finally:
        context.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--shots")
    ap.add_argument("--single", action="store_true")
    ap.add_argument("--outdated", action="store_true")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel="chrome")
        try:
            for width in (390, 1200):
                for theme in ("light", "dark"):
                    check(browser, a.base.rstrip("/"), width, theme, a.shots, a.single, a.outdated)
        finally:
            browser.close()
    print("web managers e2e: ok")


if __name__ == "__main__":
    main()
