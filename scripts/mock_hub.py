#!/usr/bin/env python3
"""A fake Jarvis hub serving made-up data, for README / App Store screenshots. Nothing here is real.

    python3 scripts/mock_hub.py            # listens on 127.0.0.1:18760 (PORT=… to change)

Pair the app with it: -pairURL "jarvis://pair?url=http%3A%2F%2F127.0.0.1%3A18760&token=mock" (any token works).
It serves machines, agents, conversations, interactions and the inbox, enough for the main screens; requests it
does not know are logged to stderr as MISSING. Timestamps are relative to when it starts. See docs/testing.md.
"""
import base64, hashlib, json, os, struct, sys, threading, uuid
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

PORT = int(os.environ.get("PORT", "18760"))
NOW = datetime.now(timezone.utc)
LOG = sys.stderr


def ago(minutes=0, hours=0, days=0):
    return (NOW - timedelta(minutes=minutes, hours=hours, days=days)).isoformat().replace("+00:00", "Z")


def later(minutes=0, hours=0):
    return (NOW + timedelta(minutes=minutes, hours=hours)).isoformat().replace("+00:00", "Z")


# ---------- machines ----------
MACHINES = [
    {"id": "server", "name": "服务器", "kind": "local", "icon": "server", "online": True, "hostname": "server",
     "os": "linux", "arch": "amd64", "home": "/home/me", "herdr_version": "0.9.5", "claude_version": "2.1.320", "last_seen": ago()},
    {"id": "macbook", "name": "MacBook", "kind": "ssh", "icon": "laptop", "ssh_host": "macbook", "online": True,
     "hostname": "MacBook", "os": "darwin", "arch": "arm64", "home": "/Users/me", "herdr_version": "0.9.5",
     "claude_version": "2.1.320", "last_seen": ago()},
    {"id": "studio", "name": "Mac Studio", "kind": "ssh", "icon": "desktop", "ssh_host": "studio", "online": True,
     "hostname": "Studio", "os": "darwin", "arch": "arm64", "home": "/Users/me", "herdr_version": "0.9.5",
     "claude_version": "2.1.320", "last_seen": ago()},
]


def sess(mid, sid, title, cwd, status, state, space, sidx, slot, slidx, minutes, last, agent="claude", pending=0, **kw):
    s = {"machine_id": mid, "session_id": sid, "title": title, "cwd": cwd, "status": status, "state": state,
         "agent": agent, "launcher": "claude", "model": "opus" if agent == "claude" else "gpt-5.5", "started_by_jarvis": True,
         "assistant": False, "space_kind": "herdr", "space_id": f"w{sidx}", "space_name": space, "space_index": sidx,
         "slot_id": f"w{sidx}:t{slidx}", "slot_name": slot, "slot_index": slidx, "pane_id": f"w{sidx}:p{slidx}",
         "tab_id": f"w{sidx}:t{slidx}", "workspace_id": f"w{sidx}", "updated_at": ago(minutes=minutes),
         "last_text": last, "pending_interactions": pending, "rev": 1000}
    if agent == "claude":
        s.update(permission_mode="default", effort="high")
    s.update(kw)
    return s


WIDGET = "a1f3c2d4-0000-4000-8000-000000000001"
TESTS = "a1f3c2d4-0000-4000-8000-000000000002"
BLOG = "a1f3c2d4-0000-4000-8000-000000000003"
ORDERS = "a1f3c2d4-0000-4000-8000-000000000004"
PHOTOS = "a1f3c2d4-0000-4000-8000-000000000005"
REVIEW = "a1f3c2d4-0000-4000-8000-000000000006"
RECIPE = "a1f3c2d4-0000-4000-8000-000000000007"
ASK1 = "b2e4d6f8-0000-4000-8000-000000000001"
ASK2 = "b2e4d6f8-0000-4000-8000-000000000002"
ASK3 = "b2e4d6f8-0000-4000-8000-000000000003"

