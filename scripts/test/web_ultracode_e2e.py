#!/usr/bin/env python3
"""End-to-end check of Claude Code's ultracode switch on the web (#3428, docs/web.md "ultracode"): the row under the
thinking effort in the model and effort popover (on / queued for the turn, by this page or elsewhere / the node's reason
in orange with the switch still usable, the Claude quota under it), the workflow mark on the thinking chip, on the
sidebar's agent row (not on an offline one) and on a Codex "ultra" chip, the line above the composer after a resume
("Turn back on", x), a message sent while a change is queued (held back by the node, sent again once it is through),
"New agent"'s switch (off each time, grayed on a computer whose Claude Code is too old), and in English and Japanese at
360 / 390 px the composer row (the session-info button stays clear of Interrupt) and the resume line (not cut). Light /
dark x wide / 390 px.

Needs the fake hub's ultracode scenario, a web page against it and Playwright for Python:

    PORT=19060 MOCK_ULTRACODE=1 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:19060 npx vite --port 19061 --strictPort &)
    python3 scripts/test/web_ultracode_e2e.py http://127.0.0.1:19061 --hub http://127.0.0.1:19060 [--shots <dir>] [--channel chrome]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves the screenshots. The run changes the
fake hub's switches and puts them back (GET /mock/ultracode?reset=1) between steps.
"""
import argparse
import json
import os
import sys
import urllib.request

from playwright.sync_api import expect, sync_playwright

WIDGET = "a1f3c2d4-0000-4000-8000-000000000001"  # on, working
TESTS = "a1f3c2d4-0000-4000-8000-000000000002"  # a queued request, needs you
ORDERS = "a1f3c2d4-0000-4000-8000-000000000004"  # Codex on "ultra"
REVIEW = "a1f3c2d4-0000-4000-8000-000000000006"  # off after a resume, with the notice
RELEASE = "a1f3c2d4-0000-4000-8000-000000000008"  # the node's version note
RECIPE = "a1f3c2d4-0000-4000-8000-000000000007"  # offline; the node still remembers it on
TITLES = {WIDGET: "给天气 App 加主屏幕小组件", TESTS: "补齐网络层的单元测试", REVIEW: "检查 PR #42 的改动", RELEASE: "整理发布说明", RECIPE: "菜谱网站加搜索"}
SUBTITLE = "每个任务都编排多 Agent workflow · 很费额度 · 只对本会话"
NOTE = "请升级到 Claude Code 2.1.284 或更新版本，旧版不支持独立的 ultracode 开关"


def ok(msg):
    print("ok  " + msg, flush=True)


def hub(base, path):
    with urllib.request.urlopen(base + path, timeout=10) as r:
        return json.loads(r.read())


def reset(hub_url):
    hub(hub_url, "/mock/ultracode?reset=1")


def requests_of(hub_url):
    return hub(hub_url, "/mock/ultracode")["requests"]


def sends_of(hub_url):
    return hub(hub_url, "/mock/ultracode")["sends"]


def chat(page, base, mid, sid):
    page.goto(f"{base}/s/{mid}/{sid}")
    expect(page.locator("textarea").first).to_be_visible()
    page.wait_for_timeout(600)


def thinking_chip(page):
    return page.locator('button[title="模型与思考强度"], button[title="思考强度"]').first


def open_popover(page):
    thinking_chip(page).click()
    pop = page.locator('[role="dialog"][aria-label="模型与思考强度"]')
    expect(pop).to_be_visible()
    row = pop.locator('[data-testid="ultracode-row"]')
    expect(row).to_be_visible()
    page.wait_for_timeout(500)
    return pop, row


def shot(page, shots, name, target=None):
    if not shots:
        return
    page.wait_for_timeout(350)
    (target or page).screenshot(path=os.path.join(shots, name))


