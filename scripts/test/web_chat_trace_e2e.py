#!/usr/bin/env python3
"""End-to-end check of how the web chat draws an agent's work (#1963, cindy's grammar): 「已工作 Xs」 groups that open and
fold, a command opening in place, an edit's diff over the page, delivered prose staying out of the fold, subagent cards
(one opened, its whole conversation in the side panel with the chat left as it was), and the 跳到底部 / 有新消息 pill.

Needs a running web page against the fake hub with the two conversations made for it, and Playwright for Python (it
drives the installed Chrome):

    MOCK_SUBAGENT_CARDS=1 MOCK_WORK_GROUPS=1 PORT=18811 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18811 npx vite --port 5311 --strictPort &)
    python3 scripts/test/web_chat_trace_e2e.py http://127.0.0.1:5311 --hub http://127.0.0.1:18811 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. Runs at desktop (1280) and phone (390) widths,
light and dark; --shots saves each state there. --hub lets the fake hub push a reply (GET /mock/say) for 有新消息.
"""
import argparse
import os
import sys
import urllib.parse
import urllib.request

from playwright.sync_api import expect, sync_playwright

GROUPS = "/s/macbook/a1f3c2d4-0000-4000-8000-0000000c1e03"  # MOCK_WORK_GROUPS
AGENTS = "/s/macbook/a1f3c2d4-0000-4000-8000-0000000c1e02"  # MOCK_SUBAGENT_CARDS
SIZES = {"wide": {"width": 1280, "height": 820}, "narrow": {"width": 390, "height": 844}}

GAP = "() => { const s = document.querySelector('[data-chat-scroll]'); return s.scrollHeight - s.scrollTop - s.clientHeight; }"
# what the reader does to leave the end: the wheel going up (the chat listens for the intent, then the offset moves)
WHEEL_UP = """(to) => { const s = document.querySelector('[data-chat-scroll]');
  s.dispatchEvent(new WheelEvent('wheel', { deltaY: -120, bubbles: true })); s.scrollTop = to; }"""
S = {"base": "", "hub": ""}  # where the page and the fake hub are
OVERFLOW = "() => document.documentElement.scrollWidth - document.documentElement.clientWidth"


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def no_overflow(page, tag, where):
    if page.evaluate(OVERFLOW) > 0:
        fail(f"{tag}: the page scrolls sideways ({where})")


def groups_page(page, tag, shot):
    page.goto(S["base"] + GROUPS)
    main = page.locator("main")
    expect(main.locator("[data-work-group]").first).to_be_visible()
    page.evaluate(WHEEL_UP, 0)
    page.wait_for_timeout(500)
    first = main.locator('[data-work-group="done"]').first
    head = first.locator("button").first
    expect(head).to_have_attribute("aria-expanded", "false")
    expect(head).to_contain_text("已工作")
    expect(first.locator("[data-work-detail]")).to_have_count(0)
    ok(f"{tag}: a finished exchange's work is one folded 「已工作 Xs」, the answer after it")
    shot("groups-folded")

    head.click()
    expect(head).to_have_attribute("aria-expanded", "true")
    detail = first.locator("[data-work-detail]")
    expect(detail).to_be_visible()
    expect(detail.locator("[data-action-row]").first).to_be_visible()
    expect(detail.locator('[data-thinking="empty"]').first).to_contain_text("思考")
    ok(f"{tag}: opening it shows its steps on the rail: tool rows, thoughts as 「思考 Ns」")
    shot("groups-open")

    # the command without a description opens in place: what went in, a line, what came out
    second = main.locator('[data-work-group="done"]').nth(1)
    second.locator("button").first.click()
    row = second.locator('[data-action-row="Bash"]').filter(has_text="运行命令").first
    expect(row).to_be_visible()
    expect(row).to_contain_text("git status --short")
    row.locator("button").first.click()
    expect(row.locator("[data-inline-detail]")).to_contain_text("M Widget/Provider.swift")
    ok(f"{tag}: a command without a description says 运行命令, its command under it, and opens in place")
    described = main.locator('[data-action-row="Bash"]').filter(has_text="看改动").first
    expect(described).not_to_contain_text("运行命令")
    ok(f"{tag}: one with a description is that sentence")

    edit = second.locator('[data-action-row="Edit"]').first
    expect(edit).to_contain_text("Provider.swift")
    expect(edit).to_contain_text("+2")
    edit.locator("button").first.click()
    dialog = page.get_by_role("dialog")
    expect(dialog.locator('[data-diff-line="add"]').first).to_be_visible()
    expect(dialog.locator('[data-diff-line="del"]').first).to_be_visible()
    ok(f"{tag}: an edit shows +N −N and opens its diff, whole lines on green and red")
    shot("diff")
    page.keyboard.press("Escape")
    expect(dialog).to_have_count(0)

    second.locator("button").first.click()
    expect(second.locator("button").first).to_have_attribute("aria-expanded", "false")
    page.wait_for_timeout(500)
    expect(second.locator("[data-work-detail]")).to_have_count(0)
    ok(f"{tag}: folding it again takes the steps away")

    # the hub folds a finished run into counts (fold_work): which tools it was is known once the group opens
    reading = main.locator("[data-segment]").filter(has_text="小组件相关的代码都有哪些文件").locator("[data-work-group]").first
    reading.locator("button").first.click()
    expect(reading.locator("button").first).to_contain_text("读取 3 个文件 · 搜索 2 次")
    ok(f"{tag}: a group that only read and searched says so in its title")
    brief = main.get_by_role("heading", name="小组件改动简报")
    expect(brief).to_be_visible()
    if brief.evaluate("(n) => !!n.closest('[data-work-group]')"):
        fail(f"{tag}: the delivered brief was folded into a group")
    ok(f"{tag}: a delivered report between two steps stays in the open")
    no_overflow(page, tag, "work groups")

    # away from the end: 跳到底部; back at the end; a reply coming in while away: 有新消息
    page.evaluate(WHEEL_UP, 0)
    pill = page.locator("[data-jump-pill]")
    expect(pill).to_have_text("跳到底部")
    shot("jump")
    pill.click()
    page.wait_for_timeout(900)
    if page.evaluate(GAP) > 8:
        fail(f"{tag}: 跳到底部 did not reach the end")
    expect(pill).to_have_count(0)
    ok(f"{tag}: scrolled up, 跳到底部 shows; clicked, the chat is at its end and it goes")
    if S["hub"]:
        page.evaluate(WHEEL_UP, 0)
        expect(pill).to_have_text("跳到底部")
        urllib.request.urlopen(S["hub"] + "/mock/say?text=" + urllib.parse.quote("测试：新到的一句话。"), timeout=5).read()
        expect(pill).to_have_text("有新消息", timeout=8000)
        ok(f"{tag}: something coming in while away makes it 有新消息")
        pill.click()
        page.wait_for_timeout(900)
        expect(main.get_by_text("测试：新到的一句话。").last).to_be_in_viewport()


