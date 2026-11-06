#!/usr/bin/env python3
"""Live check of the "Full view" conflicts (#2700) against a REAL herdr client: the fake hub runs `herdr session attach <name>`
in a PTY (MOCK_TERMINAL_FULL_EXEC) and relays its bytes the way the node does. A plain right click must open herdr's own
pane menu and not Persona's; Shift + right click must open Persona's and not herdr's; a screenshot pasted with Ctrl+V must
arrive in the focused pane as its path (herdr passes the bracketed paste on to the shell).

Only ever use a throwaway session you made yourself (`herdr --session persona-conflict-test`, then detach): the check
types a pasted path at the prompt of that session's focused pane. Stop and delete the session afterwards
(`herdr session stop <name>; herdr session delete <name>`).

    MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1 MOCK_TERMINAL_FULL=1 \\
        MOCK_TERMINAL_FULL_EXEC="herdr session attach persona-conflict-test" PORT=18982 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18982 npx vite --port 18983 --strictPort &)
    python3 scripts/test/web_herdr_full_conflicts_live.py --base http://127.0.0.1:18983 --session persona-conflict-test [--shots <dir>]
"""
import argparse
import json
import os
import subprocess
import time

from playwright.sync_api import expect, sync_playwright

SHELL = "pane%3Aw1%3Ap2"


def ok(msg):
    print("ok  ", msg, flush=True)


def herdr_env(session):
    out = subprocess.run(["herdr", "session", "list", "--json"], capture_output=True, text=True, check=True).stdout
    sock = next(s["socket_path"] for s in json.loads(out)["sessions"] if s["name"] == session)
    env = {k: v for k, v in os.environ.items() if not k.startswith("HERDR_")}
    env["HERDR_SOCKET_PATH"] = sock
    return env


def screen(page):
    return "\n".join(page.evaluate("""() => [...document.querySelectorAll('[data-surface="shown"] .xterm-rows > div')].map(r => r.textContent)"""))


def wait_screen(page, needle, what, absent=False, timeout=10):
    end = time.time() + timeout
    while time.time() < end:
        if (needle in screen(page)) != absent:
            return
        time.sleep(0.15)
    raise AssertionError(f"{what}: {needle!r} {'still on' if absent else 'not on'} the screen:\n{screen(page)}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True)
    ap.add_argument("--session", required=True)
    ap.add_argument("--shots")
    a = ap.parse_args()
    if a.session == "default":
        raise SystemExit("never the session you work in")
    env = herdr_env(a.session)
    panes = json.loads(subprocess.run(["herdr", "pane", "list"], env=env, capture_output=True, text=True, check=True).stdout)["result"]["panes"]
    pane = next((x for x in panes if x.get("focused")), panes[0])["pane_id"]

    with sync_playwright() as p:
        exe = os.environ.get("PERSONA_E2E_CHROMIUM")
        browser = p.chromium.launch(executable_path=exe) if exe else p.chromium.launch(channel="chrome")
        ctx = browser.new_context(viewport={"width": 1500, "height": 900}, locale="zh-CN", device_scale_factor=2, permissions=["clipboard-read", "clipboard-write"])
        ctx.add_init_script("localStorage.setItem('persona.token', 'mock'); window.__ctx = []; addEventListener('contextmenu', (e) => window.__ctx.push(e.defaultPrevented));")
        page = ctx.new_page()
        page.set_default_timeout(15000)
        page.goto(f"{a.base}/terminal/server/{SHELL}")
        wait_screen(page, "spaces", "herdr's sidebar")
        page.wait_for_timeout(800)
        ok("the real herdr client is drawn in the page")
        xterm = page.locator('[data-surface="shown"] .xterm-screen')
        box = xterm.bounding_box()
        x, y = box["x"] + box["width"] * 0.65, box["y"] + box["height"] * 0.3

        # a plain right click: herdr's pane menu
        page.mouse.click(x, y, button="right")
        wait_screen(page, "Rename pane", "herdr's pane menu")
        assert page.get_by_role("menuitem", name="全选", exact=True).count() == 0, "Persona's menu opened too"
        assert page.evaluate("window.__ctx.at(-1)") is True
        ok("a right click opens herdr's own pane menu (Rename pane / Split… / Close pane); Persona's menu and the browser's stay shut")
        if a.shots:
            page.screenshot(path=os.path.join(a.shots, "live-rightclick-herdr.png"))
        page.keyboard.press("Escape")
        wait_screen(page, "Rename pane", "herdr's menu closed", absent=True)

        # Shift + right click: Persona's menu, not herdr's
        page.keyboard.down("Shift")
        page.mouse.click(x, y, button="right")
        page.keyboard.up("Shift")
        expect(page.get_by_role("menuitem", name="全选", exact=True)).to_be_visible()
        page.wait_for_timeout(300)
        assert "Rename pane" not in screen(page), "herdr's menu opened under Persona's"
        ok("Shift + right click opens Persona's menu and herdr's does not open")
        if a.shots:
            page.screenshot(path=os.path.join(a.shots, "live-rightclick-persona.png"))
        page.keyboard.press("Escape")

        # a screenshot pasted with Ctrl+V: its path arrives in the focused pane
        page.locator('[data-surface="shown"] .xterm').click(position={"x": box["x"] + 5, "y": box["y"] + box["height"] * 0.8})
        page.evaluate(
            """async () => {
              const c = document.createElement('canvas'); c.width = 8; c.height = 8; c.getContext('2d').fillRect(0, 0, 8, 8);
              const blob = await new Promise((r) => c.toBlob(r, 'image/png'));
              await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })]);
            }"""
        )
        page.keyboard.press("Control+v")
        end = time.time() + 15
        seen = ""
        while time.time() < end:
            seen = subprocess.run(["herdr", "pane", "read", pane], env=env, capture_output=True, text=True).stdout
            if "/tmp/image-" in seen:
                break
            time.sleep(0.3)
        assert "/tmp/image-" in seen and ".png" in seen, f"the path did not reach the pane:\n{seen}"
        assert "\x16" not in seen
        ok("a pasted screenshot is uploaded and its path is typed into the real herdr pane (bracketed paste passed on to the shell)")
        if a.shots:
            page.screenshot(path=os.path.join(a.shots, "live-paste-image.png"))
        browser.close()
    print("PASS")


if __name__ == "__main__":
    main()
