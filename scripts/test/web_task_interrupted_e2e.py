#!/usr/bin/env python3
"""End-to-end check of a task the model service cut off (#1516), in the web's main conversation: its agent's turn ended
in an API error, so its receipt is red ("<task> Interrupted", why in words, "Continue"), not a green "Done" with the error as
its summary; its card says "Interrupted" with "Continue", and the dock has it in red. "Continue" sends one retry; the card goes to "Working" and
the receipt folds to a quiet line, and once the agent finishes the card is "Unread".

Needs a running web page against the fake hub with a cut-off task (MOCK_TASKS=1 MOCK_TASKS_CUT=1; GET /mock/cut puts
it back) and Playwright for Python (it drives the installed Chrome):

    MOCK_TASKS=1 MOCK_TASKS_CUT=1 PORT=18916 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18916 npx vite --port 5316 --strictPort &)
    python3 scripts/test/web_task_interrupted_e2e.py http://127.0.0.1:5316 --hub http://127.0.0.1:18916 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. Runs light and dark, at desktop and phone
widths; --shots saves each state there.
"""
import argparse
import os
import sys
import urllib.request

from playwright.sync_api import expect, sync_playwright

TITLE = "整理发布说明"
SIZES = {"wide": {"width": 1280, "height": 900}, "narrow": {"width": 390, "height": 844}}


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def reset(hub, token):
    req = urllib.request.Request(f"{hub}/mock/cut", headers={"Authorization": "Bearer " + token})
    urllib.request.urlopen(req, timeout=10).read()


def run(browser, base, hub, token, size, theme, shots):
    tag = f"{size}/{theme}"
    reset(hub, token)
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2, has_touch=size == "narrow")
    page = ctx.new_page()
    page.set_default_timeout(15000)
    posts = []
    page.on("request", lambda r: r.method == "POST" and r.url.endswith("/retry") and posts.append(r.url))
    page.goto(base + "/overview?token=" + token)
    page.wait_for_function("localStorage.getItem('persona.token')")
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")
    page.goto(base + "/")

    def shot(name):
        if shots:
            page.wait_for_timeout(300)
            page.screenshot(path=os.path.join(shots, f"cut-{size}-{theme}-{name}.png"))

    def bring(loc):
        vp = SIZES[size]
        page.mouse.move(vp["width"] * 0.7, vp["height"] * 0.4)
        for _ in range(80):
            box = loc.bounding_box()
            if box and vp["height"] * 0.15 < box["y"] < vp["height"] * 0.5:
                return
            page.mouse.wheel(0, -120 if not box or box["y"] < vp["height"] * 0.15 else 120)
            page.wait_for_timeout(150)
        fail(f"{tag}: could not scroll to {loc}")

    receipt = page.locator("div.bg-red-tint", has_text=f"{TITLE} 中断了")
    card = page.locator("div[role=button]", has_text=TITLE).filter(has=page.get_by_text("模型服务出错，中断了：")).first
    pill = page.get_by_test_id("task-dock").get_by_role("button", name=TITLE)
    expect(receipt).to_have_count(1)
    expect(receipt).to_contain_text("模型服务出错，中断了：连不上模型服务")
    if receipt.get_by_text("API Error").count():
        fail(f"{tag}: the receipt shows the raw error")
    if page.locator("div.bg-green-tint", has_text=f"{TITLE} 完成").count():
        fail(f"{tag}: a green 完成 receipt for the cut-off task")
    expect(pill).to_contain_text("中断了")
    ok(f"{tag}: the receipt is red with why in words, the dock says 中断了")
    bring(card)
    expect(card).to_contain_text("中断了")
    expect(card.get_by_role("button", name="继续")).to_be_visible()
    bring(receipt)
    expect(receipt.get_by_role("button", name="继续")).to_be_visible()
    shot("cut")
    ok(f"{tag}: card and receipt offer 继续")

    receipt.get_by_role("button", name="继续").click()
    quiet = page.get_by_text(f"「{TITLE}」中断后继续了")
    expect(quiet).to_be_visible()
    expect(receipt).to_have_count(0)
    bring(card := page.locator("div[role=button]", has_text=TITLE).first)
    expect(card).to_contain_text("工作中")
    expect(card.get_by_role("button", name="继续")).to_have_count(0)
    shot("continued")
    if len(posts) != 1:
        fail(f"{tag}: {len(posts)} retries posted")
    ok(f"{tag}: 继续 posted once; the card is 工作中 and the receipt a quiet line")
    expect(card).to_contain_text("完成未看", timeout=10000)
    shot("finished")
    ok(f"{tag}: once it finished the card is 完成未看")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--hub", required=True)
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as pw:
        browser = pw.chromium.launch(channel="chrome")
        for size in SIZES:
            for theme in ("light", "dark"):
                run(browser, a.base, a.hub, a.token, size, theme, a.shots)
        browser.close()
    print("PASS web task interrupted e2e", flush=True)


if __name__ == "__main__":
    main()
