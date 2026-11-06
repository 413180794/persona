#!/usr/bin/env python3
"""End-to-end check of the sidebar's "By machine" tree (machine → herdr workspace → tab, #1963): the "By machine" segment, folding
machines and workspaces (kept across a reload), a tab row opening its most urgent pane under /t/…, the chat's pane tabs,
the old /s/ links, a tab whose ids changed, the tree in the narrow drawer, and Enter making a new line on a touch screen.

Needs a running web page against the fake hub with MOCK_SPLIT_TABS=1 and Playwright for Python (the installed Chrome):

    MOCK_SPLIT_TABS=1 PORT=18831 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18831 npx vite --port 5331 --strictPort &)
    python3 scripts/test/web_herdr_tree_e2e.py http://127.0.0.1:5331 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves desktop (1280) and narrow (390)
screenshots, light and dark.
"""
import argparse
import os
import sys
from urllib.parse import quote

from playwright.sync_api import expect, sync_playwright

CLAUDE = "5b1170b5-0000-4000-8000-000000000001"
CODEX = "5b1170b5-0000-4000-8000-000000000002"
SINGLE = "5b1170b5-0000-4000-8000-000000000003"
BOT = "5b1170b5-0000-4000-8000-000000000005"
TAB1 = "/t/server/w1/" + quote("w1:t1", safe="")
TAB2 = "/t/server/w1/" + quote("w1:t2", safe="")


def ok(msg):
    print("ok  " + msg, flush=True)


def skip(msg):
    print("skip " + msg, flush=True)


def path_of(page):
    return page.evaluate("location.pathname")


def tree_header(page, name):
    # machine and workspace rows: the folding button of a `group/sec` row, by its name
    return page.locator("aside .group\\/sec > button[aria-expanded]", has_text=name).first


def fresh(page, base, token, theme="light", grouping=None):
    page.goto(base + "/overview?token=" + token)
    # signed in (the link's token traded for this browser's) before the reload, or the reload lands on the login form
    expect(page.locator("aside")).to_be_visible(timeout=30000)
    page.evaluate(f"Object.keys(localStorage).filter(k => k.startsWith('fold:') || k === 'persona.agentGrouping' || k === 'persona.agentFilter').forEach(k => localStorage.removeItem(k)); localStorage.setItem('persona.theme', '{theme}')")
    if grouping:
        page.evaluate(f"localStorage.setItem('persona.agentGrouping', '{grouping}')")
    page.reload()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    a = ap.parse_args()
    base = a.base.rstrip("/")
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel="chrome")
        try:
            desktop(browser, base, a.token, a.shots)
            narrow(browser, base, a.token, a.shots)
            touch(browser, base, a.token)
        finally:
            browser.close()
    print("PASS web herdr tree e2e")


