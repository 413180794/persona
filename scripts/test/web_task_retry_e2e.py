#!/usr/bin/env python3
"""End-to-end check of 重试 on a task whose agent did not start (#1396), in the web's main conversation: the failed task
card has 重试 next to 移除 and the failed receipt has 重试 too. Tapped, the card turns to starting where it is (no second
card), the plan's red line goes and the receipt folds to a quiet line; once the agent is up the card follows it. A retry
that fails again puts the card back with the new error, still one receipt; with the machine offline it says so.

Needs a running web page against the fake hub with tasks (MOCK_TASKS=1; the hub's GET /mock/retry?mode=… puts the
failed task back and picks how its retry goes) and Playwright for Python (it drives the installed Chrome):

    MOCK_TASKS=1 PORT=18796 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18796 npx vite --port 5296 --strictPort &)
    python3 scripts/test/web_task_retry_e2e.py http://127.0.0.1:5296 --hub http://127.0.0.1:18796 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. Runs light and dark, at desktop and phone
widths; --shots saves each state there.
"""
import argparse
import os
import sys
import urllib.request

from playwright.sync_api import expect, sync_playwright

TITLE = "设置页按钮对齐"
SIZES = {"wide": {"width": 1280, "height": 900}, "narrow": {"width": 390, "height": 844}}


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def mock_retry(hub, token, mode):
    req = urllib.request.Request(f"{hub}/mock/retry?mode={mode}", headers={"Authorization": "Bearer " + token})
    urllib.request.urlopen(req, timeout=10).read()


class Page:
    def __init__(self, browser, base, token, size, theme, shots):
        self.tag, self.size, self.theme, self.shots = f"{size}/{theme}", size, theme, shots
        self.ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2, has_touch=size == "narrow")
        self.page = self.ctx.new_page()
        self.page.set_default_timeout(15000)
        self.posts = []
        self.page.on("request", lambda r: r.method == "POST" and r.url.endswith("/retry") and self.posts.append(r.url))
        self.page.goto(base + "/overview?token=" + token)
        self.page.wait_for_function("localStorage.getItem('jarvis.token')")
        self.page.evaluate(f"localStorage.setItem('jarvis.theme', '{theme}')")
        self.page.goto(base + "/")
        self.card = self.page.locator("div[role=button]", has_text=TITLE).filter(has=self.page.get_by_text("没开起来：")).first
        self.any_card = self.page.locator("div[role=button]", has_text=TITLE)
        self.banner = self.page.locator("div.bg-red-tint", has_text=f"{TITLE}没开起来")
        self.plan_line = self.page.get_by_text(f"「{TITLE}」没开起来：")

    def bring(self, loc):
        """Scrolls loc to the upper middle with the wheel, as a reader does: a scroll by script leaves the chat
        following its end, and the next change would take it back there."""
        vp = SIZES[self.size]
        self.page.mouse.move(vp["width"] * 0.7, vp["height"] * 0.4)
        for _ in range(80):
            box = loc.bounding_box()
            if box and vp["height"] * 0.15 < box["y"] < vp["height"] * 0.5:
                return
            self.page.mouse.wheel(0, -120 if not box or box["y"] < vp["height"] * 0.15 else 120)
            self.page.wait_for_timeout(150)
        fail(f"{self.tag}: could not scroll to {loc}")

    def on_screen(self, loc, what):
        box, vp = loc.bounding_box(), SIZES[self.size]
        if not box or box["y"] < 0 or box["y"] > vp["height"] * 0.7:
            fail(f"{self.tag}: {what} left the screen: {box}")

    def shot(self, name):
        if self.shots:
            self.page.wait_for_timeout(300)
            self.page.screenshot(path=os.path.join(self.shots, f"retry-{self.size}-{self.theme}-{name}.png"))

    def failed_as_before(self, error):
        expect(self.card).to_be_visible()
        expect(self.card.get_by_role("button", name="重试")).to_be_visible()
        expect(self.card.get_by_role("button", name="移除")).to_be_visible()
        expect(self.card).to_contain_text(error)
        expect(self.banner).to_have_count(1)
        expect(self.banner.get_by_role("button", name="重试")).to_be_visible()
        expect(self.banner).to_contain_text(error)
        expect(self.any_card).to_have_count(1)

    def close(self):
        self.ctx.close()