def check_popover_on(page, base, where, theme, shots):
    chat(page, base, "macbook", WIDGET)
    mark = thinking_chip(page).locator("[data-ultracode-mark]")
    expect(mark).to_have_attribute("aria-label", "ultracode 开")
    assert "text-ai" in mark.get_attribute("class"), mark.get_attribute("class")
    pop, row = open_popover(page)
    switch = row.get_by_role("switch", name="ultracode")
    expect(switch).to_have_attribute("aria-checked", "true")
    expect(switch).to_be_enabled()
    expect(row.locator("[data-ultracode-line]")).to_have_text(SUBTITLE)
    quota = row.locator("[data-ultracode-quota]")
    expect(quota).to_contain_text("Claude 额度：5 小时 86% · 每周 41%")
    expect(quota).to_contain_text("额度快用完了")
    # under the efforts, above the models
    tabs, models = pop.locator('[role="tablist"]').bounding_box(), pop.get_by_text("模型", exact=True).first.bounding_box()
    box = row.bounding_box()
    assert tabs["y"] + tabs["height"] <= box["y"] + 1 and box["y"] + box["height"] <= models["y"] + 1, (tabs, box, models)
    # the popover stays inside the window
    pb = pop.bounding_box()
    vw, vh = page.viewport_size["width"], page.viewport_size["height"]
    assert pb["x"] >= 0 and pb["y"] >= 0 and pb["x"] + pb["width"] <= vw + 1 and pb["y"] + pb["height"] <= vh + 1, pb
    # the row brings its own height: on a wide screen the models keep the room they had without it (520 px less the rest)
    if where == "wide":
        listbox = pop.locator('[role="listbox"]')
        shown, full = listbox.evaluate("e => [e.clientHeight, e.scrollHeight]")
        assert shown >= min(full, 300) - 1, ("the models get their room back", shown, full, pb)
    ok(f"{where} {theme}: the chip carries the AI-coloured mark; the popover's row is on, says what it does, the Claude quota and its warning")
    shot(page, shots, f"popover-{where}-{theme}.png")
    page.keyboard.press("Escape")


def check_toggle_queues(page, base, hub_url, where, theme):
    """Off on a working agent (it was on): the request goes at once, the switch stays at off, where it was put, with
    "Applies next turn" through the pushes of the queue; a message sent meanwhile is held back by the node, waits in its
    bubble and goes again once the change is through; then the node's push takes the mark away."""
    chat(page, base, "macbook", WIDGET)
    _, row = open_popover(page)
    switch = row.get_by_role("switch", name="ultracode")
    expect(switch).to_have_attribute("aria-checked", "true")
    switch.click()
    expect(row.locator("[data-ultracode-line]")).to_have_text("下一轮生效")
    expect(switch).to_have_attribute("aria-checked", "false")  # the asked-for value, not the node's "on"
    page.wait_for_timeout(1500)  # pushes of the queue (pending, the turn going on)
    expect(switch).to_have_attribute("aria-checked", "false")
    expect(row.locator("[data-ultracode-line]")).to_have_text("下一轮生效")
    assert requests_of(hub_url)[-1] == {"session_id": WIDGET, "ultracode": False}, requests_of(hub_url)
    page.keyboard.press("Escape")
    box = page.locator("textarea").first
    box.fill("顺便看看小组件的刷新间隔")
    box.press("Enter")
    expect(page.get_by_text("等 ultracode 的切换在这一轮结束后生效，再发这条")).to_be_visible(timeout=5000)
    expect(box).to_have_value("")
    expect(thinking_chip(page).locator("[data-ultracode-mark]")).to_have_count(0, timeout=9000)
    expect(page.get_by_text("等 ultracode 的切换在这一轮结束后生效，再发这条")).to_have_count(0, timeout=9000)
    sends = [x for x in sends_of(hub_url) if x["text"] == "顺便看看小组件的刷新间隔"]
    assert [x["held"] for x in sends][:1] == [True] and sends[-1]["held"] is False, sends
    ok(f"{where} {theme}: turning it off sends {{ultracode: false}} and stays off through the queue (\"下一轮生效\"); a message "
       f"held back meanwhile waits in its bubble and goes once the change is through; then the mark goes")
    reset(hub_url)


