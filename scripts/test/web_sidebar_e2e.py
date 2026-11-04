#!/usr/bin/env python3
"""End-to-end check of the web sidebar's entries (#1239): Persona opens the main conversation, the one-off new
conversation and 新建 Agent are the「+」of their sections, and the old links still land where they should.

Needs a running web page against the fake hub and Playwright for Python (it drives the installed Chrome):

    PORT=18791 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18791 npx vite --port 5291 --strictPort &)
    python3 scripts/test/web_sidebar_e2e.py http://127.0.0.1:5291 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves light / dark / narrow screenshots.
"""
import argparse
import json
import os
import sys
import urllib.request

from playwright.sync_api import expect, sync_playwright

PERSONA = 'a[title="Persona 的主对话"]'
STARTER = "有什么要我做的？"


def ok(msg):
    print("ok  " + msg, flush=True)


def path_of(page):
    return page.evaluate("location.pathname")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    a = ap.parse_args()
    base = a.base.rstrip("/")
    req = urllib.request.Request(base + "/api/v1/assistant", headers={"Authorization": "Bearer " + a.token})
    assistant = json.load(urllib.request.urlopen(req, timeout=10))
    main_id = assistant["main"]
    other = next(c for c in assistant["conversations"] if c["session_id"] != main_id)
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)

    with sync_playwright() as p:
        browser = p.chromium.launch(channel="chrome")
        try:
            run(browser, base, a.token, main_id, other, a.shots)
        finally:
            browser.close()
    print("PASS web sidebar e2e")


def run(browser, base, token, main_id, other, shots):
    ctx = browser.new_context(viewport={"width": 1280, "height": 820}, device_scale_factor=2)
    page = ctx.new_page()
    page.set_default_timeout(10000)

    # 1. The page opens on Persona's main conversation.
    page.goto(base + "/?token=" + token)
    persona = page.locator(PERSONA)
    expect(persona).to_have_attribute("aria-current", "page")
    expect(page.get_by_text(STARTER)).to_have_count(0)
    assert path_of(page) == "/", path_of(page)
    ok("/ opens the main conversation, Persona selected")

    # Persona is the first entry; no accent 新建 Agent above the navigation.
    first = page.locator("aside nav, aside").first.locator("a").first
    assert first.get_attribute("title") == "Persona 的主对话", first.get_attribute("title")
    ok("Persona is the sidebar's first entry")

    # 2. 总览 and back to Persona.
    page.locator('aside a[href="/overview"]').click()
    page.wait_for_function("location.pathname === '/overview'")
    expect(persona).not_to_have_attribute("aria-current", "page")
    persona.click()
    page.wait_for_function("location.pathname === '/'")
    expect(persona).to_have_attribute("aria-current", "page")
    ok("总览 is /overview; clicking Persona goes back to the main conversation")

    # 3. The main conversation is not listed twice.
    expect(page.locator(f'aside a[href="/j/{main_id}"]')).to_have_count(0)
    expect(page.locator("aside").get_by_text("主对话", exact=True)).to_have_count(0)
    ok("the main conversation has one entry")

    if shots:
        for theme in ("light", "dark"):
            page.evaluate(f"localStorage.setItem('jarvis.theme', '{theme}')")
            page.reload()
            expect(persona).to_have_attribute("aria-current", "page")
            page.wait_for_timeout(800)
            page.screenshot(path=os.path.join(shots, f"wide-persona-{theme}.png"))
        page.evaluate("localStorage.setItem('jarvis.theme', 'light')")
        page.reload()

    # 4. A new one-off conversation is the「+」of「Persona 对话」, without a 主对话 button on it.
    page.locator('aside a[aria-label="新对话"]').click()
    page.wait_for_function("location.pathname === '/j/new'")
    expect(page.get_by_text(STARTER)).to_be_visible()
    expect(page.locator("main").get_by_role("button", name="主对话")).to_have_count(0)
    expect(persona).not_to_have_attribute("aria-current", "page")
    ok("「Persona 对话」+ opens a new conversation at /j/new, no 主对话 button")

    # 5. 新建 Agent is the「+」of Agents and still opens the dialog.
    page.locator('aside button[aria-label="新建 Agent"]').click()
    expect(page.get_by_role("dialog").get_by_text("新建 Agent").first).to_be_visible()
    if shots:
        page.wait_for_timeout(400)
        page.screenshot(path=os.path.join(shots, "wide-new-agent.png"))
    page.keyboard.press("Escape")
    expect(page.get_by_role("dialog")).to_have_count(0)
    ok("Agents + opens 新建 Agent")

    # 6. Old links.
    for old in ("/j", "/persona", f"/j/{main_id}"):
        page.goto(base + old)
        expect(persona).to_have_attribute("aria-current", "page")
        expect(page.get_by_text(STARTER)).to_have_count(0)
        ok(f"{old} opens the main conversation")
    page.goto(base + "/j/" + other["session_id"])
    expect(page.locator(f'aside a[href="/j/{other["session_id"]}"]')).to_have_attribute("aria-current", "page")
    expect(persona).not_to_have_attribute("aria-current", "page")
    ok("/j/<id> opens that conversation with its row selected")
    for old, key in (("/history", "/history"), ("/inbox", "/inbox"), ("/settings", "/settings")):
        page.goto(base + old)
        expect(page.locator(f'aside a[href="{key}"]')).to_have_attribute("aria-current", "page")
    ok("/history, /inbox, /settings still open")
    ctx.close()

    # 7. Narrow: the drawer has the same order, Persona closes it on the main conversation.
    for theme in ("light", "dark"):
        ctx = browser.new_context(viewport={"width": 390, "height": 844}, device_scale_factor=2, has_touch=True)
        page = ctx.new_page()
        page.set_default_timeout(10000)
        page.goto(base + "/overview?token=" + token)
        page.evaluate(f"localStorage.setItem('jarvis.theme', '{theme}')")
        page.reload()
        page.locator("main").get_by_role("button", name="侧边栏").first.click()
        persona = page.locator(PERSONA)
        expect(persona).to_be_in_viewport()
        if shots:
            page.wait_for_timeout(500)
            page.screenshot(path=os.path.join(shots, f"narrow-drawer-{theme}.png"))
        persona.click()
        page.wait_for_function("location.pathname === '/'")
        expect(persona).not_to_be_in_viewport()
        if shots:
            page.wait_for_timeout(800)
            page.screenshot(path=os.path.join(shots, f"narrow-persona-{theme}.png"))
        ctx.close()
    ok("narrow: the drawer leads with Persona, which opens the main conversation and closes the drawer")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:  # one line of verdict on any failure
        print(f"FAIL web sidebar e2e: {e}", file=sys.stderr)
        sys.exit(1)