def started(browser, base, hub, token, size, theme, shots):
    mock_retry(hub, token, "")
    p = Page(browser, base, token, size, theme, shots)
    p.failed_as_before("rpc: connection closed")
    expect(p.plan_line).to_be_visible()
    ok(f"{p.tag}: the failed task has 重试 and 移除 on its card and 重试 on its receipt")
    p.bring(p.card)
    p.shot("failed")

    # Two taps before the page redraws: the second must not start a second agent.
    p.card.get_by_role("button", name="重试").evaluate("(b) => { b.click(); b.click(); }")
    starting = p.page.locator("div[role=button]", has_text=TITLE).filter(has=p.page.get_by_text("正在开 Agent"))
    expect(starting).to_be_visible()
    expect(p.any_card).to_have_count(1)
    expect(p.plan_line).to_have_count(0)
    expect(p.banner).to_have_count(0)
    expect(p.page.get_by_role("button", name=f"「{TITLE}」正在重新开")).to_be_visible()
    p.on_screen(starting, "the task's card")
    ok(f"{p.tag}: tapped, the card turns to starting in place (still on screen), the red line and the banner give way to a quiet line")
    p.shot("starting")

    working = p.page.locator("div[role=button]", has_text=TITLE).filter(has=p.page.get_by_text("在看设置页底部两个按钮的布局约束"))
    expect(working).to_be_visible()
    expect(p.page.get_by_role("button", name=f"「{TITLE}」重试后开起来了")).to_be_visible()
    expect(p.any_card).to_have_count(1)
    if len(p.posts) != 1:
        fail(f"{p.tag}: {len(p.posts)} retry requests for two taps")
    ok(f"{p.tag}: the agent up, the card follows it; two taps sent one retry")
    p.shot("started")
    p.close()


def fails_again(browser, base, hub, token, size, theme, shots):
    mock_retry(hub, token, "fail")
    p = Page(browser, base, token, size, theme, shots)
    p.failed_as_before("rpc: connection closed")
    p.bring(p.banner)
    p.banner.get_by_role("button", name="重试").click()
    p.failed_as_before("claude did not start within 3m0s")
    expect(p.plan_line).to_contain_text("claude did not start within 3m0s")
    ok(f"{p.tag}: a retry that fails again shows the new error on the card, the receipt and the plan, one receipt")
    p.bring(p.card)
    p.shot("failed-again")
    p.close()


def offline(browser, base, hub, token, size, theme, shots):
    mock_retry(hub, token, "offline")
    p = Page(browser, base, token, size, theme, shots)
    expect(p.card).to_be_visible()
    p.bring(p.card)
    p.card.get_by_role("button", name="重试").click()
    expect(p.page.get_by_text("现在离线，等它连上再重试")).to_be_visible()
    p.failed_as_before("rpc: connection closed")
    ok(f"{p.tag}: with the machine offline the retry says so and the task stays as it was")
    p.shot("offline")
    p.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--hub", required=True)
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    base, hub = a.base.rstrip("/"), a.hub.rstrip("/")
    with sync_playwright() as pw:
        browser = pw.chromium.launch(channel="chrome")
        try:
            for size in SIZES:
                for theme in ("light", "dark"):
                    for case in (started, fails_again, offline):
                        case(browser, base, hub, a.token, size, theme, a.shots)
        finally:
            browser.close()
        mock_retry(hub, a.token, "")
    print("PASS web task retry e2e")


if __name__ == "__main__":
    main()
