#!/usr/bin/env python3
"""End-to-end check of Settings › Shortcuts (#2586): recording a chord, a conflict's "Replace" / "Cancel", clearing, turning one off, the
switch for all, "Reset", and that the keys follow (they fire or not on the page) and survive a reload.

Needs a running web page against the fake hub and Playwright for Python (it drives the installed Chrome):

    PORT=18797 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18797 npx vite --port 5297 --strictPort &)
    python3 scripts/test/web_shortcuts_e2e.py http://127.0.0.1:5297 [--shots <dir>]

Runs on Linux / Windows (Mod is Ctrl). Prints one line per check and a verdict; exits 1 on the first failure.
--shots saves light / dark screenshots of the page.
"""
import argparse
import os
import sys

from playwright.sync_api import expect, sync_playwright

JUMP = "跳转到会话或终端窗格"
TERMINAL_CHAT = "终端 ⇄ 对话"
PALETTE = "[data-shortcut-layer]"


def ok(msg):
    print("ok  " + msg, flush=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    a = ap.parse_args()
    base = a.base.rstrip("/")
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel="chrome")
        try:
            run(browser, base, a.token, a.shots)
        finally:
            browser.close()
    print("PASS web shortcuts e2e")


def run(browser, base, token, shots):
    ctx = browser.new_context(viewport={"width": 1280, "height": 900}, device_scale_factor=2, locale="zh-CN")
    page = ctx.new_page()
    page.set_default_timeout(10000)
    page.goto(base + "/?token=" + token)
    page.wait_for_selector("aside")
    page.goto(base + "/settings?tab=shortcuts")
    page.get_by_text("启用快捷键").first.wait_for()

    def chord_button(label):
        return page.locator(f'button[aria-label^="{label}："]')

    def palette_opens(keys):
        """Presses `keys` on the page and says whether the palette opened (closing it again)."""
        page.locator("main h1, main").first.click(position={"x": 5, "y": 5})
        page.keyboard.press(keys)
        try:
            page.wait_for_selector(PALETTE, timeout=1200)
        except Exception:
            return False
        page.keyboard.press("Escape")
        expect(page.locator(PALETTE)).to_have_count(0)
        return True

    assert palette_opens("Control+k"), "Ctrl+K should open the palette"
    ok("Ctrl+K opens the palette by default")

    # Recording: Esc cancels, a new chord is taken.
    chord_button(JUMP).click()
    expect(chord_button(JUMP)).to_have_attribute("aria-label", f"{JUMP}：按下新的快捷键，Esc 取消")
    page.keyboard.press("Escape")
    expect(chord_button(JUMP)).to_have_attribute("aria-label", f"{JUMP}：Ctrl K，点击改键")
    ok("Esc cancels a recording")
    chord_button(JUMP).click()
    page.keyboard.press("Control+Shift+o")
    expect(chord_button(JUMP)).to_have_attribute("aria-label", f"{JUMP}：Ctrl Shift O，点击改键")
    assert palette_opens("Control+Shift+o"), "the new chord should open the palette"
    assert not palette_opens("Control+k"), "the old chord should not"
    ok("a recorded chord fires, the old one does not")

    # A chord another command has: "Cancel" keeps both, "Replace" takes it and clears the other.
    chord_button(JUMP).click()
    page.keyboard.press("Control+j")
    page.get_by_text(f"Ctrl+J 已被「{TERMINAL_CHAT}」占用").wait_for()
    page.get_by_role("button", name="取消", exact=True).click()
    expect(chord_button(TERMINAL_CHAT)).to_have_attribute("aria-label", f"{TERMINAL_CHAT}：Ctrl J，点击改键")
    ok("a conflict's 取消 leaves both commands as they were")
    chord_button(JUMP).click()
    page.keyboard.press("Control+j")
    if shots:
        page.screenshot(path=os.path.join(shots, "shortcuts-conflict-light.png"))
    page.get_by_role("button", name="替换", exact=True).click()
    expect(chord_button(JUMP)).to_have_attribute("aria-label", f"{JUMP}：Ctrl J，点击改键")
    expect(chord_button(TERMINAL_CHAT)).to_have_attribute("aria-label", f"{TERMINAL_CHAT}：未设置，点击设置快捷键")
    assert palette_opens("Control+j"), "Ctrl+J should now open the palette"
    ok("替换 takes the chord and clears the command that had it")

    # Clearing: the key does nothing, no default either.
    page.get_by_role("button", name=f"清除「{JUMP}」的快捷键").click()
    expect(chord_button(JUMP)).to_have_attribute("aria-label", f"{JUMP}：未设置，点击设置快捷键")
    assert not palette_opens("Control+j"), "a cleared chord should do nothing"
    assert not palette_opens("Control+k"), "a cleared command should not fall back to its default"
    ok("a cleared command does not fire, and does not fall back to its default")

    # "Reset" for the row.
    page.get_by_role("button", name=f"恢复「{JUMP}」的默认快捷键").click()
    expect(chord_button(JUMP)).to_have_attribute("aria-label", f"{JUMP}：Ctrl K，点击改键")
    assert palette_opens("Control+k")
    ok("恢复默认 brings the default chord back")

    # Turning one off keeps its chord.
    page.get_by_role("switch", name=f"启用「{JUMP}」").click()
    expect(page.get_by_role("switch", name=f"启用「{JUMP}」")).to_have_attribute("aria-checked", "false")
    expect(chord_button(JUMP)).to_have_attribute("aria-label", f"{JUMP}：Ctrl K，点击改键")
    assert not palette_opens("Control+k"), "a command turned off should not fire"
    assert palette_opens("Control+Shift+p"), "the others still work"
    ok("a command turned off keeps its chord and does not fire; the others do")

    # It survives a reload.
    page.reload()
    page.get_by_text("启用快捷键").first.wait_for()
    expect(page.get_by_role("switch", name=f"启用「{JUMP}」")).to_have_attribute("aria-checked", "false")
    expect(chord_button(TERMINAL_CHAT)).to_have_attribute("aria-label", f"{TERMINAL_CHAT}：未设置，点击设置快捷键")
    ok("the settings survive a reload (localStorage)")
    page.get_by_role("switch", name=f"启用「{JUMP}」").click()
    assert palette_opens("Control+k")

    # The switch for all.
    page.get_by_role("switch", name="启用快捷键").click()
    page.get_by_text("快捷键已全部停用").wait_for()
    assert not palette_opens("Control+k") and not palette_opens("Control+Shift+p"), "nothing should fire with the switch off"
    if shots:
        page.screenshot(path=os.path.join(shots, "shortcuts-all-off-light.png"), full_page=True)
    page.get_by_role("switch", name="启用快捷键").click()
    assert palette_opens("Control+k")
    ok("the switch for all turns every shortcut off and back on")

    # Typing in a text field: a bare key is never a shortcut.
    page.goto(base + "/")
    box = page.locator("main textarea").first
    box.click()
    box.type("k/j,b")
    expect(page.locator(PALETTE)).to_have_count(0)
    assert box.input_value().endswith("k/j,b"), box.input_value()
    ok("typing in the input box triggers nothing")
    box.fill("")

    # "Reset all".
    page.goto(base + "/settings?tab=shortcuts")
    page.get_by_role("button", name="全部恢复默认").click()
    expect(chord_button(TERMINAL_CHAT)).to_have_attribute("aria-label", f"{TERMINAL_CHAT}：Ctrl J，点击改键")
    expect(page.get_by_role("button", name="全部恢复默认")).to_be_disabled()
    assert page.evaluate("localStorage.getItem('persona.shortcuts')") is None
    ok("全部恢复默认 puts everything back and forgets the stored settings")

    if shots:
        chord_button(JUMP).click()
        page.keyboard.press("Control+Shift+o")
        page.get_by_role("switch", name="启用「快捷键帮助」").click()
        page.get_by_role("button", name="清除「打开设置」的快捷键").click()
        page.screenshot(path=os.path.join(shots, "shortcuts-light.png"), full_page=True)
        page.emulate_media(color_scheme="dark")
        page.screenshot(path=os.path.join(shots, "shortcuts-dark.png"), full_page=True)
        page.get_by_role("button", name="全部恢复默认").click()


if __name__ == "__main__":
    try:
        main()
    except AssertionError as e:
        print("FAIL " + str(e), flush=True)
        sys.exit(1)
