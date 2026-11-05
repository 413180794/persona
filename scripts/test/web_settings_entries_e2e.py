#!/usr/bin/env python3
"""Check independent channel settings (#2196) against MOCK_CHANNELS=1 MOCK_PLUGINS=1.

Run with a dedicated fake hub and the web dev server pointing to it:
    python3 scripts/test/web_settings_entries_e2e.py <web address> [--shots <directory>]

Checks both layouts, old links, cross-links and Back; screenshots contain only mock data.
"""
import argparse
import json
import os

from playwright.sync_api import expect, sync_playwright


def check(browser, base, width, theme, shots):
    context = browser.new_context(viewport={"width": width, "height": 900}, color_scheme=theme)
    context.add_init_script("localStorage.setItem('jarvis.theme', " + repr(theme) + ")")
    page = context.new_page()
    page.set_default_timeout(15000)
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    try:
        page.goto(base + "/settings?token=mock")
        main = page.locator("main")
        nav = main.get_by_role("navigation", name="设置分类")
        expect(nav.get_by_role("button", name="渠道", exact=True)).to_be_visible()
        labels = nav.get_by_role("button").all_text_contents()
        labels = [label.strip() for label in labels]
        assert labels.index("渠道") + 1 == labels.index("连接器"), labels
        expect(main.get_by_text("认识 Persona", exact=True)).to_have_count(0)
        if shots:
            page.wait_for_timeout(400)
            main.screenshot(path=os.path.join(shots, f"settings-{width}-{theme}.png"))

        nav.get_by_role("button", name="渠道", exact=True).click()
        expect(page).to_have_url(base + "/settings?tab=channels")
        expect(main.get_by_role("heading", name="渠道", exact=True).last).to_be_visible()
        expect(main.get_by_role("button", name="扩展", exact=True)).to_have_count(0 if width < 640 else 1)
        expect(main.get_by_text("Example Rooms", exact=True)).to_be_visible()
        if shots:
            page.wait_for_timeout(400)
            main.screenshot(path=os.path.join(shots, f"channels-{width}-{theme}.png"))

        main.get_by_role("button").filter(has=page.get_by_text("Example Rooms", exact=True)).click()
        expect(page).to_have_url(base + "/settings?tab=extensions&extension=-market%2Fexample%2Fexample-rooms")
        page.goto(base + "/settings?tab=channels")
        main.get_by_role("button").filter(has=page.get_by_text("微信", exact=True)).click()
        expect(page).to_have_url(base + "/settings?tab=channels&channel=wechat")
        expect(main.get_by_role("heading", name="微信", exact=True)).to_be_visible()
        main.get_by_role("button", name="渠道", exact=True).last.click()
        expect(page).to_have_url(base + "/settings?tab=channels")
        if width < 640:
            main.get_by_role("button", name="返回设置", exact=True).click()
            expect(page).to_have_url(base + "/settings")
            expect(nav.get_by_role("button", name="渠道", exact=True)).to_be_visible()

        page.goto(base + "/settings?tab=extensions")
        expect(main.get_by_text("扩展给 Persona 加上新的连接器", exact=False)).to_be_visible()
        expect(main.get_by_role("heading", name="渠道", exact=True)).to_have_count(0)
        expect(main.get_by_role("button", name="渠道", exact=True)).to_have_count(0 if width < 640 else 1)
        expect(main.get_by_text("和 Persona 对话的入口：", exact=False)).to_have_count(0)

        for old in ("?tab=plugins&plugin=-channels%2Fwechat", "?tab=extensions&extension=-channels%2Fwechat"):
            page.goto(base + "/settings" + old)
            expect(main.get_by_role("heading", name="微信", exact=True)).to_be_visible()
            main.get_by_role("button", name="渠道", exact=True).last.click()
            expect(page).to_have_url(base + "/settings?tab=channels")
        page.goto(base + "/settings?tab=extensions&extension=-channels")
        expect(main.get_by_role("heading", name="渠道", exact=True).last).to_be_visible()
        main.get_by_role("button").filter(has=page.get_by_text("飞书机器人", exact=True)).click()
        expect(main.get_by_role("heading", name="飞书机器人", exact=True)).to_be_visible()
        main.get_by_role("button", name="更多设置", exact=True).click()
        expect(page).to_have_url(base + "/settings?tab=connectors&connector=feishu-bot")
        expect(page.get_by_role("dialog")).to_be_visible()
        page.keyboard.press("Escape")
        expect(page.get_by_role("dialog")).to_have_count(0)
        expect(main.get_by_text("在 设置 › 渠道 里设置飞书机器人", exact=False)).to_be_visible()
        main.get_by_text("在 设置 › 渠道 里设置飞书机器人", exact=False).click()
        expect(page).to_have_url(base + "/settings?tab=channels&channel=feishu")
        assert not errors, errors
        print(f"PASS settings entries {width}px {theme}", flush=True)
    finally:
        context.close()


def check_capabilities(browser, base):
    for channels, extensions in ((True, False), (False, True)):
        context = browser.new_context(viewport={"width": 390, "height": 900})

        def socket(route):
            server = route.connect_to_server()

            def message(raw):
                data = json.loads(raw)
                if data.get("type") == "hello":
                    data["features"] = [feature for feature in data.get("features", [])
                        if (channels or feature != "channels")
                        and (extensions or not feature.startswith(("extension", "plugin")))]
                route.send(json.dumps(data))

            server.on_message(message)

        context.route_web_socket("**/api/v1/ws?*", socket)
        try:
            page = context.new_page()
            page.goto(base + "/settings?token=mock")
            nav = page.locator("main").get_by_role("navigation", name="设置分类")
            expect(nav.get_by_role("button", name="渠道", exact=True)).to_have_count(int(channels))
            expect(nav.get_by_role("button", name="扩展", exact=True)).to_have_count(int(extensions))
            if channels:
                nav.get_by_role("button", name="渠道", exact=True).click()
                expect(page).to_have_url(base + "/settings?tab=channels")
                expect(page.locator("main").get_by_text("Example Rooms", exact=True)).to_be_visible()
            print(f"PASS independent capabilities channels={channels} extensions={extensions}", flush=True)
        finally:
            context.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("base")
    parser.add_argument("--shots")
    args = parser.parse_args()
    if args.shots:
        os.makedirs(args.shots, exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(channel="chrome")
        try:
            check_capabilities(browser, args.base.rstrip("/"))
            for width in (1440, 390):
                for theme in ("light", "dark"):
                    check(browser, args.base.rstrip("/"), width, theme, args.shots)
        finally:
            browser.close()
    print("PASS web settings entries e2e")


if __name__ == "__main__":
    main()