SESSIONS = [
    sess("macbook", WIDGET, "给天气 App 加主屏幕小组件", "/Users/me/code/weather", "working", "working", "weather", 1, "widget", 1, 0,
         "小组件的时间线已经能按小时刷新了，正在跑测试确认降雨概率的格式化没问题。", permission_mode="acceptEdits"),
    sess("macbook", TESTS, "补齐网络层的单元测试", "/Users/me/code/weather", "blocked", "needs_you", "weather", 1, "tests", 2, 2,
         "开始写测试前想确认一下用哪个测试框架。", pending=1),
    sess("macbook", REVIEW, "检查 PR #42 的改动", "/Users/me/code/weather", "idle", "idle", "weather", 1, "review", 3, 95,
         "PR #42 看完了：逻辑没问题，留了两条关于命名的小建议。", seen_at=ago(minutes=80)),
    sess("studio", BLOG, "博客迁移到 Astro", "/Users/me/code/blog", "done", "unread", "blog", 2, "1", 1, 12,
         "迁移完成：32 篇文章都能正常构建，旧链接全部加了 301 跳转，Lighthouse 性能分从 71 提到 98。"),
    sess("studio", PHOTOS, "照片同步脚本支持 HEIC", "/Users/me/code/photo-sync", "blocked", "needs_you", "photo-sync", 3, "1", 1, 6,
         "计划写好了，等你批准后开始改。", pending=1, permission_mode="plan", effort="xhigh"),
    sess("server", ORDERS, "排查 /orders 接口变慢", "/home/me/code/shop-api", "working", "working", "shop-api", 1, "perf", 1, 1,
         "慢在订单列表的 N+1 查询，正在改成一次 JOIN 并补索引。", agent="codex"),
    sess("macbook", RECIPE, "菜谱网站加搜索", "/Users/me/code/recipes", "offline", "offline", "", 0, "", 0, 60 * 26,
         "搜索做好了，支持按食材和用时筛选。", space_kind=None, space_id=None, space_name=None, slot_id=None, slot_name=None),
]


def assistant_sess(sid, title, minutes, last):
    return {"machine_id": "server", "session_id": sid, "title": title, "cwd": "/home/me/.jarvis/assistant",
            "status": "idle", "state": "idle", "agent": "claude", "launcher": "claude", "model": "opus",
            "started_by_jarvis": True, "assistant": True, "updated_at": ago(minutes=minutes), "last_text": last,
            "permission_mode": "default", "effort": "high",
            "pending_interactions": 0, "rev": 1000, "seen_at": ago(minutes=minutes)}


ASSISTANT = [
    assistant_sess(ASK1, "今天要处理的事", 3, "今天有 3 件事需要你……"),
    assistant_sess(ASK2, "早报 · 周三", 60 * 5, "早上好！今天 2 个会议……"),
    assistant_sess(ASK3, "帮我约周五和韩梅梅吃饭", 60 * 28, "已经在日历里加好了。"),
]

# ---------- items ----------
_seq = [0]


def it(role, kind, ts_min, **kw):
    _seq[0] += 1
    d = {"id": f"{uuid.UUID(int=_seq[0])}#0", "seq": _seq[0], "ts": ago(minutes=ts_min), "role": role, "kind": kind}
    d.update(kw)
    return d


def tool(name, inp, result, ts_min, err=False):
    tid = "toolu_%020d" % (_seq[0] + 1)
    return [it("assistant", "tool_use", ts_min, tool_use_id=tid, tool_name=name, tool_input=inp),
            it("user", "tool_result", ts_min, tool_use_id=tid, text=result, is_error=err)]


EDIT_OLD = """    func makeEntry(for date: Date) -> WeatherEntry {
        WeatherEntry(date: date, temperature: store.current.temperature)
    }"""
EDIT_NEW = """    func makeEntry(for date: Date) -> WeatherEntry {
        let hours = store.hourly.prefix(6)
        return WeatherEntry(date: date,
                            temperature: store.current.temperature,
                            rain: hours.map(\\.precipitationChance))
    }"""

