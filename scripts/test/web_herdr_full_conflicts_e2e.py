#!/usr/bin/env python3
"""End-to-end check of who gets what in the terminal's "Full view" (herdr's own client, #2700, docs/integrations/terminal.md section 26)
against the fake hub's made-up herdr client, which turns on mouse reporting and bracketed paste as the real one does,
opens a pane menu on a right-click report and types a bracketed paste at its prompt.

Checks, one line each: a plain right click is herdr's (the report goes to it, its menu is drawn, Persona's menu and the
browser's are not) also on a Mac, where xterm would select a word under it; Shift + right click is Persona's menu and
sends herdr nothing; Ctrl+B (herdr's prefix) reaches herdr while Persona's other chords still work; the wheel and a
left drag go to herdr as mouse reports, Shift + drag selects locally; a pasted image (Ctrl+V), the key bar's paste, a
dropped file and a Files panel entry dragged on it upload / name the file and type its path as a bracketed paste; text
paste is unchanged; a drop while the client is not live says so.

Needs the fake hub with terminals and the full client (MOCK_TERMINAL_FULL=1), a web page against it and Playwright for
Python (the installed Chrome, or the browser PERSONA_E2E_CHROMIUM points at):

    MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1 MOCK_TERMINAL_FULL=1 PORT=18980 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18980 npx vite --port 18981 --strictPort &)
    python3 scripts/test/web_herdr_full_conflicts_e2e.py --base 18981:18980 [--shots <dir>]

(the page is told which hub it talks to by its own Vite proxy, so the argument is a pair, page port : hub port.)
Prints one line per check and a verdict; exits 1 on the first failure.
"""
import argparse
import json
import os
import time
import urllib.request

from playwright.sync_api import expect, sync_playwright

SHELL = "pane%3Aw1%3Ap2"  # a shell of the "persona" workspace on the fake hub's first machine
HOST = "127.0.0.1"
HUB = {}


def ok(msg):
    print("ok  ", msg, flush=True)


def get(port, path):
    with urllib.request.urlopen(f"http://{HOST}:{HUB[port]}{path}") as r:
        return json.load(r)


def inputs(port, since=0):
    """The data of every herdr_input the fake hub was sent after the first `since` log entries, in order."""
    return [m.get("data", "") for m in get(port, "/mock/herdr_client")["log"][since:] if m.get("type") == "herdr_input"]


def mark(port):
    return len(get(port, "/mock/herdr_client")["log"])


def uploads(port):
    return get(port, "/mock/uploads")["uploads"]


def fresh(browser, viewport, scheme="light", mac=False, touch=False):
    ctx = browser.new_context(
        viewport=viewport,
        locale="zh-CN",  # the labels below are the Chinese ones
        color_scheme=scheme,
        device_scale_factor=2,
        has_touch=touch,
        is_mobile=touch,
        permissions=["clipboard-read", "clipboard-write"],
    )
    init = f"localStorage.setItem('persona.token', 'mock'); localStorage.setItem('persona.theme', {scheme!r});"
    if mac:  # xterm decides its Mac defaults (right click selects a word) from this when it loads
        init += "Object.defineProperty(navigator, 'platform', {get: () => 'MacIntel'}); Object.defineProperty(navigator, 'userAgentData', {get: () => undefined});"
    ctx.add_init_script(init)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    # whether the page said no to the browser's own menu: the last contextmenu event, seen after the page's handlers
    page.add_init_script(
        "window.__ctx = []; addEventListener('contextmenu', (e) => window.__ctx.push(e.defaultPrevented));"
    )
    return ctx, page


def screen_text(page):
    return page.evaluate(
        """() => [...document.querySelectorAll('[data-surface="shown"] .xterm-rows')]
            .filter((el) => !el.closest('.invisible, [inert]'))
            .flatMap((el) => [...el.children].map((r) => r.textContent)).join('\\n')"""
    )


def wait_text(page, needle, what, timeout=10000, absent=False):
    deadline = time.time() + timeout / 1000
    while time.time() < deadline:
        if (needle in screen_text(page)) != absent:
            return
        time.sleep(0.1)
    raise AssertionError(f"{what}: {needle!r} {'still in' if absent else 'not in'}\n{screen_text(page)}")


def wait_inputs(port, since, needle, what, timeout=8):
    deadline = time.time() + timeout
    while time.time() < deadline:
        got = "".join(inputs(port, since))
        if needle in got:
            return got
        time.sleep(0.1)
    raise AssertionError(f"{what}: {needle!r} not in {''.join(inputs(port, since))!r}")


