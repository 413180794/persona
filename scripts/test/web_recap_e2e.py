#!/usr/bin/env python3
"""End-to-end check of the away recap in the web chat (#1360): shown open, it is the chat's last line, in the flow with
the agent's words. At the end it is whole between the reply and the composer, covering neither; scrolled up, it goes
with the messages and comes back with them; closing it folds it to its chip in place, and it stays closed on reload.

Needs a running web page against the fake hub (MOCK_RECAP_LONG=1: the review chat, which ends with a recap, is taller
than a screen) and Playwright for Python (it drives the installed Chrome):

    MOCK_RECAP_LONG=1 PORT=18791 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18791 npx vite --port 5291 --strictPort &)
    python3 scripts/test/web_recap_e2e.py http://127.0.0.1:5291 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. Runs at desktop and phone widths, light and dark;
--shots saves each state (open / folded, at the end / scrolled up) there.
"""
import argparse
import os
import sys

from playwright.sync_api import expect, sync_playwright

REVIEW = "/s/macbook/a1f3c2d4-0000-4000-8000-000000000006"
REPLY = "PR #42 看完了"
SIZES = {"wide": {"width": 1280, "height": 820}, "narrow": {"width": 390, "height": 844}}

# The chat's scroll container (the nearest scrolling ancestor of the open recap or its chip), the boxes that matter in
# viewport coordinates, and whether the recap / chip sits inside the scroller.
GEOMETRY = """(sel) => {
  const el = document.querySelector(sel);
  if (!el) return null;
  let sc = el.parentElement;
  while (sc && !(sc.scrollHeight > sc.clientHeight && /auto|scroll/.test(getComputedStyle(sc).overflowY))) sc = sc.parentElement;
  const reply = [...document.querySelectorAll("main p, main div")].filter((n) => n.textContent.includes("%s") && !n.querySelector("p, div"))[0];
  const composer = document.querySelector("main textarea");
  const box = (n) => { if (!n) return null; const r = n.getBoundingClientRect(); return { top: r.top, bottom: r.bottom }; };
  return { inScroller: !!sc && sc.contains(el), el: box(el), scroller: box(sc), reply: box(reply), composer: box(composer),
           gap: sc ? sc.scrollHeight - sc.scrollTop - sc.clientHeight : -1 };
}""" % REPLY

SCROLL = """([sel, top]) => {
  let sc = document.querySelector(sel).parentElement;
  while (sc && !(sc.scrollHeight > sc.clientHeight && /auto|scroll/.test(getComputedStyle(sc).overflowY))) sc = sc.parentElement;
  sc.dispatchEvent(new WheelEvent("wheel", { deltaY: top ? -1 : 1 }));
  sc.scrollTop = top ? 0 : sc.scrollHeight;
  sc.dispatchEvent(new Event("scroll"));
}"""

OPEN = "[data-open-recap]"
CHIP = 'button[aria-label="离开期间回顾，点击查看"]'


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def whole_at_end(page, sel, what, tag):
    g = page.evaluate(GEOMETRY, sel)
    if not g or not g["inScroller"]:
        fail(f"{tag}: {what} is not in the chat's scroll area: {g}")
    if g["el"]["top"] < g["scroller"]["top"] - 1 or g["el"]["bottom"] > g["scroller"]["bottom"] + 1:
        fail(f"{tag}: {what} is not whole on screen at the end: {g}")
    if g["reply"] and g["reply"]["bottom"] > g["el"]["top"] + 1:
        fail(f"{tag}: {what} covers the reply: {g}")
    if g["composer"] and g["el"]["bottom"] > g["composer"]["top"]:
        fail(f"{tag}: {what} runs under the composer: {g}")
    if g["gap"] > 2:
        fail(f"{tag}: not at the end ({g['gap']}px left below)")


def off_screen(page, sel, what, tag):
    g = page.evaluate(GEOMETRY, sel)
    if g["el"]["top"] < g["scroller"]["bottom"]:
        fail(f"{tag}: {what} stayed on screen scrolled up: {g}")


def run(browser, base, token, size, theme, shots):
    tag = f"{size}/{theme}"
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2, has_touch=size == "narrow")
    page = ctx.new_page()
    page.set_default_timeout(15000)
    page.goto(base + "/overview?token=" + token)
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}'); localStorage.removeItem('persona.closedRecaps')")
    page.goto(base + REVIEW)

    def shot(name):
        if shots:
            page.wait_for_timeout(300)
            page.screenshot(path=os.path.join(shots, f"recap-{size}-{theme}-{name}.png"))

    expect(page.locator(OPEN)).to_be_visible()
    page.wait_for_timeout(800)  # the list settles at the end
    whole_at_end(page, OPEN, "the open recap", tag)
    ok(f"{tag}: entering, the end includes the open recap, whole between the reply and the composer")
    shot("open-bottom")

    page.evaluate(SCROLL, [OPEN, True])
    page.wait_for_timeout(400)
    off_screen(page, OPEN, "the open recap", tag)
    ok(f"{tag}: scrolled up, the recap goes with the messages")
    shot("open-scrolled")

    page.evaluate(SCROLL, [OPEN, False])
    page.wait_for_timeout(400)
    whole_at_end(page, OPEN, "the open recap", tag)
    ok(f"{tag}: back at the end, it comes back with the reply")

    page.locator(OPEN).get_by_role("button", name="关闭回顾").click()
    expect(page.locator(OPEN)).to_have_count(0)
    expect(page.locator(CHIP)).to_be_visible()
    page.wait_for_timeout(400)
    whole_at_end(page, CHIP, "the recap's chip", tag)
    ok(f"{tag}: closed, it folds to its chip in place, still at the end")
    shot("folded-bottom")
    page.evaluate(SCROLL, [CHIP, True])
    shot("folded-scrolled")

    page.reload()
    expect(page.locator(CHIP)).to_be_visible()
    expect(page.locator(OPEN)).to_have_count(0)
    page.wait_for_timeout(800)
    whole_at_end(page, CHIP, "the recap's chip", tag)
    ok(f"{tag}: on reload it stays closed, the chat at its end")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel="chrome")
        try:
            for size in SIZES:
                for theme in ("light", "dark"):
                    run(browser, a.base.rstrip("/"), a.token, size, theme, a.shots)
        finally:
            browser.close()
    print("PASS web recap e2e")


if __name__ == "__main__":
    main()