ITEMS = {}
ITEMS[WIDGET] = (
    [it("user", "text", 14, text="给天气 App 加一个主屏幕小组件：显示当前温度，下面是接下来 6 小时的降雨概率。中号和小号两种尺寸。")]
    + [it("assistant", "thinking", 14, text="先看看现在的数据层有没有逐小时的预报，再决定小组件的时间线怎么刷新。")]
    + tool("Grep", {"pattern": "hourly", "path": "Sources"}, "Sources/Weather/WeatherStore.swift:18:    var hourly: [HourlyForecast]\nSources/Weather/API/Forecast.swift:7:struct HourlyForecast: Codable", 13)
    + tool("Read", {"file_path": "/Users/me/code/weather/Sources/Weather/WeatherStore.swift"}, "1\timport Foundation\n2\t\n3\t@Observable final class WeatherStore {\n…", 13)
    + [it("assistant", "text", 12, text="数据层已经有逐小时预报（`hourly`），直接复用。我按这个顺序来：")]
    + tool("TodoWrite", {"todos": [
        {"content": "新建 Widget Extension 和共享的 App Group", "status": "completed", "activeForm": "新建 Widget Extension"},
        {"content": "时间线 Provider：每小时刷新一次", "status": "completed", "activeForm": "写时间线 Provider"},
        {"content": "小号 / 中号两种布局", "status": "in_progress", "activeForm": "做两种尺寸的布局"},
        {"content": "跑测试，确认降雨概率格式化", "status": "pending", "activeForm": "跑测试"}]}, "Todos have been modified successfully.", 12)
    + tool("Edit", {"file_path": "/Users/me/code/weather/WeatherWidget/Provider.swift", "old_string": EDIT_OLD, "new_string": EDIT_NEW},
           "The file /Users/me/code/weather/WeatherWidget/Provider.swift has been updated.", 8)
    + tool("Bash", {"command": "xcodebuild test -scheme Weather -destination 'platform=iOS Simulator,name=iPhone 17' -quiet", "description": "跑全部测试"},
           "Test Suite 'All tests' passed.\n    Executed 48 tests, with 0 failures (0 unexpected) in 6.214 seconds", 3)
    + [it("assistant", "text", 0, text="小组件的时间线已经能按小时刷新了。测试全部通过（48 个），接下来把中号布局的降雨条做成和 App 里一样的样式。")]
)
ITEMS[TESTS] = (
    [it("user", "text", 5, text="给网络层补单元测试，覆盖重试和超时")]
    + tool("Glob", {"pattern": "Sources/Weather/API/*.swift"}, "Sources/Weather/API/Client.swift\nSources/Weather/API/Retry.swift\nSources/Weather/API/Forecast.swift", 4)
    + [it("assistant", "text", 2, text="开始写测试前想确认一下用哪个测试框架。")]
)
ITEMS[BLOG] = (
    [it("user", "text", 50, text="把博客从 Jekyll 迁移到 Astro，旧链接不能失效")]
    + tool("Bash", {"command": "npm run build", "description": "构建站点"}, "✓ 32 page(s) built in 4.81s", 15)
    + [it("assistant", "text", 12, text="迁移完成：\n\n- **32 篇文章**都能正常构建，代码高亮、脚注、图片说明都保留了\n- 旧的 `/2023/05/xxx.html` 链接全部加了 **301 跳转**\n- Lighthouse 性能分从 **71 提到 98**\n\n要我顺便把 RSS 也换成 Astro 的官方插件吗？")]
)
ITEMS[PHOTOS] = [it("user", "text", 10, text="照片同步脚本要支持 HEIC，导出的时候转成 JPEG")] + [it("assistant", "text", 6, text="计划写好了，等你批准后开始改。")]
ITEMS[ORDERS] = (
    [it("user", "text", 20, text="/orders 接口最近 P95 到了 2 秒，查一下")]
    + tool("Bash", {"command": "go test ./internal/orders -run TestList -bench . -benchmem"}, "BenchmarkList-8   	     120	   9812345 ns/op", 6)
    + [it("assistant", "text", 1, text="慢在订单列表的 N+1 查询，正在改成一次 JOIN 并补索引。")]
)
ITEMS[REVIEW] = [it("user", "text", 100, text="看一下 PR #42")] + [it("assistant", "text", 95, text="PR #42 看完了：逻辑没问题，留了两条关于命名的小建议。")]
ITEMS[RECIPE] = [it("user", "text", 60 * 27, text="菜谱网站加个搜索")] + [it("assistant", "text", 60 * 26, text="搜索做好了，支持按食材和用时筛选。")]

