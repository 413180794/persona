#!/usr/bin/env python3
"""End-to-end check of the web sidebar's folding groups (#1417): "Other agents" (agents outside any space) folds from its
title like every other group, shows what wants a look while folded, and stays folded after a reload; under every
grouping ("By repo" / "By project" / "By status" / "By machine" / "By to-do") each group's title folds it; on a touch screen the fold arrow shows.

Needs a running web page against the fake hub with sources outside any space (MOCK_SOURCES=1) and Playwright for
Python (it drives the installed Chrome):

    MOCK_SOURCES=1 PORT=18797 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18797 npx vite --port 5297 --strictPort &)
    python3 scripts/test/web_sidebar_fold_e2e.py http://127.0.0.1:5297 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves the sidebar in light / dark,
"Other agents" open and folded.
"""
import argparse
import os
import sys

from playwright.sync_api import expect, sync_playwright

HEADERS = "aside .group\\/sec > button"
LOOSE = "其他 Agent"


def ok(msg):
    print("ok  " + msg, flush=True)


def header(page, title):
    return page.locator(HEADERS, has_text=title).first


def section_height(page, title):
    # the group's body: the Collapse grid under the title row (0fr when folded)
    return header(page, title).evaluate("b => b.parentElement.nextElementSibling.getBoundingClientRect().height")


def rows_inert(page, title):
    return header(page, title).evaluate("b => !!b.parentElement.nextElementSibling.querySelector('a[href^=\"/s/\"]').closest('[inert]')")


def toggle(btn):
    # the title's empty end: a "By to-do" group's name is a link to its todo
    box = btn.bounding_box()
    btn.click(position={"x": box["width"] - 3, "y": box["height"] / 2})


def settled(page):
    page.wait_for_timeout(500)  # Collapse's transition is 300 ms


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
    print("PASS web sidebar fold e2e")


def run(browser, base, token, shots):
    ctx = browser.new_context(viewport={"width": 1280, "height": 900}, device_scale_factor=2)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    page.goto(base + "/overview?token=" + token)
    expect(page.locator(HEADERS).first).to_be_visible()  # signed in: the token is exchanged and kept
    page.evaluate("Object.keys(localStorage).filter(k => k.startsWith('fold:')).forEach(k => localStorage.removeItem(k)); localStorage.setItem('persona.agentGrouping', 'space'); localStorage.setItem('persona.theme', 'light')")
    page.reload()

    # 1. "Other agents" is a toggle with a count; with an agent in it that wants a look it starts open.
    loose = header(page, LOOSE)
    expect(loose).to_be_enabled()
    expect(loose).to_have_attribute("aria-expanded", "true")
    count = loose.locator("xpath=..").locator("span.tabular-nums").last
    expect(count).to_have_text("2")
    assert section_height(page, LOOSE) > 20, section_height(page, LOOSE)
    ok("「其他 Agent」 is a toggle, open, with its count")
    if shots:
        page.locator("aside").screenshot(path=os.path.join(shots, "loose-open-light.png"))

    # 2. Its title folds it: the rows go away (inert, no height) and the states that want a look show instead.
    toggle(loose)
    expect(loose).to_have_attribute("aria-expanded", "false")
    settled(page)
    assert section_height(page, LOOSE) < 1, section_height(page, LOOSE)
    assert rows_inert(page, LOOSE)
    dots = loose.locator("xpath=..").locator("[class*=rounded-full]")
    assert dots.count() >= 2, dots.count()
    ok("its title folds it: rows hidden, status dots instead of the count")

    # 3. Folded stays folded after a reload, in both themes; a click opens it again.
    for theme in ("light", "dark"):
        page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")
        page.reload()
        expect(header(page, LOOSE)).to_have_attribute("aria-expanded", "false")
        settled(page)
        assert section_height(page, LOOSE) < 1
        if shots:
            page.locator("aside").screenshot(path=os.path.join(shots, f"loose-folded-{theme}.png"))
    ok("folded after a reload")
    toggle(header(page, LOOSE))
    expect(header(page, LOOSE)).to_have_attribute("aria-expanded", "true")
    settled(page)
    assert section_height(page, LOOSE) > 20
    assert not rows_inert(page, LOOSE)
    if shots:
        page.locator("aside").screenshot(path=os.path.join(shots, "loose-open-dark.png"))
    page.reload()
    expect(header(page, LOOSE)).to_have_attribute("aria-expanded", "true")
    ok("a click opens it again, and open stays open after a reload")
    page.evaluate("localStorage.setItem('persona.theme', 'light')")

    # 4. Under every grouping each group's title folds and opens it.
    for grouping in ("repo", "space", "state", "machine", "todo"):
        page.evaluate(f"localStorage.setItem('persona.agentGrouping', '{grouping}')")
        page.reload()
        heads = page.locator(HEADERS)
        expect(page.locator('aside a[href^="/s/"]').first).to_be_attached()  # the agents are in
        page.wait_for_timeout(500)
        n = heads.count()
        assert n >= 3, (grouping, n)
        names = []
        for i in range(n):
            h = heads.nth(i)
            expect(h).to_be_enabled()
            was = h.get_attribute("aria-expanded")
            assert was in ("true", "false"), (grouping, h.inner_text(), was)
            toggle(h)
            expect(h).not_to_have_attribute("aria-expanded", was)
            toggle(h)
            expect(h).to_have_attribute("aria-expanded", was)
            names.append(h.inner_text().strip().splitlines()[0])
        ok(f"{grouping}: every group folds from its title ({', '.join(names)})")
    page.evaluate("localStorage.setItem('persona.agentGrouping', 'space')")
    ctx.close()

    # 5. On a touch screen (no hover) the fold arrow is shown, not only on hover.
    ctx = browser.new_context(viewport={"width": 390, "height": 844}, device_scale_factor=2, has_touch=True)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    ctx.new_cdp_session(page).send("Emulation.setEmulatedMedia", {"features": [{"name": "hover", "value": "none"}]})
    page.goto(base + "/overview?token=" + token)
    expect(page.locator("main").get_by_role("button", name="侧边栏").first).to_be_visible()
    # "Other agents" is "By project"'s (a new browser groups "By to-do"; under "By machine" the machines' titles fold the same way)
    page.evaluate("localStorage.setItem('persona.agentGrouping', 'space')")
    page.reload()
    page.locator("main").get_by_role("button", name="侧边栏").first.click()
    arrow = header(page, LOOSE).locator("svg").last
    expect(arrow).to_be_visible()
    assert arrow.evaluate("e => getComputedStyle(e).opacity") == "1"
    ok("touch: the fold arrow shows without hover")
    ctx.close()


if __name__ == "__main__":
    try:
        main()
    except Exception as e:  # one line of verdict on any failure
        print(f"FAIL web sidebar fold e2e: {e}", file=sys.stderr)
        sys.exit(1)
