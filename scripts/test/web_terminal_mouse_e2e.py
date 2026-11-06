#!/usr/bin/env python3
"""End-to-end check of the mouse on a terminal the web page drives (#2441): press / drag / release with the button and
modifiers go to the hub as `terminal_input.mouse` (one drag event per cell), Shift keeps the pointer on the page (a
selection in xterm, Persona's right-click menu), "Select locally" turns that around, a touch-less watcher sends nothing, and the
wheel is untouched.

Needs the fake hub with terminals (and panes), a running web page against it, and Playwright for Python (the installed
Chrome, or the browser PERSONA_E2E_CHROMIUM points at):

    MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1 PORT=18960 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18960 npx vite --port 18961 --strictPort &)
    python3 scripts/test/web_terminal_mouse_e2e.py http://127.0.0.1:18961 --hub http://127.0.0.1:18960 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves light / dark screenshots.
"""
import argparse
import json
import os
import re
import urllib.request

from playwright.sync_api import expect, sync_playwright

SHELL = "pane%3Aw1%3Ap2"  # a shell of the fake hub's "persona" workspace


def ok(msg):
    print("ok  ", msg, flush=True)


def sent(hub):
    """Every terminal_input the fake hub was sent."""
    with urllib.request.urlopen(hub + "/mock/terminal_input") as r:
        return json.load(r)["input"]


def mouse(hub):
    return [m["mouse"] for m in sent(hub) if "mouse" in m]


def clicks(hub):
    return [m["click"] for m in sent(hub) if "click" in m]


def fresh(browser, token, viewport, scheme="light", mode=None):
    # the checks find controls by their Chinese names: the page follows the browser's language, not the machine's (#4027)
    ctx = browser.new_context(viewport=viewport, color_scheme=scheme, device_scale_factor=2, locale="zh-CN")
    init = f"localStorage.setItem('persona.token', {token!r}); localStorage.setItem('persona.theme', {scheme!r});"
    if mode:
        init += f"localStorage.setItem('persona.terminal.mouse', {mode!r});"
    ctx.add_init_script(init)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    return ctx, page


def driving(page):
    expect(page.get_by_role("radio", name="接管（可打字）")).to_have_attribute("aria-checked", "true")


def open_terminal(browser, base, token, viewport=None, scheme="light", mode=None):
    ctx, page = fresh(browser, token, viewport or {"width": 1500, "height": 900}, scheme, mode)
    page.goto(f"{base}/terminal/server/{SHELL}")
    driving(page)
    page.wait_for_timeout(800)  # the first screen is drawn and the page has its grid
    return ctx, page


class Grid:
    """The screen's box and cell size, to aim at the middle of a cell."""

    def __init__(self, page):
        screen = page.locator('section[aria-label="终端"] > div:not(.invisible) .xterm-screen').first
        self.box = screen.bounding_box()
        # the "Take over" / "Watch" switch carries the grid (data-grid="44x30"; its hint says it in columns and rows)
        m = re.fullmatch(r"(\d+)x(\d+)", page.locator("[role=radiogroup][data-grid]").first.get_attribute("data-grid") or "")
        assert m, "the switch does not carry the grid"
        cols, self.rows = int(m.group(1)), int(m.group(2))
        self.cw = self.box["width"] / cols
        self.ch = self.box["height"] / self.rows

    def at(self, col, row):
        return (self.box["x"] + (col + 0.5) * self.cw, self.box["y"] + (row + 0.5) * self.ch)


def selection(page):
    """xterm paints a selection as divs in its selection layer."""
    return page.locator(".xterm-selection > div").count()


def menu(page):
    return page.locator('[role="menu"]').count()