ITEMS[ASK1] = (
    [it("user", "text", 4, text="今天有什么需要我处理的？")]
    + tool("mcp__jarvis__agents_list", {}, "6 个 Agent：2 个需要你，2 个工作中，1 个完成未看", 4)
    + tool("mcp__jarvis__lark_chats", {"unread": True}, "未读 4，@我 1", 4)
    + tool("mcp__jarvis__phone_calendar_events", {}, "今天 2 个日程", 4)
    + [it("assistant", "text", 3, text="""今天有 **3 件事**需要你：

1. **补齐网络层的单元测试**（MacBook）在等你选测试框架，我建议用 Swift Testing。
2. **照片同步脚本**（Mac Studio）的计划写好了，等你批准。
3. 韩梅梅在飞书问你**周五的发布能不能赶上**，她下午要和客户确认。

另外博客迁移已经做完，Lighthouse 98 分，有空可以看一眼。

今天的日程：
- 14:00 设计评审（会议室 3）
- 16:30 和李雷 1:1""")]
)
ITEMS[ASK2] = [it("user", "text", 60 * 5, text="⏰ 早报")] + [it("assistant", "text", 60 * 5, text="早上好！今天 2 个会议，3 封邮件需要回复。")]
ITEMS[ASK3] = [it("user", "text", 60 * 29, text="帮我约周五和韩梅梅吃饭")] + [it("assistant", "text", 60 * 28, text="已经在日历里加好了。")]

INTERACTIONS = [
    {"id": "ix_tests", "machine_id": "macbook", "session_id": TESTS, "session_title": "补齐网络层的单元测试",
     "cwd": "/Users/me/code/weather", "kind": "question", "created_at": ago(minutes=2), "expires_at": later(minutes=28),
     "tool_name": "AskUserQuestion", "tool_use_id": "toolu_q1",
     "questions": [{"question": "网络层的测试用哪个框架？", "header": "测试框架", "multi_select": False, "options": [
         {"label": "Swift Testing (Recommended)", "description": "新代码统一用 @Test，参数化测试写起来更短"},
         {"label": "XCTest", "description": "和现有的 UI 测试保持一致"}]}]},
    {"id": "ix_photos", "machine_id": "studio", "session_id": PHOTOS, "session_title": "照片同步脚本支持 HEIC",
     "cwd": "/Users/me/code/photo-sync", "kind": "plan", "created_at": ago(minutes=6), "expires_at": later(minutes=24),
     "tool_name": "ExitPlanMode", "tool_use_id": "toolu_p1",
     "plan": "## 支持 HEIC\n\n1. 用 `sips` 把 HEIC 转成 JPEG，质量 90，保留 EXIF\n2. 转换放到导出前，原文件不动\n3. 已经转过的按内容哈希跳过\n4. 补测试：竖拍、实况照片、没有 EXIF 的图"},
]

