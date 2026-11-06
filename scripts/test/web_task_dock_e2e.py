#!/usr/bin/env python3
"""End-to-end check of the main conversation's task dock above the composer (#1381): "All N" and the pills are one
capsule high and share their top and bottom edges, the pills' row draws no scrollbar and takes no height for one, and
it still scrolls sideways by wheel, trackpad and touch.

The bug: a global `* { scrollbar-width: thin }` sat outside the CSS layers and beat the row's `scrollbar-width: none`,
so browsers that keep scrollbars on (Linux, Windows, macOS set to always show them) drew a bar under the pills; it
pushed them up while "All", centred on the whole row, sat lower.

Needs a running web page against the fake hub and Playwright for Python (it drives the installed Chrome; WebKit too
when Playwright's WebKit runs on this machine):

    MOCK_TASKS=1 MOCK_TASKS_DOCK=many PORT=18793 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18793 npx vite --port 5293 --strictPort &)
    python3 scripts/test/web_task_dock_e2e.py http://127.0.0.1:5293 [--browser chromium|webkit|all] [--shots <dir>]

MOCK_TASKS_DOCK=pair gives two pills, =many more than fit the screen (the sideways scrolling checks need it).
Chrome runs with its scrollbars on (Playwright hides them in headless by default), the case the bug showed in.
Prints one line per check and a verdict; exits 1 on the first failure. --shots saves light / dark, wide / narrow
screenshots of the dock.
"""
import argparse
import os
import sys

from playwright.sync_api import sync_playwright

BOARD = '[data-testid="task-dock-board"]'

# The capsules' boxes: "All", every pill, and the pills' scroller.
METRICS = """() => {
  const board = document.querySelector('[data-testid="task-dock-board"]');
  if (!board) return null;
  const dock = board.parentElement;
  const box = (el) => { const r = el.getBoundingClientRect(); return { top: r.top, bottom: r.bottom, height: r.height, left: r.left, right: r.right }; };
  const scroller = board.nextElementSibling;
  const pills = scroller ? [...scroller.querySelectorAll(':scope > button')] : [];
  const style = (el) => { const s = getComputedStyle(el); return { radius: s.borderTopLeftRadius, padL: s.paddingLeft, padR: s.paddingRight, size: s.fontSize }; };
  return {
    dock: box(dock),
    board: { ...box(board), ...style(board) },
    pills: pills.map((p) => ({ ...box(p), ...style(p) })),
    scroller: scroller && { ...box(scroller), offsetH: scroller.offsetHeight, clientH: scroller.clientHeight,
      scrollW: scroller.scrollWidth, clientW: scroller.clientWidth, scrollLeft: scroller.scrollLeft,
      barWidth: getComputedStyle(scroller).scrollbarWidth },
  };
}"""


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def near(a, b, eps=0.5):
    return abs(a - b) <= eps


def metrics(page, what):
    page.locator(BOARD).wait_for(state="visible")
    page.wait_for_timeout(300)
    m = page.evaluate(METRICS)
    if not m or not m["pills"]:
        fail(f"{what}: no dock or no pills ({m})")
    return m


def check_aligned(m, what):
    b, first = m["board"], m["pills"][0]
    # The ask: "All" and the first pill's getBoundingClientRect top and height are equal.
    if not (near(b["top"], first["top"]) and near(b["height"], first["height"])):
        fail(f"{what}: 全部 top {b['top']} height {b['height']}, first pill top {first['top']} height {first['height']}")
    for i, p in enumerate(m["pills"]):
        if not (near(p["top"], b["top"]) and near(p["bottom"], b["bottom"])):
            fail(f"{what}: pill {i} spans {p['top']}–{p['bottom']}, 全部 {b['top']}–{b['bottom']}")
        for k in ("radius", "padL", "padR", "size"):
            if p[k] != b[k]:
                fail(f"{what}: pill {i} {k} {p[k]}, 全部 {b[k]}")
    ok(f"{what}: 全部 and {len(m['pills'])} pills share top {round(b['top'], 1)} and height {b['height']}, "
       f"padding {b['padL']}, size {b['size']}")


def check_no_bar(m, what):
    s = m["scroller"]
    if s["offsetH"] != s["clientH"]:
        fail(f"{what}: the pills' row draws a {s['offsetH'] - s['clientH']} px scrollbar ({s})")
    if not near(m["dock"]["height"], m["board"]["height"]):
        fail(f"{what}: the dock is {m['dock']['height']} px high, a capsule {m['board']['height']}")
    ok(f"{what}: no scrollbar under the pills, the dock is one capsule high ({m['dock']['height']} px)")