def open_full(page, base):
    page.goto(f"{base}/terminal/server/{SHELL}")
    wait_text(page, "me@host ~/persona $", "the shell pane")
    page.wait_for_timeout(300)  # the client's mouse modes arrive with its first screen


def persona_menu(page):
    return page.get_by_role("menuitem", name="全选", exact=True)


def shot(page, shots, name):
    if shots:
        page.screenshot(path=os.path.join(shots, name))


def right_click(page, x, y, shift=False):
    if shift:
        page.keyboard.down("Shift")
    page.mouse.click(x, y, button="right")
    if shift:
        page.keyboard.up("Shift")


def screen_box(page):
    return page.locator('[data-surface="shown"] .xterm-screen').bounding_box()


def point_in(page, needle, offset=3):
    """The page coordinates of a cell `offset` characters into the first row of the surface that contains `needle`."""
    lines = screen_text(page).split("\n")
    row = next(i for i, l in enumerate(lines) if needle in l)
    col = lines[row].index(needle) + offset
    box = screen_box(page)
    return box["x"] + (col + 0.5) * box["width"] / len(lines[row]), box["y"] + (row + 0.5) * box["height"] / len(lines)


def png_to_clipboard(page):
    """A small PNG on the system clipboard (what a screenshot paste has)."""
    page.evaluate(
        """async () => {
          const c = document.createElement('canvas'); c.width = 8; c.height = 8;
          c.getContext('2d').fillRect(0, 0, 8, 8);
          const blob = await new Promise((r) => c.toBlob(r, 'image/png'));
          await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })]);
        }"""
    )


def drop(page, kind, payload=None):
    """Drags a file (kind 'file') or a Files panel entry (kind 'entry') over the surface and drops it; returns whether the ring showed."""
    return page.evaluate(
        """async ([kind, payload]) => {
          const area = document.querySelector('[data-herdr-full] .xterm').closest('.relative');
          const dt = new DataTransfer();
          if (kind === 'file') dt.items.add(new File([new Uint8Array([1, 2, 3, 4])], 'dropped shot.png', { type: 'image/png' }));
          else dt.setData('application/x-persona-file', JSON.stringify(payload));
          const fire = (type) => area.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: dt }));
          fire('dragenter'); fire('dragover');
          await new Promise((r) => setTimeout(r, 80)); // React draws the ring after the event
          const ring = area.className.includes('ring-accent');
          fire('drop');
          return ring;
        }""",
        [kind, payload],
    )


