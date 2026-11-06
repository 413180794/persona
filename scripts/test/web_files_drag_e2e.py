#!/usr/bin/env python3
"""End-to-end check of dragging an entry of the Files panel into a terminal or the chat box (#2333): a file row becomes
`@relative/path ` typed into the pane, a folder `@dir/ `, a file outside the pane's folder its absolute path, nothing
goes in while only watching (a toast says why), and nothing is uploaded; the same drag into the chat box types
`@path ` at the caret.

Needs the fake hub with terminals (and panes), a running web page against it, and Playwright for Python (the installed
Chrome, or the browser PERSONA_E2E_CHROMIUM points at):

    MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1 PORT=18850 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18850 npx vite --port 18851 --strictPort &)
    python3 scripts/test/web_files_drag_e2e.py http://127.0.0.1:18851 --hub http://127.0.0.1:18850 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves light / dark / narrow screenshots.
"""
import argparse
import json
import os
import urllib.request

from playwright.sync_api import expect, sync_playwright

# The fake hub's shells sit in folders its file tree does not have; the page is told the weather project's folder.
WEATHER = "/home/me/code/weather"
SHELL = "pane%3Aw1%3Ap2"  # a shell of the "persona" workspace, rewritten to WEATHER
AGENT = ("macbook", "a1f3c2d4-0000-4000-8000-000000000001")  # a chat whose folder is /Users/me/code/weather


def ok(msg):
    print("ok  ", msg, flush=True)


def typed(hub):
    """What the fake hub was sent as terminal input, text only."""
    with urllib.request.urlopen(hub + "/mock/terminal_input") as r:
        return [m.get("text", "") for m in json.load(r)["input"]]


def mentions(hub):
    return [t for t in typed(hub) if "@" in t]


def reroute(page):
    """The made-up shells report WEATHER as their folder, so the Files panel has something to list."""

    def rewrite(route):
        res = route.fetch()
        route.fulfill(response=res, body=res.text().replace('"cwd": "/home/me/persona"', f'"cwd": "{WEATHER}"'))

    page.route("**/api/v1/machines/*/panes", rewrite)


def row(page, name):
    return page.locator("aside[aria-label=文件] [draggable=true]", has=page.get_by_text(name, exact=True))


def screen(page):
    # the terminal on show (the ones kept open under it are invisible)
    return page.locator('section[aria-label="终端"] > div:not(.invisible) .xterm-screen').first


def fresh(browser, token, viewport, scheme="light"):
    ctx = browser.new_context(viewport=viewport, color_scheme=scheme, device_scale_factor=2, locale="zh-CN")
    ctx.add_init_script(f"localStorage.setItem('persona.token', {token!r}); localStorage.setItem('persona.theme', {scheme!r})")
    page = ctx.new_page()
    page.set_default_timeout(10000)
    return ctx, page


def driving(page):
    expect(page.get_by_role("radio", name="接管（可打字）")).to_have_attribute("aria-checked", "true")


def open_pane(page, mid, pane):
    """Another terminal from the list: the page stays, so the Files panel stays too."""
    page.locator(f'section[aria-label="窗格"] a[href="/terminal/{mid}/{pane}"]').click()
    driving(page)