def check_pending_and_grayed(page, base, hub_url, where, theme, shots):
    chat(page, base, "macbook", TESTS)
    _, row = open_popover(page)
    # queued elsewhere: the words don't attach to where the switch stands
    expect(row.locator("[data-ultracode-line]")).to_have_text("有一次切换在排队，这一轮结束后生效")
    expect(row.get_by_role("switch", name="ultracode")).to_be_enabled()
    ok(f"{where} {theme}: a change queued elsewhere says one waits for the turn, and the switch can still be used")
    shot(page, shots, f"popover-pending-{where}-{theme}.png")
    page.keyboard.press("Escape")

    chat(page, base, "macbook", RELEASE)
    _, row = open_popover(page)
    switch = row.get_by_role("switch", name="ultracode")
    line = row.locator("[data-ultracode-line]")
    expect(switch).to_be_enabled()
    expect(line).to_have_text(NOTE)
    assert "text-orange" in line.get_attribute("class"), line.get_attribute("class")
    expect(row.locator("[data-ultracode-quota]")).to_contain_text("5 小时 86%")
    expect(row.locator("[data-ultracode-quota-warn]")).to_have_count(0)  # one orange line: the reason
    ok(f"{where} {theme}: the node's reason in orange in place of the line, the switch usable, no quota warning under it")
    shot(page, shots, f"popover-grayed-{where}-{theme}.png")
    switch.click()
    expect(switch).to_have_attribute("aria-checked", "false", timeout=5000)
    expect(switch).to_be_enabled()
    expect(row.locator('[role="alert"]')).to_have_count(0)  # the refusal only repeats the reason already there
    assert requests_of(hub_url)[-1] == {"session_id": RELEASE, "ultracode": True}, requests_of(hub_url)
    ok(f"{where} {theme}: trying it asks the node again; its refusal puts it back without saying the reason twice")
    page.keyboard.press("Escape")
    reset(hub_url)


def check_lost(page, base, hub_url, where, theme, shots):
    chat(page, base, "macbook", REVIEW)
    note = page.locator('[data-testid="ultracode-lost"]')
    expect(note).to_contain_text("恢复后 ultracode 已关")
    reopen = note.locator('[data-testid="ultracode-reopen"]')
    nb, rb = note.bounding_box(), reopen.bounding_box()
    assert nb["height"] <= 40, ("one line", nb)
    assert rb["height"] <= 24 and rb["x"] + rb["width"] <= nb["x"] + nb["width"], ("the button on one line, inside", rb, nb)
    composer = page.locator("textarea").first.bounding_box()
    assert nb["y"] + nb["height"] <= composer["y"], ("above the composer", nb, composer)
    ok(f"{where} {theme}: the resume notice is one line above the composer, with 重新打开 and ×")
    shot(page, shots, f"lost-{where}-{theme}.png")
    reopen.click()
    expect(note).to_have_count(0, timeout=5000)
    expect(thinking_chip(page).locator("[data-ultracode-mark]")).to_have_count(1)
    assert requests_of(hub_url)[-1] == {"session_id": REVIEW, "ultracode": True}, requests_of(hub_url)
    ok(f"{where} {theme}: 重新打开 sends {{ultracode: true}}; once on, the notice goes and the chip has the mark")
    reset(hub_url)

    chat(page, base, "macbook", REVIEW)
    page.locator('[data-testid="ultracode-lost"] button[aria-label="关掉这条提示"]').click()
    expect(page.locator('[data-testid="ultracode-lost"]')).to_have_count(0)
    assert requests_of(hub_url)[-1] == {"session_id": REVIEW, "ultracode_dismiss_lost": True}, requests_of(hub_url)
    page.wait_for_timeout(800)
    expect(page.locator('[data-testid="ultracode-lost"]')).to_have_count(0)
    ok(f"{where} {theme}: × dismisses it (ultracode_dismiss_lost) and it stays gone")
    reset(hub_url)


