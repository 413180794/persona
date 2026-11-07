#!/usr/bin/env python3
"""End-to-end check of Persona's messages for the model in the web's main conversation (#2360, docs/protocol/protocol.md
"Messages Persona injects"): a task's notice (unmarked, as an older hub sends it) and a daily task's run (marked injected)
show as one centered line each, never the words for the model; the notice's line opens its task, the run's line
shows what the task asks.

    MOCK_TASKS=1 MOCK_INJECTED=1 PORT=18831 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18831 npx vite --port 5331 --strictPort &)
    python3 scripts/test/web_injected_e2e.py http://127.0.0.1:5331 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. Runs light and dark, at desktop and phone
widths; --shots saves each.
"""
import argparse
import os
import sys

from playwright.sync_api import expect, sync_playwright

SIZES = {"wide": {"width": 1280, "height": 900}, "narrow": {"width": 390, "height": 844}}


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def case(browser, base, token, size, theme, shots):
    tag = f"{size}/{theme}"
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2, has_touch=size == "narrow")
    page = ctx.new_page()
    page.set_default_timeout(15000)
    page.goto(base + "/overview?token=" + token)
    page.wait_for_function("localStorage.getItem('persona.token')")
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")
    page.goto(base + "/")
    notice = page.locator("[data-injected=task_notice]")
    run = page.locator("[data-injected=followup]")
    expect(notice).to_have_count(1)
    expect(notice).to_contain_text("任务「整理发布说明」做完了")
    expect(run).to_contain_text("定时任务「午间整理」开始运行")
    for words in ("这是 Persona 的任务通知", "你来决定要不要告诉用户", "⏰ 午间整理"):
        if page.get_by_text(words).count():
            fail(f"{tag}: the words for the model show: {words}")
    ok(f"{tag}: two centered lines, no words for the model")
    notice.scroll_into_view_if_needed()
    page.wait_for_timeout(400)
    if shots:
        page.screenshot(path=os.path.join(shots, f"injected-{size}-{theme}.png"))
    run.locator("button").click()
    expect(run).to_contain_text("看看上午的邮件和飞书")
    ok(f"{tag}: the run's line opens what the task asks")
    if shots:
        page.screenshot(path=os.path.join(shots, f"injected-{size}-{theme}-detail.png"))
    notice.locator("button").click()
    expect(page.get_by_text("整理发布说明").first).to_be_visible()
    page.wait_for_timeout(600)
    if "task" not in page.url and page.locator("[data-panel], aside").count() == 0:
        fail(f"{tag}: the notice did not open its task")
    ok(f"{tag}: the notice's line opens its task")
    if shots:
        page.screenshot(path=os.path.join(shots, f"injected-{size}-{theme}-task.png"))
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--token", default="mock")
    ap.add_argument("--shots")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as pw:
        browser = pw.chromium.launch(channel="chrome")
        try:
            for size in SIZES:
                for theme in ("light", "dark"):
                    case(browser, a.base.rstrip("/"), a.token, size, theme, a.shots)
        finally:
            browser.close()
    print("PASS")


if __name__ == "__main__":
    main()