# MOCK_SCREEN_DIALOG=1: a terminal dialog (screen interaction) as a newer hub reads it; MOCK_PROMPT_CHANGED=1 makes
# answering it fail as if the dialog changed on the computer (409 prompt_changed).
if os.environ.get("MOCK_SCREEN_DIALOG"):
    INTERACTIONS.append(
        {"id": "scr_macbook_review_3f9a1c2b7d4e", "machine_id": "macbook", "session_id": REVIEW, "session_title": "检查 PR #42 的改动",
         "cwd": "/Users/me/code/weather", "kind": "screen", "created_at": ago(minutes=1), "expires_at": later(hours=24),
         "screen": " Bash command\n\n   git push origin review/naming\n   Push the naming fixes\n\n Do you want to proceed?\n"
                   " ❯ 1. Yes\n   2. Yes, and don't ask again for git push commands in /Users/me/code/weather\n"
                   "   3. No, and tell Claude what to do differently (esc)",
         "dialog": {"question": "Do you want to proceed?", "detail": "Bash command\ngit push origin review/naming\nPush the naming fixes",
                    "rev": "3f9a1c2b7d4e", "options": [
                        {"key": "1", "label": "Yes", "keys": ["1"]},
                        {"key": "2", "label": "Yes, and don't ask again for git push commands in /Users/me/code/weather", "keys": ["2"]},
                        {"key": "3", "label": "No, and tell Claude what to do differently (esc)", "keys": ["3"]}]}})

# MOCK_SCREEN_SECRET=1: the terminal asks for a password (a card with a password field).
if os.environ.get("MOCK_SCREEN_SECRET"):
    INTERACTIONS.append(
        {"id": "scr_macbook_review", "machine_id": "macbook", "session_id": REVIEW, "session_title": "检查 PR #42 的改动",
         "cwd": "/Users/me/code/weather", "kind": "screen", "created_at": ago(minutes=1), "expires_at": later(hours=24),
         "secret": True, "screen": "⏺ Bash(sudo make install)\n  ⎿  Running…\n\n[sudo] password for me:"})

PREFS = {"input": True, "done": "long", "long_turn_seconds": 60, "done_delay_seconds": 60}  # one device is enough
RESOLVED = []  # resolve calls, newest last (GET /mock/resolved)
SENT = []  # bodies of sends while MOCK_SEND_REFUSE is set (GET /mock/sent)

INBOX = [
    {"id": "lark:f1:100", "source": "lark", "kind": "dm", "account": "lark", "account_name": "飞书", "title": "韩梅梅",
     "sender": "韩梅梅", "preview": "周五的发布能赶上吗？下午我要和客户确认时间", "count": 2, "started_at": ago(minutes=40),
     "updated_at": ago(minutes=18), "tier": "action", "summary": "问周五的发布能不能赶上，下午要和客户确认",
     "reason": "直接问你，有时间要求", "read": False,
     "lark": {"feed_id": "f1", "is_group": False, "start_position": "100"}},
    {"id": "lark:f2:200", "source": "lark", "kind": "mention", "account": "lark", "account_name": "飞书", "title": "设计评审",
     "sender": "李雷", "preview": "@你 新版图标放在这里了，下午评审前看一眼？", "count": 3, "started_at": ago(minutes=90),
     "updated_at": ago(minutes=55), "tier": "action", "summary": "李雷请你在下午评审前看新版图标",
     "reason": "@了你，下午就要用", "read": False,
     "lark": {"feed_id": "f2", "is_group": True, "start_position": "200", "mention_position": "202"}},
    {"id": "mail:acc1:t1:501", "source": "mail", "kind": "mail", "account": "acc1", "account_name": "个人邮箱",
     "title": "下周的分享能不能提前到周二？", "sender": "Lucy Wang", "preview": "Hi，周四我临时要出差，分享能不能挪到周二下午……",
     "count": 1, "started_at": ago(hours=3), "updated_at": ago(hours=3), "tier": "action",
     "summary": "Lucy 想把下周的分享从周四挪到周二下午", "reason": "需要你回复确认", "read": False,
     "mail": {"account_id": "acc1", "mailbox": "INBOX", "uids": [501], "from": {"name": "Lucy Wang", "email": "lucy@example.com"}, "direct": True}},
    {"id": "lark:f3:300", "source": "lark", "kind": "group", "account": "lark", "account_name": "飞书", "title": "周末徒步群",
     "sender": "Tom", "preview": "周六早上 8 点地铁站集合，记得带水", "count": 12, "started_at": ago(hours=2),
     "updated_at": ago(minutes=30), "tier": "fyi", "summary": "周六 8 点地铁站集合", "read": False,
     "lark": {"feed_id": "f3", "is_group": True, "start_position": "300"}},
    {"id": "mail:acc1:t2:502", "source": "mail", "kind": "mail", "account": "acc1", "account_name": "个人邮箱",
     "title": "你的订单已发货", "sender": "示例商城", "preview": "订单 2026102800123 已由快递揽收，预计周五送达",
     "count": 1, "started_at": ago(hours=5), "updated_at": ago(hours=5), "tier": "fyi", "summary": "订单已发货，周五送达",
     "read": False, "mail": {"account_id": "acc1", "mailbox": "INBOX", "uids": [502], "from": {"name": "示例商城", "email": "shop@example.com"}}},
    {"id": "mail:acc1:t3:503", "source": "mail", "kind": "mail", "account": "acc1", "account_name": "个人邮箱",
     "title": "本周精选：10 个 SwiftUI 技巧", "sender": "Weekly Digest", "preview": "这周我们整理了……", "count": 1,
     "started_at": ago(hours=8), "updated_at": ago(hours=8), "tier": "noise", "summary": "技术周刊", "read": False,
     "mail": {"account_id": "acc1", "mailbox": "INBOX", "uids": [503], "from": {"name": "Weekly Digest", "email": "news@example.com"}}},
]

