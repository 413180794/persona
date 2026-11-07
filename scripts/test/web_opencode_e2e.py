#!/usr/bin/env python3
"""End-to-end check of opencode sessions in the web app (#3070, docs/integrations/opencode.md): the sidebar lists opencode panes as
agents (the plugin-completed ones with their titles and state, "needs you" for the one waiting on a permission, the one
without the plugin and an agent nothing reads, each with why), and the chat opens their conversation (tool calls, the
permission card) or says it is terminal only.

    MOCK_OPENCODE=1 PORT=19020 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:19020 npx vite --port 19021 --strictPort &)
    python3 scripts/test/web_opencode_e2e.py http://127.0.0.1:19021 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. Runs light and dark, at desktop and phone widths;
--shots saves each.
"""
import argparse
import os
import sys

from playwright.sync_api import expect, sync_playwright

SIZES = {"wide": {"width": 1280, "height": 1500}, "narrow": {"width": 390, "height": 844}}
WORK = "ses_e5612a2ffeBQKMrUbj51TjJc"
ASK = "ses_e5611b1c4ffeKdPz7WnQo3VhLs"
BARE = "ses_e560ee4f2ffeSbNm9QyDx6PfAr"
GEMINI = "gemini-w9-pq"


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def shot(page, shots, name):
    if shots:
        page.screenshot(path=os.path.join(shots, name + ".png"))


def open_folds(page):
    """The work between a question and the answer (and a notice) is folded: open every group."""
    for label in ("已工作", "正在工作"):
        for i in range(page.get_by_text(label).count()):
            try:
                page.get_by_text(label).nth(i).click(timeout=2000)
            except Exception:  # a live "working" line is not always a fold
                continue
            page.wait_for_timeout(250)


def case(browser, base, token, size, theme, shots, channel_exe):
    tag = f"{size}/{theme}"
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2, locale="zh-CN")
    page = ctx.new_page()
    page.set_default_timeout(15000)
    page.goto(base + "/?token=" + token)
    page.wait_for_function("localStorage.getItem('persona.token')")
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")
    page.goto(base + "/")
    page.wait_for_timeout(1500)
    if size == "narrow":  # on a phone the list is the Agents tab of the top switch
        page.goto(base + "/agents")
        page.wait_for_timeout(1200)
    for title in ("重构解析器的错误处理", "迁移数据库脚本", "整理 README 的安装说明", "一个没装插件的会话", "Gemini CLI"):
        if page.get_by_text(title).count() == 0:
            fail(f"{tag}: {title} is not in the sidebar")
    page.get_by_text("重构解析器的错误处理").first.scroll_into_view_if_needed()
    ok(f"{tag}: the opencode sessions and the terminal-only agent are in the sidebar")
    shot(page, shots, f"opencode-sidebar-{size}-{theme}")

    page.goto(base + "/persona/" + WORK)
    expect(page.get_by_text("解析器遇到语法错误时只返回一个字符串").first).to_be_visible()
    open_folds(page)
    expect(page.get_by_text("run the parser tests").first).to_be_visible()
    expect(page.get_by_text("errors.go").first).to_be_visible()
    ok(f"{tag}: the chat shows the user message, the tool calls and the answer")
    page.wait_for_timeout(500)
    shot(page, shots, f"opencode-chat-{size}-{theme}")

    page.goto(base + "/persona/" + ASK)
    expect(page.get_by_text("rm -rf build").first).to_be_visible()
    ok(f"{tag}: the permission card shows the command")
    page.wait_for_timeout(500)
    shot(page, shots, f"opencode-ask-{size}-{theme}")

    page.goto(base + "/persona/" + BARE)
    page.wait_for_timeout(1500)
    open_folds(page)
    if "没有装 Persona 的插件" not in page.locator("main").inner_text():
        fail(f"{tag}: the session without the plugin does not say why")
    ok(f"{tag}: the session without the plugin says what is missing")
    shot(page, shots, f"opencode-bare-{size}-{theme}")

    page.goto(base + "/persona/" + GEMINI)
    page.wait_for_timeout(1500)
    open_folds(page)
    if "还不能读 gemini" not in page.locator("main").inner_text():
        fail(f"{tag}: the terminal-only agent does not say why")
    ok(f"{tag}: the terminal-only agent says why")
    shot(page, shots, f"opencode-gemini-{size}-{theme}")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    ap.add_argument("--exe", default=os.environ.get("CHROMIUM", ""))
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(executable_path=a.exe or None, args=["--no-sandbox"])
        for size in SIZES:
            for theme in ("light", "dark"):
                case(browser, a.base.rstrip("/"), a.token, size, theme, a.shots, a.exe)
        browser.close()
    print("all opencode web checks passed")


if __name__ == "__main__":
    main()