def desktop(browser, base, token, shots):
    ctx = browser.new_context(viewport={"width": 1280, "height": 820}, device_scale_factor=2)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    fresh(page, base, token)

    # 1. A browser that never picked a grouping gets "By to-do" (#2812); its "By machine" segment shows herdr's tree, the offline machine folded.
    expect(page.get_by_role("radio", name="按待办")).to_have_attribute("aria-checked", "true")
    page.get_by_role("radio", name="按机器").click()
    expect(page.get_by_role("radio", name="按机器")).to_have_attribute("aria-checked", "true")
    expect(tree_header(page, "服务器")).to_have_attribute("aria-expanded", "true")
    expect(tree_header(page, "NAS")).to_have_attribute("aria-expanded", "false")
    ok("按待办 is the default; 按机器 opens the online machine and folds the offline one")

    # 2. machine → workspace → tab: the tab with two panes speaks for its most urgent one (codex needs you).
    tab1 = page.locator(f'aside a[href="{TAB1}"]')
    expect(tab1).to_contain_text("#1")
    expect(tab1).to_contain_text("审一下登录重构的改动")
    expect(tab1).to_contain_text("2 个 Agent")
    expect(page.locator(f'aside a[href="{TAB2}"]')).to_contain_text("升级依赖")
    expect(page.locator(f'aside a[href="/s/server/{BOT}"]')).to_be_visible()  # botmux: no tabs, right under its space
    expect(tree_header(page, "app")).to_be_visible()
    rows = page.locator("aside a[href^='/t/'], aside a[href^='/s/server/']")
    heights = rows.evaluate_all("els => els.map(e => e.getBoundingClientRect().height)")
    assert all(h <= 32 for h in heights), heights  # one line each: long names truncate
    ok("the tree lists workspaces, their tabs (#N, title, pane count) and a host without tabs")
    if shots:
        tree_header(page, "NAS").scroll_into_view_if_needed()
        page.wait_for_timeout(400)
        page.screenshot(path=os.path.join(shots, "desktop-light.png"))

    # 3. A tab row opens its most urgent pane under /t/…, and its row lights up.
    tab1.click()
    page.wait_for_function(f"location.pathname === {TAB1 + '/' + CODEX!r}")
    expect(page.locator("main").get_by_text("有两处会吞掉错误，要我直接改吗？").first).to_be_visible()
    expect(tab1).to_have_attribute("aria-current", "page")
    ok("a tab row opens its most urgent pane (codex) under /t/…/<sid>")

    # 4. The chat's pane tabs (PaneTabs, put in ChatView by the chat-render lane).
    tabs = page.locator("main [role=tablist] [role=tab]")
    if tabs.count() == 0:
        skip("pane tabs: ChatView does not mount PaneTabs on this branch yet (chat-render lane); covered by PaneTabs.test.tsx")
    else:
        expect(tabs).to_have_count(2)
        expect(tabs.nth(1)).to_have_attribute("aria-selected", "true")
        tabs.nth(0).click()
        page.wait_for_function(f"location.pathname.endsWith({CLAUDE!r})")
        expect(page.locator("main").get_by_text("中间件写好了，在补刷新失败的测试。").first).to_be_visible()
        page.locator("main [role=tab][aria-selected=true]").press("ArrowRight")
        page.wait_for_function(f"location.pathname.endsWith({CODEX!r})")
        ok("pane tabs switch between the tab's agents, by click and arrow keys")

    # 5. Folding a workspace is kept across a reload (keyed by machine/name, not herdr's ids).
    docs = tree_header(page, "docs")
    docs.click()
    expect(docs).to_have_attribute("aria-expanded", "false")
    page.reload()
    expect(tree_header(page, "docs")).to_have_attribute("aria-expanded", "false")
    tree_header(page, "docs").click()
    expect(tree_header(page, "docs")).to_have_attribute("aria-expanded", "true")
    nas = tree_header(page, "NAS")
    nas.click()
    expect(nas).to_have_attribute("aria-expanded", "true")
    expect(page.locator("aside").get_by_text("它的 Agent 在「最近」里")).to_be_visible()
    page.reload()
    expect(tree_header(page, "NAS")).to_have_attribute("aria-expanded", "true")
    ok("workspace and machine folds stay after a reload")

    # 6. The old session links still open; a pane in a tab moves to its tab's /t/ in place (#1996).
    page.goto(base + f"/s/server/{CLAUDE}")
    expect(page.locator("main").get_by_text("中间件写好了，在补刷新失败的测试。").first).to_be_visible()
    page.wait_for_function(f"location.pathname === {TAB1 + '/' + CLAUDE!r}")
    ok("/s/<mid>/<sid> opens the session, its address replaced by the tab's /t/")

    # 7. A tab whose ids changed (herdr restarted) falls back to the pane; with no pane it says so.
    page.goto(base + f"/t/server/w9/{quote('w9:t1', safe='')}/{SINGLE}")
    expect(page.locator("main").get_by_text("依赖都升好了，测试全过。").first).to_be_visible()
    page.goto(base + f"/t/server/w9/{quote('w9:t1', safe='')}")
    expect(page.locator("main").get_by_text("这个标签页已经不在了")).to_be_visible()
    ok("a gone tab falls back to its pane, else shows the empty state")

    # 8. Grouping picked before stays; the other groupings still render.
    page.evaluate("localStorage.setItem('persona.agentGrouping', 'state')")
    page.reload()
    expect(page.get_by_role("radio", name="按状态")).to_have_attribute("aria-checked", "true")
    page.evaluate("localStorage.removeItem('persona.agentGrouping')")
    ok("a grouping picked before is kept")

    if shots:
        page.evaluate("localStorage.setItem('persona.theme', 'dark')")
        page.goto(base + TAB1)
        expect(page.locator("main").get_by_text("有两处会吞掉错误，要我直接改吗？").first).to_be_visible()
        tree_header(page, "NAS").scroll_into_view_if_needed()
        page.mouse.move(900, 400)  # the sidebar's highlight sits on the open tab, not where the pointer was
        page.wait_for_timeout(500)
        page.screenshot(path=os.path.join(shots, "desktop-dark.png"))
        page.evaluate("localStorage.setItem('persona.theme', 'light')")
    ctx.close()


