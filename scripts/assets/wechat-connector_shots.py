#!/usr/bin/env python3
"""The README's WeChat (wechat-connector) pictures in docs/assets/readme/: the web client with WeChat connected through the
wechat-connector public API (docs/wechat-connector.md), every name and id made up.

    python3 scripts/assets/wechat-connector_shots.py [--only inbox,signin] [--out DIR] [--chromium PATH]

Self-contained and re-runnable after any visual change: it starts the fake hub (scripts/mock/mock_hub.py with
MOCK_CONNECTOR_PLUGIN=wechat-connector MOCK_wechat-connector_MORE=1) and the web dev server on free loopback ports, drives them with
Playwright for Python in zh-CN at fixed viewports, writes <name>-light.webp and <name>-dark.webp and stops both servers
(only the processes it started). Each picture is a CROP (1100 x 700) of the 1440 x 900 window (the inbox's window narrowed to sidebar + crop), without the sidebar: the
inbox list with the open conversation, and the sign-in form; the README shows the two side by side. Before writing a picture it checks the page's visible text with scripts/privacy_check.py's
rules, for WeChat ids other than the reserved examples (wxid_example_*, 10000000001@chatroom) and for hosts other than
wechat-connector.example.com, example.com and 127.0.0.1; any finding aborts without writing. Each picture must stay under
250 KB. Needs `npm ci` in web/, Playwright for Python and Pillow (pip install playwright pillow; `playwright install
chromium`, or pass --chromium with a Chrome / Chromium binary).
"""
import argparse
import io
import os
import re
import signal
import socket
import subprocess
import sys
import time
import urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
OUT = os.path.join(ROOT, "docs/assets/readme")
MAX_BYTES = 250 * 1024
VIEW = {"width": 1440, "height": 900}
CROP = {"width": 1100, "height": 700}
sys.path.insert(0, os.path.join(ROOT, "scripts"))

# Only these ids and hosts may be visible (docs/wechat-connector.md's examples).
WXID = re.compile(r"wxid_[A-Za-z0-9_-]+")
CHATROOM = re.compile(r"\d+@chatroom")
HOST = re.compile(r"\b(?:https?://)?((?:[a-z0-9-]+\.)+[a-z]{2,})(?::\d+)?(?=/|\b)", re.I)
OK_HOSTS = {"wechat-connector.example.com", "example.com"}


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def wait_http(url, seconds=60):
    end = time.time() + seconds
    while time.time() < end:
        try:
            urllib.request.urlopen(url, timeout=2).read()
            return
        except Exception:
            time.sleep(0.5)
    raise SystemExit(f"wechat-connector_shots: {url} did not answer in {seconds}s")


def start(cmd, cwd, env):
    """A server in its own process group, so stopping it stops its children (npx → vite) and nothing else."""
    return subprocess.Popen(cmd, cwd=cwd, env={**os.environ, **env}, stdout=subprocess.DEVNULL,
                            stderr=subprocess.DEVNULL, start_new_session=True)


def stop(proc):
    try:
        os.killpg(proc.pid, signal.SIGTERM)
        proc.wait(timeout=10)
    except (ProcessLookupError, subprocess.TimeoutExpired):
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass


def findings(text):
    """What in the visible text must not be published: private values, real-looking ids and other hosts."""
    import privacy_check
    words, _ = privacy_check.terms()
    found = privacy_check.problems(text, words)
    found += [w for w in WXID.findall(text) if not w.startswith("wxid_example_")]
    found += [c for c in CHATROOM.findall(text) if c != "10000000001@chatroom"]
    found += [h for h in HOST.findall(text) if h.lower() not in OK_HOSTS and not h.lower().endswith(".example.com")]
    return found


def settle(page):
    page.wait_for_load_state("networkidle")
    page.evaluate("document.fonts.ready")
    page.wait_for_timeout(600)


def clip(left, top):
    """The CROP rectangle at (left, top), moved inside the window."""
    x = min(max(0, left), VIEW["width"] - CROP["width"])
    y = min(max(0, top), VIEW["height"] - CROP["height"])
    return {"x": x, "y": y, **CROP}


def inbox(page, base, hub):
    """The inbox narrowed to WeChat, the group that @-mentions you open with its AI draft, its picture and avatars.
    Cropped from the inbox's left edge (no sidebar) to the conversation."""
    page.goto(base + "/inbox")
    page.get_by_role("button", name="微信", exact=True).first.click()
    page.get_by_text("示例同事在群里 @你").first.click()
    page.get_by_text("场地和预算今天能定吗").last.wait_for()
    page.locator("img[alt='']").last.wait_for()
    page.wait_for_function("[...document.images].every((i) => i.complete)")
    settle(page)
    # The window is made exactly as wide as the sidebar plus the crop, so the crop ends at its right edge. The left
    # edge is the inbox pane's: the nearest ancestor of the search field that starts at least 12 px further left.
    search = page.get_by_placeholder("搜索标题、发件人、内容")
    pane = """(input) => { const x = input.getBoundingClientRect().left;
        for (let e = input.parentElement; e; e = e.parentElement) { const r = e.getBoundingClientRect();
            if (r.left < x - 12 && r.height > 400) return Math.round(r.left); } return 0; }"""
    left = search.evaluate(pane)
    page.set_viewport_size({"width": left + CROP["width"], "height": VIEW["height"]})
    settle(page)
    left = search.evaluate(pane)
    return {"x": left, "y": 0, **CROP}


