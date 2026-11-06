#!/usr/bin/env python3
"""End-to-end check of folding the web sidebar (#2524): the button and Ctrl+Alt+B fold it to a rail of icons and back,
a reload keeps the fold, "Pages" / Agents / a state group fold under their titles and stay folded, and a narrow screen
still has the full drawer whatever the wide screen chose.

Needs a running web page against the fake hub and Playwright for Python (it drives the installed Chrome):

    PORT=18797 MOCK_SOURCES=1 python3 scripts/mock/mock_hub.py &
    (cd web && npm run build && PERSONA_HUB_URL=http://127.0.0.1:18797 npx vite preview --port 5297 --strictPort &)
    python3 scripts/test/web_sidebar_collapse_e2e.py http://127.0.0.1:5297 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves expanded / rail / groups-folded
screenshots in light and dark.
"""
import argparse
import os

from playwright.sync_api import expect, sync_playwright

FOLD = 'button[aria-label="收起侧边栏"]'
UNFOLD = 'button[aria-label="展开侧边栏"]'


def ok(msg):
    print("ok  " + msg, flush=True)


def width(page):
    return page.evaluate("document.querySelector('aside').parentElement.getBoundingClientRect().width")


def title(page, text):
    return page.locator("aside button[aria-expanded]", has_text=text).first


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
    print("PASS web sidebar collapse e2e")


def shoot(page, shots, name):
    if not shots:
        return
    for theme in ("light", "dark"):
        page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")
        page.reload()
        page.wait_for_selector(f"{FOLD}, {UNFOLD}")
        page.wait_for_timeout(800)
        page.screenshot(path=os.path.join(shots, f"{name}-{theme}.png"))
    page.evaluate("localStorage.removeItem('persona.theme')")
    page.reload()
    page.wait_for_selector(f"{FOLD}, {UNFOLD}")


def run(browser, base, token, shots):
    ctx = browser.new_context(viewport={"width": 1280, "height": 820}, device_scale_factor=2, locale="zh-CN")
    page = ctx.new_page()
    page.set_default_timeout(10000)
    errors = []
    page.on("pageerror", lambda e: errors.append(str(e)))
    try:
        steps(page, base, token, shots)
    except Exception:
        page.screenshot(path="/tmp/sidebar-collapse-failure.png")
        print("failure screenshot: /tmp/sidebar-collapse-failure.png", flush=True)
        for e in errors[-5:]:
            print("page error: " + e, flush=True)
        raise
    ctx.close()


def steps(page, base, token, shots):
    page.goto(base + "/overview?token=" + token)
    page.wait_for_selector(FOLD)
    full = width(page)
    assert full > 200, full
    ok(f"the sidebar starts open ({full:.0f}px) with its fold button")
    shoot(page, shots, "1-expanded")

    # 1. the button folds it to the rail; a reload keeps it; the rail's button opens it
    page.click(FOLD)
    page.wait_for_selector(UNFOLD)
    assert width(page) < 64, width(page)
    expect(page.locator('aside nav a[aria-label^="总览"]')).to_have_attribute("aria-current", "page")
    expect(page.locator('aside a[aria-label^="收件箱"]')).to_have_count(1)
    ok(f"the button folds it to a {width(page):.0f}px rail of icons, the open page marked")
    page.reload()
    page.wait_for_selector(UNFOLD)
    ok("a reload keeps it folded")
    shoot(page, shots, "2-rail")
    page.locator('aside nav a[aria-label="设置"]').click()
    page.wait_for_function("location.pathname === '/settings'")
    ok("a rail icon opens its page")
    page.click(UNFOLD)
    page.wait_for_selector(FOLD)
    assert width(page) > 200
    ok("the rail's button opens it again")

    # 2. Ctrl+Alt+B (Mod+Alt+B; Mod+B is the right panel's)
    page.locator("main").click(position={"x": 400, "y": 300})
    page.keyboard.press("Control+Alt+KeyB")
    page.wait_for_selector(UNFOLD)
    page.keyboard.press("Control+Alt+KeyB")
    page.wait_for_selector(FOLD)
    ok("Ctrl+Alt+B folds and opens it")

    # 3. the blocks fold under their titles and stay folded
    page.goto(base + "/overview")
    page.wait_for_selector(FOLD)
    page.evaluate("localStorage.setItem('persona.agentGrouping', 'state')")
    page.reload()
    page.wait_for_selector(FOLD)
    pages = title(page, "页面")
    pages.click()
    expect(pages).to_have_attribute("aria-expanded", "false")
    expect(page.locator('aside [inert] a[href="/inbox"]')).to_have_count(1)
    ok("页面 folds under its title")
    groups = page.locator("aside .mt-3 > .group\\/sec > button[aria-expanded]")
    folded_group = None
    for i in range(groups.count()):
        b = groups.nth(i)
        text = b.evaluate("e => e.textContent.trim()")
        if b.get_attribute("aria-expanded") == "true" and text not in ("页面", "Persona 对话"):
            b.click()
            expect(b).to_have_attribute("aria-expanded", "false")
            folded_group = text
            break
    assert folded_group, "no state group to fold"
    ok(f"the Agents group「{folded_group}」folds under its title")
    shoot(page, shots, "3-groups-folded")
    expect(title(page, "页面")).to_have_attribute("aria-expanded", "false")
    agents = title(page, "Agents")
    agents.click()
    expect(agents).to_have_attribute("aria-expanded", "false")
    page.reload()
    page.wait_for_selector(FOLD)
    expect(title(page, "页面")).to_have_attribute("aria-expanded", "false")
    expect(title(page, "Agents")).to_have_attribute("aria-expanded", "false")
    ok("页面 and Agents stay folded after a reload")
    title(page, "Agents").click()
    page.wait_for_timeout(400)
    expect(page.locator("aside button[aria-expanded]", has_text=folded_group).first).to_have_attribute("aria-expanded", "false")
    ok(f"「{folded_group}」is still folded inside")
    page.evaluate("for (const k of Object.keys(localStorage)) if (k.startsWith('fold:')) localStorage.removeItem(k)")

    # 4. narrow: the full drawer, folded or not
    page.click(FOLD)
    page.wait_for_selector(UNFOLD)
    page.set_viewport_size({"width": 800, "height": 820})
    page.reload()
    page.wait_for_selector("aside", state="attached")
    expect(page.locator(UNFOLD)).to_have_count(0)
    expect(page.locator(FOLD)).to_have_count(0)
    page.locator("main button[aria-label='侧边栏']").first.click()
    expect(page.locator('[role="dialog"][aria-label="侧边栏"]')).to_be_visible()
    ok("a narrow screen still opens the full drawer, without the fold button")
    page.evaluate("localStorage.removeItem('persona.sidebarCollapsed'); localStorage.removeItem('persona.agentGrouping')")


if __name__ == "__main__":
    main()
