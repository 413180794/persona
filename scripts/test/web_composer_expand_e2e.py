#!/usr/bin/env python3
"""End-to-end check of the phone composer's grow-then-expand (#5688) in a real browser: at phone width the box grows
with the draft and shows no button, past its line limit it stops growing, scrolls and shows Expand at the text's top
right (the text inset for it), Expand opens the draft full screen, collapsing keeps the text and caret, and deleting
back under the limit takes the button away. At desktop width the box is unchanged (no button).

Needs a running web page against the fake hub and Playwright for Python (it drives the installed Chrome):

    PORT=18797 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18797 npx vite --port 5297 --strictPort &)
    python3 scripts/test/web_composer_expand_e2e.py http://127.0.0.1:5297 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves the four states at phone width,
light and dark.
"""
import argparse
import json
import os
import sys
import urllib.request

from playwright.sync_api import expect, sync_playwright

LINE = "检查登录流程在弱网下的表现，记下每次重试的耗时"  # i18n-ignore: draft text typed in the test


def ok(msg):
    print("ok  " + msg, flush=True)


def lines(n):
    return "\n".join(f"第 {i + 1} 步：{LINE}" for i in range(n))  # i18n-ignore: draft text typed in the test


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    a = ap.parse_args()
    base = a.base.rstrip("/")
    req = urllib.request.Request(base + "/api/v1/assistant", headers={"Authorization": "Bearer " + a.token})
    main_id = json.load(urllib.request.urlopen(req, timeout=10))["main"]
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel=os.environ.get("PERSONA_E2E_CHANNEL", "chrome"))
        try:
            for theme in ("light", "dark"):
                phone(browser, base, a.token, main_id, theme, a.shots)
            desktop(browser, base, a.token, main_id)
        except AssertionError as e:
            print("FAIL web composer expand e2e: " + str(e))
            sys.exit(1)
        finally:
            browser.close()
    print("PASS web composer expand e2e")


def open_chat(browser, base, token, main_id, theme, viewport, touch):
    ctx = browser.new_context(viewport=viewport, device_scale_factor=2, has_touch=touch, locale="zh-CN")
    page = ctx.new_page()
    page.set_default_timeout(10000)
    page.goto(base + "/overview?token=" + token)
    # the token is traded for this browser's own credential first; leaving before that lands on the login page
    expect(page.locator("aside")).to_be_attached()
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")
    page.goto(base + "/persona/" + main_id)
    box = page.locator("main textarea").last
    expect(box).to_be_visible(timeout=15000)
    return ctx, page, box


def phone(browser, base, token, main_id, theme, shots):
    ctx, page, box = open_chat(browser, base, token, main_id, theme, {"width": 390, "height": 844}, True)
    expand = page.get_by_test_id("composer-expand")
    shot = lambda name: shots and page.screenshot(path=os.path.join(shots, f"{name}-{theme}.png"))
    try:
        box.fill(lines(2))
        page.wait_for_timeout(200)
        assert expand.count() == 0, "a short draft shows Expand"
        short_h = box.bounding_box()["height"]
        ok(f"{theme}: a short draft grows the box ({short_h:.0f}px) with no button")
        shot("1-growing")

        box.fill(lines(10))
        page.wait_for_timeout(200)
        expect(expand).to_be_visible()
        tall_h = box.bounding_box()["height"]
        assert tall_h > short_h, "the box did not grow"
        assert box.evaluate("el => el.scrollHeight > el.clientHeight"), "the box does not scroll past its limit"
        pad = box.evaluate("el => parseFloat(getComputedStyle(el).paddingRight)")
        assert pad >= 28, f"the text is not inset for the button ({pad}px)"
        bb, eb = box.bounding_box(), expand.bounding_box()
        assert eb["y"] < bb["y"] + 24 and eb["x"] + eb["width"] > bb["x"] + bb["width"] - 40, "Expand is not at the text's top right"
        # the toolbar row keeps its own buttons only
        assert page.locator("main").get_by_test_id("composer-expand").count() == 1
        box.fill(lines(16))
        page.wait_for_timeout(200)
        assert abs(box.bounding_box()["height"] - tall_h) < 1, "the box kept growing past its limit"
        ok(f"{theme}: past the limit the box stops at {tall_h:.0f}px, scrolls, and shows Expand at its top right")
        box.evaluate("el => { el.scrollTop = 0; el.focus(); el.setSelectionRange(8, 8); }")
        shot("2-button")

        expand.click()
        editor = page.get_by_test_id("composer-expanded")
        expect(editor).to_be_visible()
        big = editor.locator("textarea")
        assert big.input_value() == lines(16), "the full editor lost the draft"
        assert big.evaluate("el => [el.selectionStart, el.selectionEnd]") == [8, 8], "the full editor lost the caret"
        expect(page.get_by_test_id("composer-collapse")).to_be_visible()
        expect(page.get_by_test_id("composer-expanded-send")).to_be_enabled()
        eb = editor.bounding_box()
        assert eb["height"] >= 800 and eb["width"] == 390, f"the editor is not full screen ({eb})"
        ok(f"{theme}: Expand opens the draft full screen with the caret where it was")
        page.wait_for_timeout(400)  # past its fade-in
        shot("3-expanded")

        big.evaluate("el => el.setSelectionRange(20, 20)")
        page.get_by_test_id("composer-collapse").click()
        expect(editor).to_have_count(0)
        assert box.input_value() == lines(16), "collapsing changed the draft"
        assert box.evaluate("el => [el.selectionStart, el.selectionEnd, document.activeElement === el]") == [20, 20, True], \
            "collapsing lost the caret"
        expect(expand).to_be_visible()
        ok(f"{theme}: collapsing keeps the text and caret, the button stays")
        box.evaluate("el => { el.scrollTop = 0; }")
        shot("4-collapsed")

        box.fill(lines(2))
        page.wait_for_timeout(200)
        assert expand.count() == 0, "Expand stayed after deleting under the limit"
        assert box.evaluate("el => parseFloat(getComputedStyle(el).paddingRight)") < 28, "the inset stayed"
        ok(f"{theme}: deleting under the limit takes the button and the inset away")
    finally:
        ctx.close()


def desktop(browser, base, token, main_id):
    ctx, page, box = open_chat(browser, base, token, main_id, "light", {"width": 1280, "height": 820}, False)
    try:
        box.fill(lines(16))
        page.wait_for_timeout(200)
        assert page.get_by_test_id("composer-expand").count() == 0, "desktop shows Expand"
        h = box.bounding_box()["height"]
        assert h <= 241, f"desktop box past its own 240px cap ({h})"
        ok(f"desktop: no button, the box keeps its own cap ({h:.0f}px)")
    finally:
        ctx.close()


if __name__ == "__main__":
    main()