def check_codex(page, base, where, theme, shots):
    chat(page, base, "server", ORDERS)
    chip = page.locator('button[title="思考强度"]').first
    mark = chip.locator("[data-ultracode-mark]")
    expect(mark).to_have_count(1)
    assert "很费额度" in mark.get_attribute("aria-label"), mark.get_attribute("aria-label")
    ok(f"{where} {theme}: a Codex thread on \"ultra\" has the same mark on its chip")
    if shots:
        box = chip.bounding_box()
        page.screenshot(path=os.path.join(shots, f"codex-ultra-{where}-{theme}.png"),
                        clip={"x": max(0, box["x"] - 120), "y": box["y"] - 40, "width": min(page.viewport_size["width"] - max(0, box["x"] - 120), 520), "height": 80})


def check_sidebar(page, base, where, theme, shots, narrow):
    page.goto(base + "/overview")
    if narrow:
        page.locator("main").get_by_role("button", name="侧边栏").first.click()
    row = page.locator(f'aside a[title^="{TITLES[WIDGET]}"]').first
    expect(row).to_be_visible()
    mark = row.locator("[data-ultracode-mark]")
    expect(mark).to_have_count(1)
    assert "text-ai" in mark.get_attribute("class")
    for sid in (TESTS, REVIEW, RELEASE, RECIPE):
        expect(page.locator(f'aside a[title^="{TITLES[sid]}"] [data-ultracode-mark]')).to_have_count(0)
    if page.locator(f'aside a[title^="{TITLES[RECIPE]}"]').count():
        ok(f"{where} {theme}: the offline agent's row has no mark (its process is gone)")
    title = row.locator("span.truncate").first.bounding_box()
    mb = mark.bounding_box()
    assert mb["x"] >= title["x"] + title["width"] - 1 and abs((mb["y"] + mb["height"] / 2) - (title["y"] + title["height"] / 2)) <= 2, (title, mb)
    ok(f"{where} {theme}: the sidebar row of the agent with ultracode on has the mark right after its title; others none")
    if shots:
        # to the middle of the sidebar's list: "if needed" leaves a row at the list's bottom edge, under the usage bars.
        # The list keeps the open page's row in view until it is touched (Sidebar useGlide), so it is told of a wheel
        # first (no pointer over it: no hover box in the picture), then scrolled by its own scrollTop
        row.evaluate("""e => {
            const nav = e.closest('[data-sidebar-rows]');
            nav.dispatchEvent(new WheelEvent('wheel'));
            nav.scrollTop += e.getBoundingClientRect().top - nav.getBoundingClientRect().top - nav.clientHeight / 2;
        }""")
        page.wait_for_timeout(300)
        page.locator("aside").first.screenshot(path=os.path.join(shots, f"sidebar-{where}-{theme}.png"))


def pick_machine(page, dialog, name):
    """Chooses the computer and waits out its launchers and models loading (the effort field hides meanwhile)."""
    dialog.get_by_role("button", name=name).click()
    page.wait_for_timeout(1200)
    expect(dialog.get_by_text("思考强度", exact=True)).to_be_visible(timeout=10000)


