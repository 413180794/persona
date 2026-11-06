#!/usr/bin/env python3
"""End-to-end check of the web chat's streaming (#1963): the assistant's reply fades in word by word while its deltas
arrive and is plain Markdown once it is written (no fade spans left); a working Claude's status bar shows the status
line's verb ("Thinking" while thinking), the time and the tokens counting up, then lingers and fades out when the turn ends.

Needs a running web page against the fake hub with MOCK_STREAM=1 MOCK_LIVE_STATUS=1 (the status bar's run starts
when the page subscribes to the widget session) and Playwright for Python (it drives the installed Chrome):

    MOCK_STREAM=1 MOCK_LIVE_STATUS=1 MOCK_LIVE_STATUS_TICK=1.5 PORT=18821 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18821 npx vite --port 5321 --strictPort &)
    python3 scripts/test/web_stream_e2e.py http://127.0.0.1:5321 --hub http://127.0.0.1:18821 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. Runs wide light, wide dark and phone width
(390) light; --shots saves the reply mid-stream and the status bar thinking / counting tokens for each.
"""
import argparse
import os
import sys
import time
import urllib.parse
import urllib.request

from playwright.sync_api import sync_playwright

WIDGET = "/s/macbook/a1f3c2d4-0000-4000-8000-000000000001"
RUNS = [("wide", "light"), ("wide", "dark"), ("narrow", "light")]
SIZES = {"wide": {"width": 1280, "height": 820}, "narrow": {"width": 390, "height": 844}}
REPLY = ("先说结论：小组件的刷新预算够用。**时间线**按小时排，降雨概率的格式化也对上了。\n\n"
         "- 主屏幕小组件每小时刷新一次，耗电在预算之内\n- 锁屏小组件沿用同一条时间线\n\n"
         "```swift\nlet entry = WeatherEntry(date: .now, rain: 0.3)\n```\n\n最后一句：测试全部通过，可以合并了")

# Samples every frame while the reply streams: how many fade spans there were at most, the lowest opacity one of them
# had (a word mid-fade), and whether one ever sat inside a code block.
WATCH = """() => {
  window.__fade = { max: 0, minOpacity: 1, inPre: false };
  const tick = () => {
    const words = document.querySelectorAll("main .prose-j .stream-word, main .prose-j [data-wf-key]");
    const f = window.__fade;
    f.max = Math.max(f.max, words.length);
    for (const w of words) {
      f.minOpacity = Math.min(f.minOpacity, parseFloat(getComputedStyle(w).opacity));
      if (w.closest("pre")) f.inPre = true;
    }
    window.__fadeRaf = requestAnimationFrame(tick);
  };
  tick();
}"""


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def wait(page, js, what, tag, timeout=15):
    end = time.time() + timeout
    while time.time() < end:
        if page.evaluate(js):
            return
        page.wait_for_timeout(50)
    fail(f"{tag}: {what} (timed out after {timeout}s)")


def bar(page):
    return page.evaluate("""() => {
      const b = document.querySelector("[data-running-status]");
      if (!b) return null;
      const t = (s) => b.querySelector(s)?.textContent ?? null;
      return { phase: b.dataset.runningStatus, verb: t("[data-running-verb]"), elapsed: t("[data-running-elapsed]"),
               tokens: t("[data-running-tokens]"), opacity: parseFloat(getComputedStyle(b).opacity),
               accent: getComputedStyle(b.querySelector("[data-running-verb]")).color };
    }""")


def shot(page, shots, name, tag):
    if shots:
        page.screenshot(path=os.path.join(shots, f"stream-{tag}-{name}.png"))


def check_reply(page, base, hub, shots, tag):
    page.goto(base + "/")
    page.wait_for_selector("main textarea", timeout=15000)
    page.wait_for_timeout(800)  # subscribed
    page.evaluate(WATCH)
    last = f"（{tag}）。"  # each run's reply ends its own way: the earlier runs' replies are in the conversation too
    q = urllib.parse.urlencode({"stream": "1", "steps": "50", "ms": "80", "text": REPLY + last})
    urllib.request.urlopen(f"{hub}/mock/say?{q}", timeout=5).read()
    wait(page, "() => document.querySelectorAll('main .prose-j .stream-word').length > 3", "the reply's words fade in while it streams", tag)
    page.wait_for_timeout(1500)
    shot(page, shots, "reply-streaming", tag)
    wait(page, f"() => [...document.querySelectorAll('main .prose-j')].some((p) => p.textContent.includes({last!r}) && !p.querySelector('[data-wf-key]'))",
         "the written reply is plain Markdown", tag, timeout=20)
    page.wait_for_timeout(500)
    f = page.evaluate("() => { cancelAnimationFrame(window.__fadeRaf); return window.__fade; }")
    if f["max"] < 10:
        fail(f"{tag}: too few fading words seen while streaming: {f}")
    if f["minOpacity"] >= 0.95:
        fail(f"{tag}: no word was seen mid-fade: {f}")
    if f["inPre"]:
        fail(f"{tag}: a fade span sat inside a code block: {f}")
    ok(f"{tag}: reply faded in word by word (up to {f['max']} spans, lowest opacity {f['minOpacity']:.2f}), none in code")
    left = page.evaluate("() => document.querySelectorAll('main [data-wf-key], main .stream-word, main [data-stream-marker]').length")
    if left:
        fail(f"{tag}: {left} fade spans left after the reply was written")
    if not page.evaluate("() => !!document.querySelector('main .prose-j pre code .hljs-keyword')"):
        fail(f"{tag}: the swift block is not highlighted")
    ok(f"{tag}: no fade spans left, code highlighted")
    shot(page, shots, "reply-done", tag)