def check_wheel(page, m, what):
    s = m["scroller"]
    if s["scrollW"] <= s["clientW"]:
        return
    x, y = (s["left"] + s["right"]) / 2, (s["top"] + s["bottom"]) / 2
    page.mouse.move(x, y)
    page.mouse.wheel(240, 0)  # a trackpad's sideways swipe
    page.wait_for_timeout(400)
    after = page.evaluate(METRICS)["scroller"]["scrollLeft"]
    if after <= s["scrollLeft"]:
        fail(f"{what}: a sideways wheel did not scroll the pills ({s['scrollLeft']} → {after})")
    ok(f"{what}: a sideways wheel scrolls the pills ({round(s['scrollLeft'])} → {round(after)})")
    page.evaluate("() => { document.querySelector('[data-testid=\"task-dock-board\"]').nextElementSibling.scrollLeft = 0; }")


def check_touch(page, m, what):
    s = m["scroller"]
    if s["scrollW"] <= s["clientW"]:
        return
    # A finger dragged right to left (raw touch events: headless Chrome ignores synthesized scroll gestures).
    cdp = page.context.new_cdp_session(page)
    x, y = s["right"] - 20, (s["top"] + s["bottom"]) / 2
    cdp.send("Input.dispatchTouchEvent", {"type": "touchStart", "touchPoints": [{"x": x, "y": y}]})
    for i in range(1, 11):
        cdp.send("Input.dispatchTouchEvent", {"type": "touchMove", "touchPoints": [{"x": x - i * 20, "y": y}]})
        page.wait_for_timeout(16)
    cdp.send("Input.dispatchTouchEvent", {"type": "touchEnd", "touchPoints": []})
    page.wait_for_timeout(400)
    after = page.evaluate(METRICS)["scroller"]["scrollLeft"]
    if after <= 0:
        fail(f"{what}: a touch swipe did not scroll the pills")
    ok(f"{what}: a touch swipe scrolls the pills (→ {round(after)})")
    page.evaluate("() => { document.querySelector('[data-testid=\"task-dock-board\"]').nextElementSibling.scrollLeft = 0; }")


def check_forced_bar(page, what):
    """Even an engine that still draws a bar under the pills does not move them off "All"'s line."""
    page.add_style_tag(content='[data-testid="task-dock-board"] + div { scrollbar-width: auto !important; overflow-x: scroll !important; }'
                               '[data-testid="task-dock-board"] + div::-webkit-scrollbar { display: block !important; height: 12px; }')
    m = metrics(page, what)
    check_aligned(m, what + ", a scrollbar forced on")


def shot(page, shots, name):
    if shots:
        page.locator(BOARD).scroll_into_view_if_needed()
        page.wait_for_timeout(300)
        page.screenshot(path=os.path.join(shots, name))


def run(p, engine, base, a):
    if engine == "webkit":
        browser = p.webkit.launch()
    else:
        # Playwright starts headless Chrome with --hide-scrollbars; without it Chrome on Linux draws classic,
        # always-on scrollbars, the case the bug showed in.
        browser = p.chromium.launch(channel="chrome", ignore_default_args=["--hide-scrollbars"])
    try:
        for label, size, mobile in (("wide", (1280, 820), False), ("narrow", (390, 844), True)):
            for theme in ("light", "dark"):
                what = f"{engine} {label} {theme}"
                opts = {"viewport": {"width": size[0], "height": size[1]}, "device_scale_factor": 2}
                if mobile and engine != "webkit":
                    opts.update(is_mobile=True, has_touch=True)
                ctx = browser.new_context(**opts)
                ctx.add_init_script(f"localStorage.setItem('persona.theme', '{theme}')")
                page = ctx.new_page()
                page.set_default_timeout(15000)
                page.goto(base + "/?token=" + a.token)
                m = metrics(page, what)
                check_aligned(m, what)
                check_no_bar(m, what)
                shot(page, a.shots, f"{engine}-{label}-{theme}.png")
                if theme == "light":
                    if mobile and engine != "webkit":
                        check_touch(page, m, what)
                    elif not mobile:
                        check_wheel(page, m, what)
                    check_forced_bar(page, what)
                    shot(page, a.shots, f"{engine}-{label}-{theme}-forced-bar.png")
                ctx.close()
    finally:
        browser.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--browser", default="chromium", choices=["chromium", "webkit", "all"])
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    a = ap.parse_args()
    base = a.base.rstrip("/")
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    engines = ["chromium", "webkit"] if a.browser == "all" else [a.browser]
    with sync_playwright() as p:
        for engine in engines:
            run(p, engine, base, a)
    print("PASS web task dock e2e")


if __name__ == "__main__":
    main()