def defaults(browser, base, hub, token, shots):
    ctx, page = open_terminal(browser, base, token)
    g = Grid(page)
    assert g.cw > 3 and g.rows > 5, (g.cw, g.rows)
    start = len(mouse(hub))
    new = lambda: mouse(hub)[start:]
    start_clicks = len(clicks(hub))

    # a plain click: press and release at the cell, left, no modifiers; not also the older click
    x, y = g.at(10, 5)
    page.mouse.move(x, y)
    page.mouse.down()
    page.mouse.up()
    page.wait_for_timeout(300)
    assert new() == [
        {"action": "down", "button": "left", "at": [10, 5], "mods": 0},
        {"action": "up", "button": "left", "at": [10, 5], "mods": 0},
    ], new()
    assert len(clicks(hub)) == start_clicks, "the press also went as a click"
    ok("a click is a press and a release of the left button at the cell")

    # a drag: motion with the button held, one event per cell change, and no selection on the page
    n = len(new())
    x, y = g.at(5, 4)
    page.mouse.move(x, y)
    page.mouse.down()
    for col in (5, 5, 6, 6, 7, 8):
        page.mouse.move(*g.at(col, 4), steps=3)
    page.mouse.move(*g.at(8, 6), steps=3)
    page.mouse.up()
    page.wait_for_timeout(300)
    got = new()[n:]
    assert got[0] == {"action": "down", "button": "left", "at": [5, 4], "mods": 0}, got
    drags = [m["at"] for m in got if m["action"] == "drag"]
    assert drags[:3] == [[6, 4], [7, 4], [8, 4]] and drags[-1] == [8, 6], drags
    assert len({tuple(d) for d in drags}) == len(drags) or all(a != b for a, b in zip(drags, drags[1:])), drags
    assert got[-1] == {"action": "up", "button": "left", "at": [8, 6], "mods": 0}, got[-1]
    assert selection(page) == 0, "the page selected text under a drag that belongs to the program"
    ok("a drag sends the cells it crosses once each, then the release; the page selects nothing")

    # a drag that leaves the screen keeps pointing at the nearest edge cell; the release still arrives
    n = len(new())
    page.mouse.move(*g.at(3, 3))
    page.mouse.down()
    page.mouse.move(g.box["x"] + g.box["width"] + 80, g.box["y"] + 3 * g.ch, steps=4)
    page.mouse.up()
    page.wait_for_timeout(300)
    got = new()[n:]
    assert got[-1]["action"] == "up" and got[-1]["at"][0] == int(g.box["width"] / g.cw - 1e-6), got[-1]
    ok("a drag out of the panel is clamped to the edge and its release is sent")

    # buttons
    for name, label in (("middle", "middle"), ("right", "right")):
        n = len(new())
        page.mouse.move(*g.at(12, 8))
        page.mouse.down(button=name)
        page.mouse.up(button=name)
        page.wait_for_timeout(200)
        got = new()[n:]
        assert [(m["action"], m["button"]) for m in got] == [("down", label), ("up", label)], got
    assert menu(page) == 0, "the right click also opened Persona's menu"
    ok("middle and right buttons go to the program, and the right click opens no menu")

    # modifiers: ctrl 2, alt 4 (shift is the page's key)
    for key, bit in (("Control", 2), ("Alt", 4)):
        n = len(new())
        page.keyboard.down(key)
        page.mouse.move(*g.at(4, 2))
        page.mouse.down()
        page.mouse.up()
        page.keyboard.up(key)
        page.wait_for_timeout(200)
        got = new()[n:]
        assert [m["mods"] for m in got] == [bit, bit], got
    ok("ctrl and alt are sent as herdr's modifier bits")

    # Shift: the page's pointer — a selection in xterm, nothing to the program
    n = len(new())
    page.keyboard.down("Shift")
    page.mouse.move(*g.at(2, 3))
    page.mouse.down()
    page.mouse.move(*g.at(14, 3), steps=6)
    page.mouse.up()
    page.keyboard.up("Shift")
    page.wait_for_timeout(300)
    assert new()[n:] == [], new()[n:]
    assert selection(page) > 0, "Shift+drag did not select"
    ok("Shift+drag selects text on the page and sends nothing")
    if shots:
        page.screenshot(path=os.path.join(shots, "mouse-shift-selection-light.png"))

    # a press afterwards is the program's again, and the old selection goes
    page.mouse.move(*g.at(20, 10))
    page.mouse.down()
    page.mouse.up()
    page.wait_for_timeout(200)
    assert selection(page) == 0, "the Shift selection stayed under a click for the program"
    ok("a click for the program clears the selection")

    # Shift+right click is Persona's menu (the browser's own does not come up)
    n = len(new())
    page.keyboard.down("Shift")
    page.mouse.move(*g.at(6, 6))
    page.mouse.down(button="right")
    page.mouse.up(button="right")
    page.keyboard.up("Shift")
    page.wait_for_timeout(300)
    assert new()[n:] == [], new()[n:]
    expect(page.get_by_role("menuitem", name="全选")).to_be_visible()
    if shots:
        page.screenshot(path=os.path.join(shots, "mouse-shift-menu-light.png"))
    page.keyboard.press("Escape")
    ok("Shift+right click opens Persona's menu and sends nothing")

    # the wheel is as it was
    page.mouse.move(*g.at(10, 5))
    page.mouse.wheel(0, -120)
    page.wait_for_timeout(400)
    assert any("scroll" in m for m in sent(hub)), "the wheel no longer scrolls"
    ok("the wheel still goes as terminal_input.scroll")
    ctx.close()