def checks(browser, port, shots):
    base = f"http://{HOST}:{port}"
    ctx, page = fresh(browser, {"width": 1500, "height": 900})
    open_full(page, base)
    page.locator('[data-surface="shown"] .xterm').click()  # a press and release: herdr gets them as mouse reports
    box = screen_box(page)
    mid_x, mid_y = box["x"] + box["width"] * 0.6, box["y"] + box["height"] * 0.4

    # ── a plain right click is herdr's ──
    n = mark(port)
    right_click(page, mid_x, mid_y)
    got = wait_inputs(port, n, "\x1b[<2;", "the right press, as an SGR report")
    assert "\x1b[<2;" in got and "m" in got, repr(got)
    wait_text(page, "Rename pane", "herdr's own menu drawn by the client")
    assert persona_menu(page).count() == 0, "Persona's menu opened over herdr's"
    assert page.evaluate("window.__ctx.at(-1)") is True, "the browser's own menu was not held off"
    ok("a right click goes to herdr as a mouse report, its menu is drawn, Persona's menu and the browser's are not")
    shot(page, shots, "full-rightclick-herdr-light.png")
    page.keyboard.press("Escape")
    wait_text(page, "Rename pane", "herdr's menu closed by Esc", absent=True)

    # ── Shift + right click is Persona's, herdr hears nothing ──
    n = mark(port)
    right_click(page, mid_x, mid_y, shift=True)
    expect(persona_menu(page)).to_be_visible()
    for name in ("复制", "粘贴", "搜索…"):
        expect(page.get_by_role("menuitem", name=name, exact=True)).to_be_visible()
    page.wait_for_timeout(200)
    assert not [d for d in inputs(port, n) if d.startswith("\x1b[<2;")], inputs(port, n)
    assert "Rename pane" not in screen_text(page)
    ok("Shift + right click opens Persona's menu (复制 / 粘贴 / 全选 / 搜索…) and herdr gets no right-button report")
    shot(page, shots, "full-rightclick-persona-light.png")
    page.keyboard.press("Escape")
    expect(persona_menu(page)).to_have_count(0)

    # ── Ctrl+B is herdr's prefix; the other chords are still Persona's ──
    page.locator('[data-surface="shown"] .xterm').click()
    n = mark(port)
    page.keyboard.press("Control+b")
    wait_inputs(port, n, "\x02", "herdr's prefix (^B) typed to it")
    ok("Ctrl+B reaches herdr (its prefix) and is not Persona's 右侧面板 here")
    n = mark(port)
    page.keyboard.press("Control+k")
    expect(page.get_by_role("dialog")).to_be_visible()
    assert "\x0b" not in "".join(inputs(port, n))
    page.keyboard.press("Escape")
    ok("Ctrl+K still opens Persona's palette, and ^K is not sent")

    # ── the wheel and a drag are herdr's; Shift + drag is the page's ──
    page.locator('[data-surface="shown"] .xterm').click()
    n = mark(port)
    page.mouse.move(mid_x, mid_y)
    page.mouse.wheel(0, -300)
    got = wait_inputs(port, n, "\x1b[<64;", "the wheel as SGR wheel-up reports")
    keys = [d for d in inputs(port, n) if not d.startswith("\x1b[<")]
    assert not keys, f"the wheel also typed keys: {keys!r}"
    ok("the wheel goes to herdr as mouse wheel reports only (no arrow keys, no second handler)")
    n = mark(port)
    page.mouse.move(mid_x, mid_y)
    page.mouse.down()
    page.mouse.move(mid_x + 120, mid_y + 4, steps=6)
    page.mouse.up()
    got = wait_inputs(port, n, "\x1b[<0;", "the left press")
    assert "\x1b[<32;" in got and "\x1b[<0;" in got, repr(got)
    ok("a left drag goes to herdr as press / drag / release (its own selection and OSC 52 copy)")
    n = mark(port)
    page.keyboard.down("Shift")
    page.mouse.move(box["x"] + 10, box["y"] + 4)
    page.mouse.down()
    page.mouse.move(box["x"] + 300, box["y"] + 4, steps=6)
    page.mouse.up()
    page.keyboard.up("Shift")
    page.wait_for_timeout(200)
    held = [d for d in inputs(port, n) if d.startswith("\x1b[<0;") or d.startswith("\x1b[<32;")]
    assert not held, held
    ok("Shift + drag keeps the mouse on the page (xterm's local selection): herdr gets no press or drag")
    page.locator('[data-surface="shown"] .xterm').click()

    # ── text paste is unchanged: Ctrl+Shift+V pastes, Ctrl+V is still the program's ^V ──
    page.evaluate("navigator.clipboard.writeText('plain words')")
    n, before = mark(port), len(uploads(port))
    page.keyboard.press("Control+Shift+v")
    wait_inputs(port, n, "\x1b[200~plain words\x1b[201~", "a text paste, bracketed")
    assert len(uploads(port)) == before
    n = mark(port)
    page.keyboard.press("Control+v")
    wait_inputs(port, n, "\x16", "^V with only text on the clipboard")
    page.wait_for_timeout(300)
    assert "plain words" not in "".join(inputs(port, n)) and len(uploads(port)) == before
    page.keyboard.press("Enter")  # the fake pane's prompt starts empty again
    ok("Ctrl+Shift+V is xterm's own bracketed text paste; Ctrl+V with only text on the clipboard is still ^V; nothing uploaded")

    # ── an image on the clipboard: Ctrl+V uploads it and types its path ──
    png_to_clipboard(page)
    n, before = mark(port), len(uploads(port))
    page.keyboard.press("Control+v")
    got = wait_inputs(port, n, ".png \x1b[201~", "the path typed as a paste")
    up = uploads(port)
    assert len(up) == before + 1 and up[-1]["name"] == "image.png", up
    assert f"\x1b[200~{up[-1]['path']} \x1b[201~" in got, repr(got)
    wait_text(page, "$ /tmp/", "the path at the prompt (the fake pane is narrow: the line is cut)")
    assert "\x16" not in got, repr(got)
    ok(f"Ctrl+V with an image on the clipboard uploads it as {up[-1]['name']} and types its path to herdr as one bracketed paste (no ^V)")
    shot(page, shots, "full-paste-image-light.png")

    # ── the key bar's paste button reads the clipboard for files ──
    page.get_by_role("button", name="显示按键栏").click()
    png_to_clipboard(page)
    n, before = mark(port), len(uploads(port))
    page.get_by_role("button", name="粘贴").click()
    wait_inputs(port, n, ".png \x1b[201~", "the key bar's paste of an image")
    assert len(uploads(port)) == before + 1
    ok("the key bar's 粘贴 uploads an image on the clipboard too")
    page.evaluate("navigator.clipboard.writeText('from the bar')")
    n = mark(port)
    page.get_by_role("button", name="粘贴").click()
    wait_inputs(port, n, "\x1b[200~from the bar\x1b[201~", "the key bar's paste of text")
    ok("and its text paste is unchanged")

    # ── dropping a file, and a Files panel entry ──
    n, before = mark(port), len(uploads(port))
    ring = drop(page, "file")
    assert ring, "the drop ring was not drawn"
    got = wait_inputs(port, n, ".png \x1b[201~", "the dropped file's path")
    up = uploads(port)
    assert len(up) == before + 1 and up[-1]["name"] == "dropped shot.png", up
    ok("a dropped file lights the surface, is uploaded under its own name and its path typed to herdr")
    n = mark(port)
    drop(page, "entry", {"mid": "server", "path": "/home/me/persona/src/a b.ts", "dir": False})
    wait_inputs(port, n, "\x1b[200~@/home/me/persona/src/a\\ b.ts \x1b[201~", "the Files entry as @path")
    ok("a Files panel entry dropped on it types @path (escaped, absolute) and uploads nothing")
    n = mark(port)
    drop(page, "entry", {"mid": "somewhere-else", "path": "/x", "dir": False})
    page.wait_for_timeout(300)
    assert not inputs(port, n), inputs(port, n)
    ok("an entry of another computer is refused: nothing typed")
    ctx.close()

    # ── a Mac: xterm selects a word under a right click, which must not open Persona's menu ──
    ctx, page = fresh(browser, {"width": 1500, "height": 900}, mac=True)
    open_full(page, base)
    page.locator('[data-surface="shown"] .xterm').click()
    n = mark(port)
    assert page.evaluate("navigator.platform") == "MacIntel"
    page.get_by_role("button", name="显示按键栏").click()  # its "Copy" key is enabled while xterm holds a selection
    x, y = point_in(page, "On branch main")  # on a word of the shell pane
    right_click(page, x, y)
    wait_inputs(port, n, "\x1b[<2;", "the right press on a Mac")
    wait_text(page, "Rename pane", "herdr's menu on a Mac")
    assert persona_menu(page).count() == 0, "on a Mac a right click opened Persona's menu"
    page.wait_for_timeout(200)
    expect(page.get_by_role("button", name="复制选中的文字")).to_be_disabled()  # no word of herdr's screen was selected under its menu
    ok("on a Mac a right click still goes to herdr alone (xterm does not select a word under it)")
    # ⌘V: the browser's own paste event, with the files in it
    n, before = mark(port), len(uploads(port))
    page.evaluate(
        """() => {
          const dt = new DataTransfer();
          dt.items.add(new File([new Uint8Array([1, 2, 3])], 'pasted.png', { type: 'image/png' }));
          document.querySelector('[data-herdr-full] .xterm-helper-textarea').dispatchEvent(new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: dt }));
        }"""
    )
    wait_inputs(port, n, "pasted", "the path of the file in a paste event")
    assert len(uploads(port)) == before + 1 and uploads(port)[-1]["name"] == "pasted.png"
    ok("a paste event with a file (⌘V) uploads it and types the path")
    ctx.close()

    # ── 390 px, dark ──
    ctx, page = fresh(browser, {"width": 390, "height": 800}, scheme="dark")
    open_full(page, base)
    box = screen_box(page)
    page.mouse.click(box["x"] + box["width"] * 0.5, box["y"] + 90, button="right")
    wait_text(page, "Rename pane", "herdr's menu on a phone-width screen")
    shot(page, shots, "full-rightclick-herdr-390-dark.png")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True, help="page port:hub port")
    ap.add_argument("--shots")
    a = ap.parse_args()
    page_port, hub_port = (int(x) for x in a.base.split(":"))
    HUB[page_port] = hub_port
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        exe = os.environ.get("PERSONA_E2E_CHROMIUM")
        browser = p.chromium.launch(executable_path=exe) if exe else p.chromium.launch(channel="chrome")
        try:
            checks(browser, page_port, a.shots)
        finally:
            browser.close()
    print("PASS")


if __name__ == "__main__":
    main()
