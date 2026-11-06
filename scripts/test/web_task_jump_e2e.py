#!/usr/bin/env python3
"""End-to-end check of "Jump to the step that started it" in the web chat (#2719, iOS TaskNoteRow): a background task's notice, opened,
has the button; pressing it opens the work it is folded in, brings the call that started the task into the middle of
the chat and stops following the end.

Needs a running web page against the fake hub with its background-task session, and Playwright for Python (it drives
the installed Chrome):

    MOCK_TASKS=1 PORT=18823 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18823 npx vite --port 5323 --strictPort &)
    python3 scripts/test/web_task_jump_e2e.py http://127.0.0.1:5323 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. Runs at desktop (1280) and phone (390) widths,
light and dark; --shots saves each state there.
"""
import argparse
import os
import sys

from playwright.sync_api import expect, sync_playwright

TASKS = "/s/macbook/a1f3c2d4-0000-4000-8000-000000000009"  # MOCK_TASKS
SIZES = {"wide": {"width": 1280, "height": 820}, "narrow": {"width": 390, "height": 844}}
# where a row sits in the chat's scroller: its middle against the view's, in px
MIDDLE = """(el) => { const s = document.querySelector('[data-chat-scroll]').getBoundingClientRect(); const r = el.getBoundingClientRect();
  return { off: Math.abs((r.top + r.bottom) / 2 - (s.top + s.bottom) / 2), inView: r.bottom > s.top && r.top < s.bottom }; }"""


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def open_all_groups(main):
    """Opens every folded work group, outer ones first, until none is left folded."""
    for _ in range(10):
        folded = main.locator('[data-work-group] > button[aria-expanded="false"]:not([disabled])')
        if folded.count() == 0:
            return
        folded.first.click()
        main.page.wait_for_timeout(250)


def check(page, tag, shot):
    page.goto(page.url.split("/overview")[0] + TASKS)
    main = page.locator("main")
    expect(main.locator("[data-work-group]").first).to_be_visible()
    shot("folded")
    # the failed test run's notice: open what it is in, then the notice itself
    open_all_groups(main)
    notice = main.get_by_role("button").filter(has_text="后台命令").filter(has_text="跑单元测试").first
    expect(notice).to_be_visible()
    # fold the groups again but the notice's own: the call is folded away from it
    for g in main.locator('[data-work-group] > button[aria-expanded="true"]').all()[::-1]:
        holder = g.locator("xpath=..")
        if holder.locator('[data-tool-use]').count() and not holder.get_by_role("button").filter(has_text="跑单元测试").filter(has_text="后台命令").count():
            g.click()
            page.wait_for_timeout(200)
    notice.scroll_into_view_if_needed()
    notice.click()
    jump = main.locator("[data-jump-to-call]").first
    expect(jump).to_be_visible()
    expect(jump).to_have_text("跳到发起的这一步")
    ok(f'{tag}: the opened notice offers "Jump to the step that started it" beside "View output"')
    shot("notice-open")

    # the chat at its end first, so the jump has to move it and stop following
    page.evaluate("() => { const s = document.querySelector('[data-chat-scroll]'); s.scrollTop = s.scrollHeight; }")
    page.wait_for_timeout(300)
    jump.scroll_into_view_if_needed()
    jump.click()
    call = main.locator('[data-tool-use][data-action-row="Bash"]').filter(has_text="跑单元测试").first
    expect(call).to_be_visible()
    page.wait_for_timeout(900)  # the smooth scroll
    where = call.evaluate(MIDDLE)
    if not where["inView"] or where["off"] > 120:
        fail(f"{tag}: the call is not brought to the middle: {where}")
    ok(f"{tag}: the call that started it is opened and in the middle of the chat (off by {round(where['off'])} px)")
    shot("jumped")
    page.wait_for_timeout(1500)  # nothing pulls the chat back to its end
    if call.evaluate(MIDDLE)["off"] > 160:
        fail(f"{tag}: the chat went back to its end after the jump")
    ok(f"{tag}: the chat stays there (it does not follow the end)")


def run(browser, base, token, size, theme, shots):
    tag = f"{size}/{theme}"
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2, has_touch=size == "narrow", color_scheme=theme)
    page = ctx.new_page()
    page.set_default_timeout(15000)
    errors = []
    page.on("pageerror", lambda e: errors.append(str(e)))
    page.goto(base + "/overview?token=" + token)
    page.wait_for_load_state("networkidle")
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")

    def shot(name):
        if shots:
            page.wait_for_timeout(350)
            page.screenshot(path=os.path.join(shots, f"jump-{size}-{theme}-{name}.png"))

    check(page, tag, shot)
    if errors:
        fail(f"{tag}: page errors: {errors[:3]}")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
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
                    run(browser, a.base.rstrip("/"), a.token, size, theme, a.shots)
        finally:
            browser.close()
    print("PASS web task jump e2e")


if __name__ == "__main__":
    main()