def check_bar(page, base, shots, tag):
    page.goto(base + WIDGET)
    seen = {"thinking": False, "verbs": set(), "tokens": [], "phases": [], "elapsed": set()}
    end = time.time() + 40
    shot_thinking = shot_tokens = False
    while time.time() < end:
        b = bar(page)
        if b is None:
            if "fading" in seen["phases"]:
                break
            page.wait_for_timeout(50)
            continue
        if not seen["phases"] or seen["phases"][-1] != b["phase"]:
            seen["phases"].append(b["phase"])
        if b["verb"] == "思考中":
            seen["thinking"] = True
            if not shot_thinking:
                page.wait_for_timeout(200)
                shot(page, shots, "bar-thinking", tag)
                shot_thinking = True
        elif b["verb"]:
            seen["verbs"].add(b["verb"])
        if b["tokens"] and (not seen["tokens"] or seen["tokens"][-1] != b["tokens"]):
            seen["tokens"].append(b["tokens"])
        if b["elapsed"]:
            seen["elapsed"].add(b["elapsed"])
        if b["verb"] == "Moonwalking…" and not shot_tokens:
            page.wait_for_timeout(500)
            shot(page, shots, "bar-tokens", tag)
            shot_tokens = True
        page.wait_for_timeout(50)
    if not seen["thinking"]:
        fail(f"{tag}: the bar never said 思考中: {seen}")
    if not {"Frosting…", "Moonwalking…"} <= seen["verbs"]:
        fail(f"{tag}: the verb did not follow the status line: {seen['verbs']}")
    if "↓ 22.8k tokens" not in seen["tokens"] or len(seen["tokens"]) < 5:
        fail(f"{tag}: the tokens did not count up to 22.8k: {seen['tokens']}")
    # the tween shows numbers between the pushed ones
    pushed = {"↓ 400 tokens", "↓ 900 tokens", "↓ 1.2k tokens", "↓ 2.3k tokens", "↓ 3.4k tokens", "↓ 4.8k tokens", "↓ 22.8k tokens"}
    if not set(seen["tokens"]) - pushed:
        fail(f"{tag}: the token count jumped instead of easing: {seen['tokens']}")
    if len(seen["elapsed"]) < 4 or not all("秒" in e for e in seen["elapsed"]):
        fail(f"{tag}: the time did not tick: {sorted(seen['elapsed'])}")
    ok(f"{tag}: bar said 思考中, then {sorted(seen['verbs'])}, tokens eased {seen['tokens'][0]} → {seen['tokens'][-1]} ({len(seen['tokens'])} steps), time ticked")
    if seen["phases"][-2:] != ["linger", "fading"] or bar(page) is not None:
        fail(f"{tag}: the bar did not linger, fade and go: {seen['phases']}")
    ok(f"{tag}: after the turn the bar lingered, faded out and went ({' → '.join(seen['phases'])})")


def run(browser, base, hub, token, size, theme, shots):
    tag = f"{size}-{theme}"
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2, has_touch=size == "narrow")
    page = ctx.new_page()
    page.goto(base + "/?token=" + token)
    page.wait_for_selector("main textarea", timeout=15000)  # paired
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}'); localStorage.removeItem('persona.replyMotion')")
    check_reply(page, base, hub, shots, tag)
    check_bar(page, base, shots, tag)
    ctx.close()


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("base", help="the web page, e.g. http://127.0.0.1:5321")
    ap.add_argument("--hub", required=True, help="the fake hub, e.g. http://127.0.0.1:18821")
    ap.add_argument("--token", default="mock")
    ap.add_argument("--shots", help="directory for screenshots")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel="chrome")
        try:
            for size, theme in RUNS:
                run(browser, a.base.rstrip("/"), a.hub.rstrip("/"), a.token, size, theme, a.shots)
        finally:
            browser.close()
    print("PASS web_stream_e2e", flush=True)


if __name__ == "__main__":
    main()