def local_mode(browser, base, hub, token, shots):
    ctx, page = open_terminal(browser, base, token, mode="local")
    g = Grid(page)
    start = len(mouse(hub))
    new = lambda: mouse(hub)[start:]
    start_clicks = len(clicks(hub))

    # a drag selects on the page, nothing goes to the program
    page.mouse.move(*g.at(2, 3))
    page.mouse.down()
    page.mouse.move(*g.at(14, 3), steps=6)
    page.mouse.up()
    page.wait_for_timeout(300)
    assert new() == [], new()
    assert selection(page) > 0, "a drag did not select in 本地选择"
    ok("本地选择: a plain drag selects on the page and sends nothing")

    # a plain right click is Persona's menu
    page.mouse.move(*g.at(6, 6))
    page.mouse.down(button="right")
    page.mouse.up(button="right")
    expect(page.get_by_role("menuitem", name="全选")).to_be_visible()
    assert new() == [], new()
    page.keyboard.press("Escape")
    ok("本地选择: a plain right click is Persona's menu")

    # a plain click is the older click (as before this feature)
    page.mouse.move(*g.at(9, 9))
    page.mouse.click(*g.at(9, 9))
    page.wait_for_timeout(300)
    assert new() == [] and clicks(hub)[start_clicks:] == [[9, 9]], (new(), clicks(hub)[start_clicks:])
    ok("本地选择: a plain click is still terminal_input.click")

    # Shift sends the pointer to the program, without the shift bit
    page.keyboard.down("Shift")
    page.mouse.move(*g.at(7, 7))
    page.mouse.down()
    page.mouse.move(*g.at(9, 7), steps=3)
    page.mouse.up()
    page.keyboard.up("Shift")
    page.wait_for_timeout(300)
    got = new()
    assert got[0] == {"action": "down", "button": "left", "at": [7, 7], "mods": 0}, got
    assert got[-1]["action"] == "up" and got[-1]["at"] == [9, 7], got
    ok("本地选择: Shift+drag goes to the program, with no shift bit")
    ctx.close()


def watching(browser, base, hub, token):
    ctx, page = open_terminal(browser, base, token)
    page.get_by_role("radio", name="只看").click()
    expect(page.get_by_role("radio", name="只看")).to_have_attribute("aria-checked", "true")
    page.wait_for_timeout(500)
    g = Grid(page)
    start, start_clicks = len(mouse(hub)), len(clicks(hub))
    page.mouse.move(*g.at(10, 5))
    page.mouse.down()
    page.mouse.move(*g.at(14, 5), steps=4)
    page.mouse.up()
    page.mouse.click(*g.at(3, 3), button="right")
    page.wait_for_timeout(300)
    assert mouse(hub)[start:] == [] and clicks(hub)[start_clicks:] == [], mouse(hub)[start:]
    ok("only watching: nothing is sent")
    ctx.close()


def setting(browser, base, token, shots):
    ctx, page = open_terminal(browser, base, token)
    page.get_by_role("button", name="终端设置").click()
    group = page.get_by_role("radiogroup", name="鼠标")
    expect(group.get_by_role("radio", name="交给程序")).to_have_attribute("aria-checked", "true")
    group.get_by_role("radio", name="本地选择").click()
    expect(group.get_by_role("radio", name="本地选择")).to_have_attribute("aria-checked", "true")
    assert page.evaluate("localStorage.getItem('persona.terminal.mouse')") == "local"
    if shots:
        page.screenshot(path=os.path.join(shots, "mouse-setting-light.png"))
    group.get_by_role("radio", name="交给程序").click()
    assert page.evaluate("localStorage.getItem('persona.terminal.mouse')") is None
    ok("终端设置 › 鼠标 switches the mode and remembers it (the default stores nothing)")
    ctx.close()


def dark_shot(browser, base, token, shots):
    ctx, page = open_terminal(browser, base, token, scheme="dark")
    g = Grid(page)
    page.keyboard.down("Shift")
    page.mouse.move(*g.at(2, 3))
    page.mouse.down()
    page.mouse.move(*g.at(20, 3), steps=6)
    page.mouse.up()
    page.keyboard.up("Shift")
    page.wait_for_timeout(300)
    page.screenshot(path=os.path.join(shots, "mouse-shift-selection-dark.png"))
    ctx.close()
    ctx, page = open_terminal(browser, base, token, viewport={"width": 390, "height": 800})
    page.screenshot(path=os.path.join(shots, "mouse-narrow-light.png"))
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
    with sync_playwright() as p:
        exe = os.environ.get("PERSONA_E2E_CHROMIUM")
        browser = p.chromium.launch(executable_path=exe) if exe else p.chromium.launch(channel="chrome")
        try:
            base, hub = a.base.rstrip("/"), a.hub.rstrip("/")
            defaults(browser, base, hub, a.token, a.shots)
            local_mode(browser, base, hub, a.token, a.shots)
            watching(browser, base, hub, a.token)
            setting(browser, base, a.token, a.shots)
            if a.shots:
                dark_shot(browser, base, a.token, a.shots)
        finally:
            browser.close()
    print("PASS web terminal mouse e2e")


if __name__ == "__main__":
    main()
