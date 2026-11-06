#!/usr/bin/env python3
"""End-to-end check of the web Agents list's "herdr is not running on <computer>" row with "Start" (#1916): a row only for the
machine whose herdr is down; "Start" turns it to "Starting..." (the button kept unseen, not pressable) and then the row goes,
the node asked once; a start that fails says why in words (not the node's English) and keeps "Start", the raw text and
request id behind "Copy details".

Needs a running web page against the fake hub (its GET /mock/herdr?stopped=&start= picks which machine is down and how
"Start" goes) and Playwright for Python:

    PORT=18871 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18871 npx vite --port 5871 --strictPort &)
    python3 scripts/test/web_herdr_notice_e2e.py http://127.0.0.1:5871 --hub http://127.0.0.1:18871 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. Runs light and dark, at desktop and phone
widths; --shots saves each state there. --channel msedge where there is no Chrome.
"""
import argparse
import json
import os
import sys
import urllib.request

from playwright.sync_api import expect, sync_playwright

SIZES = {"wide": {"width": 1280, "height": 860}, "narrow": {"width": 390, "height": 844}}


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def mock(hub, query=""):
    with urllib.request.urlopen(f"{hub}/mock/herdr{query}", timeout=10) as r:
        return json.loads(r.read()).get("starts", {})


def run(browser, base, hub, token, size, theme, shots):
    tag = f"{size}/{theme}"
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2, has_touch=size == "narrow", locale="zh-CN")
    page = ctx.new_page()
    page.set_default_timeout(20000)
    page.goto(base + "/overview?token=" + token)
    page.wait_for_function("localStorage.getItem('persona.token')")
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")

    def shot(name):
        if shots:
            page.wait_for_timeout(300)
            page.screenshot(path=os.path.join(shots, f"herdr-{size}-{theme}-{name}.png"))

    def open_list(query):
        urllib.request.urlopen(f"{hub}/mock/reset", timeout=10).read()
        mock(hub, query)
        page.goto(base + ("/agents" if size == "narrow" else "/"))

    row = page.get_by_test_id("herdr-notice")

    # down, then a start that fails
    open_list("?stopped=macbook&start=fail")
    expect(row).to_have_count(1)
    expect(row).to_contain_text("MacBook 的 herdr 没在运行")
    if page.get_by_test_id("herdr-notice").filter(has_text="Mac Studio").count():
        fail(f"{tag}: a row for a machine whose herdr runs")
    ok(f"{tag}: one row, for the machine that is down")
    shot("stopped")
    row.get_by_role("button", name="启动").click()
    failure = page.get_by_test_id("herdr-notice-failure")
    expect(failure).to_contain_text("没启动：herdr 起来后马上退出了（退出码 1）")
    if "stopped right after starting" in row.inner_text():
        fail(f"{tag}: the node's raw words are in the row")
    expect(row.get_by_role("button", name="启动")).to_be_enabled()
    expect(row.get_by_role("button", name="复制详情")).to_be_visible()
    ok(f"{tag}: a failed start says why in words, keeps 启动, details behind 复制详情")
    shot("failed")
    if mock(hub) != {"macbook": 1}:
        fail(f"{tag}: the node was not asked exactly once")

    # a slow start that works
    open_list("?stopped=macbook&start=slow")
    expect(row).to_have_count(1)
    row.get_by_role("button", name="启动").click()
    expect(row).to_contain_text("正在启动 MacBook 的 herdr…")
    if row.get_by_role("button", name="启动").count():
        fail(f"{tag}: 启动 can still be found while it starts")
    ok(f"{tag}: 正在启动…, and 启动 cannot be pressed again")
    shot("starting")
    expect(row).to_have_count(0, timeout=15000)
    page.wait_for_timeout(11000)  # past a read of the list (HERDR_POLL_MS)
    expect(row).to_have_count(0)
    if mock(hub) != {"macbook": 1}:
        fail(f"{tag}: the node was not asked exactly once")
    ok(f"{tag}: the row goes and stays gone")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--hub", required=True)
    ap.add_argument("--token", default="mock")
    ap.add_argument("--shots")
    ap.add_argument("--channel", default="chrome", help="the installed browser Playwright drives (chrome, msedge)")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel=a.channel)
        for size in SIZES:
            for theme in ("light", "dark"):
                run(browser, a.base, a.hub, a.token, size, theme, a.shots)
        browser.close()
    print("PASS web herdr notice", flush=True)


if __name__ == "__main__":
    main()
