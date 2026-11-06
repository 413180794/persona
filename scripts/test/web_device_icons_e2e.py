#!/usr/bin/env python3
"""Check Settings device icons (#3596) against a dedicated fake hub and web server.

    python3 scripts/test/web_device_icons_e2e.py <web address> [--shots <directory>]
    python3 scripts/test/web_device_icons_e2e.py <web address> --before --shots <directory>

Only /auth/tokens is replaced with fictional devices. --before checks and captures
the original bug; the default checks the fix. Both layouts and themes are covered.
"""
import argparse
import json
import os

from playwright.sync_api import expect, sync_playwright


DEVICES = [
    {"id": "sample-phone", "name": "示例手机", "kind": "phone", "role": "full"},
    {"id": "sample-watch", "name": "示例手表", "kind": "watch", "role": "answer"},
    {"id": "sample-web", "name": "示例网页", "kind": "web", "role": "full"},
    {"id": "sample-cli", "name": "示例命令行", "kind": "other", "role": "full"},
    {"id": "sample-unknown", "name": "示例未知设备", "kind": "future-device", "role": "full"},
    {"id": "sample-revoked-phone", "name": "已撤销手机", "kind": "phone", "role": "full",
     "revoked_at": "2026-11-01T09:00:00Z"},
    {"id": "sample-revoked-watch", "name": "已撤销手表", "kind": "watch", "role": "answer",
     "revoked_at": "2026-11-01T08:00:00Z"},
]


def geometry(icon):
    """Read rendered SVG geometry, independently of the icon's source name."""
    return icon.evaluate("""svg => {
        const shapes = [...svg.querySelectorAll('path, rect, circle, line, polyline, polygon')];
        return {
            markup: svg.innerHTML,
            shapes: shapes.map(shape => {
                const box = shape.getBBox();
                return {tag: shape.tagName, x: box.x, y: box.y, width: box.width, height: box.height};
            }),
        };
    }""")


def check_shape(shape, kind, before):
    parts = shape["shapes"]
    assert parts and any(part["width"] or part["height"] for part in parts), (kind, shape)
    if kind not in ("phone", "watch"):
        assert any(part["tag"] == "circle" and part["width"] >= 16 for part in parts), shape
        return

    rectangles = [part for part in parts if part["tag"] == "rect"]
    assert len(rectangles) == 1, (kind, shape)
    face = rectangles[0]
    if before:
        # Both broken icons have a horizontal screen and a stand below it.
        assert face["width"] > face["height"], (kind, shape)
        assert any(part["y"] + part["height"] > face["y"] + face["height"] for part in parts), shape
    elif kind == "phone":
        assert face["height"] >= 1.5 * face["width"] and face["height"] >= 16, shape
        # A phone outline encloses its details, with no desktop stand below it.
        assert all(part["y"] >= face["y"] and
                   part["y"] + part["height"] <= face["y"] + face["height"] for part in parts), shape
    else:
        # A compact watch face has a strap continuing above and below it.
        assert 0.75 <= face["width"] / face["height"] <= 1.5, shape
        paths = [part for part in parts if part["tag"] == "path"]
        assert any(part["y"] < face["y"] for part in paths), shape
        assert any(part["y"] + part["height"] > face["y"] + face["height"] for part in paths), shape


def check(browser, base, width, theme, shots, before):
    context = browser.new_context(viewport={"width": width, "height": 900}, color_scheme=theme, locale="zh-CN")
    context.add_init_script("localStorage.setItem('persona.theme', " + repr(theme) + ")")
    context.route("**/api/v1/auth/tokens", lambda route: route.fulfill(
        status=200, content_type="application/json",
        body=json.dumps({"tokens": DEVICES, "current": "sample-web"})))
    page = context.new_page()
    page.set_default_timeout(15000)
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    try:
        page.goto(base + "/settings?tab=devices&token=mock")
        # Vite's module errors cover the page without emitting a browser pageerror.
        expect(page.locator("vite-error-overlay")).to_have_count(0)
        main = page.locator("main")
        icons = {}
        labels = {"phone": "手机", "watch": "手表", "web": "网页", "other": "命令行"}
        for device in DEVICES:
            name = main.get_by_text(device["name"], exact=True)
            expect(name).to_be_visible()
            row = name.locator("..").locator("..").locator("..")
            icon = row.locator(":scope > svg")
            expect(icon).to_have_count(1)
            expect(icon).to_be_visible()
            expect(icon).to_have_attribute("aria-hidden", "true")
            expect(icon).to_have_attribute("stroke", "currentColor")
            details = row.locator(":scope > div > div").last
            expect(details).to_contain_text(labels.get(device["kind"], device["kind"]))
            icons[device["id"]] = geometry(icon)
            check_shape(icons[device["id"]], device["kind"], before)
            can_revoke = not device.get("revoked_at") and device["id"] != "sample-web"
            expect(row.get_by_role("button", name="撤销", exact=True)).to_have_count(int(can_revoke))

        phone, watch, web = (icons["sample-" + kind]["markup"] for kind in ("phone", "watch", "web"))
        assert len({phone, watch, web}) == 3, "Phone, watch and web must have distinct visible shapes"
        assert icons["sample-revoked-phone"]["markup"] == phone
        assert icons["sample-revoked-watch"]["markup"] == watch
        assert icons["sample-cli"]["markup"] == web
        assert icons["sample-unknown"]["markup"] == web
        expect(main.get_by_text("这个浏览器", exact=True)).to_be_visible()
        if shots:
            page.wait_for_timeout(400)
            expect(page.locator("vite-error-overlay")).to_have_count(0)
            phase = "before" if before else "after"
            main.screenshot(path=os.path.join(shots, f"devices-{phase}-{width}-{theme}.png"))
        expect(page.locator("vite-error-overlay")).to_have_count(0)
        assert not errors, errors
        phase = "original bug reproduced" if before else "device icons"
        print(f"PASS {phase} {width}px {theme}", flush=True)
    finally:
        context.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("base")
    parser.add_argument("--shots")
    parser.add_argument("--before", action="store_true", help="Check the original monitor-icon bug")
    args = parser.parse_args()
    if args.shots:
        os.makedirs(args.shots, exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(channel="chrome")
        try:
            for width in (1440, 390):
                for theme in ("light", "dark"):
                    check(browser, args.base.rstrip("/"), width, theme, args.shots, args.before)
        finally:
            browser.close()
    print("PASS original device icon bug reproduced" if args.before else "PASS web device icons e2e")


if __name__ == "__main__":
    main()