def check_new_agent(page, base, hub_url, where, theme, shots, narrow, start):
    page.goto(base + "/overview")
    if narrow:
        page.locator("main").get_by_role("button", name="侧边栏").first.click()
    page.locator('aside button[aria-label="新建 Agent"]').first.click()
    dialog = page.locator('[role="dialog"]').last
    pick_machine(page, dialog, "MacBook")
    box = dialog.locator('[data-testid="new-agent-ultracode"]')
    expect(box).to_be_visible(timeout=10000)
    switch = box.get_by_role("switch", name="ultracode")
    expect(switch).to_have_attribute("aria-checked", "false")
    expect(box.locator("[data-ultracode-line]")).to_have_text(SUBTITLE)
    expect(box.locator("[data-ultracode-quota]")).to_contain_text("5 小时 86%")
    effort = dialog.get_by_text("思考强度", exact=True).bounding_box()
    assert box.bounding_box()["y"] > effort["y"], "under 思考强度"
    switch.click()
    expect(switch).to_have_attribute("aria-checked", "true")
    box.scroll_into_view_if_needed()
    ok(f"{where} {theme}: New agent's switch is under 思考强度, off, flips on, quota under it")
    shot(page, shots, f"new-agent-{where}-{theme}.png")
    pick_machine(page, dialog, "Mac Studio")
    box = dialog.locator('[data-testid="new-agent-ultracode"]')
    expect(box.get_by_role("switch", name="ultracode")).to_be_disabled(timeout=10000)
    expect(box.locator("[data-ultracode-line]")).to_have_text("这台电脑的 Claude Code 是 2.1.250，要 2.1.284 或更新才能开 ultracode")
    box.scroll_into_view_if_needed()
    ok(f"{where} {theme}: on a computer whose Claude Code is too old it is grayed and says why")
    shot(page, shots, f"new-agent-old-{where}-{theme}.png")
    pick_machine(page, dialog, "MacBook")
    expect(dialog.locator('[data-testid="new-agent-ultracode"]').get_by_role("switch", name="ultracode")).to_be_enabled(timeout=10000)
    if not start:
        page.keyboard.press("Escape")
        return
    switch = dialog.locator('[data-testid="new-agent-ultracode"]').get_by_role("switch", name="ultracode")
    if switch.get_attribute("aria-checked") != "true":
        switch.click()
    folder = dialog.locator("input").first
    folder.fill("/Users/me/code/weather")
    dialog.get_by_role("button", name="启动", exact=True).click()
    page.wait_for_timeout(800)
    started = hub(hub_url, "/mock/ultracode")["started"]
    assert started and started[-1].get("ultracode") is True, started
    ok(f"{where} {theme}: starting with it on sends ultracode: true")
    # opened again: off, nothing remembered
    page.goto(base + "/overview")
    if narrow:
        page.locator("main").get_by_role("button", name="侧边栏").first.click()
    page.locator('aside button[aria-label="新建 Agent"]').first.click()
    dialog = page.locator('[role="dialog"]').last
    pick_machine(page, dialog, "MacBook")
    expect(dialog.locator('[data-testid="new-agent-ultracode"]').get_by_role("switch", name="ultracode")).to_have_attribute("aria-checked", "false", timeout=10000)
    ok(f"{where} {theme}: opened again it is off: the last choice is not remembered")
    page.keyboard.press("Escape")


INFO = {"en": "Session info", "ja": "セッション情報", "zh-Hans": "会话信息"}
STOP = {"en": "Interrupt", "ja": "中断", "zh-Hans": "中断"}


def check_narrow_languages(browser, base, hub_url, token, shots):
    """At 360 / 390 px in English and Japanese (longer than Chinese): the composer row of the working agent with ultracode
    on keeps the session-info button clear of Interrupt (the chips truncate instead), and the resume line is not cut."""
    reset(hub_url)
    for lang in ("en", "ja", "zh-Hans"):
        for width in (360, 390):
            ctx = browser.new_context(viewport={"width": width, "height": 844}, device_scale_factor=2, has_touch=True)
            page = ctx.new_page()
            page.set_default_timeout(10000)
            page.goto(base + "/overview?token=" + token)
            expect(page.locator("aside")).to_be_attached()
            page.evaluate(f"localStorage.setItem('persona.language', '{lang}'); localStorage.setItem('persona.theme', 'light')")
            page.reload()
            page.goto(f"{base}/s/macbook/{WIDGET}")
            expect(page.locator("textarea").first).to_be_visible()
            page.wait_for_timeout(700)
            info = page.locator(f'button[aria-label="{INFO[lang]}"]').first
            stop = page.locator(f'button[aria-label="{STOP[lang]}"]').first
            expect(stop).to_be_visible()
            expect(page.locator("button [data-ultracode-mark]").first).to_be_visible()
            ib, sb = info.bounding_box(), stop.bounding_box()
            assert ib["x"] + ib["width"] <= sb["x"] + 0.5, (lang, width, "session info under Interrupt", ib, sb)
            if shots and width == 390 and lang != "zh-Hans":
                page.screenshot(path=os.path.join(shots, f"composer-{lang}-{width}-light.png"), clip={"x": 0, "y": sb["y"] - 70, "width": width, "height": 110})
            page.goto(f"{base}/s/macbook/{REVIEW}")
            note = page.locator('[data-testid="ultracode-lost"]')
            expect(note).to_be_visible()
            words = note.locator("span[title]").first
            cut = words.evaluate("e => e.scrollWidth > e.clientWidth + 0.5")
            assert not cut, (lang, width, "resume line cut", words.text_content())
            if shots and width == 390 and lang != "zh-Hans":
                page.wait_for_timeout(350)  # past its fade-up
                nb = note.bounding_box()
                page.screenshot(path=os.path.join(shots, f"lost-{lang}-{width}-light.png"), clip={"x": 0, "y": nb["y"] - 12, "width": width, "height": nb["height"] + 24})
            ok(f"{lang} {width}px: session info clear of Interrupt with the mark on the chip; the resume line whole")
            ctx.close()


