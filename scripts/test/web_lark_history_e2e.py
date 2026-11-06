#!/usr/bin/env python3
"""End-to-end check of a Feishu chat's older pages on the web page (#2893, iOS #1033 / #1034): scrolling up loads the
next older page by itself with the message being read held in place, a failed page waits for a click, a topic group
keeps its button, and "Back to latest" shows once the list is more than a screen up.

Needs the fake hub's synthetic history (20 messages a page) and Playwright for Python:

    MOCK_LARK_HISTORY=1 PORT=18871 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18871 npx vite --port 5371 --strictPort &)
    python3 scripts/test/web_lark_history_e2e.py http://127.0.0.1:5371 --hub http://127.0.0.1:18871 [--shots <dir>] [--chrome]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves light / dark screenshots.
"""
import argparse
import json
import os
import sys
import urllib.request

from playwright.sync_api import expect, sync_playwright


def ok(msg):
    print("ok  " + msg, flush=True)


def state(hub, **change):
    req = urllib.request.Request(hub + "/test/lark-history/state", data=json.dumps(change).encode(), method="POST",
                                 headers={"Content-Type": "application/json", "Authorization": "Bearer mock"})
    with urllib.request.urlopen(req, timeout=5) as r:
        return json.load(r)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--hub", required=True)
    ap.add_argument("--shots")
    ap.add_argument("--chrome", action="store_true", help="drive the installed Chrome instead of Playwright's Chromium")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel="chrome") if a.chrome else p.chromium.launch()
        try:
            run(browser, a.base.rstrip("/"), a.hub.rstrip("/"), a.shots)
        except Exception as e:  # noqa: BLE001 - one verdict line
            print("FAIL " + str(e).splitlines()[0])
            sys.exit(1)
        finally:
            browser.close()
    print("PASS web Feishu history e2e")


def run(browser, base, hub, shots):
    ctx = browser.new_context(viewport={"width": 1100, "height": 760}, device_scale_factor=2)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    page.goto(base + "/?token=mock")
    page.wait_for_function("() => !location.search.includes('token=')")
    page.wait_for_timeout(500)
    page.evaluate("localStorage.setItem('lark.homeMessages', '1'); localStorage.setItem('persona.theme', 'light')")
    list_ = lambda: page.locator("div.overflow-y-auto:has([data-mid])").last  # noqa: E731
    row = lambda i: page.locator(f'[data-mid="history-{i}"]')  # noqa: E731

    def open_chat(newest=48, feed="f1"):
        page.goto(base + f"/messages/{feed}")
        expect(row(newest)).to_be_visible()

    def shot(name):
        if not shots:
            return
        for theme in ("light", "dark"):
            page.evaluate(f"document.documentElement.classList.toggle('dark', '{theme}' === 'dark'); localStorage.setItem('persona.theme', '{theme}')")
            page.wait_for_timeout(300)
            page.screenshot(path=os.path.join(shots, f"{name}-{theme}.png"))
        page.evaluate("document.documentElement.classList.remove('dark'); localStorage.setItem('persona.theme', 'light')")

    # 1. Opens at the newest; a first page shorter than the list asks for one more by itself, then stops.
    state(hub, reset=True)
    state(hub, total=120)
    open_chat(120)
    page.wait_for_timeout(800)
    first = state(hub)["calls"]
    assert first in ([], ["101"]), first
    expect(row(120)).to_be_in_viewport()
    expect(page.get_by_role("button", name="回到最新")).to_have_count(0)
    ok("opens at the newest messages; a short first page filled up with %d more page(s)" % len(first))

    # 2. Scrolling near the top loads the older page by itself, and the message read stays where it was.
    state(hub, hold=True)
    list_().evaluate("el => { el.scrollTop = 0; }")
    page.wait_for_timeout(400)
    oldest = int(page.locator("[data-mid]").first.get_attribute("data-mid").split("-")[1])
    before = row(oldest + 3).bounding_box()["y"]
    state(hub, hold=False)
    expect(row(oldest - 20)).to_be_attached()
    page.wait_for_timeout(300)
    after = row(oldest + 3).bounding_box()["y"]
    assert abs(after - before) <= 2, (before, after)
    calls = state(hub)["calls"]
    assert calls[len(first)] == str(oldest), calls
    ok("near the top the older page (before=%d) loads by itself; the message read stays put (%.0f → %.0f)" % (oldest, before, after))

    # 3. All the way up: every boundary asked for once, nothing more at the oldest.
    for _ in range(8):
        list_().evaluate("el => { el.scrollTop = 0; }")
        page.wait_for_timeout(350)
    expect(row(1)).to_be_attached()
    calls = state(hub)["calls"]
    assert len(calls) == len(set(calls)), calls
    n = len(calls)
    list_().evaluate("el => { el.scrollTop = 0; }")
    page.wait_for_timeout(500)
    assert len(state(hub)["calls"]) == n, state(hub)["calls"]
    ok("up to the first message, each boundary asked for once (%d pages), none after" % n)
    expect(page.get_by_role("button", name="回到最新")).to_be_visible()
    ok("回到最新 shows more than a screen up")
    shot("lark-history-older")

    # 4. "Back to latest" goes to the newest message.
    page.get_by_role("button", name="回到最新").click()
    page.wait_for_timeout(900)
    expect(row(120)).to_be_in_viewport()
    expect(page.get_by_role("button", name="回到最新")).to_have_count(0)
    ok("回到最新 scrolls to the newest message and hides")

    # 5. A failed page waits for a click.
    state(hub, reset=True)
    state(hub, fail=True)
    open_chat()
    list_().evaluate("el => { el.scrollTop = 0; }")
    retry = page.get_by_role("button", name="更早的消息未加载，点击重试")
    expect(retry).to_be_visible()
    page.wait_for_timeout(500)
    assert len(state(hub)["calls"]) == 1, state(hub)["calls"]
    shot("lark-history-failed")
    state(hub, fail=False)
    retry.click()
    expect(row(9)).to_be_attached()
    ok("a failed page shows one retry line, asks once, and loads on click")

    # 6. A page that brings nothing new stops instead of looping.
    state(hub, reset=True)
    state(hub, duplicate=True)
    open_chat()
    list_().evaluate("el => { el.scrollTop = 0; }")
    expect(page.get_by_role("button", name="暂时无法加载更早的消息，点击重试")).to_be_visible()
    page.wait_for_timeout(800)
    assert len(state(hub)["calls"]) == 1, state(hub)["calls"]
    ok("a duplicate page stops paging after one request")

    # 7. A topic group keeps its button.
    state(hub, reset=True)
    state(hub, topic=True)
    open_chat()
    list_().evaluate("el => { el.scrollTop = 0; }")
    page.wait_for_timeout(700)
    assert state(hub)["calls"] == [], state(hub)["calls"]
    page.get_by_role("button", name="更早的消息").click()
    expect(row(9)).to_be_attached()
    ok("a topic group pages only on its button")
    state(hub, reset=True)


if __name__ == "__main__":
    main()