def agents_page(page, tag, shot):
    page.goto(S["base"] + AGENTS)
    main = page.locator("main")
    running = main.locator('[data-subagent-card="running"]')
    expect(running).to_be_visible()
    if running.evaluate("(n) => !!n.closest('[data-work-group]')"):
        fail(f"{tag}: the running subagent's card is inside a group")
    expect(main.locator('[data-work-group="live"]')).to_contain_text("正在工作…")
    ok(f"{tag}: a running subagent's card stays in the open next to the live 「正在工作…」")
    if page.locator('[role="tablist"][aria-label="子 Agent"]').count():
        fail(f"{tag}: the subagent tab strip is still there")
    ok(f"{tag}: no tab strip of subagents")

    page.evaluate(WHEEL_UP, 0)
    done = main.locator('[data-work-group="done"]').first
    done.locator("button").first.click()
    expect(done.locator('[data-subagent-card="completed"]').first).to_be_visible()
    expect(done.locator('[data-subagent-card="failed"]')).to_contain_text("失败")
    ok(f"{tag}: finished and failed subagents fold with the work they were part of")

    card = done.locator('[data-subagent-card="completed"]').nth(1)
    card.locator("button").first.click()
    expect(card.locator("[data-subagent-prompt]")).to_contain_text("查价格筛选的回归")
    expect(card.locator("[data-subagent-summary]")).to_contain_text("测试数据的问题")
    expect(card.locator("[data-subagent-recent] [data-action-row]").first).to_be_visible()
    expect(card.locator("[data-subagent-nested]")).to_contain_text("查价格的测试数据")
    expect(card.locator("[data-subagent-nested]")).to_contain_text("核对下架商品")
    ok(f"{tag}: opened, a card shows what it was asked, what it reported, its latest steps and the ones it launched")
    shot("agent-card")

    title = page.locator("main header h1").first.inner_text()
    card.locator("[data-subagent-open]").click()
    panel = page.locator("aside[aria-label=Agent]")
    expect(panel).to_contain_text("回归：价格筛选")
    expect(panel).to_contain_text("查价格筛选的回归，必要时再拆给子 Agent")
    expect(panel.locator("textarea")).to_have_count(0)
    if page.locator("main header h1").first.inner_text() != title:
        fail(f"{tag}: opening the subagent changed the chat")
    ok(f"{tag}: 查看完整过程 opens its conversation read-only in the side panel; the chat stays")
    no_overflow(page, tag, "subagent panel")
    shot("agent-panel")
    panel.get_by_role("button", name="关闭面板").click()
    expect(panel).to_have_count(0)


def run(browser, base, hub, token, size, theme, shots):
    tag = f"{size}/{theme}"
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2, has_touch=size == "narrow", color_scheme=theme)
    page = ctx.new_page()
    S.update(base=base, hub=hub)
    page.set_default_timeout(15000)
    errors = []
    page.on("pageerror", lambda e: errors.append(str(e)))
    page.goto(base + "/overview?token=" + token)
    page.wait_for_load_state("networkidle")
    page.evaluate(f"localStorage.setItem('jarvis.theme', '{theme}')")

    def shot(name):
        if shots:
            page.wait_for_timeout(350)
            page.screenshot(path=os.path.join(shots, f"trace-{size}-{theme}-{name}.png"))

    groups_page(page, tag, shot)
    agents_page(page, tag, shot)
    if errors:
        fail(f"{tag}: page errors: {errors[:3]}")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--hub")
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel="chrome")
        try:
            for size in SIZES:
                for theme in ("light", "dark"):
                    run(browser, a.base.rstrip("/"), (a.hub or "").rstrip("/"), a.token, size, theme, a.shots)
        finally:
            browser.close()
    print("PASS web chat trace e2e")


if __name__ == "__main__":
    main()