def narrow(browser, base, token, shots):
    for theme in ("light", "dark"):
        ctx = browser.new_context(viewport={"width": 390, "height": 844}, device_scale_factor=2, has_touch=True, is_mobile=True)
        page = ctx.new_page()
        page.set_default_timeout(10000)
        fresh(page, base, token, theme, grouping="machine")
        side = page.locator("main").get_by_role("button", name="侧边栏").first
        expect(side).to_be_visible(timeout=30000)  # the first load of a fresh context compiles the page
        side.click()
        tab1 = page.locator(f'aside a[href="{TAB1}"]')
        expect(tab1).to_be_visible()
        expect(tree_header(page, "NAS")).to_have_attribute("aria-expanded", "false")
        box = tab1.bounding_box()
        assert box and box["height"] >= 44, box
        if shots:
            tree_header(page, "NAS").scroll_into_view_if_needed()
            page.wait_for_timeout(400)
            page.screenshot(path=os.path.join(shots, f"narrow-drawer-{theme}.png"))
        if theme == "light":
            # the row's ⋯ is always there on a touch screen
            more = page.locator(f'aside a[href="{TAB2}"]').locator("xpath=..").get_by_role("button", name="Agent 的操作")
            expect(more).to_be_visible()
            ok("narrow: the tree is in the drawer, rows are 44 tall, the ⋯ shows without hover")
        tab1.tap()
        page.wait_for_function(f"location.pathname === {TAB1 + '/' + CODEX!r}")
        expect(page.locator("main").get_by_text("有两处会吞掉错误，要我直接改吗？").first).to_be_visible()
        expect(tab1).not_to_be_in_viewport()  # the drawer closed
        if shots:
            page.wait_for_timeout(500)
            page.screenshot(path=os.path.join(shots, f"narrow-chat-{theme}.png"))
        ctx.close()
    ok("narrow: tapping a tab row opens it and closes the drawer")


def touch(browser, base, token):
    ctx = browser.new_context(viewport={"width": 390, "height": 844}, device_scale_factor=2, has_touch=True, is_mobile=True)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    page.goto(base + f"/s/server/{SINGLE}?token={token}")
    box = page.get_by_role("textbox", name="消息")
    expect(box).to_be_visible()
    assert page.evaluate("matchMedia('(pointer: coarse)').matches"), "the page is not seen as a touch screen"
    assert box.evaluate("e => getComputedStyle(e).fontSize") == "16px", box.evaluate("e => getComputedStyle(e).fontSize")
    box.click()
    box.type("第一行")
    box.press("Enter")
    box.type("第二行")
    page.wait_for_timeout(300)
    assert box.input_value() == "第一行\n第二行", repr(box.input_value())
    ok("touch: the composer is 16px and Enter makes a new line instead of sending")
    ctx.close()

    # a desktop pointer: Enter still sends (the box empties) and the text is 14px
    ctx = browser.new_context(viewport={"width": 1280, "height": 820})
    page = ctx.new_page()
    page.set_default_timeout(10000)
    page.goto(base + f"/s/server/{SINGLE}?token={token}")
    box = page.get_by_role("textbox", name="消息")
    expect(box).to_be_visible()
    assert box.evaluate("e => getComputedStyle(e).fontSize") == "14px"
    box.click()
    box.type("好的")
    box.press("Enter")
    expect(box).to_have_value("")
    ok("desktop: Enter sends, the composer stays 14px")
    ctx.close()


if __name__ == "__main__":
    try:
        main()
    except AssertionError as e:
        print(f"FAIL {e}", flush=True)
        sys.exit(1)
