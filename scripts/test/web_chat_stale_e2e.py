#!/usr/bin/env python3
"""End-to-end check of the chat page's safety net under its live stream (#3139): the fake hub writes a reply and moves the
session's updated_at but drops the items push (/mock/say?drop=1), and the page must still show the reply within seconds
(it asks for `items?after_seq=` once, silently, without reloading), must not ask more often than every 10 s, and after
repeated recoveries says "Not live" with a reload that clears it.

Needs a running web page against the fake hub and Playwright for Python (it drives the installed Chrome, or the
bundled Chromium when there is none):

    PORT=19031 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:19031 npx vite --port 19032 --strictPort &)
    python3 scripts/test/web_chat_stale_e2e.py http://127.0.0.1:19032 --hub http://127.0.0.1:19031 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure.
"""
import argparse
import os
import sys
import time
import urllib.parse
import urllib.request

from playwright.sync_api import sync_playwright

AGENT = ("macbook", "a1f3c2d4-0000-4000-8000-000000000001")
CHAT = "[data-chat-scroll]"
FIRST = "给天气 App 加一个主屏幕小组件"
NOT_LIVE = "[data-testid=chat-not-live]"


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--hub", required=True)
    ap.add_argument("--token", default="mock")
    ap.add_argument("--shots")
    a = ap.parse_args()

    def hub(path, **q):
        urllib.request.urlopen(a.hub + path + ("?" + urllib.parse.urlencode(q) if q else "")).read()

    with sync_playwright() as p:
        try:
            browser = p.chromium.launch(channel="chrome")
        except Exception:
            browser = p.chromium.launch()
        page = browser.new_context(viewport={"width": 1280, "height": 820}, locale="zh-CN").new_page()
        pulls = []  # when the page asked for what it was missing
        page.on("request", lambda r: pulls.append(time.time()) if "after_seq=" in r.url and "/items" in r.url else None)

        def appears(text, what, within=16000):
            try:
                page.locator(CHAT).get_by_text(text).first.wait_for(timeout=within)
            except Exception:
                fail(f"{what}: {text!r} did not appear on its own")
            ok(f"{what}: {text!r} appeared without a reload")

        try:
            hub("/mock/reset")
            page.goto(a.base + "/?token=" + a.token)
            page.wait_for_timeout(800)
            for attempt in range(3):  # a cold dev server optimizes its dependencies and reloads the page once
                page.goto(f"{a.base}/s/{AGENT[0]}/{AGENT[1]}")
                try:
                    page.locator(CHAT).get_by_text(FIRST).first.wait_for(timeout=15000)
                    break
                except Exception:
                    if attempt == 2:
                        fail("the agent page did not open")
            page.wait_for_timeout(1500)  # the page's own subscription is on the socket
            page.evaluate("window.__stale_marker = 1")  # a reload would take it away
            pulls.clear()

            # A healthy stream: a pushed reply arrives, and nothing is asked for.
            hub("/mock/say", user="正常的问题", text="正常的直播回复")
            appears("正常的直播回复", "a pushed reply")
            page.wait_for_timeout(7000)
            if pulls:
                fail("the page asked for items although its stream was working")
            ok("a working stream is not second-guessed")

            # One push is lost: the page notices, asks once, shows it, and does not say anything about it.
            hub("/mock/say", user="丢了推送的问题", text="丢了推送的回复", drop="1")
            appears("丢了推送的回复", "a reply whose push was dropped")
            if len(pulls) != 1:
                fail(f"expected one silent ask, saw {len(pulls)}")
            ok("one ask brought it")
            if page.evaluate("window.__stale_marker") != 1:
                fail("the page reloaded to get it")
            ok("the page did not reload")
            if page.locator(NOT_LIVE).count():
                fail("a single recovery already says the page is not live")
            ok("a single recovery says nothing")

            # Another is lost right away: asks are at least 10 s apart; a second recovery in a row says so.
            first_pull = pulls[0]
            hub("/mock/say", user="又丢了一次的问题", text="又丢了一次的回复", drop="1")
            appears("又丢了一次的回复", "a second dropped reply", within=30000)
            if len(pulls) != 2 or pulls[1] - first_pull < 9.5:
                fail(f"asks were {[round(x - first_pull, 1) for x in pulls]} s after the first; at most one per 10 s")
            ok(f"asks are throttled ({pulls[1] - first_pull:.1f} s apart)")
            try:
                page.locator(NOT_LIVE).wait_for(timeout=3000)
            except Exception:
                fail("repeated recoveries did not say 不是实时的")
            if "不是实时的" not in page.locator(NOT_LIVE).inner_text() or "重新加载" not in page.locator(NOT_LIVE).inner_text():
                fail("the hint is not 「不是实时的」 with a reload")
            ok("repeated recoveries say 不是实时的 with a reload")
            if a.shots:
                os.makedirs(a.shots, exist_ok=True)
                page.screenshot(path=os.path.join(a.shots, "chat-not-live.png"))

            page.locator(NOT_LIVE).get_by_role("button", name="重新加载").click()
            page.locator(NOT_LIVE).wait_for(state="detached", timeout=5000)
            appears("又丢了一次的回复", "after the reload")
            page.wait_for_timeout(1500)
            hub("/mock/say", user="重新加载之后的问题", text="重新加载之后的直播回复")
            appears("重新加载之后的直播回复", "a pushed reply after the reload")
        finally:
            try:
                hub("/mock/reset")
            finally:
                browser.close()
    print("PASS web_chat_stale_e2e", flush=True)


if __name__ == "__main__":
    main()