def terminal_checks(browser, base, hub, token, shots):
    ctx, page = fresh(browser, token, {"width": 1800, "height": 900})
    reroute(page)
    page.goto(f"{base}/terminal/server/{SHELL}")
    driving(page)
    page.get_by_role("button", name="文件", exact=True).click()
    expect(row(page, "README.md")).to_be_visible()
    ok("the terminal page opens the Files panel for the pane's folder")

    start = len(mentions(hub))
    new = lambda: mentions(hub)[start:]

    # a file inside the pane's folder: relative
    row(page, "README.md").drag_to(screen(page))
    page.wait_for_timeout(300)
    assert new() == ["@README.md "], new()
    ok("a file row dropped on the terminal types '@README.md '")

    # a folder: a trailing slash
    row(page, "Sources").drag_to(screen(page))
    page.wait_for_timeout(300)
    assert new() == ["@README.md ", "@Sources/ "], new()
    ok("a folder row types '@Sources/ '")

    # a file two folders down: the path down from the pane's folder
    row(page, "Sources").click()
    row(page, "App").click()
    expect(row(page, "main.swift")).to_be_visible()
    row(page, "main.swift").drag_to(screen(page))
    page.wait_for_timeout(300)
    assert new()[-1] == "@Sources/App/main.swift ", new()
    ok("a file two folders down types '@Sources/App/main.swift '")
    if shots:
        page.screenshot(path=os.path.join(shots, "terminal-wide-light.png"))

    # a pane of another folder: the file is named by its absolute path
    open_pane(page, "server", "pane%3Aw2%3Ap1")
    n = len(new())
    row(page, "main.swift").drag_to(screen(page))
    page.wait_for_timeout(300)
    assert new()[n:] == [f"@{WEATHER}/Sources/App/main.swift "], new()
    ok("a file outside the pane's folder types its absolute path")

    # a file of another computer is not named in this terminal
    open_pane(page, "macbook", "pane%3Aw2%3Ap1")
    n = len(new())
    row(page, "main.swift").drag_to(screen(page))
    expect(page.get_by_text("这个文件在另一台电脑上，不能拖进这个终端")).to_be_visible()
    assert len(new()) == n, new()
    ok("a file of another computer types nothing and a toast says why")
    if shots:
        page.screenshot(path=os.path.join(shots, "terminal-wide-toast-light.png"))

    # watching: nothing goes in
    open_pane(page, "server", "pane%3Aw1%3Ap2")
    page.get_by_role("radio", name="只看").click()
    expect(page.get_by_role("radio", name="只看")).to_have_attribute("aria-checked", "true")
    n = len(new())
    row(page, "main.swift").drag_to(screen(page))
    expect(page.get_by_text("接管终端后才能粘贴文件")).to_be_visible()
    assert len(new()) == n, new()
    ok("while only watching nothing is typed and the 接管 toast shows")

    # no upload happened anywhere
    uploads = page.evaluate("() => performance.getEntriesByType('resource').filter(e => e.name.includes('/files?dest=terminal')).length")
    assert uploads == 0, uploads
    ok("no file was uploaded")
    ctx.close()

    # a drag of real files from the desktop still uploads (the old path is untouched)
    ctx, page = fresh(browser, token, {"width": 1800, "height": 900})
    page.goto(f"{base}/terminal/server/{SHELL}")
    driving(page)
    sent = []
    page.on("request", lambda r: sent.append(r.url) if "dest=terminal" in r.url else None)
    page.evaluate(
        """() => {
          const dt = new DataTransfer();
          dt.items.add(new File(['x'], 'note.txt', { type: 'text/plain' }));
          const el = document.querySelector('section[aria-label="终端"] .xterm-screen');
          for (const type of ['dragenter', 'dragover', 'drop']) el.dispatchEvent(new DragEvent(type, { dataTransfer: dt, bubbles: true, cancelable: true }));
        }"""
    )
    page.wait_for_timeout(800)
    assert sent, "a dropped OS file should still be uploaded"
    ok("a file dragged in from the desktop still goes the upload way")
    ctx.close()


def chat_checks(browser, base, token, shots, scheme="light"):
    mid, sid = AGENT
    ctx, page = fresh(browser, token, {"width": 1800, "height": 900}, scheme)
    page.goto(f"{base}/s/{mid}/{sid}")
    box = page.locator("textarea").first
    expect(box).to_be_visible()
    page.get_by_role("button", name="文件", exact=True).click()
    expect(row(page, "README.md")).to_be_visible()
    row(page, "README.md").drag_to(box)
    expect(box).to_have_value("@README.md ")
    ok("a file row dropped on the chat box types '@README.md '")
    box.fill("看看")
    box.press("End")
    row(page, "Sources").drag_to(box)
    expect(box).to_have_value("看看 @Sources/ ")
    ok("a folder row lands after the typed text, a space between")
    if shots:
        page.screenshot(path=os.path.join(shots, f"chat-wide-{scheme}.png"))
    ctx.close()


def narrow_shot(browser, base, token, shots, scheme):
    ctx, page = fresh(browser, token, {"width": 390, "height": 800}, scheme)
    page.goto(f"{base}/terminal/server/{SHELL}")
    driving(page)
    expect(page.get_by_role("button", name="文件", exact=True)).to_be_hidden()  # no pointer to drag with: no Files button
    page.wait_for_timeout(500)
    page.screenshot(path=os.path.join(shots, f"terminal-narrow-{scheme}.png"))
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
            terminal_checks(browser, base, hub, a.token, a.shots)
            chat_checks(browser, base, a.token, a.shots)
            if a.shots:
                chat_checks(browser, base, a.token, a.shots, "dark")
                for scheme in ("light", "dark"):
                    narrow_shot(browser, base, a.token, a.shots, scheme)
        finally:
            browser.close()
    print("PASS web files drag e2e")


if __name__ == "__main__":
    main()
