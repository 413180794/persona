#!/usr/bin/env python3
"""End-to-end check of the installable web client (PWA, #1772, docs/web/web.md "Install as an app"): Chrome finds nothing
missing to install it, the service worker takes over, keeps the shell and its assets but nothing from the API, the
page still opens with the hub unreachable, and Settings shows the install row.

The service worker only runs in a build, so this needs `vite preview` (not `vite`) against the fake hub, on 127.0.0.1
(a secure context like https), and Playwright for Python (it drives the installed Chrome):

    PORT=18792 python3 scripts/mock/mock_hub.py &
    (cd web && npm run build && PERSONA_HUB_URL=http://127.0.0.1:18792 npx vite preview --port 5292 --strictPort &)
    python3 scripts/test/web_pwa_e2e.py http://127.0.0.1:5292 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. --shots saves the install row (light / dark,
desktop / phone width) and the page opened offline.
"""
import argparse
import os
import sys
import tempfile

from playwright.sync_api import expect, sync_playwright


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--token", default="mock")
    ap.add_argument("--shots")
    a = ap.parse_args()
    base = a.base.rstrip("/")
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        # A profile of its own: Playwright's usual contexts are incognito, where Chrome installs nothing.
        with tempfile.TemporaryDirectory() as profile:
            ctx = p.chromium.launch_persistent_context(profile, channel="chrome", viewport={"width": 1280, "height": 820}, device_scale_factor=2)
            try:
                run(ctx, base, a.token, a.shots)
            finally:
                ctx.close()
        browser = p.chromium.launch(channel="chrome")
        try:
            shots(browser, base, a.token, a.shots)
        finally:
            browser.close()
    print("PASS web PWA", flush=True)


def run(ctx, base, token, shots_dir):
    page = ctx.pages[0] if ctx.pages else ctx.new_page()
    page.goto(base + "/?token=" + token)
    expect(page.locator("#root > *").first).to_be_visible(timeout=15000)

    cdp = ctx.new_cdp_session(page)
    m = cdp.send("Page.getAppManifest")
    if m.get("errors"):
        fail("manifest errors: %s" % m["errors"])
    if not m.get("url", "").endswith("/manifest.webmanifest"):
        fail("no manifest linked: %r" % m.get("url"))
    ok("manifest parses without errors")

    # The worker registers after load and claims the page; the next load goes through it.
    page.wait_for_function("navigator.serviceWorker.ready.then(() => true)", timeout=15000)
    page.wait_for_function("caches.has('persona-shell-v1')", timeout=15000)
    page.reload()
    page.wait_for_function("!!navigator.serviceWorker.controller", timeout=15000)
    ok("service worker controls the page")

    errs = cdp.send("Page.getInstallabilityErrors").get("installabilityErrors", [])
    if errs:
        fail("installability errors: %s" % errs)
    ok("Chrome reports no installability errors")

    # Installable, Chrome offers its prompt; Settings turns it into the "Install" button (lib/pwa.ts).
    page.goto(base + "/settings")
    button = page.get_by_role("button", name="安装", exact=True)
    try:
        expect(button).to_be_visible(timeout=10000)
        ok("设置 offers the browser's install prompt as 安装")
        if shots_dir:
            button.scroll_into_view_if_needed()
            page.wait_for_timeout(500)
            page.screenshot(path=os.path.join(shots_dir, "install-prompt.png"))
    except AssertionError:
        print("note Chrome sent no install prompt here (it may hold it back under automation); the hint row shows instead", flush=True)

    keys = page.evaluate("""async () => {
        const out = {};
        for (const n of await caches.keys()) out[n] = (await (await caches.open(n)).keys()).map((r) => new URL(r.url).pathname);
        return out;
    }""")
    shell = keys.get("persona-shell-v1", [])
    assets = keys.get("persona-assets-v1", [])
    if "/" not in shell:
        fail("shell not kept under /: %s" % shell)
    if not any(k.endswith(".js") for k in assets) or not any(k.endswith(".css") for k in assets):
        fail("shell's script / CSS not kept: %s" % assets)
    every = [k for v in keys.values() for k in v]
    if any(k.startswith("/api") or k.startswith("/admin") for k in every):
        fail("API or admin response cached: %s" % every)
    if any("token" in k for k in every):
        fail("a token in a cache key: %s" % every)
    ok("shell (%d) and assets (%d) kept, nothing from /api" % (len(shell), len(assets)))

    # The hub unreachable: the installed app still opens (the shell, then the client's own offline state).
    ctx.set_offline(True)
    page.goto(base + "/inbox")
    expect(page.locator("#root > *").first).to_be_visible(timeout=15000)
    if not page.title().endswith("Persona"):
        fail("offline page: %r" % page.title())
    ok("opens with the hub unreachable")
    if shots_dir:
        page.wait_for_timeout(800)
        page.screenshot(path=os.path.join(shots_dir, "offline.png"))
    ctx.set_offline(False)


def shots(browser, base, token, shots_dir):
    for scheme in ("light", "dark"):
        for name, size in (("desktop", (1280, 820)), ("phone", (390, 844))):
            ctx = browser.new_context(viewport={"width": size[0], "height": size[1]}, device_scale_factor=2, color_scheme=scheme)
            page = ctx.new_page()
            page.goto(base + "/settings?token=" + token)
            row = page.get_by_text("安装为 App")
            expect(row).to_be_visible(timeout=15000)
            hint = row.locator("xpath=following-sibling::div[1]").inner_text()
            if not hint:
                fail("install row without a hint")
            if scheme == "light" and name == "desktop":
                ok("设置 shows the install row: %s" % hint)
            # theme-color follows the page's scheme (index.html, views/theme.ts)
            active = page.evaluate("""[...document.querySelectorAll('meta[name="theme-color"]')]
                .filter((m) => matchMedia(m.media).matches).map((m) => m.content)""")
            if len(active) != 1:
                fail("theme-color metas matching in %s: %s" % (scheme, active))
            if shots_dir:
                row.scroll_into_view_if_needed()
                page.wait_for_timeout(500)
                page.screenshot(path=os.path.join(shots_dir, "install-%s-%s.png" % (name, scheme)))
            ctx.close()
    ok("theme-color matches the scheme, light and dark")


if __name__ == "__main__":
    main()