def run(browser, base, hub_url, token, shots):
    reset(hub_url)
    for width, height, touch in ((1280, 860, False), (390, 844, True)):
        where = "wide" if width > 600 else "390"
        narrow = width <= 600
        for theme in ("light", "dark"):
            ctx = browser.new_context(viewport={"width": width, "height": height}, device_scale_factor=2, has_touch=touch, locale="zh-CN")
            page = ctx.new_page()
            page.set_default_timeout(10000)
            page.goto(base + "/overview?token=" + token)
            expect(page.locator("aside")).to_be_attached()
            page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")
            page.reload()
            check_sidebar(page, base, where, theme, shots, narrow)
            check_popover_on(page, base, where, theme, shots)
            check_pending_and_grayed(page, base, hub_url, where, theme, shots)
            check_lost(page, base, hub_url, where, theme, shots)
            check_codex(page, base, where, theme, shots)
            if where == "wide" and theme == "light":
                check_toggle_queues(page, base, hub_url, where, theme)
            check_new_agent(page, base, hub_url, where, theme, shots, narrow, start=False)
            reset(hub_url)
            ctx.close()

    check_narrow_languages(browser, base, hub_url, token, shots)

    # starting with it on (last: the started agent joins the fake hub's list and would be in the screenshots)
    ctx = browser.new_context(viewport={"width": 1280, "height": 860}, locale="zh-CN")
    page = ctx.new_page()
    page.set_default_timeout(10000)
    page.goto(base + "/overview?token=" + token)
    expect(page.locator("aside")).to_be_attached()
    check_new_agent(page, base, hub_url, "wide", "light", None, False, start=True)
    ctx.close()

    # reduced motion: the mark is there at once (no fade left running)
    ctx = browser.new_context(viewport={"width": 1280, "height": 860}, reduced_motion="reduce", locale="zh-CN")
    page = ctx.new_page()
    page.goto(base + f"/s/macbook/{WIDGET}?token=" + token)
    mark = thinking_chip(page).locator("[data-ultracode-mark]")
    expect(mark).to_have_count(1)
    page.wait_for_timeout(50)
    opacity = mark.evaluate("e => getComputedStyle(e).opacity")
    assert float(opacity) == 1.0, opacity
    ok("reduced motion: the mark shows at once")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--hub", required=True, help="the mock hub's URL (its /mock/ultracode)")
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    ap.add_argument("--channel")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel=a.channel) if a.channel else p.chromium.launch()
        try:
            run(browser, a.base.rstrip("/"), a.hub.rstrip("/"), a.token, a.shots)
        finally:
            browser.close()
            try:
                reset(a.hub.rstrip("/"))
            except OSError:
                pass
    print("PASS web ultracode e2e")


if __name__ == "__main__":
    try:
        main()
    except AssertionError as e:
        print("FAIL", e)
        sys.exit(1)