def signin(page, base, hub):
    """设置 › 连接器 › 微信: the sign-in step, the wechat-connector address and an app key (shown as dots) filled in."""
    req = urllib.request.Request(hub + "/api/v1/connectors/wechat-connector/logout", data=b"{}", method="POST",
                                 headers={"Authorization": "Bearer mock", "Content-Type": "application/json"})
    urllib.request.urlopen(req).read()
    page.goto(base + "/settings?tab=connectors&connector=wechat-connector")
    page.get_by_role("button", name="登录").first.click()
    page.get_by_placeholder("https://wechat-connector.example.com").fill("https://wechat-connector.example.com")
    page.locator("input[type=password]").fill("wcpat_example_key")
    page.locator("input[type=password]").blur()
    settle(page)
    # Centred on the form, from its 连接器 back link (left edge, top) to the address field's right edge.
    back = page.get_by_text("连接器", exact=True).last.bounding_box()
    field = page.get_by_placeholder("https://wechat-connector.example.com").bounding_box()
    centre = (back["x"] + field["x"] + field["width"]) / 2
    return clip(round(centre - CROP["width"] / 2), round(back["y"] - 24))


SHOTS = {"inbox": inbox, "signin": signin}  # in this order: signin signs the connector out


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--only", default=",".join(SHOTS), help="comma-separated: " + ", ".join(SHOTS))
    ap.add_argument("--out", default=OUT)
    ap.add_argument("--chromium", help="a Chrome / Chromium binary instead of Playwright's own")
    a = ap.parse_args()
    asked = [n for n in a.only.split(",") if n]
    unknown = [n for n in asked if n not in SHOTS]
    names = [n for n in SHOTS if n in asked]
    if unknown:
        raise SystemExit("wechat-connector_shots: unknown shot " + ", ".join(unknown))
    if not os.path.isdir(os.path.join(ROOT, "web/node_modules")):
        raise SystemExit("wechat-connector_shots: run `npm ci` in web/ first")

    from PIL import Image
    from playwright.sync_api import sync_playwright

    hub_port, web_port = free_port(), free_port()
    hub = start([sys.executable, "scripts/mock/mock_hub.py"], ROOT,
                {"PORT": str(hub_port), "MOCK_CONNECTOR_PLUGIN": "wechat-connector", "MOCK_CONNECTOR_PLUGIN_SIGNED_IN": "1",
                 "MOCK_wechat-connector_MORE": "1", "MOCK_HUB_ID": "readme-shots"})
    web = start(["npx", "vite", "--port", str(web_port), "--strictPort", "--host", "127.0.0.1"], os.path.join(ROOT, "web"),
                {"PERSONA_HUB_URL": f"http://127.0.0.1:{hub_port}"})
    base, hub_url = f"http://127.0.0.1:{web_port}", f"http://127.0.0.1:{hub_port}"
    try:
        wait_http(hub_url + "/api/v1/health")
        wait_http(base + "/")
        os.makedirs(a.out, exist_ok=True)
        with sync_playwright() as p:
            browser = p.chromium.launch(executable_path=a.chromium) if a.chromium else p.chromium.launch()
            for name in names:
                for theme in ("light", "dark"):
                    ctx = browser.new_context(viewport=VIEW, device_scale_factor=1, locale="zh-CN",
                                              timezone_id="Asia/Shanghai", color_scheme=theme, reduced_motion="reduce")
                    ctx.add_init_script(f"localStorage.setItem('jarvis.token', 'mock');"
                                        f"localStorage.setItem('jarvis.theme', '{theme}')")
                    page = ctx.new_page()
                    page.set_default_timeout(30000)
                    page.goto(base + "/")  # a cold dev server optimizes its dependencies and reloads once
                    page.wait_for_timeout(1500)
                    area = SHOTS[name](page, base, hub_url)
                    bad = findings(page.locator("body").inner_text())
                    if bad:
                        raise SystemExit(f"wechat-connector_shots: {name}-{theme} shows {sorted(set(bad))}; nothing written")
                    png = page.screenshot(clip=area)
                    ctx.close()
                    path = os.path.join(a.out, f"wechat-connector-{name}-{theme}.webp")
                    buf = io.BytesIO()
                    Image.open(io.BytesIO(png)).convert("RGB").save(buf, "WEBP", quality=82, method=6)
                    if buf.tell() > MAX_BYTES:
                        raise SystemExit(f"wechat-connector_shots: {path} would be {buf.tell() // 1024} KB (max 250)")
                    with open(path, "wb") as f:
                        f.write(buf.getvalue())
                    print(f"{os.path.relpath(path, ROOT)}  {buf.tell() // 1024} KB")
            browser.close()
    finally:
        stop(web)
        stop(hub)


if __name__ == "__main__":
    main()