MAIL_ACCOUNTS = [{"id": "acc1", "name": "个人邮箱", "email": "me@example.com", "provider": "imap", "auth": "password",
                  "imap_host": "imap.example.com", "imap_port": 993, "smtp_host": "smtp.example.com", "smtp_port": 465}]


def all_sessions():
    return SESSIONS + ASSISTANT


def find_items(sid):
    return ITEMS.get(sid, [])


# ---------- HTTP ----------
class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *a):
        LOG.write("%s %s\n" % (self.command, self.path)); LOG.flush()

    def send_json(self, obj, status=200):
        body = json.dumps(obj, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def body(self):
        n = int(self.headers.get("Content-Length") or 0)
        return self.rfile.read(n) if n else b""

    def do_GET(self):
        u = urlparse(self.path); p = u.path; q = parse_qs(u.query)
        if p == "/api/v1/ws":
            return self.websocket()
        if p == "/api/v1/health": return self.send_json({"ok": True, "version": "mock"})
        if p == "/api/v1/machines": return self.send_json({"machines": MACHINES})
        if p == "/api/v1/sessions": return self.send_json({"sessions": all_sessions()})
        if p == "/api/v1/interactions": return self.send_json({"interactions": INTERACTIONS})
        if p == "/api/v1/interactions/history": return self.send_json({"interactions": []})
        if p == "/mock/resolved": return self.send_json({"resolved": RESOLVED})
        if p == "/mock/sent": return self.send_json({"sent": SENT})
        if p.startswith("/api/v1/devices/") and p.endswith("/prefs"): return self.send_json(PREFS)
        if p in ("/api/v1/pins",): return self.send_json({"spaces": ["macbook/weather"], "sessions": []})
        if p == "/api/v1/mutes": return self.send_json({"spaces": [], "sessions": [], "allowed": []})
        if p == "/api/v1/assistant":
            return self.send_json({"machine_id": "server", "launcher": "claude", "cwd": "/home/me/.jarvis/assistant",
                                   "timezone": "Asia/Shanghai", "notes_effective": "", "conversations": ASSISTANT})
        if p == "/api/v1/assistant/routines": return self.send_json({"routines": []})
        if p == "/api/v1/assistant/followups": return self.send_json({"followups": []})
        if p == "/api/v1/assistant/memory": return self.send_json({"files": [], "dir": "/home/me/.jarvis/assistant/memory"})
        if p == "/api/v1/inbox": return self.send_json({"items": INBOX})
        if p.startswith("/api/v1/inbox/"):
            iid = p[len("/api/v1/inbox/"):]
            for x in INBOX:
                if x["id"] == iid: return self.send_json(dict(x, context=x["preview"]))
        if p == "/api/v1/mail/accounts": return self.send_json({"accounts": MAIL_ACCOUNTS})
        if p == "/api/v1/mail/messages": return self.send_json({"messages": [], "has_more": False})
        if p == "/api/v1/lark/unread-count": return self.send_json({"count": 4})
        if p in ("/api/v1/lark/conversations", "/api/v1/lark/pinned"): return self.send_json({"conversations": [], "next_cursor": ""})
        if p == "/api/v1/lark/labels": return self.send_json({"labels": []})
        if p == "/api/v1/lark/flags": return self.send_json({"conversations": [], "messages": []})
        if p == "/api/v1/lark/emojis": return self.send_json({"emojis": {}})
        if p.startswith("/api/v1/machines/") and p.endswith("/launchers"):
            return self.send_json({"launchers": [{"name": "claude", "model": "opus"}, {"name": "codex", "model": "gpt-5.5"}]})
        if p.startswith("/api/v1/machines/") and p.endswith("/claude-settings"): return self.send_json({"settings": []})
        if p.startswith("/api/v1/sessions/") and p.endswith("/items"):
            sid = p.split("/")[5]
            items = find_items(sid)
            if "after_seq" in q:
                a = int(q["after_seq"][0] or 0)
                return self.send_json({"items": [x for x in items if x["seq"] > a]})
            b = q.get("before_seq", [""])[0]
            if b: items = [x for x in items if x["seq"] < int(b)]
            return self.send_json({"items": items, "has_more": False})
        if p.startswith("/api/v1/sessions/") and p.endswith("/info"):  # the chat's 开发信息 card
            s = next((s for s in all_sessions() if s["session_id"] == p.split("/")[5]), {})
            return self.send_json({"session_id": s.get("session_id", ""), "cwd": s.get("cwd"), "model": "claude-opus-5",
                                   "launcher": s.get("launcher"), "git_branch": "main", "version": "2.1.320",
                                   "permission_mode": s.get("permission_mode"), "effort": s.get("effort"),
                                   "context_tokens": 48200, "output_tokens": 12800, "compactions": 0, "turns": 6,
                                   "started_at": ago(minutes=40), "last_activity": ago()})
        if p.startswith("/api/v1/sessions/") and p.endswith("/screen"):
            return self.send_json({"text": "", "status": "idle"})
        LOG.write("  MISSING GET %s\n" % p); LOG.flush()
        self.send_json({"error": "not found"}, 404)

    def do_POST(self):
        self.last_body = self.body()
        p = urlparse(self.path).path
        if p == "/api/v1/inbox/refresh": return self.send_json({"items": INBOX})
        if p.startswith("/api/v1/interactions/") and p.endswith("/resolve"):  # notification buttons (UI test reads it back)
            RESOLVED.append({"id": p.split("/")[4], "resolution": json.loads(self.last_body or b"{}")})
            if os.environ.get("MOCK_PROMPT_CHANGED") and p.split("/")[4].startswith("scr_"):
                return self.send_json({"error": "终端上的对话框已经变了（可能在电脑上答过了），这次什么都没按：看看新的再答。",
                                       "code": "prompt_changed", "delivered": "none"}, 409)
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/devices/") and p.endswith("/prefs"):  # 设置 › 通知
            PREFS.update({k: v for k, v in json.loads(self.last_body or b"{}").items() if k in ("input", "done")})
            return self.send_json(PREFS)
        if p.startswith("/api/v1/sessions/") and p.endswith("/messages") and os.environ.get("MOCK_SEND_REFUSE"):
            # MOCK_SEND_REFUSE=input_draft|dialog_open: the computer refuses sends (SendSafety); clear_draft then succeeds
            body = json.loads(self.last_body or b"{}")
            SENT.append(body)
            if body.get("clear_draft"):
                return self.send_json({"ok": True})
            if os.environ["MOCK_SEND_REFUSE"] == "dialog_open":
                return self.send_json({"error": "电脑上的终端正开着一个对话框（确认、选择列表等），这条消息没有发出去：先回答或关掉它再发。",
                                       "code": "dialog_open", "delivered": "none"}, 409)
            return self.send_json({"error": "电脑上的输入框里有没发出去的内容：「顺便把 README 也更新一下」。这条消息没有发出去，免得拼在一起。",
                                   "code": "input_draft", "delivered": "none", "draft": "顺便把 README 也更新一下",
                                   "clearable": "顺便把 README 也更新一下"}, 409)
        if p.startswith("/api/v1/sessions/") and p.endswith("/modes"):  # permission mode / effort chips
            body = json.loads(self.last_body or b"{}")
            for s in all_sessions():
                if s["session_id"] == p.split("/")[5]:
                    s.update({k: v for k, v in body.items() if k in ("permission_mode", "effort") and v})
            return self.send_json({"ok": True})
        self.send_json({"ok": True})

    do_PUT = do_POST
    do_DELETE = do_POST

    # ---------- minimal WebSocket ----------
    def websocket(self):
        key = self.headers.get("Sec-WebSocket-Key", "")
        accept = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
        self.send_response(101, "Switching Protocols")
        self.send_header("Upgrade", "websocket"); self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accept); self.end_headers(); self.wfile.flush()
        lock = threading.Lock()

        def send(obj):
            data = json.dumps(obj, ensure_ascii=False).encode()
            n = len(data)
            hdr = bytes([0x81]) + (bytes([n]) if n < 126 else bytes([126]) + struct.pack(">H", n) if n < 65536 else bytes([127]) + struct.pack(">Q", n))
            with lock:
                self.wfile.write(hdr + data); self.wfile.flush()

        send({"type": "hello", "server_version": "mock", "client_id": uuid.uuid4().hex[:8]})
        send({"type": "machines", "machines": MACHINES})
        by = {}
        for s in all_sessions(): by.setdefault(s["machine_id"], []).append(s)
        for m, ss in by.items(): send({"type": "sessions", "machine_id": m, "sessions": ss})
        r = self.rfile
        try:
            while True:
                h = r.read(2)
                if len(h) < 2: break
                op = h[0] & 0x0F; n = h[1] & 0x7F
                if n == 126: n = struct.unpack(">H", r.read(2))[0]
                elif n == 127: n = struct.unpack(">Q", r.read(8))[0]
                mask = r.read(4) if h[1] & 0x80 else b"\0\0\0\0"
                payload = bytes(b ^ mask[i % 4] for i, b in enumerate(r.read(n)))
                if op == 8: break
                if op == 9:
                    with lock: self.wfile.write(bytes([0x8A, 0])); self.wfile.flush()
                    continue
                if op != 1: continue
                msg = json.loads(payload or b"{}")
                LOG.write("  WS %s\n" % msg.get("type")); LOG.flush()
                if msg.get("type") == "subscribe":
                    sid = msg.get("session_id")
                    send({"type": "items", "machine_id": msg.get("machine_id"), "session_id": sid,
                          "items": find_items(sid), "backlog": True})
        except Exception as e:
            LOG.write("  WS closed %r\n" % e); LOG.flush()
        self.close_connection = True


if __name__ == "__main__":
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), H)
    srv.daemon_threads = True
    print("mock hub on 127.0.0.1:%d" % PORT, flush=True)
    srv.serve_forever()
