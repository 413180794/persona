#!/usr/bin/env python3
"""A fake Jarvis hub serving made-up data, for README / App Store screenshots. Nothing here is real.

    python3 scripts/mock/mock_hub.py            # listens on 127.0.0.1:18760 (PORT=… to change)

Pair the app with it: -pairURL "jarvis://pair?url=http%3A%2F%2F127.0.0.1%3A18760&token=mock" (any token works).
It serves machines, agents, conversations, interactions and the inbox, enough for the main screens; requests it
does not know are logged to stderr as MISSING. Timestamps are relative to when it starts. See docs/testing.md.
"""
import base64, unicodedata
import copy, hashlib, json, os, struct, sys, threading, time, uuid
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs, unquote

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
if os.environ.get("MOCK_NODE_MISMATCH"):  # a machine whose jarvisd is another build than the hub's
    MACHINES[2]["node_mismatch"] = True

# ---------- machine monitoring (docs/machines.md) ----------
# What each machine runs, and made-up but smooth 24-hour curves: the server is busy right now (CPU and memory
# high), the Mac Studio was away for two hours this morning (a gap), the NAS has been offline for three hours.
GB = 1 << 30
SYSTEMS = {
    "server": {"os": "linux", "distro": "ubuntu", "like": "debian", "name": "Ubuntu 24.04.1 LTS", "version": "24.04",
               "kernel": "6.8.0-48-generic", "arch": "amd64", "model": "Standard PC (Q35 + ICH9, 2009)",
               "cpu": "AMD EPYC 9354 32-Core Processor", "cores": 16, "memory": 64 * GB},
    "macbook": {"os": "darwin", "distro": "macos", "name": "macOS 26.1", "version": "26.1", "build": "25B78",
                "kernel": "Darwin 25.1.0", "arch": "arm64", "model": "MacBook Pro", "cpu": "Apple M4 Pro", "cores": 14,
                "memory": 48 * GB},
    "studio": {"os": "darwin", "distro": "macos", "name": "macOS 26.0.1", "version": "26.0.1", "build": "25A362",
               "kernel": "Darwin 25.0.0", "arch": "arm64", "model": "Mac Studio", "cpu": "Apple M2 Ultra", "cores": 24,
               "memory": 192 * GB},
    "nas": {"os": "linux", "distro": "debian", "name": "Debian GNU/Linux 12 (bookworm)", "version": "12",
            "kernel": "6.1.0-26-arm64", "arch": "arm64", "model": "Raspberry Pi 5 Model B Rev 1.0", "cpu": "", "cores": 4,
            "memory": 8 * GB},
}
if not os.environ.get("MOCK_MACHINES"):  # that mode has its own offline machine (MachinePagesTests counts them)
    MACHINES.append({"id": "nas", "name": "NAS", "kind": "ssh", "icon": "server", "ssh_host": "nas", "online": False,
                     "error": "ssh: connect to host nas port 22: Operation timed out", "hostname": "nas", "os": "linux",
                     "arch": "arm64", "home": "/home/me", "last_seen": ago(hours=3)})
# MOCK_LONG_OS=1: the server's OS has a long name, so its card's chips (OS, herdr, claude) do not fit on one line.
if os.environ.get("MOCK_LONG_OS"):
    SYSTEMS["server"]["name"] = "Example GNU/Linux 2 (codename)"
    MACHINES[0]["node_mismatch"] = True
for _m in MACHINES:
    _m["system"] = dict(SYSTEMS[_m["id"]], boot_time=int((NOW - timedelta(days=3, hours=4)).timestamp()))
# Machines added later (MOCK_MACHINES) have no system and never sent a sample.
T0 = int(NOW.timestamp())


def metrics_present(mid, t):
    """Whether the machine was sending at Unix time t."""
    if mid not in SYSTEMS:
        return False
    if mid == "studio" and T0 - 7 * 3600 <= t < T0 - 5 * 3600:
        return False
    if mid == "nas" and t >= T0 - 3 * 3600:
        return False
    return True


def metric_at(mid, t):
    """A smooth made-up reading: a daily rhythm, a few slower waves and a little jitter, by machine."""
    import math
    sys_ = SYSTEMS[mid]
    h = int(hashlib.md5(f"{mid}{t // 10}".encode()).hexdigest()[:6], 16) / 0xFFFFFF  # jitter 0…1
    day = math.sin(2 * math.pi * (t % 86400) / 86400)
    wave = math.sin(t / 1900) * 0.5 + math.sin(t / 530 + len(mid)) * 0.3
    base = {"server": 38, "macbook": 18, "studio": 27, "nas": 6}[mid]
    cpu = base + 10 * day + 12 * wave + 8 * h
    if mid == "server" and t > T0 - 25 * 60:  # a build running since 25 minutes before the mock started
        cpu = 92 + 6 * h  # always over the 90 % "critical" line
    cpu = max(0.5, min(99.5, cpu))
    mem_frac = {"server": 0.62, "macbook": 0.55, "studio": 0.31, "nas": 0.42}[mid] + 0.05 * day + 0.04 * wave
    if mid == "server" and t > T0 - 25 * 60:
        mem_frac = 0.88 + 0.02 * h
    rx = max(0.0, {"server": 2.4e6, "macbook": 4.5e5, "studio": 1.2e6, "nas": 3e5}[mid] * (1 + 0.6 * wave + 0.5 * h))
    tx = rx * {"server": 0.35, "macbook": 0.2, "studio": 0.6, "nas": 1.4}[mid] * (0.7 + 0.6 * h)
    load = sys_["cores"] * cpu / 100 * (0.9 + 0.3 * h)
    disk_frac = {"server": 0.64, "macbook": 0.71, "studio": 0.38, "nas": 0.93}[mid]
    return {"cpu": round(cpu, 1), "load": round(load, 2), "mem": round(mem_frac * sys_["memory"]),
            "swap": round({"server": 0.0, "macbook": 1.2, "studio": 0.0, "nas": 0.4}[mid] * GB),
            "rx": round(rx, 1), "tx": round(tx, 1), "disk": round(disk_frac * {"nas": 4000, "studio": 2000}.get(mid, 1000) * GB)}


def metric_series(mid, span, step):
    """Like the hub: one point a step from span ago, a gap marker where the machine sent nothing for > 3 steps."""
    out, missing = [], 0
    now = int(time.time())  # live, like the hub: the charts end at now
    start = (now - span) // step * step
    for t in range(start, now + 1, step):
        if not metrics_present(mid, t):
            missing += 1
            if missing * step > 3 * step and (not out or not out[-1].get("gap")):
                out.append({"t": t - (missing - 1) * step, "gap": True})
            continue
        missing = 0
        out.append({"t": t, **metric_at(mid, t)})
    return out


def metric_latest(mid):
    now = int(time.time()) // 10 * 10
    if not metrics_present(mid, now):
        return None
    v, sys_ = metric_at(mid, now), SYSTEMS[mid]
    out = {"t": now, "cpu": v["cpu"], "load": v["load"], "mem_used": v["mem"], "mem_total": sys_["memory"],
           "swap_used": v["swap"], "swap_total": 4 * GB if v["swap"] else 0, "net_rx": v["rx"], "net_tx": v["tx"],
           "disk_used": v["disk"], "disk_total": {"nas": 4000, "studio": 2000}.get(mid, 1000) * GB}
    if mid == "server":
        out["cores"] = [round(min(100, max(1, v["cpu"] + 9 * ((i * 37) % 7 - 3))), 1) for i in range(sys_["cores"])]
    return out


def machine_metrics(mid, span, step):
    m = next(x for x in MACHINES if x["id"] == mid)
    return {"machine_id": mid, "online": m["online"], "system": m.get("system"), "latest": metric_latest(mid),
            "points": metric_series(mid, span, step)}


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
RELEASE = "a1f3c2d4-0000-4000-8000-000000000008"
ASK1 = "b2e4d6f8-0000-4000-8000-000000000001"
ASK2 = "b2e4d6f8-0000-4000-8000-000000000002"
ASK3 = "b2e4d6f8-0000-4000-8000-000000000003"
ASK4 = "b2e4d6f8-0000-4000-8000-000000000004"  # a long one (the top bar must not move as it scrolls)

SESSIONS = [
    sess("macbook", WIDGET, "给天气 App 加主屏幕小组件", "/Users/me/code/weather", "working", "working", "weather", 1, "widget", 1, 0,
         "小组件的时间线已经能按小时刷新了，正在跑测试确认降雨概率的格式化没问题。", permission_mode="acceptEdits",
         loop={"kind": "wakeup", "prompt": "/loop 每 30 分钟检查 CI，红了就修", "interval_seconds": 1800,
               "next_at": (datetime.now(timezone.utc) + timedelta(minutes=17)).isoformat().replace("+00:00", "Z"), "ticks": 3},
         background=[{"id": "b1", "kind": "shell", "description": "跑小组件快照测试",
                      "command": "xcodebuild test -scheme WeatherWidget -only-testing:SnapshotTests",
                      "output_file": "/tmp/claude/b1.output", "started_at": ago(minutes=4)}]),
    sess("macbook", TESTS, "补齐网络层的单元测试", "/Users/me/code/weather", "blocked", "needs_you", "weather", 1, "tests", 2, 2,
         "开始写测试前想确认一下用哪个测试框架。", pending=1),
    sess("macbook", REVIEW, "检查 PR #42 的改动", "/Users/me/code/weather", "idle", "idle", "weather", 1, "review", 3, 95,
         "PR #42 看完了：逻辑没问题，留了两条关于命名的小建议。", seen_at=ago(minutes=80)),
    sess("macbook", RELEASE, "整理发布说明", "/Users/me/code/weather", "done", "unread", "weather", 1, "release", 4, 3,
         "发布说明整理好了，按功能分了三段，中英文各一份。"),
    sess("studio", BLOG, "博客迁移到 Astro", "/Users/me/code/blog", "done", "unread", "blog", 2, "1", 1, 12,
         "迁移完成：32 篇文章都能正常构建，旧链接全部加了 301 跳转，Lighthouse 性能分从 71 提到 98。"),
    sess("studio", PHOTOS, "照片同步脚本支持 HEIC", "/Users/me/code/photo-sync", "blocked", "needs_you", "photo-sync", 3, "1", 1, 6,
         "计划写好了，等你批准后开始改。", pending=1, permission_mode="plan", effort="xhigh"),
    sess("server", ORDERS, "排查 /orders 接口变慢", "/home/me/code/shop-api", "working", "working", "shop-api", 1, "perf", 1, 1,
         "慢在订单列表的 N+1 查询，正在改成一次 JOIN 并补索引。", agent="codex",
         # how Jarvis reaches it (docs/codex.md「守护进程上的会话」); MOCK_CODEX_ROUTE=terminal: the terminal, with why
         codex_route=os.environ.get("MOCK_CODEX_ROUTE", "app_server"),
         codex_route_note="不在共享的 app-server 上（带 -c 的启动器、在自己进程里跑的会话、没有守护进程的 0.153）"
         if os.environ.get("MOCK_CODEX_ROUTE") == "terminal" else None),
    sess("macbook", RECIPE, "菜谱网站加搜索", "/Users/me/code/recipes", "offline", "offline", "", 0, "", 0, 60 * 26,
         "搜索做好了，支持按食材和用时筛选。", space_kind=None, space_id=None, space_name=None, slot_id=None, slot_name=None),
]


# MOCK_MACHINES=1: the machine pages' edge cases (MachinePagesTests): a fourth machine that is offline with an ssh
# error (POST …/reconnect brings it back), and Mac Studio with many agents across several projects.
MOCK_MACHINES = bool(os.environ.get("MOCK_MACHINES"))
if MOCK_MACHINES:
    MACHINES.append({"id": "lab", "name": "实验室主机", "kind": "ssh", "icon": "desktop", "ssh_host": "lab", "online": False,
                     "error": "ssh: connect to host lab port 22: Connection refused", "last_seen": ago(hours=3)})
    for i in range(36):
        proj = ["infra", "ml-train", "docs-site", "mobile"][i % 4]
        state = ["working", "idle", "idle", "unread", "idle", "needs_you"][i % 6]
        status = {"working": "working", "idle": "idle", "unread": "done", "needs_you": "blocked"}[state]
        SESSIONS.append(sess("studio", "c3d5e7f9-0000-4000-8000-%012d" % i, "%s 的第 %d 个任务" % (proj, i + 1),
                             "/Users/me/code/" + proj, status, state, proj, 4 + i % 4, "t%d" % (i // 4 + 1), i // 4 + 1,
                             i * 7, "第 %d 个 Agent 的最新一句话，长一点好看看会不会被截断或者挤到别的地方去。" % (i + 1),
                             pending=1 if state == "needs_you" else 0))
# MOCK_AGENTS=1: a long Agents list (AgentsListTests): ~120 agents, one big space with 35 finished-unread, a quiet
# space, idle ones to fold. Sessions marked seen (WS "seen" or POST …/seen) turn idle; GET /mock/seen lists them.
# MOCK_QUIET=1: a quiet space (two seen agents, nothing running) under 空闲的空间, for screenshots of its card
# (`-openQuietSpaces YES` opens it).
if os.environ.get("MOCK_QUIET"):
    for i, (slot, title, last) in enumerate((("feed", "首页信息流加下拉刷新", "下拉刷新做好了，空列表时显示占位。"),
                                             ("rss", "抓取 RSS 时跳过重复条目", "按链接去重，测试都过了。"))):
        SESSIONS.append(sess("macbook", "e4f6a8c0-0000-4000-8000-%012d" % (i + 1), title, "/Users/me/code/news",
                             "idle", "idle", "news-dev", 9, slot, i + 1, 60 * 24 * (i + 2), last, seen_at=ago(minutes=60 * 24)))

if os.environ.get("MOCK_AGENTS"):
    n = 0
    for mid, proj, sidx, mix in (("studio", "platform", 10, {"needs_you": 3, "unread": 35, "working": 12}),
                                 ("studio", "infra", 11, {"working": 2, "unread": 4, "idle": 24}),
                                 ("studio", "docs-site", 12, {"idle": 20}),
                                 ("macbook", "mobile", 13, {"unread": 5, "idle": 10}),
                                 ("server", "tools", 14, {"needs_you": 1, "working": 3, "unread": 2, "idle": 4})):
        slot = 0
        for state, count in mix.items():
            for _ in range(count):
                slot += 1
                n += 1
                status = {"working": "working", "idle": "idle", "unread": "done", "needs_you": "blocked"}[state]
                SESSIONS.append(sess(mid, "e5f7a9b1-0000-4000-8000-%012d" % n, "%s 任务 %d" % (proj, slot), "/Users/me/code/" + proj,
                                     status, state, proj, sidx, "t%d" % slot, slot, n * 3,
                                     "第 %d 个 Agent 的最新一句话，看看细列表和卡片里会不会挤在一起。" % n,
                                     pending=1 if state == "needs_you" else 0))
ORIG_STATE = {s["session_id"]: (s["state"], s["status"]) for s in SESSIONS}
SEEN = []  # session ids marked seen (GET /mock/seen)


def mark_seen(sid):
    SEEN.append(sid)
    for s in SESSIONS:
        if s["session_id"] == sid and s["state"] == "unread":
            s.update(state="idle", status="idle", seen_at=ago())


HITS = {}  # "GET /api/v1/machines" -> how many (GET /mock/hits): pull to refresh and reloads
RECONNECTS = []  # machine ids of POST …/reconnect (GET /mock/reconnects)
STARTED = []  # bodies of POST /api/v1/sessions (GET /mock/started)
START_IDS = [int(time.time()) % 10**8 * 1000]  # new sessions' ids: never reused, across runs too (the app caches chats by id)


def mock_dirs(mid, path):
    home = next((m.get("home") for m in MACHINES if m["id"] == mid), None) or "/home/me"
    path = path or home + "/code"
    names = {"code": ["weather", "blog", "photo-sync", "shop-api", "recipes"]}.get(path.rstrip("/").rsplit("/", 1)[-1], ["src", "docs"])
    return {"path": path, "parent": path.rsplit("/", 1)[0] or "/",
            "entries": [{"name": n, "path": path.rstrip("/") + "/" + n, "is_git": path.endswith("/code")} for n in names],
            "recent": [{"path": home + "/code/weather", "name": "weather", "last_used": ago(minutes=30)},
                       {"path": home + "/code/blog", "name": "blog", "last_used": ago(days=2)}]}


def machine_online(mid):
    return any(m["id"] == mid and m.get("online") for m in MACHINES)


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
    assistant_sess(ASK4, "整理这个季度的读书笔记", 60 * 50, "第 30 本整理好了。"),
]

# ---------- the assistant's schedule (Jarvis › 资料 › 定时任务) ----------
def next_at(hhmm, weekdays):
    """The routine's next run after NOW, in UTC+8 (the screenshot simulators' zone)."""
    tz = timezone(timedelta(hours=8))
    h, m = map(int, hhmm.split(":"))
    local = NOW.astimezone(tz)
    for i in range(8):
        d = (local + timedelta(days=i)).replace(hour=h, minute=m, second=0, microsecond=0)
        if d > local and (not weekdays or d.isoweekday() in weekdays):
            return d.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


# One list, like the hub's (assistant_schedule.go): every day at a time, every N minutes, or once.
SCHEDULE = [
    {"id": "morning", "title": "早报", "prompt": "给我今天的早报，一屏以内，结论先行。", "repeat": "daily", "time": "09:00",
     "weekdays": [1, 2, 3, 4, 5], "enabled": True, "last_run": ago(hours=3), "last_session_id": ASK2, "last_status": "ok",
     "run_machine_id": "server", "run_session_id": ASK2, "run_launcher": "claude",  # its own conversation (早报 · 周三)
     "next_run": next_at("09:00", [1, 2, 3, 4, 5])},
    {"id": "fu_patrol", "title": "Agent 巡检", "prompt": "看看各台电脑上的 Agent：哪些在等我、哪些卡住了，有要我处理的就列出来；都正常就回一句。",
     "repeat": "interval", "every_minutes": 30, "at": later(minutes=12), "enabled": True, "created_by": "assistant",
     "permission_mode": "auto", "effort": "low", "machine_id": "server", "last_run": ago(minutes=18), "last_status": "ok", "last_session_id": ASK1,
     "next_run": later(minutes=12)},
    {"id": "rt_weekly", "title": "周报", "prompt": "帮我起草这周的周报，先给我草稿。", "repeat": "daily", "time": "17:00", "weekdays": [5],
     "enabled": True, "created_by": "assistant", "last_run": ago(days=7), "last_status": "failed",
     "last_error": "assistant machine is offline", "next_run": next_at("17:00", [5])},
    {"id": "evening", "title": "晚间回顾", "prompt": "今天的晚间回顾。", "repeat": "daily", "time": "21:30", "weekdays": [1, 2, 3, 4, 5],
     "enabled": False, "last_run": ago(days=2), "last_status": "skipped"},
    {"id": "fu_review", "repeat": "once", "at": later(minutes=40), "next_run": later(minutes=40), "title": "会前准备 · 产品评审",
     "prompt": "产品评审前 10 分钟：整理上次纪要和要点。", "enabled": True, "created_by": "assistant", "machine_id": "server",
     "session_id": ASK1, "created_at": ago(hours=1)},
    {"id": "fu_ci", "repeat": "once", "at": later(hours=2), "next_run": later(hours=2), "title": "看看 CI 跑完没有",
     "prompt": "看一下 weather-app 的 CI，跑完把结果告诉我。", "enabled": True, "created_by": "assistant", "machine_id": "server",
     "created_at": ago(minutes=20)},
]
TASK_FIELDS = ("title", "prompt", "repeat", "at", "time", "weekdays", "every_minutes", "enabled", "launcher", "permission_mode", "effort")


def save_task(old, new):
    """What the hub keeps of an edit: the editable fields ("" = default), a session that can only be cleared, the next run."""
    t = {**old, **{k: v for k, v in new.items() if k in TASK_FIELDS}}
    for k in ("launcher", "permission_mode", "effort"):
        if not t.get(k): t.pop(k, None)
    if not new.get("session_id"): t.pop("session_id", None)
    if t.get("repeat") == "daily":
        t["next_run"] = next_at(t.get("time", "09:00"), t.get("weekdays") or [])
    elif t.get("repeat") == "interval":
        if old.get("every_minutes") != t.get("every_minutes") or not t.get("at"):
            t["at"] = later(minutes=t.get("every_minutes") or 30)
        t["next_run"] = t["at"]
    else:
        t["next_run"] = t.get("at")
    if not t.get("enabled"): t.pop("next_run", None)
    return t


RUNS = [
    {"id": "run_1", "kind": "routine", "ref_id": "morning", "title": "早报", "due": ago(hours=3), "at": ago(hours=3), "status": "ok",
     "machine_id": "server", "session_id": ASK2},
    {"id": "run_2", "kind": "followup", "ref_id": "fu_lunch", "title": "订午饭提醒", "due": ago(hours=5), "at": ago(hours=5),
     "status": "ok", "machine_id": "server", "session_id": ASK1},
    {"id": "run_3", "kind": "routine", "ref_id": "evening", "title": "晚间回顾", "due": ago(days=2), "at": ago(days=2, minutes=-45),
     "status": "skipped", "error": "Jarvis 停机，错过了时间"},
    {"id": "run_4", "kind": "routine", "ref_id": "morning", "title": "早报", "due": ago(days=1, hours=3), "at": ago(days=1, hours=3),
     "status": "ok", "machine_id": "server", "session_id": ASK2},
    {"id": "run_5", "kind": "routine", "ref_id": "rt_weekly", "title": "周报", "due": ago(days=7), "at": ago(days=7), "status": "failed",
     "error": "assistant machine is offline"},
]
MEMORY = [
    {"name": "MEMORY.md", "content": "- [早报格式](morning-format.md) — 一屏以内，结论先行", "updated_at": ago(days=1)},
    {"name": "morning-format.md", "content": "---\nname: morning-format\ndescription: 早报一屏以内\nmetadata:\n  type: feedback\n---\n\n早报一屏以内，结论先行。",
     "updated_at": ago(days=1)},
    {"name": "weekly-report.md", "content": "---\nname: weekly-report\ndescription: 周报先给草稿\nmetadata:\n  type: feedback\n---\n\n周报先给草稿，不要直接发。",
     "updated_at": ago(days=3)},
]
SCHEDULE_RAN = []  # what 立即运行 started (UI tests read it back at /mock/schedule)

# ---------- todos (待办, docs/todos.md) ----------
TODOS_INIT = [
    {
        "id": "todo_weather",
        "title": "天气 App 发 1.3 版",
        "notes": "1.3 的内容：主屏幕小组件、网络层补测试、发布说明。周五前提交审核。",
        "done_at": None,
        "created_at": ago(hours=2),
        "updated_at": ago(minutes=2),
        "agents": [
            {
                "machine_id": "macbook",
                "session_id": TESTS,
                "linked_at": ago(minutes=45),
                "released_at": None,
                "title": "补齐网络层的单元测试",
                "cwd": "/Users/me/code/weather",
                "agent": "claude",
                "start_error": "",
            },
            {
                "machine_id": "macbook",
                "session_id": RELEASE,
                "linked_at": ago(minutes=50),
                "released_at": None,
                "title": "整理发布说明",
                "cwd": "/Users/me/code/weather",
                "agent": "claude",
                "start_error": "",
            },
            {
                "machine_id": "macbook",
                "session_id": WIDGET,
                "linked_at": ago(minutes=60),
                "released_at": None,
                "title": "给天气 App 加主屏幕小组件",
                "cwd": "/Users/me/code/weather",
                "agent": "claude",
                "start_error": "",
            },
            {
                "machine_id": "macbook",
                "session_id": "a1f3c2d4-0000-4000-8000-000000000009",
                "linked_at": ago(hours=2),
                "released_at": ago(hours=1),
                "title": "初步调研 WidgetKit API",
                "cwd": "/Users/me/code/weather",
                "agent": "claude",
                "start_error": "",
            },
        ],
    },
    {
        "id": "todo_photos",
        "title": "照片同步脚本支持 HEIC",
        "notes": "计划写好了，等你批准后开始改。",
        "done_at": None,
        "created_at": ago(hours=3),
        "updated_at": ago(minutes=6),
        "agents": [
            {
                "machine_id": "studio",
                "session_id": PHOTOS,
                "linked_at": ago(minutes=30),
                "released_at": None,
                "title": "照片同步脚本支持 HEIC",
                "cwd": "/Users/me/code/photo-sync",
                "agent": "claude",
                "start_error": "",
            },
        ],
    },
    {
        "id": "todo_blog",
        "title": "博客迁移到 Astro",
        "notes": "32 篇文章都能正常构建，旧链接全部加了 301 跳转。",
        "done_at": None,
        "created_at": ago(hours=5),
        "updated_at": ago(minutes=12),
        "agents": [
            {
                "machine_id": "studio",
                "session_id": BLOG,
                "linked_at": ago(hours=2),
                "released_at": None,
                "title": "博客迁移到 Astro",
                "cwd": "/Users/me/code/blog",
                "agent": "claude",
                "start_error": "",
            },
        ],
    },
    {
        "id": "todo_orders",
        "title": "排查 /orders 接口变慢",
        "notes": "慢在订单列表的 N+1 查询，正在改成一次 JOIN 并补索引。",
        "done_at": None,
        "created_at": ago(hours=6),
        "updated_at": ago(minutes=1),
        "agents": [
            {
                "machine_id": "server",
                "session_id": ORDERS,
                "linked_at": ago(hours=1),
                "released_at": None,
                "title": "排查 /orders 接口变慢",
                "cwd": "/home/me/code/shop-api",
                "agent": "codex",
                "start_error": "",
            },
        ],
    },
    {
        "id": "todo_review",
        "title": "检查 PR #42",
        "notes": "PR #42 看完了：逻辑没问题，留了两条关于命名的小建议。",
        "done_at": None,
        "created_at": ago(hours=8),
        "updated_at": ago(minutes=95),
        "agents": [
            {
                "machine_id": "macbook",
                "session_id": REVIEW,
                "linked_at": ago(hours=3),
                "released_at": None,
                "title": "检查 PR #42 的改动",
                "cwd": "/Users/me/code/weather",
                "agent": "claude",
                "start_error": "",
            },
        ],
    },
    {
        "id": "todo_nas",
        "title": "给 NAS 配自动备份",
        "notes": "配置 NAS 的定期备份任务。",
        "done_at": None,
        "created_at": ago(days=1),
        "updated_at": ago(days=1),
        "agents": [],
    },
    {
        "id": "todo_done_1",
        "title": "重构邮件解析模块",
        "notes": "支持多部分邮件与附件提取。",
        "done_at": ago(days=1),
        "created_at": ago(days=3),
        "updated_at": ago(days=1),
        "agents": [],
    },
    {
        "id": "todo_done_2",
        "title": "修复登录页在 Safari 上的样式问题",
        "notes": "兼容 Safari 17 的 flex 布局。",
        "done_at": ago(days=2),
        "created_at": ago(days=4),
        "updated_at": ago(days=2),
        "agents": [],
    },
    {
        "id": "todo_done_3",
        "title": "升级 Tailwind CSS 到 4.0",
        "notes": "升级依赖并调整样式引用。",
        "done_at": ago(days=3),
        "created_at": ago(days=6),
        "updated_at": ago(days=3),
        "agents": [],
    },
]
TODOS = copy.deepcopy(TODOS_INIT)


def active_todo_for_session(mid, sid):
    for t in TODOS:
        if t.get("done_at"):
            continue
        for a in t.get("agents", []):
            if a.get("machine_id") == mid and a.get("session_id") == sid and not a.get("released_at"):
                return {"id": t["id"], "title": t["title"]}
    return None


def decorated_session(s):
    s_copy = dict(s)
    todo = active_todo_for_session(s.get("machine_id"), s.get("session_id"))
    if todo:
        s_copy["todo"] = todo
    else:
        s_copy.pop("todo", None)
    return s_copy


def format_todo(t):
    t_copy = copy.deepcopy(t)
    curr = [a for a in t_copy.get("agents", []) if not a.get("released_at")]
    curr.sort(key=lambda a: a.get("linked_at") or "", reverse=True)
    past = [a for a in t_copy.get("agents", []) if a.get("released_at")]
    past.sort(key=lambda a: a.get("released_at") or "", reverse=True)
    t_copy["agents"] = curr + past
    return t_copy


def sorted_todos():
    tds = copy.deepcopy(TODOS)
    tds.sort(key=lambda t: t.get("created_at") or "", reverse=True)
    return [format_todo(t) for t in tds]


def push_todos():
    for send, _ in list(LIVE):
        try:
            send({"type": "todos"})
        except Exception:
            pass


def is_read_only(h):
    auth = h.headers.get("Authorization", "")
    token_str = auth.replace("Bearer ", "").strip()
    tok = next((t for t in TOKENS if t.get("token") == token_str or t.get("id") == token_str), None)
    if tok and tok.get("role") in ("answer", "watch", "readonly"):
        return True
    if h.headers.get("X-Mock-Role") in ("answer", "watch", "readonly"):
        return True
    return False

# ---------- the knowledge base (Jarvis › 知识库, docs/knowledge.md) ----------
KB = {
    "projects": [
        {"kind": "projects", "id": "weather-app", "name": "天气 App", "one_line": "小组件时间线已按小时刷新，等中号布局的降雨条",
         "importance": "P0", "relevance": 92, "tags": ["iOS", "小组件"], "updated": ago(minutes=20), "extra": "short · active"},
        {"kind": "projects", "id": "blog", "name": "个人博客迁移到 Astro", "one_line": "32 篇文章迁完，旧链接都加了 301",
         "importance": "P2", "relevance": 55, "tags": ["前端"], "updated": ago(days=1), "extra": "short · done"},
        {"kind": "projects", "id": "reading", "name": "读书笔记", "one_line": "每季度整理一次", "importance": "P3",
         "relevance": 30, "tags": ["个人"], "updated": ago(days=3), "extra": "long · active"},
        {"kind": "projects", "id": "promo", "name": "促销邮件", "importance": "P3", "relevance": 5, "updated": ago(days=6),
         "extra": "short · ignored"},
    ],
    "people": [{"kind": "people", "id": "p_han", "name": "韩梅梅", "one_line": "产品经理，周五约了午饭", "importance": "P1",
                "relevance": 70, "tags": ["同事"], "updated": ago(hours=28)}],
    "chats": [{"kind": "chats", "id": "oc_team", "name": "天气 App 项目群", "one_line": "今天在讨论降雨条的样式", "importance": "P1",
               "relevance": 64, "updated": ago(hours=2), "extra": "group"}],
    "docs": [{"kind": "docs", "id": "doc_widget", "name": "小组件设计稿 v3", "one_line": "小号、中号两种布局", "importance": "P2",
              "relevance": 60, "tags": ["iOS"], "updated": ago(days=2)}],
    "meetings": [{"kind": "meetings", "id": "m_review", "name": "小组件评审", "one_line": "降雨条改成逐小时柱状", "importance": "P1",
                  "relevance": 75, "updated": ago(days=5)}],
    "events": [],
}
KB_TAGS = {"projects": ["iOS", "前端", "个人"]}
KB_UPDATES = []  # what 打标 saved (read back at /mock/kb_updates)
KB_DETAIL = {
    ("projects", "weather-app"): {"summary": "# 天气 App\n\n给自己用的天气 App 和桌面小组件。\n\n- **现在**：小组件按小时刷新\n- **下一步**：中号布局的降雨条",
                                  "log": ["- 2026-10-24 09:10 · 飞书 · 天气 App 项目群 · 定了小组件先做小号和中号",
                                          "- 2026-10-26 15:30 · 会议 · 小组件评审 · 降雨条改成逐小时柱状",
                                          "- 2026-10-28 11:02 · Agent · 数据层 · MacBook · 数据层复用逐小时预报",
                                          "- 2026-10-29 18:40 · 邮件 · 设计稿 v3 发来了",
                                          "- 2026-10-30 10:15 · 飞书 · 韩梅梅 · 周五前要一版能装的",
                                          "- 2026-10-30 21:05 · Agent · 小组件 · MacBook · 时间线按小时刷新",
                                          "- 2026-10-31 09:30 · Agent · 小组件 · MacBook · 降雨概率格式化的测试过了"],
                                  "links": [{"kind": "people", "id": "p_han", "name": "韩梅梅", "one_line": "产品经理，周五约了午饭", "importance": "P1", "relevance": 70},
                                            {"kind": "chats", "id": "oc_team", "name": "天气 App 项目群", "one_line": "今天在讨论降雨条的样式", "importance": "P1", "relevance": 64},
                                            {"kind": "docs", "id": "doc_widget", "name": "小组件设计稿 v3", "one_line": "小号、中号两种布局", "importance": "P2", "relevance": 60}],
                                  "moments": [{"id": "mo1", "author": "王五", "text": "有人在用天气小组件吗？中号的降雨条看不太清", "at": ago(hours=30), "comments": 12, "note": "用户反馈降雨条看不清"},
                                              {"id": "mo2", "author": "匿名", "text": "推荐一个天气 App，小组件逐小时刷新", "at": ago(days=3), "comments": 3, "note": "同事推荐"},
                                              {"id": "mo3", "author": "赵六", "text": "小组件设计分享：信息密度和可读性", "at": ago(days=9), "comments": 0, "note": "设计讨论"},
                                              {"id": "mo4", "author": "孙七", "text": "iOS 小组件时间线刷新踩坑", "at": ago(days=20), "comments": 7, "note": "技术讨论"},
                                              {"id": "mo5", "author": "周八", "text": "天气数据源对比", "at": ago(days=40), "comments": 1, "note": "数据源"}],
                                  "meta": {"name": "天气 App", "importance": "P0", "relevance": 92, "tags": ["iOS", "小组件"], "kind": "short",
                                           "status": "active", "dirs": ["macbook:/Users/me/code/weather"]}},
}

# The archived messages of a chat (chats/<id>/messages/<day>.md), for search and its day page.
KB_DAYS = {
    "oc_team": {
        "2026-10-29": ["- 09:12 韩梅梅: 早，设计稿 v3 今天发", "- 09:40 我: 好，降雨条先按逐小时柱状做", "- 10:05 韩梅梅: 灰度名单我来拉"],
        "2026-10-30": ["- 10:15 韩梅梅: 周五前要一版能装的", "- 10:20 我: 可以，今晚先灰度给自己", "  明天再给你", "- 10:22 韩梅梅: 👍"],
    },
}


def kb_messages(cid):
    """(day, line number, text) of every message of a chat, continuation lines folded in."""
    out = []
    for day, lines in sorted(KB_DAYS.get(cid, {}).items()):
        for i, l in enumerate(lines):
            if l.startswith("- "): out.append([day, i + 3, l[2:]])  # after the "# title" line and a blank one
            elif out: out[-1][2] += "\n" + l.strip()
    return out


def kb_match(text, q):
    return all(w in text.lower() for w in q.lower().split())


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


# Whole texts of the outputs pages carry cut (GET …/items/{id}/full), by item id.
FULL = {}


def long_tool(name, inp, whole, ts_min):
    """A tool call whose output the hub cut for the page, the way it does past 4 KB."""
    items = tool(name, inp, "", ts_min)
    data = whole.encode()
    end = data.rfind(b"\n", 0, 4096)
    items[1]["text"] = data[:end].decode() + "\n…（已截断，点开看全文，共 %d 字节）" % len(data)
    items[1]["full_size"] = len(data)
    FULL[items[1]["id"]] = {k: v for k, v in items[1].items() if k != "full_size"} | {"text": whole}
    return items


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
    + long_tool("Bash", {"command": "xcodebuild test -scheme Weather -only-testing:WeatherTests/APITests", "description": "跑网络层现有的测试"},
                "".join("Test Case '-[WeatherTests.APITests test%03d_fetchForecastRetries]' passed (0.%03d seconds).\n" % (i, i % 1000) for i in range(240))
                + "Test Suite 'APITests' passed.\n    Executed 240 tests, with 0 failures (0 unexpected) in 4.812 seconds\n", 3)
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
ITEMS[REVIEW] = [it("user", "text", 100, text="看一下 PR #42")] + [it("assistant", "text", 95, text="PR #42 看完了：逻辑没问题，留了两条关于命名的小建议。")] + [
    it("system", "recap", 30, text="在审 PR #42：逻辑没问题，留了两条命名建议（`fetchAll` → `loadAll`，`tmp` → `cached`）。下一步等作者改完再看一遍，顺便确认新加的重试测试在 CI 上稳定。")]
# MOCK_RECAPS=1: three recaps in a row on the review chat (one line, newest first in its drawer).
if os.environ.get("MOCK_RECAPS"):
    ITEMS[REVIEW] += [it("system", "recap", 11, text="作者改了 `fetchAll` 的命名，`tmp` 还没改。CI 上新的重试测试跑了 3 次都过。"),
                      it("system", "recap", 3, text="两条建议都改完了，CI 绿。等你看一眼就可以合。")]
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
ITEMS[ASK4] = [x for n in range(1, 31) for x in (
    it("user", "text", 60 * 50 + 61 - 2 * n, text=f"第 {n} 本也整理一下"),
    it("assistant", "text", 60 * 50 + 60 - 2 * n, text=f"第 {n} 本整理好了：\n\n- 三个主要观点\n- 两段摘抄\n- 一条和上一本的联系\n\n要接着整理第 {n + 1} 本吗？"))]

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

if os.environ.get("MOCK_SETTINGS_INPUT"):
    INTERACTIONS.append({
        "id": "ix_settings_input", "machine_id": "macbook", "session_id": REVIEW,
        "session_title": "检查 PR #42 的改动", "kind": "permission", "tool_name": "jarvis_settings",
        "summary": "添加邮箱 me@example.com，填上授权码后先连一遍再保存",
        "created_at": ago(minutes=1), "expires_at": later(minutes=15),
        "fields": [{"key": "password", "label": "密码 / 授权码", "placeholder": "填写邮箱授权码", "secret": True}],
    })

# MOCK_CODEX=1: the Codex session's question (Plan mode, two questions) and an approval, as a node reads them off the
# Codex TUI (docs/codex.md「提问和审批」); the first answer to the approval is refused as if the computer's input box held
# a draft (409 input_draft), the next one goes through (GET /mock/resolved has them all).
if os.environ.get("MOCK_CODEX"):
    INTERACTIONS.append(
        {"id": "cxa_orders_curl", "machine_id": "server", "session_id": ORDERS, "session_title": "排查 /orders 接口变慢",
         "cwd": "/home/me/code/shop-api", "kind": "permission", "agent": "codex", "created_at": ago(minutes=1),
         "expires_at": later(hours=24), "tool_name": "Bash",
         "summary": "curl -s 'http://localhost:8080/orders?page=1' -o /dev/null -w '%{time_total}'",
         "tool_input": {"command": "curl -s 'http://localhost:8080/orders?page=1' -o /dev/null -w '%{time_total}'"},
         "reason": "Allow this command to access localhost:8080 over the network?",
         "always_label": "以后不再问以 curl -s 开头的命令"})
    INTERACTIONS.append(
        {"id": "cxq_orders_plan", "machine_id": "server", "session_id": ORDERS, "session_title": "排查 /orders 接口变慢",
         "cwd": "/home/me/code/shop-api", "kind": "question", "agent": "codex", "created_at": ago(minutes=2),
         "expires_at": later(hours=24), "tool_name": "request_user_input", "tool_use_id": "call_plan1",
         "questions": [
             {"question": "订单列表的索引怎么加？", "header": "索引", "multi_select": False, "options": [
                 {"label": "(user_id, created_at) 联合索引 (Recommended)", "description": "覆盖按用户分页的查询，写入稍慢一点。"},
                 {"label": "只给 user_id 加索引", "description": "改动最小，排序仍要回表。"}]},
             {"question": "改完以后怎么验证？", "header": "验证", "multi_select": False, "options": [
                 {"label": "预发环境压测", "description": "用上周的流量回放跑 10 分钟。"},
                 {"label": "灰度 10% 上线", "description": "看 30 分钟的 p95 再全量。"}]}]})

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

# MOCK_SCREEN_MULTI=1: a multi-select AskUserQuestion read off the terminal (Claude Code 2.1.316), the second of
# three questions: two boxes already ticked at the computer, a few words in its free-text field, "Next" to send it.
if os.environ.get("MOCK_SCREEN_MULTI"):
    INTERACTIONS.append(
        {"id": "scr_macbook_review_5c1d2e3f4a5b", "machine_id": "macbook", "session_id": REVIEW, "session_title": "检查 PR #42 的改动",
         "cwd": "/Users/me/code/weather", "kind": "screen", "created_at": ago(minutes=1), "expires_at": later(hours=24),
         "screen": "←  ☒ 环境  ☐ 平台  ☐ 通知  ✔ Submit  →\n哪些平台要一起发？\n❯ 1. [✔] 手机\n     iPhone 版本。\n"
                   "  2. [ ] 手表\n     Watch 版本。\n  3. [✔] 电脑\n     Mac 版本。\n  4. [✔] 平板也看一眼\n     Next\n"
                   "──────\n  5. Chat about this",
         "dialog": {"question": "哪些平台要一起发？", "rev": "5c1d2e3f4a5b", "multi": True, "submit": "Next",
                    "steps": [{"label": "环境", "done": True}, {"label": "平台", "done": False}, {"label": "通知", "done": False}],
                    "options": [
                        {"key": "1", "label": "手机", "detail": "iPhone 版本。", "keys": ["1"], "checked": True},
                        {"key": "2", "label": "手表", "detail": "Watch 版本。", "keys": ["2"]},
                        {"key": "3", "label": "电脑 (Recommended)", "detail": "Mac 版本。", "keys": ["3"], "checked": True},
                        {"key": "4", "label": "Type something", "kind": "type_something", "keys": ["4"], "checked": True,
                         "text": "平板也看一眼"},
                        {"key": "5", "label": "Chat about this", "kind": "chat", "keys": ["5"]}]}})

# MOCK_SCREEN_RAW=1: a terminal waiting with no dialog to read, only tool output above the input box (its tree
# glyphs must not show on the Agents card).
if os.environ.get("MOCK_SCREEN_RAW"):
    INTERACTIONS.append(
        {"id": "scr_macbook_raw", "machine_id": "macbook", "session_id": REVIEW, "session_title": "lark-connector 自动回复逻辑",
         "cwd": "/Users/me/code/weather", "kind": "screen", "created_at": ago(minutes=1), "expires_at": later(hours=24),
         "screen": "⏺ Bash(git stash && git rebase origin/master)\n  ⎿  Saved working directory and index state\n"
                   "     … 40 more files changed\n  ⎿  (a git step in this command can move the working tree, so the hook stopped it)\n"
                   "\n" + "─" * 60 + "\n> \n" + "─" * 60 + "\n  ? for shortcuts"})

# MOCK_SCREEN_SECRET=1: the terminal asks for a password (a card with a password field).
if os.environ.get("MOCK_SCREEN_SECRET"):
    INTERACTIONS.append(
        {"id": "scr_macbook_review", "machine_id": "macbook", "session_id": REVIEW, "session_title": "检查 PR #42 的改动",
         "cwd": "/Users/me/code/weather", "kind": "screen", "created_at": ago(minutes=1), "expires_at": later(hours=24),
         "secret": True, "screen": "⏺ Bash(sudo make install)\n  ⎿  Running…\n\n[sudo] password for me:"})

# The terminal tab (GET …/screen): a made-up shell. MOCK_SCREEN_FAIL_AFTER=N: N seconds after the first read every
# read fails (503) — the stale bar over the terminal; MOCK_SCREEN_FAIL_AFTER=N:M recovers M seconds later.
TERMINAL = "\n".join(
    ["$ swift test --filter NetworkTests", "Building for debugging...", "[1/4] Compiling Weather Forecast.swift",
     "[2/4] Compiling Weather Client.swift", "[3/4] Emitting module Weather", "[4/4] Linking WeatherPackageTests",
     "Build complete! (12.48s)", "Test Suite 'NetworkTests' started at 2026-10-29 10:02:11.204",
     "Test Case '-[NetworkTests testRetriesOnTimeout]' passed (0.412 seconds).",
     "Test Case '-[NetworkTests testDecodesForecastWithMissingHumidityAndFallsBackToTheLastKnownValue]' passed (0.031 seconds).",
     "Test Case '-[NetworkTests testCachesResponsesForTenMinutes]' passed (0.006 seconds).",
     "Test Suite 'NetworkTests' passed at 2026-10-29 10:02:11.660.",
     "\t Executed 3 tests, with 0 failures (0 unexpected) in 0.449 (0.455) seconds", "$ git status --short",
     " M Sources/Weather/Client.swift", " M Tests/WeatherTests/NetworkTests.swift", "$ "])
SCREEN_FIRST = []

# MOCK_TERMINAL_STREAM=1: the hub says terminal_stream and answers terminal_subscribe with frames of a made-up Claude
# Code screen (96×24) as herdr's observer sends them: a whole frame, then the spinner ticking once a second
# (MOCK_TERMINAL_INTERVAL seconds: 0.02 plays a recording through at once).
TERMINAL_STREAM = bool(os.environ.get("MOCK_TERMINAL_STREAM"))


def terminal_frames(cols=96, rows=24, ticks=40):
    if os.environ.get("MOCK_TERMINAL_FIXTURE"):  # frames recorded from herdr (apple/JarvisTests/Fixtures/terminal-observe.jsonl)
        recorded = [json.loads(l) for l in open(os.environ["MOCK_TERMINAL_FIXTURE"]) if l.startswith("{")]
        return [{"seq": i + 1, "full": f["full"], "cols": f["width"], "rows": f["height"], "data": f["bytes"]}
                for i, f in enumerate(recorded)]
    E = "\x1b["
    o, g, d, b, p, r = E + "38;5;209m", E + "32m", E + "2m", E + "1m", E + "38;5;141m", E + "0m"
    box = "─" * (cols - 2)
    screen = [
        o + "✻" + r + " " + b + "Welcome to Claude Code" + r,
        "",
        b + "> " + r + "给天气 App 加主屏幕小组件，显示当前温度和未来 3 小时的预报",
        "",
        "⏺ 我先看一下现有的 Widget 扩展和数据来源。",
        "",
        "⏺ " + b + "Read" + r + "(WeatherWidget/Provider.swift)",
        "  ⎿  Read " + b + "84" + r + " lines",
        "",
        "⏺ " + b + "Bash" + r + "(swift test --filter WidgetTests)",
        "  ⎿  " + g + "Test Suite 'WidgetTests' passed" + r + " at 2026-10-29 10:02:11.660.",
        "     Executed 6 tests, with 0 failures (0 unexpected) in 0.412 (0.418) seconds; the timeline provider returns 4 entries",
        "",
        "⏺ 小组件已经能显示当前温度和未来 3 小时的预报，" + E + "4m" + "小号" + r + "和" + E + "4m" + "中号" + r + "两种尺寸都做了。",
        "",
        o + "✻ Brewing… " + r + d + "(%ds · ↓ 1.2k tokens · esc to interrupt)" + r,
        "",
        d + "╭" + box + "╮" + r,
        d + "│" + r + " > " + " " * (cols - 5) + d + "│" + r,
        d + "╰" + box + "╯" + r,
        "  " + p + "⏵⏵ accept edits on" + r + d + " (shift+tab to cycle)" + r,
    ]
    out = []
    for n in range(ticks):
        rows_now = [line % (12 + n) if "%d" in line else line for line in screen]
        if n == 0:
            body = E + "2J" + "".join(E + "%d;1H" % (i + 1) + line for i, line in enumerate(rows_now))
        else:
            i = 15
            body = E + "%d;1H" % (i + 1) + rows_now[i] + E + "K"
        data = E + "?2026h" + E + "?25l" + body + E + "19;4H" + E + "?25h" + E + "?2026l"
        out.append({"seq": n + 1, "full": n == 0, "cols": cols, "rows": rows,
                    "data": base64.b64encode(data.encode()).decode()})
    return out

# MOCK_TERMINAL_CONTROL=1 (with MOCK_TERMINAL_STREAM): the hub also says terminal_control; a subscribe with control
# gets terminal_control active and the made-up screen laid out at the phone's grid (re-laid at each new size), what is
# typed shows in its prompt, and every terminal_input is kept (GET /mock/terminal_input). MOCK_TERMINAL_BUSY=1:
# another program holds the terminal — busy until the app subscribes with takeover.
TERMINAL_CONTROL = bool(os.environ.get("MOCK_TERMINAL_CONTROL"))
TERMINAL_INPUT = []


def cell_width(ch):
    return 2 if unicodedata.east_asian_width(ch) in "WF" else 1


def phone_screen(cols, rows, typed="", tick=0):
    """The made-up Claude screen at cols × rows, lines folded the way Claude lays them out at that width."""
    E = "\x1b["
    o, g, d, b, p, r = E + "38;5;209m", E + "32m", E + "2m", E + "1m", E + "38;5;141m", E + "0m"
    text = [
        (o, "✻ Welcome to Claude Code"), ("", ""),
        (b, "> 给天气 App 加主屏幕小组件，显示当前温度和未来 3 小时的预报"), ("", ""),
        ("", "⏺ 我先看一下现有的 Widget 扩展和数据来源。"), ("", ""),
        ("", "⏺ Read(WeatherWidget/Provider.swift)"), ("", "  ⎿  Read 84 lines"), ("", ""),
        ("", "⏺ Bash(swift test --filter WidgetTests)"),
        (g, "  ⎿  Test Suite 'WidgetTests' passed at 2026-10-29 10:02:11.660."),
        ("", "     Executed 6 tests, with 0 failures (0 unexpected) in 0.412 seconds"), ("", ""),
        ("", "⏺ 小组件已经能显示当前温度和未来 3 小时的预报，小号和中号两种尺寸都做了。"), ("", ""),
        (o, "✻ Brewing… (%ds · esc to interrupt)" % (12 + tick)), ("", ""),
    ]
    lines = []
    for style, t in text:
        indent = len(t) - len(t.lstrip(" ⏺⎿>✻")) if t else 0
        cur, w = "", 0
        for ch in t:
            if w + cell_width(ch) > cols:
                lines.append(style + cur + r); cur, w = " " * min(indent, 6), min(indent, 6)
            cur += ch; w += cell_width(ch)
        lines.append(style + cur + r)
    prompt = "> " + typed
    box = [d + "─" * cols + r, prompt, d + "─" * cols + r, "  " + p + "⏵⏵ accept edits on" + r + d + " (shift+tab to cycle)" + r]
    lines = (lines + box)[-rows:]
    return lines, len(lines) - 3


PREFS = {"input": True, "done": "long", "live_activity": True, "lock_details": False, "long_turn_seconds": 60, "done_delay_seconds": 60,
         "island": {"preset": "a", "left": "", "right": "", "machine": ""}}  # one device is enough
RESOLVED = []  # resolve calls, newest last (GET /mock/resolved)
TOKENS = [  # 设置 › 设备 (GET/POST/DELETE /api/v1/auth/tokens)
    {"id": "dev_phone", "name": "我的 iPhone", "kind": "phone", "role": "full", "created_at": ago(days=12), "last_seen": ago()},
    {"id": "dev_watch", "name": "Apple Watch", "kind": "watch", "role": "answer", "created_at": ago(days=12), "last_seen": ago(hours=3)},
    {"id": "dev_ipad", "name": "iPad", "kind": "phone", "role": "full", "created_at": ago(days=40), "last_seen": ago(days=20),
     "revoked_at": ago(days=2)},
]
def _claude_settings():  # 设置 › Claude Code 设置: the node's whitelist (server/internal/node/claudeconfig.go)
    return [
        {"key": "awaySummaryEnabled", "title": "离开回顾（recap）", "detail": "离开 5 分钟以上回来时，Claude 先写一段进展回顾",
         "type": "bool", "default": True, "value": None},
        {"key": "alwaysThinkingEnabled", "title": "总是深度思考", "detail": "每轮都开启 extended thinking", "type": "bool",
         "default": False, "value": True},
        {"key": "effortLevel", "title": "思考强度", "detail": "越高越慢、越仔细", "type": "enum",
         "options": ["low", "medium", "high", "xhigh", "max"], "default": "high", "value": None},
        {"key": "language", "title": "回复语言", "detail": "Claude 回复用的语言，比如「中文」", "type": "string", "default": "", "value": "中文"},
    ]


def _codex_settings():  # 设置 › Codex 设置: the node's whitelist (server/internal/node/codexconfig.go)
    return [
        {"key": "model", "title": "模型", "detail": "没指定模型时 Codex 用哪个，比如 gpt-5.5", "type": "string", "default": "", "value": "gpt-5.5"},
        {"key": "model_reasoning_effort", "title": "思考强度", "detail": "越高越慢、越仔细", "type": "enum",
         "options": ["minimal", "low", "medium", "high", "xhigh"], "default": "medium", "value": "high"},
        {"key": "approval_policy", "title": "什么时候问你", "detail": "执行命令前要不要先确认；never 一律不问", "type": "enum",
         "options": ["untrusted", "on-failure", "on-request", "never"], "default": "on-request", "value": None},
        {"key": "hide_agent_reasoning", "title": "隐藏思考过程", "detail": "不显示模型的思考事件", "type": "bool", "default": False, "value": None},
    ]


CLAUDE_SETTINGS = {}  # machine id → its settings, made on first use
CODEX_SETTINGS = {}
KEYS = []  # bodies of …/keys (the terminal's key bar and text field; GET /mock/keys)
UPLOADS = []  # files received (GET /mock/uploads)
SENT = []  # bodies of sends, one per client_msg_id (GET /mock/sent)

INBOX = [
    {"id": "lark:f1:100", "source": "lark", "kind": "dm", "account": "lark", "account_name": "飞书", "title": "韩梅梅",
     "sender": "韩梅梅", "preview": "周五的发布能赶上吗？下午我要和客户确认时间", "count": 2, "started_at": ago(minutes=40),
     "updated_at": ago(minutes=18), "tier": "action", "summary": "问周五的发布能不能赶上，下午要和客户确认",
     "reason": "直接问你，有时间要求", "read": False,
     "lark": {"feed_id": "f1", "is_group": False, "start_position": "100"},
     "agent": {"state": "drafted", "task": "查发布分支和剩余的阻塞问题，回复周五能否发布", "draft": "能赶上。剩下两个阻塞问题今天都会合进去，周四晚上出候选包，周五上午发。有变化我第一时间告诉你",
               "note": "release 分支还剩 2 个阻塞问题，都已有 MR 在评审；上次发版从候选包到上线用了半天。"}},
    {"id": "lark:f2:200", "source": "lark", "kind": "mention", "account": "lark", "account_name": "飞书", "title": "设计评审",
     "sender": "李雷", "preview": "@你 新版图标放在这里了，下午评审前看一眼？", "count": 3, "started_at": ago(minutes=90),
     "updated_at": ago(minutes=55), "tier": "action", "summary": "李雷请你在下午评审前看新版图标",
     "reason": "@了你，下午就要用", "read": False,
     "agent": {"state": "running", "task": "打开新版图标的文档，整理改动和意见"},
     "lark": {"feed_id": "f2", "is_group": True, "start_position": "200", "mention_position": "202"}},
    {"id": "moment:mp1:mn1", "source": "lark", "kind": "moment", "account": "feishu", "account_name": "飞书",
     "title": "换新电脑以后大家都怎么迁移数据", "sender": "林小雨", "preview": "我用的迁移助手，一晚上就好了，你是不是开了 FileVault",
     "count": 3, "started_at": ago(hours=2), "updated_at": ago(minutes=25), "tier": "fyi",
     "summary": "林小雨在你的removed帖子下回复了迁移数据的办法", "reason": "评论你的帖子",
     "moment": {"post_id": "mp1", "notice": "comment", "reply_to": "mc3", "my_post": True, "post_text": "换新电脑以后大家都怎么迁移数据？迁移助手跑了一下午还没好"}},
    {"id": "mail:acc1:t1:501", "source": "mail", "kind": "mail", "account": "acc1", "account_name": "个人邮箱",
     "title": "下周的分享能不能提前到周二？", "sender": "Lucy Wang", "preview": "Hi，周四我临时要出差，分享能不能挪到周二下午……",
     "count": 1, "started_at": ago(hours=3), "updated_at": ago(hours=3), "tier": "action",
     "summary": "Lucy 想把下周的分享从周四挪到周二下午", "reason": "需要你回复确认", "read": False,
     "mail": {"account_id": "acc1", "mailbox": "INBOX", "uids": [501], "from": {"name": "Lucy Wang", "email": "lucy@example.com"}, "direct": True}},
    {"id": "lark:f3:300", "source": "lark", "kind": "group", "account": "lark", "account_name": "飞书", "title": "周末徒步群",
     "sender": "Tom", "preview": "周六早上 8 点地铁站集合，记得带水", "count": 12, "started_at": ago(hours=2),
     "updated_at": ago(minutes=30), "tier": "fyi", "summary": "周六 8 点地铁站集合", "read": False,
     "lark": {"feed_id": "f3", "is_group": True, "start_position": "300", "read_position": "299"}},
    {"id": "mail:acc1:t2:502", "source": "mail", "kind": "mail", "account": "acc1", "account_name": "个人邮箱",
     "title": "你的订单已发货", "sender": "示例商城", "preview": "订单 2026102800123 已由快递揽收，预计周五送达",
     "count": 1, "started_at": ago(hours=5), "updated_at": ago(hours=5), "tier": "fyi", "summary": "订单已发货，周五送达",
     "read": False, "mail": {"account_id": "acc1", "mailbox": "INBOX", "uids": [502], "from": {"name": "示例商城", "email": "shop@example.com"}}},
    {"id": "mail:acc1:t3:503", "source": "mail", "kind": "mail", "account": "acc1", "account_name": "个人邮箱",
     "title": "本周精选：10 个 SwiftUI 技巧", "sender": "Weekly Digest", "preview": "这周我们整理了……", "count": 1,
     "started_at": ago(hours=8), "updated_at": ago(hours=8), "tier": "noise", "summary": "技术周刊", "read": False,
     "mail": {"account_id": "acc1", "mailbox": "INBOX", "uids": [503], "from": {"name": "Weekly Digest", "email": "news@example.com"}}},
]

# MOCK_DOCS=1: Feishu documents that @'d me (docs/inbox.md「云文档」): a Base with my rows and their status, a sheet
# whose AI 分身 proposes a status move with its draft, a docx comment.
DOC_STATES = [{"name": "待处理", "color": "#bacefd"}, {"name": "进行中", "color": "#fed4a4"},
              {"name": "待验收", "color": "#b1e8fc"}, {"name": "已完成", "color": "#c5f2bb"}]
if os.environ.get("MOCK_DOCS"):
    INBOX[1:1] = [
        {"id": "doc:n1:1", "source": "lark", "kind": "doc", "account": "feishu", "account_name": "飞书", "title": "线上问题跟进表",
         "sender": "李雷", "preview": "李雷提及了你", "count": 1, "started_at": ago(minutes=25), "updated_at": ago(minutes=25),
         "tier": "action", "summary": "李雷在缺陷表里把两条线上问题指给你跟进", "reason": "@了你，要跟进", "read": False,
         "doc": {"url": "https://example.com/base/demo", "type": "base", "notice": "mention", "notice_id": "n1",
                 "reply": {"kind": "record", "label": "评论这条记录"},
                 "rows": [
                     {"ref": "tbl1/rec1", "table": "缺陷", "title": "任务结束后消息长时间没有推送",
                      "fields": [{"name": "环境", "value": "PROD"}, {"name": "跟进人", "value": "@我"}],
                      "status": [{"field": "fld1", "name": "状态", "value": "进行中", "options": DOC_STATES}]},
                     {"ref": "tbl1/rec2", "table": "缺陷", "title": "带 PDF 的请求 Agent 读不到",
                      "fields": [{"name": "环境", "value": "STG"}],
                      "status": [{"field": "fld1", "name": "状态", "value": "待处理", "options": DOC_STATES}]}]}},
        {"id": "doc:n2:1", "source": "lark", "kind": "doc", "account": "feishu", "account_name": "飞书", "title": "发版检查清单",
         "sender": "韩梅梅", "preview": "韩梅梅提及了你", "count": 1, "started_at": ago(minutes=70), "updated_at": ago(minutes=70),
         "tier": "action", "summary": "韩梅梅问压测报告做完没有，等你更新这一行", "reason": "@了你，等结果", "read": False,
         "doc": {"url": "https://example.com/sheets/demo", "type": "sheet", "notice": "mention", "notice_id": "n2",
                 "reply": {"kind": "cell", "label": "评论这一行"},
                 "rows": [{"ref": "s1!4", "table": "Sheet1", "title": "压测报告",
                           "fields": [{"name": "负责人", "value": "@我"}, {"name": "备注", "value": "数据在附件"}],
                           "status": [{"field": "2", "name": "状态", "value": "待验收", "options": DOC_STATES}]}]},
         "agent": {"state": "drafted", "task": "查压测报告是否已完成，回复并更新状态",
                   "draft": "压测报告做完了，结论和数据都放在附件里，P99 在目标内。",
                   "note": "压测 Agent 昨晚跑完，报告已上传到附件。", "changes": [{"ref": "s1!4", "field": "2", "value": "已完成"}]}},
        {"id": "doc:n3:1", "source": "lark", "kind": "doc", "account": "feishu", "account_name": "飞书", "title": "收件箱方案评审",
         "sender": "Tom", "preview": "Tom评论了你", "count": 1, "started_at": ago(hours=2), "updated_at": ago(hours=2),
         "tier": "action", "summary": "Tom 在评论里请你周五前确认收件箱方案", "reason": "评论 @你", "read": False,
         "doc": {"url": "https://example.com/docx/demo", "type": "docx", "notice": "comment", "notice_id": "n3",
                 "reply": {"kind": "thread", "label": "回复评论"}}},
    ]

# MOCK_GITHUB_INBOX=1: the GitHub connector's items (the generic `connector` block), listed only to apps that ask with
# ?sources=all, like the hub; made-up repos and people.
if os.environ.get("MOCK_GITHUB_INBOX"):
    INBOX[1:1] = [
        {"id": "github:review12", "source": "github", "kind": "review_requested", "account": "github", "account_name": "GitHub",
         "title": "Add retries to the example uploader", "sender": "alice", "preview": "请看一下重试的退避时间", "count": 1,
         "started_at": ago(minutes=25), "updated_at": ago(minutes=25), "tier": "action",
         "summary": "alice 请你 review 上传重试的 PR，问退避时间是否合适", "reason": "请你 review", "read": False,
         "context": "GitHub PR example-org/example-repo#12：Add retries to the example uploader\n状态：open · 作者 alice · 等这些人 review：me\n"
                    "https://github.com/example-org/example-repo/pull/12\n\n描述：\n上传失败时按指数退避重试三次。\n\n"
                    "最近的评论和 review（旧的在前，共 1 条）：\n\n2026-11-02 09:00Z alice：\n请看一下重试的退避时间",
         "connector": {"thread": "example-org/example-repo#12", "label": "请你 review", "url": "https://github.com/example-org/example-repo/pull/12",
                       "reply_to": "评论到 example-org/example-repo#12（仓库可见的人都能看到）", "can_reply": True}},
        {"id": "github:ci7", "source": "github", "kind": "ci_failed", "account": "github", "account_name": "GitHub",
         "title": "CI workflow run failed for main branch", "sender": "GitHub Actions", "preview": "CI workflow run failed for main branch",
         "count": 1, "started_at": ago(hours=2), "updated_at": ago(hours=2), "tier": "action",
         "summary": "main 分支的 CI 挂了", "reason": "CI 失败", "read": False,
         "context": "仓库 example-org/example-repo 的 CI（GitHub Actions）：CI workflow run failed for main branch",
         "connector": {"thread": "notification:7", "label": "CI 失败", "url": "https://github.com/example-org/example-repo/actions"}},
    ]


def inbox_for(query):
    """The list as the hub gives it: connectors' items only with ?sources=all."""
    all_sources = (query.get("sources") or [""])[0] == "all"
    return [{k: v for k, v in x.items() if k != "context"} for x in INBOX if all_sources or "connector" not in x]


# Mail bodies (GET /api/v1/mail/messages/{aid}/{uid}); 504 is in English, for 设置 › 翻译.
INBOX.insert(next(i for i, x in enumerate(INBOX) if x["kind"] == "mail"), {"id": "mail:acc1:t4:504", "source": "mail", "kind": "mail", "account": "acc1", "account_name": "个人邮箱",
     "title": "Can we move the design review?", "sender": "Lucy Wang",
     "preview": "Hi, I'm travelling on Thursday. Could we move the design review to Tuesday afternoon?", "count": 1,
     "started_at": ago(hours=1), "updated_at": ago(hours=1), "tier": "action", "summary": "Lucy 想把设计评审挪到周二下午",
     "reason": "要你确认时间", "read": False,
     "mail": {"account_id": "acc1", "mailbox": "INBOX", "uids": [504], "from": {"name": "Lucy Wang", "email": "lucy@example.com"}, "direct": True},
     # Translated by the hub as it came in (设置 › 翻译 on this device).
     "translations": {"zh-Hans": {"subject": "设计评审能改期吗？", "preview": "你好，我周四要出差，赶不上设计评审了。能不能改到周二下午？"}}})
# MOCK_INBOX=empty: nothing in the inbox (the shell's persona button without its inbox mark).
# The hub's GitHub connector (GET /api/v1/connectors), signed in with the gh login of a machine, or not.
GITHUB_CONNECTOR = {"id": "github", "name": "GitHub", "status": "ok", "account": "alice", "via": "来自 MacBook 的 gh 登录",
                    "last_sync": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
                    "capabilities": ["status", "hosts"], "hosts": ["github.com"]}
if os.environ.get("MOCK_GITHUB") == "signed_out":
    GITHUB_CONNECTOR = {"id": "github", "name": "GitHub", "status": "not_signed_in", "via": "来自 MacBook 的 gh 登录",
                        "error": "MacBook 上的 gh 登录失效了：在那台机器上运行 gh auth login",
                        "capabilities": ["status", "hosts"], "hosts": ["github.com"]}

# 信息 (GET /api/v1/imessage…, docs/plan-connectors.md「五」): one Mac's Messages, read for the user's own eyes. Every
# name, number (+1 555 01xx) and text is made up. MOCK_IMESSAGE=denied: that Mac's node has no Full Disk Access.
IMESSAGE_DENIED = os.environ.get("MOCK_IMESSAGE") == "denied"
IMESSAGE_STATUS = {"machine": "MacBook", "status": "ok", "via": "ssh"}
if IMESSAGE_DENIED:
    IMESSAGE_STATUS = {"machine": "MacBook", "status": "no_permission", "via": "ssh",
                       "error": "macOS 不让 Jarvis 读这台 Mac 上的「信息」：要给它「完全磁盘访问权限」",
                       "guide": ["在这台 Mac 上打开「系统设置 › 通用 › 共享」", "点「远程登录」右边的 ⓘ，打开「允许远程用户完全访问磁盘」",
                                 "回到 Jarvis 刷新。注意：这样经 ssh 登录这台 Mac 的任何会话都能读整个磁盘"]}
IMESSAGE_CONNECTOR = {"id": "imessage", "name": "信息", "status": IMESSAGE_STATUS["status"], "account": "MacBook",
                      "via": "MacBook 上的「信息」", "error": IMESSAGE_STATUS.get("error"), "capabilities": ["status"]}


def _im_person(handle, name=None):
    return {"handle": handle, "name": name} if name else {"handle": handle}


_ZHANG, _LI, _ALICE = _im_person("+15550100", "张三"), _im_person("+15550101", "李四"), _im_person("alice@example.com", "Alice Example")
IMESSAGE_CHATS = [
    {"id": "imessage-0a1b2c3d4e5f", "name": "张三", "members": [_ZHANG], "services": ["SMS", "iMessage"], "last": ago(minutes=8),
     "preview": "👍 好的，周五见 🎉", "unread": 1},
    {"id": "imessage-1b2c3d4e5f60", "name": "周末徒步", "group": True, "members": [_ZHANG, _LI, _ALICE], "services": ["iMessage"],
     "last": ago(minutes=40), "preview": "张三：我也去", "unread": 2},
    {"id": "imessage-2c3d4e5f6071", "name": "12345", "members": [_im_person("12345")], "services": ["SMS"], "last": ago(hours=3),
     "preview": "【示例】验证码 123456，5 分钟内有效", "unread": 1},
    {"id": "imessage-3d4e5f607182", "name": "Alice Example", "members": [_ALICE], "services": ["iMessage"], "last": ago(hours=20),
     "preview": "[语音]"},
    {"id": "imessage-4e5f60718293", "name": "李四、+15550102", "group": True, "members": [_LI, _im_person("+15550102")],
     "services": ["iMessage"], "last": ago(days=2), "preview": "收到", "from_me": True},
    {"id": "imessage-5f6071829304", "name": "王五", "members": [_im_person("+15550103", "王五")], "services": ["iMessage"],
     "last": ago(days=4), "preview": "示例：明天把文件带上", "from_me": True},
    {"id": "imessage-607182930415", "name": "示例快递", "members": [_im_person("95500")], "services": ["SMS"], "last": ago(days=9),
     "preview": "【示例快递】您的包裹已放到驿站，取件码 ••••"},
    {"id": "imessage-718293041526", "name": "赵六", "members": [_im_person("+15550104", "赵六")], "services": ["iMessage", "RCS"],
     "last": ago(days=40), "preview": "[图片]"},
]
IMESSAGE_MESSAGES = {
    "imessage-0a1b2c3d4e5f": [
        {"id": "1", "at": ago(days=1, hours=3), "kind": "message", "sender": _ZHANG, "text": "示例：今天的快递到了吗", "service": "SMS"},
        {"id": "2", "at": ago(days=1, hours=2, minutes=55), "kind": "message", "from_me": True, "text": "到了，谢谢", "service": "SMS"},
        {"id": "3", "at": ago(minutes=50), "kind": "message", "sender": _ZHANG, "text": "示例消息：周五下午三点见，地方还是上次那家咖啡馆", "service": "iMessage"},
        {"id": "5", "at": ago(minutes=46), "kind": "message", "from_me": True, "text": "好的，我准时到", "service": "iMessage",
         "reply_to": "示例消息：周五下午三点见，地方还是上次那家咖啡馆"},
        {"id": "6", "at": ago(minutes=40), "kind": "message", "sender": _ZHANG, "service": "iMessage",
         "attachments": [{"kind": "image", "name": "IMG_0001.HEIC", "mime": "image/heic", "size": 1234567}]},
        {"id": "7", "at": ago(minutes=30), "kind": "message", "from_me": True, "text": "改一下：三点半", "service": "iMessage", "edited": True},
        {"id": "8", "at": ago(minutes=20), "kind": "message", "sender": _ZHANG, "service": "iMessage", "unsent": True},
        {"id": "9", "at": ago(minutes=12), "kind": "message", "sender": _ZHANG, "service": "iMessage", "undecoded": True},
        {"id": "10", "at": ago(minutes=8), "kind": "message", "sender": _ZHANG, "text": "👍 好的，周五见 🎉", "service": "iMessage", "unread": True},
    ],
    "imessage-1b2c3d4e5f60": [
        {"id": "12", "at": ago(hours=2), "kind": "event", "from_me": True, "text": "你把群名改成「周末徒步」"},
        {"id": "13", "at": ago(hours=1, minutes=58), "kind": "message", "sender": _LI, "text": "@张三 明天的示例行程改到十点出发"},
        {"id": "14", "at": ago(hours=1, minutes=50), "kind": "message", "sender": _ALICE, "text": "示例：带水和零食",
         "attachments": [{"kind": "file", "name": "路线.pdf", "mime": "application/pdf", "size": 345678}]},
        {"id": "15", "at": ago(hours=1, minutes=45), "kind": "message", "sender": _ALICE, "text": "还有防晒"},
        {"id": "16", "at": ago(hours=1, minutes=30), "kind": "event", "from_me": True, "text": "你把+15550102加进了群聊"},
        {"id": "17", "at": ago(minutes=42), "kind": "message", "from_me": True, "text": "我开车，能坐四个人"},
        {"id": "18", "at": ago(minutes=40), "kind": "message", "sender": _ZHANG, "text": "我也去", "unread": True},
    ],
}

if os.environ.get("MOCK_INBOX") == "empty":
    INBOX.clear()
MAIL_BODIES = {
    504: ("Hi,\n\nI'm travelling on Thursday, so I won't make the design review. Could we move it to Tuesday afternoon, "
          "say 3 pm? I've put the new icon set in the shared folder; the two open questions are the tab bar and the empty states.\n\n"
          "Thanks,\nLucy"),
    501: "Hi，周四我临时要出差，分享能不能挪到周二下午？\n\nLucy",
}
MAIL_TRANSLATIONS = {
    504: {"subject": "设计评审能改期吗？",
          "text": "你好，\n\n我周四要出差，赶不上设计评审了。能不能改到周二下午，比如 3 点？新的图标已经放在共享文件夹里了；"
                  "还没定的两个问题是标签栏和空状态。\n\n谢谢，\nLucy"},
}

# The 设计评审 chat (inbox f2): a few messages, some in English (设置 › 翻译 picks chats to translate).
LARK_CHATS = [{"id": "f2", "name": "设计评审", "is_group": True, "unread": 3, "at_me": True, "muted": False, "preview": "@你 新版图标放在这里了",
               "updated_at": ago(minutes=55)},
              {"id": "f3", "name": "周末徒步群", "is_group": True, "unread": 12, "at_me": False, "muted": False, "preview": "周六早上 8 点地铁站集合",
               "preview_sender": "Tom", "read_position": "299", "updated_at": ago(minutes=30)},
              {"id": "f1", "name": "韩梅梅", "is_group": False, "unread": 2, "at_me": False, "muted": False, "preview": "新包发你了",
               "read_position": "99", "updated_at": ago(minutes=2)}]
def _lm(pos, who, text, mine=False, mins=60):
    return {"id": "m" + pos, "position": pos, "sender_id": "u-" + who, "sender_name": who, "is_mine": mine, "type": "TEXT", "text": text,
            "created_at": ago(minutes=mins), "recalled": False, "reply_count": 0}
LARK_MESSAGES = {"f2": [
    _lm("200", "Lucy Wang", "The new icon set is in the shared folder. Could you take a look before the review this afternoon?", mins=58),
    _lm("201", "李雷", "我先看了一下，标签栏的图标偏细", mins=57),
    _lm("202", "Lucy Wang", "Agreed. I'll make the tab bar icons one weight heavier and send an update by 3 pm.", mins=56),
    _lm("203", "我", "Sounds good, thanks!", mine=True, mins=55),
]}

# The 周末徒步群 chat (inbox f3): what the chat page has to draw. Three days of history (day separators), more than
# one page (40) before the first unread (paging back, has_older), an image, a quoted reply, reactions, a bot and a
# thread with more replies than one thread page. Unread from 300 (12 messages), read up to 299.
# MOCK_LARK_LOGGED_OUT=1: every chat and thread read answers 502 with code lark_not_logged_in, like a hub whose lark-connector
# lost its Feishu session.
LARK_LOGGED_OUT = "lark: lark-connector 未登录飞书 (AUTH_SESSION_EXPIRED)"
LARK_EMOJIS = {k: {"label": label, "url": "/api/v1/lark/media/emoji-" + k}
               for k, label in (("THUMBSUP", "赞"), ("OK", "OK"), ("JIAYI", "+1"))}
LARK_MEDIA = {"trail-photo": (480, 320), "emoji-THUMBSUP": (32, 32), "emoji-OK": (32, 32), "emoji-JIAYI": (32, 32)}


def _lark_f3():
    chatter = ["这周六还去吗", "去，天气预报说是晴天", "我带两个登山杖，谁要可以借", "上次那条线下山有点滑", "这次走北边那条，缓一点",
               "几点集合？", "八点吧，早点出发人少", "我开车，能带三个人", "我要一个位置", "+1", "记得带够水，山上没有小卖部",
               "要不要中午在山顶吃", "可以，我带点水果", "我带三明治", "有人有防晒霜吗", "我有", "下雨的话改期吗", "小雨照走，大雨改期"]
    who = ["Tom", "小周", "阿杰", "Mia", "老王"]
    out, pos = [], 240
    for day in (2, 1):  # 30 messages around this time 2 days ago, 30 around this time yesterday
        for i in range(30):
            out.append(_lm(str(pos), who[i % len(who)], chatter[i % len(chatter)], mins=day * 1440 + 90 - i * 3))
            pos += 1
    # pos is 300 now: the 12 unread, today.
    photo = _lm("300", "Tom", "", mins=50)
    photo.update(type="IMAGE", parts=[{"kind": "image", "url": "/api/v1/lark/media/trail-photo",
                                       "metadata": {"width": 480, "height": 320}}])
    route = _lm("301", "Tom", "这是这次的路线，北坡上去、南坡下来，全程大约 12 公里", mins=49)
    route["reactions"] = [{"key": "THUMBSUP", "count": 4, "mine": True, "users": ["小周", "阿杰", "Mia", "我"]},
                          {"key": "OK", "count": 1, "users": ["老王"]}]
    asked = [m for m in out if m["text"] == chatter[2]][-1]  # yesterday's, quoted below
    reply = _lm("302", "Mia", "那我也带一个，上次没带膝盖疼了两天", mins=47)
    reply["reply_to"] = {"id": asked["id"], "sender_name": asked["sender_name"], "text": asked["text"]}
    bot = _lm("303", "天气助手", "周六 08:00–14:00 晴，12–21℃，西北风 3 级，紫外线强，注意防晒", mins=45)
    bot.update(sender_id="bot-weather", sender_type="bot")
    thread = _lm("304", "阿杰", "集合地点和拼车在这个话题里报名，按顺序排", mins=44)
    thread.update(reply_count=75, thread_reply_count=75)
    rest = [_lm("305", "小周", "我报名拼车", mins=40), _lm("306", "我", "我也去，带两瓶水", mine=True, mins=38),
            _lm("307", "老王", "我可能晚到半小时，你们先走", mins=35), _lm("308", "Mia", "好的，到了在群里说一声", mins=34),
            _lm("309", "Tom", "装备清单：水、午饭、外套、头灯", mins=33), _lm("310", "阿杰", "头灯也要带？", mins=32),
            _lm("311", "Tom", "周六早上 8 点地铁站集合，记得带水", mins=30)]
    rest[-2]["reactions"] = [{"key": "JIAYI", "count": 2, "users": ["小周", "Mia"]}]
    rest[1]["reactions"] = [{"key": "THUMBSUP", "count": 2, "users": ["Tom", "Mia"]}]  # on mine: chips under a bubble on the right
    out += [photo, route, reply, bot, thread] + rest
    replies = [_lm(str(n), who[n % len(who)], "报名 %d：%s" % (n, ["拼车", "自己去", "坐地铁"][n % 3]), mins=43 - n * 0.4)
               for n in range(1, 76)]
    for r in replies:
        r["id"] = "m304-" + r["position"]
    return out, {"m304": replies}


LARK_MESSAGES["f3"], LARK_THREADS = _lark_f3()

# The direct chat with 韩梅梅 (inbox f1): unread from 100, then my answers with each kind of read receipt.
LARK_MESSAGES["f1"] = [_lm(str(pos), who, text, mine=who == "我", mins=mins) for pos, who, text, mins in (
    (90, "韩梅梅", "早，周五的发布还按计划吗？", 300), (91, "我", "按计划，今天把最后两个问题合进去", 298),
    (92, "韩梅梅", "好的，合完和我说一声", 296), (93, "我", "两个都合进去了，晚上出包", 180), (94, "韩梅梅", "收到", 178),
    (95, "我", "安装包在测试群里，装好了帮忙点一下登录", 120), (96, "韩梅梅", "登录没问题，首页加载有点慢", 100),
    (97, "我", "我看一下，可能是图片没走缓存", 95), (98, "我", "找到了，缓存的 key 每次都变，已经修了", 70),
    (99, "韩梅梅", "👍", 66), (100, "韩梅梅", "周五的发布能赶上吗？下午我要和客户确认时间", 22), (101, "韩梅梅", "顺便看一下这个", 20),
    (102, "我", "能赶上，下午三点前给你确认", 8), (103, "我", "文档我晚上看", 5), (104, "我", "新包发你了", 2))]

# Read receipts of my messages (POST /api/v1/lark/conversations/{id}/read-states): (read, unread) by message id; None is
# a message lark-connector has no receipt for (left out of the answer). My messages not listed here, e.g. ones sent while
# the mock runs, are unread by everyone else in the chat (LARK_MEMBER_COUNT - 1).
LARK_MEMBER_COUNT = {"f1": 2, "f2": 6, "f3": 6}
LARK_READ_STATES = {
    "m91": (1, 0), "m93": (1, 0), "m95": (1, 0), "m97": (1, 0), "m98": (1, 0), "m102": (1, 0), "m103": (0, 1), "m104": None,
    "m282": (5, 0), "m296": (3, 2), "m306": (0, 5), "m203": (4, 1),
}
for _pos in (282, 296):  # two of mine in f3's history, next to the one from today (306)
    next(m for m in LARK_MESSAGES["f3"] if m["position"] == str(_pos)).update(sender_id="u-我", sender_name="我", is_mine=True)


def lark_read_states(feed, ids):
    """The read-states answer for my messages among ids in feed; others' and unknown ids are left out."""
    mine = {m["id"]: m for m in LARK_MESSAGES.get(feed, []) if m.get("is_mine")}
    out = []
    for mid in ids:
        if mid not in mine: continue
        state = LARK_READ_STATES.get(mid, (0, LARK_MEMBER_COUNT.get(feed, 2) - 1))
        if state is not None:
            out.append({"message_id": mid, "read_count": state[0], "unread_count": state[1]})
    return out


# Members of f2 / f3 for the @ picker (GET …/members?q=&limit=); user ids are the senders' ids in the chats. Two have
# an avatar, the rest none (the app draws their initial).
LARK_MEMBERS = {
    "f2": [("Lucy Wang", "Lucy"), ("李雷", ""), ("韩梅梅", ""), ("Tom", ""), ("小周", ""), ("我", "")],
    "f3": [("Tom", ""), ("小周", ""), ("阿杰", "杰哥"), ("Mia", ""), ("老王", ""), ("我", ""), ("天气助手", "")],
}
LARK_MEDIA.update({"avatar-tom": (96, 96), "avatar-mia": (96, 96)})
# Avatars in f3: Tom's loads, the weather bot's is gone (404: the app falls back to the initial), the rest have none.
for _m in LARK_MESSAGES["f3"]:
    if _m["sender_name"] == "Tom": _m["sender_avatar"] = "/api/v1/lark/media/avatar-tom"
    if _m["sender_id"] == "bot-weather": _m["sender_avatar"] = "/api/v1/lark/media/avatar-gone"


def lark_members(feed, q, limit):
    """A chat's members the way the hub answers: the group nickname first, else the name; q filters by either."""
    out = []
    for who, nick in LARK_MEMBERS.get(feed, []):
        if q and q.lower() not in (who + " " + nick).lower(): continue
        m = {"user_id": "bot-weather" if who == "天气助手" else "u-" + who, "name": nick or who}
        if who in ("Tom", "Mia"): m["avatar"] = "/api/v1/lark/media/avatar-" + who.lower()
        out.append(m)
    return out[:limit]


def lark_mention_parts(text, mentions):
    """The parts of a sent message: each mention's literal "@name" becomes an at part, the rest text parts."""
    parts, rest = [], text
    while mentions:
        hits = [(rest.find("@" + m["name"]), m) for m in mentions if "@" + m["name"] in rest]
        if not hits: break
        i, m = min(hits, key=lambda h: h[0])
        if i: parts.append({"kind": "text", "text": rest[:i]})
        parts.append({"kind": "at", "text": "@" + m["name"], "refId": m["user_id"]})
        rest = rest[i + len(m["name"]) + 1:]
    if rest: parts.append({"kind": "text", "text": rest})
    return parts


def lark_check_mentions(body):
    """The hub's 400 for mentions: more than 100, or one without user_id / name; None when fine."""
    ms = body.get("mentions") or []
    if len(ms) > 100: return "at most 100 mentions"
    if any(not (m.get("user_id") or "").strip() or not (m.get("name") or "").strip() for m in ms):
        return "a mention needs user_id and name"
    return None


def lark_window(msgs, q, default_limit=40):
    """A page of a chat or thread the way the hub answers: oldest first, before / after / around a position."""
    msgs = sorted(msgs, key=lambda m: int(m["position"]))
    arg = lambda k: (q.get(k) or [""])[0]
    limit = int(arg("limit") or 0)
    limit = default_limit if limit <= 0 or limit > 100 else limit
    pos = [int(m["position"]) for m in msgs]
    if arg("before"):
        end = sum(1 for p in pos if p < int(arg("before"))); start = max(0, end - limit)
    elif arg("after"):
        start = sum(1 for p in pos if p <= int(arg("after"))); end = min(len(msgs), start + limit)
    elif arg("around"):
        mid = sum(1 for p in pos if p < int(arg("around"))); start = max(0, mid - limit // 2); end = min(len(msgs), start + limit)
    else:
        end = len(msgs); start = max(0, end - limit)
    page = msgs[start:end]
    return {"messages": page, "has_older": start > 0, "has_newer": end < len(msgs),
            "older_cursor": page[0]["position"] if page else "", "newer_cursor": page[-1]["position"] if page else ""}
# ---------- area: feishu chat page (Android's Feishu chat and removed pages; made-up data only) ----------
# MOCK_LARK=1: the chats behind the inbox's Feishu items (f1 / f2 / f3) have messages covering every kind of part the
# chat page draws (rich post, mentions, links, emoji, pictures, a file, voice, video, a sticker, cards with columns and
# buttons, a table and a chart, merge-forward, a removed share, reactions, replies, a thread, system lines), with paging
# (before / after / around), people's cards, the emoji catalogue and the media (made-up PNGs, Range answered). Off by
# default: the iOS screenshots and TranslationTests read the plainer f2 above. What the app sends is kept: GET
# /mock/lark (sent / reads / reactions / card actions); POST /mock/lark/incoming?feed=f1 adds a message from someone
# else and pushes {"type":"lark"} to every socket.
import zlib as _fs_zlib
MOCK_LARK = bool(os.environ.get("MOCK_LARK"))
FS_LOG = {"sent": [], "reads": [], "reactions": [], "actions": []}
FS_EMOJI_KEYS = ["THUMBSUP", "OK", "DONE", "HEART", "SMILE", "LAUGH", "SURPRISED", "CLAP", "FIRE", "PARTY", "THINKING", "MUSCLE", "SOB", "WAVE"]
FS_EMOJI_LABELS = ["赞", "OK", "完成", "爱心", "微笑", "大笑", "惊讶", "鼓掌", "火", "庆祝", "思考", "加油", "流泪", "再见"]


def _fs_png(w, h, rgb, rgb2=None):
    """A tiny PNG: a flat colour, or a vertical gradient to rgb2."""
    raw = bytearray()
    for y in range(h):
        raw.append(0)
        t = y / max(h - 1, 1)
        c = rgb if rgb2 is None else tuple(int(a + (b - a) * t) for a, b in zip(rgb, rgb2))
        raw.extend(bytes(c) * w)

    def chunk(tag, data):
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", _fs_zlib.crc32(tag + data) & 0xffffffff)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", _fs_zlib.compress(bytes(raw), 9)) + chunk(b"IEND", b""))


def fs_media(key):
    """Bytes for /api/v1/lark/media/<key>: emoji, avatars, pictures (made-up colours)."""
    h = int(hashlib.md5(key.encode()).hexdigest()[:6], 16)
    rgb = ((h >> 16) & 0xff, (h >> 8) & 0xff, h & 0xff)
    if key.startswith("emoji-"): return _fs_png(48, 48, rgb)
    if key.startswith("avatar-"): return _fs_png(96, 96, rgb)
    if key.startswith("wide-"): return _fs_png(320, 180, rgb, (40, 40, 60))
    if key.startswith("tall-"): return _fs_png(180, 320, rgb, (250, 250, 250))
    if key.startswith("sticker-"): return _fs_png(160, 120, rgb)
    return _fs_png(240, 240, rgb, (255, 255, 255))


FS_USERS = {"韩梅梅": "u_hmm", "李雷": "u_ll", "Tom": "u_tom", "我": "u_me", "构建机器人": "u_bot"}


def _fsm(feed, pos, sender, text, minutes, parts=None, mine=False, kind="TEXT", **kw):
    sid = FS_USERS.get(sender, "u_x")
    m = {"id": "lm_%s_%d" % (feed, pos), "position": str(pos), "sender_id": sid, "sender_name": sender,
         "sender_avatar": "/api/v1/lark/media/avatar-" + sid, "sender_type": "bot" if sid == "u_bot" else "user",
         "is_mine": mine, "type": kind, "text": text, "created_at": ago(minutes=minutes), "recalled": False,
         "reply_count": 0, "parts": parts if parts is not None else [{"kind": "text", "text": text}]}
    m.update(kw)
    return m


def _fst(text, **meta):
    return {"kind": "text", "text": text, **({"metadata": meta} if meta else {})}


def _fsblk(*chain):
    return [{"id": i, "type": t, **({"listType": 0, "start": 1} if t.endswith("-list") else {})} for i, t in chain]


def _fsmd(text, **style):
    return {"tag": "markdown", "children": [{"tag": "plain_text", "text": text, **({"style": style} if style else {})}]}


def _fscol(label, value, color=None):
    return {"tag": "column", "style": {"weight": 1, "backgroundStyle": "grey"}, "children": [
        _fsmd(label, textColor="grey-600"),
        _fsmd(value, textAttributes=["bold"], textSize="large", **({"textColor": color} if color else {}))]}


FS_CARD = {"kind": "card", "text": "发布检查", "metadata": {
    "theme": "green",
    "header": {"tag": "card_header", "children": [
        {"tag": "standard_icon", "token": "succeed_filled"},
        {"tag": "plain_text", "text": "v2.3 发布检查通过"},
        {"tag": "text_tag", "text": "已通过", "style": {"color": "green"}},
        {"tag": "plain_text", "text": "构建 #1842 · 用时 6 分 12 秒"}]},
    "body": {"tag": "body", "children": [
        {"tag": "markdown", "children": [{"tag": "plain_text", "text": "所有检查都过了，<font color='green'>可以发布</font>。详情见 "},
                                         {"tag": "link", "url": "https://example.com/builds/1842", "text": "构建记录"}]},
        {"tag": "column_set", "style": {"flexMode": "none", "horizontalSpacingValue": 8}, "children": [
            _fscol("单元测试", "1,204 / 1,204"), _fscol("覆盖率", "82.4%", "green"), _fscol("包大小", "+0.3 MB", "orange")]},
        {"tag": "hr"},
        {"tag": "collapsible_panel", "expanded": False, "header": _fsmd("变更（3）"), "children": [{"tag": "list", "items": [
            {"type": "ul", "level": 0, "children": [{"tag": "plain_text", "text": "天气卡片支持深色模式"}]},
            {"type": "ul", "level": 0, "children": [{"tag": "plain_text", "text": "修复定位权限弹窗重复出现"}]},
            {"type": "ul", "level": 1, "children": [{"tag": "code_span", "text": "LocationManager"}, {"tag": "plain_text", "text": " 只请求一次"}]}]}]},
        {"tag": "action", "children": [
            {"tag": "button", "style": {"type": "primary"}, "actionId": "approve", "actionMethod": 2, "children": [{"tag": "plain_text", "text": "批准发布"}]},
            {"tag": "button", "style": {"type": "default"}, "url": "https://example.com/builds/1842", "children": [{"tag": "plain_text", "text": "查看详情"}]},
            {"tag": "overflow", "options": [{"text": "重新构建", "actionId": "rebuild", "actionMethod": 2}, {"text": "打开流水线", "url": "https://example.com/pipeline"}]}]},
        {"tag": "note", "children": [{"tag": "standard_icon", "token": "time_outlined"}, {"tag": "plain_text", "text": "由构建机器人发送"}]}]}}}

FS_TABLE_CARD = {"kind": "card", "text": "本周值班", "metadata": {
    "theme": "blue", "title": "本周值班",
    "body": {"tag": "body", "children": [
        {"tag": "table", "children": [
            {"tag": "markdown_table_row", "children": [{"tag": "markdown_table_header", "text": "日期"}, {"tag": "markdown_table_header", "text": "值班"}, {"tag": "markdown_table_header", "text": "备份"}]},
            {"tag": "markdown_table_row", "children": [{"tag": "markdown_table_cell", "text": "周一"}, {"tag": "markdown_table_cell", "text": "李雷"}, {"tag": "markdown_table_cell", "text": "Tom"}]},
            {"tag": "markdown_table_row", "children": [{"tag": "markdown_table_cell", "text": "周二"}, {"tag": "markdown_table_cell", "text": "韩梅梅"}, {"tag": "markdown_table_cell", "text": "李雷"}]}]},
        {"tag": "chart", "chart": {"type": "bar", "title": {"text": "告警数"}, "xField": "d", "yField": "n",
                                   "data": {"values": [{"d": "周一", "n": 4}, {"d": "周二", "n": 7}, {"d": "周三", "n": 2}, {"d": "周四", "n": 5}]}}}]}}}

FS_FORWARD = {"kind": "card", "text": "聊天记录", "metadata": {
    "variant": "merge-forward", "title": "李雷和韩梅梅的聊天记录", "messageCount": 3,
    "previews": [{"senderName": "李雷", "text": "图标的圆角再小一点"}, {"senderName": "韩梅梅", "text": "**好的**，下午给你新版"}],
    "messages": [
        {"id": "fw1", "senderName": "李雷", "senderAvatarUrl": "/api/media/avatar-u_ll", "createdAt": ago(days=1),
         "parts": [{"kind": "text", "text": "图标的圆角再小一点"}]},
        {"id": "fw2", "senderName": "韩梅梅", "senderAvatarUrl": "/api/media/avatar-u_hmm", "createdAt": ago(days=1),
         "parts": [{"kind": "text", "text": "好的，下午给你新版"}, {"kind": "emotion", "refId": "OK", "text": "[OK]"}]},
        {"id": "fw3", "senderName": "李雷", "createdAt": ago(days=1),
         "parts": [{"kind": "image", "url": "/api/v1/lark/media/img-fw3", "metadata": {"width": 400, "height": 400}}]}]}}

FS_MEETING = {"kind": "card", "text": "视频会议", "metadata": {
    "variant": "video-chat", "title": "设计评审", "eyebrow": "视频会议", "status": "进行中",
    "fields": [{"label": "时间", "value": later(minutes=30)}, {"label": "发起人", "value": "李雷"}],
    "actions": [{"type": "copyMeetingNumber", "value": "123 456 789"}]}}

FS_MOMENTS_LINK = "https://applink.example.com/client/moments/detail?postId=7000000000000000001"

FS_MESSAGES = {
    "f1": [
        _fsm("f1", 90, "韩梅梅", "早，周五的发布还按计划吗？", 300),
        _fsm("f1", 91, "我", "按计划，今天把最后两个问题合进去", 290, mine=True),
        _fsm("f1", 92, "韩梅梅", "好的", 289, reactions=[{"key": "THUMBSUP", "count": 1, "mine": True, "users": ["我"]}]),
        _fsm("f1", 93, "韩梅梅", "[文件]", 120, kind="FILE", parts=[{"kind": "file", "text": "发布计划.pdf", "url": "/api/v1/lark/media/file-plan",
                                                                "metadata": {"size": "245760", "extension": "pdf"}}]),
        _fsm("f1", 94, "韩梅梅", "[语音]", 60, kind="MEDIA",
             parts=[{"kind": "media", "url": "/api/v1/lark/media/voice-1", "metadata": {"mime": "audio/opus", "duration": 9000}}]),
        _fsm("f1", 95, "韩梅梅", "[removed]", 40, parts=[{"kind": "link", "text": "removed动态", "url": FS_MOMENTS_LINK}]),
        _fsm("f1", 100, "韩梅梅", "周五的发布能赶上吗？下午我要和客户确认时间", 20,
             reply_to={"id": "lm_f1_91", "sender_name": "我", "text": "按计划，今天把最后两个问题合进去"}),
        _fsm("f1", 101, "韩梅梅", "顺便看一下这个", 18, parts=[_fst("顺便看一下 "), {"kind": "docs", "text": "客户沟通记录", "url": "https://example.com/docx/abc"},
                                                           _fst(" 和 "), {"kind": "link", "text": "这篇文章", "url": "https://example.com/blog/1"}]),
    ],
    "f2": [
        _fsm("f2", 180, "李雷", "新版图标的方向", 400, kind="POST", parts=[
            {"kind": "text", "text": "新版图标的方向", "metadata": {"blocks": _fsblk(("h", "heading-2"))}},
            {"kind": "text", "text": "保留现在的配色", "metadata": {"blocks": _fsblk(("l", "ordered-list"), ("i1", "list-item"))}},
            {"kind": "text", "text": "深色模式单独出一套", "metadata": {"blocks": _fsblk(("l", "ordered-list"), ("l2", "unordered-list"), ("i3", "list-item"))}},
            {"kind": "text", "text": "圆角统一成 ", "metadata": {"blocks": _fsblk(("l", "ordered-list"), ("i2", "list-item"))}},
            {"kind": "text", "text": "22pt", "metadata": {"inlineCode": True, "blocks": _fsblk(("l", "ordered-list"), ("i2", "list-item"))}},
            {"kind": "text", "text": "细节以设计稿为准", "metadata": {"marks": ["italic"], "blocks": _fsblk(("q", "quote"))}}]),
        _fsm("f2", 181, "韩梅梅", "收到 [OK] 我先出一版", 398, parts=[_fst("收到 "), {"kind": "emotion", "refId": "OK", "text": "[OK]"}, _fst(" 我先出一版")],
             reactions=[{"key": "OK", "count": 2, "mine": False, "users": ["李雷", "Tom"]}]),
        _fsm("f2", 182, "韩梅梅", "[图片]", 395, kind="IMAGE", parts=[{"kind": "image", "url": "/api/v1/lark/media/wide-icon1", "metadata": {"width": 1280, "height": 720}}]),
        _fsm("f2", 183, "韩梅梅", "[图片]", 395, kind="IMAGE", parts=[{"kind": "image", "url": "/api/v1/lark/media/tall-icon2", "metadata": {"width": 720, "height": 1280}}]),
        _fsm("f2", 184, "", "韩梅梅 把群名改成了「设计评审」", 300, kind="SYSTEM", parts=[{"kind": "text", "text": "韩梅梅 把群名改成了「设计评审」",
             "metadata": {"system": True, "systemTemplate": "{from_user} 把群名改成了「设计评审」", "systemHolders": [{"key": "from_user", "text": "韩梅梅"}]}}]),
        _fsm("f2", 185, "构建机器人", "v2.3 发布检查通过", 200, kind="CARD", parts=[FS_CARD]),
        _fsm("f2", 186, "Tom", "这段配置有问题吗", 190, kind="POST", parts=[_fst("这段配置有问题吗"),
             {"kind": "code", "text": "# 发布配置\nversion: \"2.3.0\"\nchannels: [beta, stable]\nretry: 3", "metadata": {"language": "yaml"}}]),
        _fsm("f2", 187, "李雷", "没问题", 188, thread_reply_count=2, reply_count=2),
        _fsm("f2", 188, "构建机器人", "本周值班", 150, kind="CARD", parts=[FS_TABLE_CARD]),
        _fsm("f2", 189, "李雷", "[聊天记录]", 140, kind="MERGE_FORWARD", parts=[FS_FORWARD]),
        _fsm("f2", 190, "Tom", "[贴纸]", 130, kind="STICKER", parts=[{"kind": "emotion", "url": "/api/v1/lark/media/sticker-cat", "text": "贴纸"}]),
        _fsm("f2", 191, "我", "我去看看", 125, mine=True, edited=True),
        _fsm("f2", 192, "韩梅梅", "[视频]", 120, kind="MEDIA", parts=[{"kind": "media", "url": "/api/v1/lark/media/video-demo", "text": "演示.mp4",
             "metadata": {"mime": "video/mp4", "duration": 19000, "posterUrl": "/api/v1/lark/media/wide-poster", "posterWidth": 1280, "posterHeight": 720}}]),
        _fsm("f2", 193, "李雷", "[视频会议]", 100, kind="CARD", parts=[FS_MEETING]),
        _fsm("f2", 194, "Tom", "", 95, recalled=True, parts=[]),
        _fsm("f2", 200, "李雷", "@韩梅梅 新版图标放在这里了 设计稿", 60, kind="POST", parts=[{"kind": "at", "text": "@韩梅梅", "refId": "u_hmm"},
             _fst(" 新版图标放在这里了 "), {"kind": "link", "text": "设计稿", "url": "https://example.com/design/icons"}]),
        _fsm("f2", 201, "李雷", "这次改了配色，联调好了", 59, parts=[_fst("这次改了配色，联调好了", lingo=[{"start": 9, "end": 11}])]),
        _fsm("f2", 202, "李雷", "@我 下午评审前看一眼？", 55, at_me=True, parts=[{"kind": "at", "text": "@我", "refId": "u_me"}, _fst(" 下午评审前看一眼？")]),
        _fsm("f2", 203, "韩梅梅", "我也看看", 50, reply_to={"id": "lm_f2_200", "sender_name": "李雷", "text": "新版图标放在这里了"}),
    ],
    "f3": [_fsm("f3", 290 + i, ["Tom", "李雷", "韩梅梅"][i % 3], t, 600 - i * 30) for i, t in enumerate(
        ["这周六去爬山吗", "去！", "几点集合", "早上 8 点？", "可以", "地铁站见", "记得带水", "我带零食", "天气预报说晴", "那就定了"])]
    + [_fsm("f3", 300, "Tom", "周六早上 8 点地铁站集合，记得带水", 30)],
}
FS_THREADS = {"lm_f2_187": [_fsm("f2", 1871, "Tom", "那我直接合了", 185), _fsm("f2", 1872, "李雷", "合吧", 184)]}
FS_CONVS = [
    {"id": "f1", "name": "韩梅梅", "avatar": "/api/v1/lark/media/avatar-u_hmm", "is_group": False, "unread": 2, "preview": "顺便看一下这个",
     "updated_at": ago(minutes=18), "read_position": "99"},
    {"id": "f2", "name": "设计评审", "is_group": True, "unread": 4, "at_me": True, "preview": "我也看看", "updated_at": ago(minutes=50),
     "read_position": "199", "mention_position": "202"},
    {"id": "f3", "name": "周末徒步群", "is_group": True, "unread": 1, "preview": "周六早上 8 点地铁站集合", "updated_at": ago(minutes=30),
     "read_position": "299"},
]


def fs_page(feed, q):
    msgs = sorted(FS_MESSAGES.get(feed, []), key=lambda m: int(m["position"]))
    limit = int((q.get("limit") or ["40"])[0] or 40)
    before, after, around = [(q.get(k) or [""])[0] for k in ("before", "after", "around")]
    if before:
        page = [m for m in msgs if int(m["position"]) < int(before)][-limit:]
    elif after:
        page = [m for m in msgs if int(m["position"]) > int(after)][:limit]
    elif around:
        a = int(around)
        lo = [m for m in msgs if int(m["position"]) < a][-(limit // 2):]
        page = lo + [m for m in msgs if int(m["position"]) >= a][:limit - len(lo)]
    else:
        page = msgs[-limit:]
    first = int(page[0]["position"]) if page else 0
    last = int(page[-1]["position"]) if page else 0
    return {"messages": page, "has_older": any(int(m["position"]) < first for m in msgs),
            "has_newer": any(int(m["position"]) > last for m in msgs),
            "older_cursor": page[0]["position"] if page else None, "newer_cursor": page[-1]["position"] if page else None}


def fs_push(feed):
    """{"type":"lark"} to every open socket (the hub relays lark-connector's realtime events like this)."""
    for send, _ in list(LIVE):
        try: send({"type": "lark", "feed_ids": [feed]})
        except Exception: pass


def fs_find(mid):
    for ms in list(FS_MESSAGES.values()) + list(FS_THREADS.values()):
        for m in ms:
            if m["id"] == mid: return m
    return None


def fs_get(h, p, q):
    """GET /api/v1/lark/… and /mock/lark with MOCK_LARK (True when handled; the rest falls through)."""
    if p == "/mock/lark":
        h.send_json(FS_LOG); return True
    if p.startswith("/api/v1/lark/media/"):
        key = p.rsplit("/", 1)[-1]
        if key.startswith("video-") or key.startswith("voice-"):
            h.send_json({"error": "no media in the fake hub"}, 404); return True
        data = b"%PDF-1.4\n% made-up file\n" if key.startswith("file-") else fs_media(key)
        status, extra = 200, {}
        rng = h.headers.get("Range")
        if rng and rng.startswith("bytes="):
            a, _, b = rng[6:].partition("-")
            a = int(a or 0); b = min(int(b) if b else len(data) - 1, len(data) - 1)
            extra["Content-Range"] = "bytes %d-%d/%d" % (a, b, len(data))
            data, status = data[a:b + 1], 206
        h.send_response(status)
        h.send_header("Content-Type", "application/pdf" if key.startswith("file-") else "image/png")
        h.send_header("Content-Length", str(len(data))); h.send_header("Accept-Ranges", "bytes")
        for k, v in extra.items(): h.send_header(k, v)
        h.end_headers(); h.wfile.write(data)
        return True
    if p == "/api/v1/lark/conversations":
        h.send_json({"conversations": FS_CONVS, "next_cursor": ""}); return True
    if p.startswith("/api/v1/lark/conversations/") and p.endswith("/messages"):
        h.send_json(fs_page(p.split("/")[5], q)); return True
    if p.startswith("/api/v1/lark/messages/") and p.endswith("/thread"):
        mid = p.split("/")[5]
        root = fs_find(mid)
        h.send_json({"messages": ([root] if root else []) + FS_THREADS.get(mid, []), "has_older": False}); return True
    if p.startswith("/api/v1/lark/messages/") and p.endswith("/url-preview"):
        h.send_json({"title": "removed", "card": {"author": "Tom", "avatarUrl": "/api/v1/lark/media/avatar-u_tom", "subtitle": "工程效率",
                                                 "body": "周末徒步回来，顺手整理了一份新人上手清单，欢迎补充。", "footer": "12 赞 · 1 评论"}})
        return True
    # The inbox's own post (mp1) is the plainer one above; a post shared in a chat is this one.
    if p.startswith("/api/v1/lark/moments/posts/") and "/mp1" not in p:
        if p.endswith("/comments"):
            h.send_json({"comments": [{"id": "c1", "userName": "李雷", "userId": "u_ll", "userAvatarUrl": "/api/v1/lark/media/avatar-u_ll",
                                       "content": {"segments": [{"kind": "text", "text": "很有用 "}, {"kind": "emoji", "key": "THUMBSUP", "text": "[赞]"}]},
                                       "createTime": time.time() * 1000 - 3600e3}]})
            return True
        h.send_json({"id": p.split("/")[-1], "userName": "Tom", "userId": "u_tom", "userAvatarUrl": "/api/v1/lark/media/avatar-u_tom",
                     "content": {"segments": [{"kind": "text", "text": "周末徒步回来，顺手整理了一份新人上手清单，欢迎补充。"},
                                              {"kind": "emoji", "key": "SMILE", "text": "[微笑]"}]},
                     "images": [{"url": "/api/v1/lark/media/img-m%d" % i, "width": 600, "height": 600} for i in range(1, 4)],
                     "reactionGroups": [{"type": "THUMBSUP", "count": 12, "selfInvolved": True}],
                     "commentCount": 1, "createTime": time.time() * 1000 - 86400e3})
        return True
    if p.startswith("/api/v1/lark/users/"):
        uid = p.split("/")[-1]
        name = {v: k for k, v in FS_USERS.items()}.get(uid, "同事")
        h.send_json({"displayName": name, "enName": {"u_hmm": "Meimei Han", "u_ll": "Lei Li"}.get(uid),
                     "avatarUrl": "/api/v1/lark/media/avatar-" + uid, "description": "做设计，也写一点代码",
                     "userType": "bot" if uid == "u_bot" else "user", "customStatus": {"title": "专注中"}, "timezone": "Europe/London",
                     "fields": [{"key": "B-DEPARTMENT", "value": "设计部"}, {"key": "B-LEADER", "value": "李雷", "url": "lark://client/profile?uid=u_ll"},
                                {"key": "email", "label": "邮箱", "value": "me@example.com"}]})
        return True
    if p == "/api/v1/lark/reactions/quick":
        h.send_json({"keys": FS_EMOJI_KEYS[:7]}); return True
    if p == "/api/v1/lark/emojis":
        h.send_json({"emojis": {k.lower(): {"label": l, "url": "/api/v1/lark/media/emoji-" + k.lower()} for k, l in zip(FS_EMOJI_KEYS, FS_EMOJI_LABELS)},
                     "groups": [{"title": "默认表情", "keys": FS_EMOJI_KEYS}]})
        return True
    return False


def fs_post(h, p, q, body):
    """POST /api/v1/lark/… and /mock/lark/… with MOCK_LARK (True when handled)."""
    if p == "/mock/lark/incoming":
        feed = (q.get("feed") or ["f1"])[0]
        msgs = FS_MESSAGES.setdefault(feed, [])
        pos = max([int(m["position"]) for m in msgs] or [0]) + 1
        msgs.append(_fsm(feed, pos, "韩梅梅", (q.get("text") or ["新消息 %d" % pos])[0], 0))
        fs_push(feed)
        h.send_json({"ok": True, "position": str(pos)}); return True
    if not p.startswith("/api/v1/lark/"): return False
    b = json.loads(body or b"{}")
    if p.startswith("/api/v1/lark/conversations/") and p.endswith("/messages"):
        feed = p.split("/")[5]
        FS_LOG["sent"].append({"feed": feed, **b})
        msgs = FS_MESSAGES.setdefault(feed, [])
        pos = max([int(m["position"]) for m in msgs] or [0]) + 1
        msgs.append(_fsm(feed, pos, "我", b.get("text", ""), 0, mine=True))
        fs_push(feed)
        h.send_json({"ok": True}); return True
    if p.startswith("/api/v1/lark/conversations/") and p.endswith("/read"):
        FS_LOG["reads"].append({"feed": p.split("/")[5], **b})
        h.send_json({"ok": True}); return True
    if p.startswith("/api/v1/lark/messages/") and p.endswith("/reactions"):
        mid = p.split("/")[5]
        FS_LOG["reactions"].append({"message": mid, **b})
        m = fs_find(mid)
        if m is not None:
            rs = m.setdefault("reactions", [])
            r = next((x for x in rs if x["key"].lower() == b.get("key", "").lower()), None)
            if b.get("remove"):
                if r and r.get("mine"):
                    r["count"] -= 1; r["mine"] = False; r["users"] = [u for u in r.get("users", []) if u != "我"]
                    if r["count"] <= 0: rs.remove(r)
            elif r is None:
                rs.append({"key": b.get("key"), "count": 1, "mine": True, "users": ["我"]})
            elif not r.get("mine"):
                r["count"] += 1; r["mine"] = True; r.setdefault("users", []).append("我")
        h.send_json({"ok": True}); return True
    if "/card-actions/" in p:
        FS_LOG["actions"].append({"message": p.split("/")[5], "action": p.split("/")[-1]})
        h.send_json({"ok": True}); return True
    return False
# ---------- end of area: feishu chat page ----------

# Unified model configuration uses only made-up providers and purposes.
MODEL_BINDINGS = {}
MODEL_CONFIG_LOCK = threading.RLock()
MODEL_DEFINITIONS = [
    ("assistant", "Jarvis 对话", "对话与 Agent", "launcher"),
    ("inbox.agent", "AI 分身", "对话与 Agent", "launcher"),
    ("lark.agent", "飞书 /agent", "对话与 Agent", "launcher"),
    ("agent.new", "新建 Agent", "对话与 Agent", "launcher"),
    ("inbox.triage", "分拣与摘要", "收件箱", "api"),
    ("inbox.judge", "AI 分身判断", "收件箱", "api"),
    ("inbox.draft", "草拟回复", "收件箱", "api"),
    ("translate", "文本与邮件翻译", "翻译", "api"),
    ("kb", "知识处理默认模型", "知识处理", "api"),
    ("kb.compress", "会话压缩与打标", "知识处理", "api"),
    ("kb.project", "项目档案重写", "知识处理", "api"),
    ("kb.dedupe", "项目去重", "知识处理", "api"),
    ("kb.items", "事项提取与打标", "知识处理", "api"),
    ("kb.moments", "removed关联", "知识处理", "api"),
    ("assistant.title", "对话标题", "辅助功能", "api"),
    ("agent.title", "Agent 标签名", "辅助功能", "api"),
    ("schedule", "定时任务默认模型", "定时任务", "launcher"),
]


def model_configuration():
    providers = [dict(id="launcher." + l["name"], name=l["name"], kind=l.get("kind", "launcher"),
                      model=l.get("model"), hidden=l.get("hidden", False)) for l in LAUNCHERS]
    providers += [{"id": "launcher.claude", "name": "claude", "kind": "launcher", "model": "model-chat"},
                  {"id": "legacy.ai", "name": "轻量 API", "kind": "api", "api": "messages", "model": "model-fast"}]
    definitions = MODEL_DEFINITIONS + [("task." + t["id"], t.get("title") or "跟进任务", "定时任务", "launcher") for t in SCHEDULE]
    purposes = []
    with MODEL_CONFIG_LOCK:
        for pid, name, group, kind in definitions:
            stored = MODEL_BINDINGS.get(pid, {"revision": 0, "override": {}})
            binding = dict(stored["override"])
            effective = {"provider": "legacy.ai" if kind == "api" else "launcher.claude", "model": "model-fast" if kind == "api" else "model-chat"}
            if binding.get("provider"):
                provider = next((p for p in providers if p["id"] == binding["provider"]), {})
                effective = {"provider": binding["provider"], "model": provider.get("model", "")}
            effective.update(binding)
            purpose = dict(id=pid, name=name, group=group, kind=kind, override=binding, effective=effective,
                           revision=stored["revision"], inherited_from="现有默认" if not binding else "此用途",
                           fields=["provider", "model"] if kind == "api" else ["provider", "model", "effort"],
                           provider_ids=[p["id"] for p in providers if p["kind"] == kind], status="")
            # MOCK_MODEL_LIMIT=1: the default API connection is out of quota for 23 more minutes (docs/model-config.md「上游限流」).
            if os.environ.get("MOCK_MODEL_LIMIT") and effective["provider"] == "legacy.ai":
                purpose["limit"] = {"reason": "quota", "until": later(minutes=23)}
                purpose["status"] = "额度用完，23 分钟后重试"
            purposes.append(purpose)
    return {"purposes": purposes, "providers": providers}


# 设置 › 模型供应商: two ~/.zshrc launchers, one through relay-history and one through FAKEMODEL, with their quota.
LAUNCHERS = [
    {"name": "cc_kiro", "source": "zshrc", "base_url": "http://gateway.example.com:18000", "model": "kiro/claude-opus-5-5", "machines": ["macbook"]},
    {"name": "example-cli", "source": "zshrc", "base_url": "http://fakemodel.example.com", "model": "gpt-6.1-sol", "machines": ["macbook"]},
    {"name": "cc_glm", "source": "zshrc", "base_url": "https://open.bigmodel.example.com/api/anthropic", "model": "glm-5.3", "machines": ["macbook"]},
    {"name": "cc_old", "source": "zshrc", "base_url": "https://relay.example.com", "model": "claude-opus-4-7", "machines": ["macbook"], "hidden": True},
    {"name": "cc_test", "source": "zshrc", "base_url": "https://relay.example.com", "model": "model_api/experimental", "machines": ["macbook"], "hidden": True},
]
if os.environ.get("MOCK_MANAGED_LAUNCHER"):  # one added in Jarvis (LauncherSwipeTests swipes it to 删除)
    LAUNCHERS.insert(0, {"name": "jv_strong", "source": "jarvis", "base_url": "https://gateway.example.com", "model": "claude-opus-5-5", "has_key": True})
def _w(label, ratio=None, used=None, limit=None, unit="percent", resets=None, exhausted=False):
    w = {"label": label, "unit": unit}
    if ratio is not None: w["used_ratio"] = ratio
    if used is not None: w["used"] = used
    if limit is not None: w["limit"] = limit
    if resets: w["resets_at"] = resets
    if exhausted: w["exhausted"] = True
    return w
LAUNCHER_QUOTAS = {
    "cc_kiro": {"gateway": "relay-history", "key_name": "cc_kiro",
        "budget": [_w("每周预算", 0.62, 496, 800, "usd", later(hours=60))],
        "pools": [
            {"name": "kiro #2328da", "state": "available", "detail": "可用", "windows": [_w("每月", 0.64, 6401, 10000, "credits", later(hours=11))]},
            {"name": "claude · max", "state": "cooldown", "detail": "冷却中，23:00 恢复",
             "windows": [_w("5 小时", 1.0, resets=later(hours=2), exhausted=True), _w("每周", 0.77, resets=later(hours=70))]}],
        "usage": [{"label": "今日", "requests": 287, "tokens": 210552323, "cost_usd": 123.12},
                  {"label": "本周", "requests": 10432, "tokens": 3359748900, "cost_usd": 1109.02},
                  {"label": "本月", "requests": 20004, "tokens": 6012000000, "cost_usd": 2190.4}]},
    "example-cli": {"gateway": "fakemodel",
        "pools": [
            {"name": "火山 · Agent Plan", "state": "ok", "windows": [_w("5 小时额度", 0.42, 42, 100, "percent", later(hours=3)),
                                                                   _w("每周额度", 0.86, 86, 100, "percent", later(hours=50))],
             "models": [{"model": "deepseek-v4.1-flash", "windows": [_w("TPM", 0.02, 8400, 500000, "tokens")]}]},
            {"name": "modelhub", "state": "unsupported", "detail": "上游没有额度接口，只有网关统计"}]},
}

# The removed post behind the inbox's moment item (LarkMomentsPostView reads these).
def _ms(minutes): return int((datetime.now(timezone.utc) - timedelta(minutes=minutes)).timestamp() * 1000)
MOMENT_POST = {"id": "mp1", "userName": "我", "userId": "me", "commentCount": 3, "reactionCount": 12, "createTime": _ms(150),
               "content": {"text": "换新电脑以后大家都怎么迁移数据？迁移助手跑了一下午还没好", "segments": [{"kind": "text", "text": "换新电脑以后大家都怎么迁移数据？迁移助手跑了一下午还没好"}]},
               "categoryNames": ["技术交流"], "reactionGroups": [{"type": "THUMBSUP", "count": 12}]}
MOMENT_COMMENTS = [
    {"id": "mc1", "userName": "周一航", "userId": "u1", "createTime": _ms(110), "reactionCount": 2,
     "content": {"text": "我是重新装的，配置文件放云盘，干净"}},
    {"id": "mc2", "userName": "陈子墨", "userId": "u2", "createTime": _ms(70), "content": {"text": "插雷电线快很多"}},
    {"id": "mc3", "userName": "林小雨", "userId": "u3", "createTime": _ms(25), "replyCommentId": "mc2", "replyUserName": "陈子墨",
     "content": {"text": "我用的迁移助手，一晚上就好了，你是不是开了 FileVault"}},
]

MAIL_ACCOUNTS = [{"id": "acc1", "name": "个人邮箱", "email": "me@example.com", "provider": "imap", "auth": "password",
                  "imap_host": "imap.example.com", "imap_port": 993, "smtp_host": "smtp.example.com", "smtp_port": 465}]

# ---------- area: mail (Android mail lane; fake data only) ----------
# The mail's write paths: flags, sending, adding / testing / deleting accounts, Google sign-in's start. Nothing is sent
# anywhere: what the app posted is in GET /mock/mail. MOCK_MAIL=1 adds whole mails for the reader (an HTML one with a
# tracking pixel, a script and an attachment; a two-mail thread) and a Gmail account whose authorization ran out and a
# Feishu one; MOCK_GOOGLE=1 makes google/start answer (with a made-up URL).
MAIL_LOG = {"sent": [], "flags": [], "accounts_added": [], "deleted": [], "tested": []}
MAIL_FULL = {}
if os.environ.get("MOCK_MAIL"):
    MAIL_ACCOUNTS += [
        {"id": "acc2", "name": "me@gmail.example", "email": "me@gmail.example", "provider": "gmail", "auth": "oauth2",
         "imap_host": "imap.gmail.com", "status": "error", "error": "Google authorization expired or was revoked"},
        {"id": "acc3", "name": "我的飞书邮箱", "email": "me@corp.example.com", "provider": "feishu", "auth": "password",
         "imap_host": "imap.larkoffice.com", "status": "ok"},
    ]
    MAIL_FULL = {
        501: {"subject": "下周的分享能不能提前到周二？", "from": {"name": "Lucy Wang", "email": "lucy@example.com"},
              "to": [{"name": "我", "email": "me@example.com"}], "cc": [{"name": "Tom", "email": "tom@example.com"}],
              "date": ago(hours=3), "message_id": "<501@example.com>", "references": ["<499@example.com>"],
              "text": "Hi，\n\n周四我临时要出差，分享能不能挪到周二下午？三点以后都行。\n\nLucy", "html": "", "seen": False},
        502: {"subject": "你的订单已发货", "from": {"name": "示例商城", "email": "shop@example.com"},
              "to": [{"name": "", "email": "me@example.com"}], "date": ago(hours=5), "message_id": "<502@example.com>",
              "text": "", "seen": True,
              "html": "<html><head><script>alert(1)</script><style>.box{padding:12px}</style></head>"
                      "<body><div class=box><h2>你的订单已发货</h2><p>订单 <b>2026102800123</b> 已由快递揽收，预计<b>周五</b>送达。</p>"
                      "<p><img src=\"https://images.example.com/track.png\" width=1 height=1>"
                      "<img src=\"https://images.example.com/parcel.jpg\" alt=\"包裹\"></p>"
                      "<p><a href=\"https://shop.example.com/orders/2026102800123\" onclick=\"steal()\">查看物流</a></p></div></body></html>",
              "attachments": [{"filename": "发票.pdf", "mime": "application/pdf", "size": 48213}]},
        505: {"subject": "Re: 季度复盘的时间", "from": {"name": "Jack Li", "email": "jack@example.com"},
              "to": [{"name": "我", "email": "me@example.com"}], "date": ago(hours=30), "message_id": "<505@example.com>",
              "text": "周三上午可以吗？", "html": "", "seen": True},
        506: {"subject": "Re: 季度复盘的时间", "from": {"name": "Jack Li", "email": "jack@example.com"},
              "to": [{"name": "我", "email": "me@example.com"}, {"name": "Tom", "email": "tom@example.com"}], "date": ago(hours=1),
              "message_id": "<506@example.com>", "references": ["<505@example.com>"], "flagged": True, "seen": False,
              "text": "改到周三下午两点吧，会议室我订好了。\n\n议程：\n1. 上季度目标回顾\n2. 下季度计划\n\nJack", "html": ""},
    }
    INBOX.insert(0, {"id": "mail:acc1:t5:505", "source": "mail", "kind": "mail", "account": "acc1", "account_name": "个人邮箱",
                     "title": "Re: 季度复盘的时间", "sender": "Jack Li", "preview": "改到周三下午两点吧，会议室我订好了。", "count": 2,
                     "started_at": ago(hours=30), "updated_at": ago(hours=1), "tier": "action", "summary": "Jack 把季度复盘改到周三下午两点",
                     "reason": "要你确认时间", "read": False,
                     "mail": {"account_id": "acc1", "mailbox": "INBOX", "uids": [505, 506],
                              "from": {"name": "Jack Li", "email": "jack@example.com"}, "direct": True}})


def mail_area(h, method, p, q):
    """The mail paths above (True when it answered)."""
    if p == "/mock/mail" and method == "GET":
        h.send_json(MAIL_LOG); return True
    if not p.startswith("/api/v1/mail/"): return False
    body = json.loads(getattr(h, "last_body", b"") or b"{}") if method != "GET" else {}
    parts = p.split("/")  # ['', 'api', 'v1', 'mail', 'messages', aid, uid, (flags)]
    if p.startswith("/api/v1/mail/messages/") and len(parts) >= 7 and parts[6].isdigit():
        uid = int(parts[6])
        if len(parts) > 7 and parts[7] == "flags":
            MAIL_LOG["flags"].append(dict(body, account_id=unquote(parts[5]), uid=uid))
            if uid in MAIL_FULL: MAIL_FULL[uid].update({k: body[k] for k in ("seen", "flagged") if k in body})
            h.send_json({"ok": True}); return True
        if method == "GET" and uid in MAIL_FULL:
            h.send_json({"message": dict(MAIL_FULL[uid], account_id=unquote(parts[5]), mailbox=(q.get("mailbox") or ["INBOX"])[0], uid=uid)})
            return True
        return False
    if p.startswith("/api/v1/mail/accounts/") and p.endswith("/test"):
        aid = unquote(parts[5])
        a = next((x for x in MAIL_ACCOUNTS if x["id"] == aid), None)
        MAIL_LOG["tested"].append(aid)
        h.send_json({"ok": a is not None and a.get("status") != "error", "error": (a or {}).get("error") or (None if a else "no such account")})
        return True
    if p.startswith("/api/v1/mail/accounts/") and method == "DELETE":
        aid = unquote(parts[5])
        MAIL_ACCOUNTS[:] = [a for a in MAIL_ACCOUNTS if a["id"] != aid]
        MAIL_LOG["deleted"].append(aid)
        h.send_json({"ok": True}); return True
    if p == "/api/v1/mail/accounts" and method == "POST":
        email = (body.get("email") or "").strip()
        if "@" not in email or not body.get("password"):
            h.send_json({"error": "email and password are required"}, 400); return True
        if body.get("password") == "wrong":
            h.send_json({"error": "imap login: wrong authorization code"}, 400); return True
        prov = body.get("provider") or "imap"
        dom = email.split("@")[1]
        acc = {"id": "acc_" + uuid.uuid4().hex[:6], "name": body.get("name") or email, "email": email, "provider": prov,
               "auth": "password", "imap_host": body.get("imap_host") or ("imap.larkoffice.com" if prov == "feishu" else "imap." + dom),
               "imap_port": body.get("imap_port") or 993, "smtp_host": body.get("smtp_host") or ("smtp.larkoffice.com" if prov == "feishu" else "smtp." + dom),
               "smtp_port": body.get("smtp_port") or 465, "status": "ok"}
        MAIL_ACCOUNTS.append(acc)
        MAIL_LOG["accounts_added"].append({k: v for k, v in body.items() if k != "password"})
        h.send_json({"account": acc}); return True
    if p == "/api/v1/mail/send" and method == "POST":
        if not body.get("to"):
            h.send_json({"error": "at least one recipient is required"}, 400); return True
        MAIL_LOG["sent"].append(body)
        h.send_json({"ok": True}); return True
    if p == "/api/v1/mail/oauth/google/start" and method == "POST":
        if not os.environ.get("MOCK_GOOGLE"):
            h.send_json({"error": "Google sign-in is not configured"}, 400); return True
        h.send_json({"auth_url": "https://accounts.example.com/o/oauth2/auth?state=mock", "callback_scheme": "com.example.mock", "state": "mock"})
        return True
    return False
# ---------- end area: mail ----------


# MOCK_LONG_SLOTS=1: the weather space's tabs have long names (the space page's two-column cards must still fit).
if os.environ.get("MOCK_LONG_SLOTS"):
    for x, name in zip([y for y in SESSIONS if y.get("space_name") == "weather"], ["ai-person-kiro", "leader-kiro", "release-notes-bot"]):
        x["slot_name"] = name


# MOCK_SUGGESTION=1: the idle review session has Claude's suggested next message (grey in its input box).
if os.environ.get("MOCK_SUGGESTION"):
    for s in SESSIONS:
        if s["session_id"] == REVIEW:
            s["suggestion"] = "把命名问题修完再跑一遍测试，然后推到 review/naming"


# MOCK_BIG=1: a Codex session whose last exchange ran 650 commands (a kilobyte of output each) with 110 notes and a few
# progress replies between them — 1400 rows that the chat folds into one work line. Pages fold that work for apps that
# ask (fold_work, like the hub; fold_work() below is transcript.FoldWork), a fold opens with from_seq / through_seq, and
# /mock/served records what each items batch of it weighed (LongSessionTests).
BIG = "a1f3c2d4-0000-4000-8000-000000000008"
SERVED = []
if os.environ.get("MOCK_BIG") == "1":
    SESSIONS.append(sess("server", BIG, "整理构建脚本", "/home/me/code/shop-api", "done", "idle", "shop-api", 1, "build", 2, 30,
                         "构建脚本整理完了：650 个步骤都过了一遍。", agent="codex", seen_at=ago(minutes=20)))
    _out = "".join("step output line %d\n" % k for k in range(50))  # ~1 KB
    _big = [it("user", "text", 200, text="先看看构建为什么慢"), it("assistant", "text", 190, text="慢在依赖下载，已经加了缓存。"),
            it("user", "text", 100, text="把构建脚本都整理一遍")]
    for _n in range(650):
        _m = 100 - _n * 70 / 650
        if _n % 6 == 0 and _n // 6 < 110:
            _big.append(it("assistant", "thinking", _m, text="先看第 %d 步要不要保留。" % _n))
        if _n and _n % 160 == 0:
            _big.append(it("assistant", "text", _m, text="已经整理到第 %d 步。" % _n))
        _big += tool("Bash", {"command": "make step-%d" % _n}, _out, _m)
    _big.append(it("assistant", "text", 30, text="构建脚本整理完了：650 个步骤都过了一遍。"))
    ITEMS[BIG] = _big


# MOCK_TASKS=1: a session whose agent works with background tasks (docs/protocol.md TaskNote): a heredoc written in
# the background (no description: its notice used to be the whole heredoc, centered), a test run that fails, a Monitor
# reporting two events between two commands, then ending (BackgroundNoticeTests).
TASKS = "a1f3c2d4-0000-4000-8000-000000000009"
if os.environ.get("MOCK_TASKS") == "1":
    SESSIONS.append(sess("macbook", TASKS, "整理发布说明", "/Users/me/code/weather", "done", "unread", "weather", 1, "release", 4, 3,
                         "发布说明写好了，测试有一项没过，已经记下来。"))

    def note(ts_min, kind, status, title, task_id, tool=None, code=None, out=None, detail=None, text=""):
        t = {"kind": kind, "status": status, "title": title, "task_id": task_id}
        if tool: t["tool_use_id"] = tool
        if code is not None: t["exit_code"] = code
        if out: t["output_file"] = out
        if detail: t["detail"] = detail
        return it("system", "notice", ts_min, text=text, task=t)
    _brief = "cat > brief.md <<'EOF'\n# 发布说明\n\n## 新功能\n- 主屏幕小组件支持按小时刷新\n- 降雨概率改成百分比\n\n## 修复\n- 夜间模式下图标看不清\nEOF"
    _w = [it("user", "text", 20, text="写一下这版的发布说明，顺便把测试跑一遍，CI 有结果告诉我")]
    _w += tool("Bash", {"command": _brief, "run_in_background": True},
               "Command running in background with ID: bk1brief. Output is being written to: /tmp/claude/bk1brief.output", 19)
    _brief_call = _w[-2]["tool_use_id"]
    _w += tool("Bash", {"command": "xcodebuild test -scheme Weather", "description": "跑单元测试", "run_in_background": True},
               "Command running in background with ID: bk2tests. Output is being written to: /tmp/claude/bk2tests.output", 18)
    _test_call = _w[-2]["tool_use_id"]
    _w += tool("Monitor", {"command": "gh run watch --exit-status", "description": "等 CI 跑完", "timeout_ms": 600000},
               "Monitor started (task mo3ci, timeout 600000ms). You will be notified on each event.", 17)
    _w.append(note(16, "shell", "completed", "cat > brief.md <<'EOF'", "bk1brief", _brief_call, 0, "/tmp/claude/bk1brief.output",
                   _brief, "后台命令完成 · cat > brief.md <<'EOF'"))
    _w += tool("Read", {"file_path": "/Users/me/code/weather/brief.md"}, "# 发布说明", 15)
    _w.append(note(14, "monitor", "event", "等 CI 跑完", "mo3ci", detail="build: 3/5 jobs passed\nlint ok, unit tests running",
                   text="Monitor 事件 · build: 3/5 jobs passed"))
    _w += tool("Bash", {"command": "git log --oneline -5"}, "a1b2c3d release notes", 13)
    _w.append(note(12, "monitor", "event", "等 CI 跑完", "mo3ci", detail="build: 5/5 jobs passed", text="Monitor 事件 · build: 5/5 jobs passed"))
    _w.append(note(11, "shell", "failed", "跑单元测试", "bk2tests", _test_call, 1, "/tmp/claude/bk2tests.output",
                   text="后台命令失败（退出码 1） · xcodebuild test -scheme Weather"))
    _w.append(note(10, "monitor", "ended", "等 CI 跑完", "mo3ci",
                   detail="build: 5/5 jobs passed", text="Monitor 结束 · 等 CI 跑完"))
    _w.append(it("assistant", "text", 9, text="发布说明写好了（brief.md），CI 全绿；本地单元测试有一项没过，已经记下来。"))
    # A local command's output (/context): a long notice that is not a task.
    _w.append(it("system", "notice", 8, text="Context Usage\nclaude-opus · 48k/200k tokens (24%)\nSystem prompt: 3.1k\nSystem tools: 12.4k\n"
                                              "Messages: 30.2k\nFree space: 152k (76%)\nAutocompact buffer: 22.5k"))
    ITEMS[TASKS] = _w


# MOCK_WAKES=1: a session whose answer started three background tasks; each woke the agent when it ended (wake notices),
# and it worked a little and answered each (the 3 个后台任务完成 drawer).
WAKES = "a1f3c2d4-0000-4000-8000-00000000000b"
if os.environ.get("MOCK_WAKES") == "1":
    SESSIONS.append(sess("macbook", WAKES, "发版前检查", "/Users/me/code/weather", "done", "unread", "weather", 1, "check", 6, 2,
                         "lint 有两处要改，已经改好提交了。"))

    def wake(ts_min, status, title, task_id, text):
        return it("system", "notice", ts_min, text=text, wake=True,
                  task={"kind": "shell", "status": status, "title": title, "task_id": task_id})
    _k = [it("user", "text", 40, text="发版前把测试、构建、lint 都在后台跑一下，好了告诉我")]
    _k += tool("Bash", {"command": "xcodebuild test", "description": "跑测试", "run_in_background": True},
               "Command running in background with ID: bkt. Output is being written to: /tmp/claude/bkt.output", 39)
    _k += tool("Bash", {"command": "xcodebuild build", "description": "构建", "run_in_background": True},
               "Command running in background with ID: bkb. Output is being written to: /tmp/claude/bkb.output", 39)
    _k += tool("Bash", {"command": "swiftlint", "description": "lint", "run_in_background": True},
               "Command running in background with ID: bkl. Output is being written to: /tmp/claude/bkl.output", 39)
    _k.append(it("assistant", "text", 38, text="三项都在后台跑了，结束了我逐个告诉你。"))
    _k.append(wake(30, "completed", "跑测试", "bkt", "后台命令完成 · 跑测试"))
    _k.append(it("assistant", "thinking", 30, text="看一下测试输出有没有警告。"))
    _k += tool("Read", {"file_path": "/tmp/claude/bkt.output"}, "Test Suite 'All tests' passed", 29)
    _k.append(it("assistant", "text", 29, text="测试全过了，128 个。"))
    _k.append(wake(20, "completed", "构建", "bkb", "后台命令完成 · 构建"))
    _k += tool("Bash", {"command": "ls build/"}, "Weather.app", 19)
    _k.append(it("assistant", "text", 19, text="构建也好了，产物在 build/Weather.app。"))
    _k.append(wake(10, "failed", "lint", "bkl", "后台命令失败（退出码 2） · lint"))
    _k += tool("Read", {"file_path": "/tmp/claude/bkl.output"}, "2 violations", 9)
    _k += tool("Edit", {"file_path": "/Users/me/code/weather/Widget.swift", "old_string": "let x = 1", "new_string": "let count = 1"}, "ok", 8)
    _k += tool("Bash", {"command": "git commit -am 'lint'"}, "[main 1a2b3c] lint", 7)
    _k.append(it("assistant", "text", 6, text="lint 有两处要改，已经改好提交了。"))
    ITEMS[WAKES] = _k


# MOCK_API_ERRORS=1: model API errors as Claude Code writes them when it gives up. The home conversation ends with the
# same 502 three times (the user said 继续 twice in between); an idle Agent's last answer is a login error. The texts
# follow real ones with every id, host and region replaced.
if os.environ.get("MOCK_API_ERRORS") == "1":
    _gpu = ("API Error: 502 upstream stream error: rpc error: code = 13 desc = Req 202610302115A1B2C3D4E5F6 "
            "Session 1ebd0000:7e470000 streamingcall loadbalance failed (target=default, roles=ROLE_DECODE,ROLE_PREFILL): "
            "no available GPU pod [biz error]. This is a server-side issue, usually temporary — try again in a moment. "
            "If it persists, check your inference gateway (gateway.example.com).")
    ITEMS[ASK1] = ITEMS[ASK1] + [
        it("user", "text", 2, text="帮我看看今天下午的会要准备什么"),
        it("assistant", "text", 2, text=_gpu),
        it("user", "text", 1, text="继续"),
        it("assistant", "text", 1, text=_gpu),
        it("user", "text", 1, text="继续"),
        it("assistant", "text", 1, text=_gpu),
    ]
    ITEMS[REVIEW] = ITEMS.get(REVIEW, []) + [
        it("user", "text", 1, text="接着把命名改完"),
        it("assistant", "text", 1, text="API Error: 401 {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\","
           "\"message\":\"OAuth token has expired. Please obtain a new token or refresh your existing token.\"},"
           "\"request_id\":\"req_011CUdExampleExample01\"} · Please run /login"),
    ]

# MOCK_LONG_HISTORY=1: the weather chat with fifteen earlier exchanges in front, so its work line and thought sit in
# the middle of a long chat and the list can scroll both ways around them (opening and closing them in place).
if os.environ.get("MOCK_LONG_HISTORY") == "1":
    _hist = []
    for _k in range(15):
        _hist.append(it("user", "text", 600 - _k * 30, text="第 %d 个问题：顺便看一下这一块的写法有没有问题？" % (_k + 1)))
        _hist.append(it("assistant", "text", 599 - _k * 30, text="看过了，第 %d 块没有问题。命名可以再统一一点，"
                                                            "测试也覆盖到了主要的分支，我顺手把两处重复的判断合并了。" % (_k + 1)))
    _all = _hist + ITEMS[WIDGET]
    for _i, _d in enumerate(_all):  # transcript order: the history first
        _d["seq"] = _i + 1
    ITEMS[WIDGET] = _all

# MOCK_DRAWER=1: the main conversation ends with a reply whose code block is much wider than the phone, for the sidebar's
# drag (DrawerSwipeTests: a drag on the code block scrolls it, a drag anywhere else opens the sidebar).
if os.environ.get("MOCK_DRAWER") == "1":
    ITEMS[ASK1] = ITEMS[ASK1] + [
        it("user", "text", 2, text="给我一条命令，把今天的照片按拍摄时间改名"),
        it("assistant", "text", 1, text=(
            "在照片目录里跑这一条：\n\n```sh\n"
            "exiftool '-FileName<DateTimeOriginal' -d '%Y-%m-%d %H.%M.%S%%-c.%%e' -ext jpg -ext heic -if '$DateTimeOriginal ge \"$(date +%Y:%m:%d)\"' .\n"
            "```\n\n同一秒拍的会自动加上 -1、-2。")),
    ]

# MOCK_TITLES=1: the assistant's conversations have the hub's titles (docs/assistant.md「对话标题」), one with a topic
# too long for a narrow drawer; 重命名 is recorded at /mock/renamed (AssistantTitlesTests).
RENAMED = []
if os.environ.get("MOCK_TITLES") == "1":
    for s, title in zip(ASSISTANT[1:], ("1029 | 定时 | 早报", "1031 | 功能 | 约周五和韩梅梅吃饭", "1028 | 研究 | 这个季度读书笔记的主题和书单整理")):
        s["title"] = title

# MOCK_LONG_ASSISTANT=1: the Jarvis tab's main conversation is a long one, like the one a user keeps for weeks: sixty
# exchanges, each with a few tool calls and a long Markdown reply (headings, lists, a table, a code block), for measuring
# the Jarvis page's frames (docs/testing.md「卡顿测量」).
if os.environ.get("MOCK_LONG_ASSISTANT") == "1":
    _long = []
    for _n in range(60):
        _m = 60 * 24 * 3 - _n * 70
        _long.append(it("user", "text", _m + 2, text="第 %d 件事：帮我看看 weather 项目第 %d 个 PR 的改动，顺便查一下今天的日程。" % (_n + 1, _n + 1)))
        _long.append(it("assistant", "thinking", _m + 2, text="先看 PR 的改动，再查日历。"))
        _long += tool("mcp__jarvis__agents_list", {}, "6 个 Agent：2 个需要你，2 个工作中", _m + 1)
        _long += tool("Bash", {"command": "gh pr diff %d --repo me/weather | head -200" % (_n + 1)}, "".join("+ line %d\n" % k for k in range(40)), _m + 1)
        _long += tool("mcp__jarvis__phone_calendar_events", {}, "今天 2 个日程", _m + 1)
        _long.append(it("assistant", "text", _m, text=f"""## PR #{_n + 1} 看完了

改动集中在网络层，**整体没问题**，有三处建议：

1. `WeatherClient.fetch` 的重试没有上限，断网时会一直重试，建议最多 3 次、指数退避。
2. 缓存的 key 只用了城市名，同名城市（比如两个「朝阳」）会串，建议加上经纬度。
3. 新加的 `ForecastView` 在深色模式下标题对比度不够，见 https://example.com/pr/{_n + 1}/files 。

| 文件 | 改动 | 结论 |
| --- | --- | --- |
| WeatherClient.swift | +120 / -30 | 要改重试 |
| Cache.swift | +40 / -8 | 要改 key |
| ForecastView.swift | +200 / -0 | 小问题 |

建议的写法：

```swift
for attempt in 0..<3 {{
    do {{ return try await session.data(for: request) }}
    catch {{ try await Task.sleep(for: .seconds(1 << attempt)) }}
}}
```

今天的日程：
- 14:00 设计评审（会议室 3）
- 16:30 和李雷 1:1

要我直接在 PR 上留评论吗？"""))
    ITEMS[ASK1] = _long + ITEMS[ASK1]
    for _x in ITEMS[ASK1]:  # seq goes up through the conversation
        _seq[0] += 1
        _x.update(seq=_seq[0], id=f"{uuid.UUID(int=_seq[0])}#0")

# MOCK_RICH=1: a reply with a link in Chinese prose (full-width brackets right after it), a three-column table whose
# last column is long Chinese text, and a seven-column table that cannot fit (RichReplyTests).
RICH = "a1f3c2d4-0000-4000-8000-00000000000a"
if os.environ.get("MOCK_RICH") == "1":
    SESSIONS.append(sess("macbook", RICH, "整理待拍板事项", "/Users/me/code/weather", "done", "unread", "weather", 1, "decide", 5, 2,
                         "三件事要你拍板。"))
    _rich = """对比页已经放好了（http://192.0.2.10:23140/），出来我转给你拍板。拍板后整块开发，预计两天。

| # | 事项 | 裁定 |
| --- | --- | --- |
| 1 | 小组件刷新频率 | 按小时刷新，夜间（0 点到 6 点）降到每三小时一次，省电优先，用户在设置里可以改回每小时 |
| 2 | 降雨概率格式 | 统一显示成百分比，低于 10% 的不显示，避免“1% 降雨”这种没有意义的提示占位置 |
| 3 | 图标 | 夜间模式换成描边版本 |

各城市的数据源（宽表，横着滑）：

| 城市 | 温度 | 湿度 | 风速 | 降雨 | 空气质量 | 数据来源 |
| --- | --- | --- | --- | --- | --- | --- |
| 北京 | 18℃ | 40% | 3 级 | 10% | 良 | 中国气象局开放平台 |
| 上海 | 21℃ | 65% | 2 级 | 30% | 优 | 中国气象局开放平台 |

参考：https://en.wikipedia.org/wiki/Weather_(disambiguation)。"""
    ITEMS[RICH] = [it("user", "text", 5, text="还有哪些要我拍板？"), it("assistant", "text", 2, text=_rich)]

# MOCK_PORTS=1: an agent running a dev server (docs/ports.md): its session lists the port, its reply links it (and a
# server it has stopped), and …/ports/{port}/connect forwards a WebSocket's bytes to a real page served here on
# another port (PortForwardTests). GET /mock/ports lists the connects.
LANDING = "a1f3c2d4-0000-4000-8000-00000000000d"
PORT_LOG = []
if os.environ.get("MOCK_PORTS") == "1":
    import socket
    from http.server import SimpleHTTPRequestHandler

    _page = """<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>天气 · 落地页</title>
<meta name="viewport" content="width=device-width,initial-scale=1"><style>
:root{color-scheme:light dark;--bg:#f7f7fb;--fg:#15151a;--card:#fff;--muted:#6b6b76}
@media (prefers-color-scheme:dark){:root{--bg:#0f0f14;--fg:#f2f2f7;--card:#1c1c24;--muted:#9a9aa6}}
body{margin:0;font:17px -apple-system,system-ui;background:var(--bg);color:var(--fg)}
header{padding:56px 24px 24px}h1{font-size:34px;margin:0 0 8px}p{color:var(--muted);margin:0}
.card{margin:16px 24px;padding:20px;border-radius:16px;background:var(--card)}
.big{font-size:56px;font-weight:700}</style></head><body>
<header><h1 id="headline">明天有雨</h1><p>本地开发服务器 · 热更新已连接</p></header>
<div class="card"><div class="big">18℃</div><p>降雨概率 70%，记得带伞</p></div>
<div class="card"><p>这是 Agent 在电脑上起的页面，通过 hub 转发到手机。</p></div>
</body></html>"""

    class _Dev(BaseHTTPRequestHandler):
        def do_GET(self):
            b = _page.encode()
            self.send_response(200); self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)

        def log_message(self, *a): pass

    _dev = ThreadingHTTPServer(("127.0.0.1", 0), _Dev)
    threading.Thread(target=_dev.serve_forever, daemon=True).start()
    DEV_PORT = _dev.server_address[1]
    _s = socket.socket(); _s.bind(("127.0.0.1", 0)); GONE_PORT = _s.getsockname()[1]; _s.close()
    SESSIONS.insert(0, sess("macbook", LANDING, "天气 App 的落地页", "/Users/me/code/weather-site", "idle", "unread", "weather", 1,
                            "site", 6, 1, "落地页起好了，在 :%d。" % DEV_PORT,
                            ports=[{"port": DEV_PORT, "addr": "127.0.0.1", "pid": 4242, "process": "node", "title": "天气 · 落地页"}]))
    ITEMS[LANDING] = [it("user", "text", 6, text="把落地页跑起来给我看看"),
                      it("assistant", "text", 1, text=(
                          "落地页起好了：http://localhost:%d/ ，改动会热更新。\n\n"
                          "之前那版预览（http://127.0.0.1:%d/preview）已经停掉了。" % (DEV_PORT, GONE_PORT)))]


def port_listening(port):
    import socket
    try:
        socket.create_connection(("127.0.0.1", port), timeout=1).close()
        return True
    except OSError:
        return False


# MOCK_LINKS=1: links in the Jarvis tab's main conversation and in an inbox item, to open in the link sheet
# (docs/android.md「链接」): a plain web page, a jarvis:// link that only navigates, and a javascript: link that is dropped.
if os.environ.get("MOCK_LINKS") == "1":
    ITEMS[ASK1] = ITEMS[ASK1] + [it("assistant", "text", 1, text=(
        "资料在 [示例页面](https://example.com/) 里；要看那个 Agent 就点 [这里](jarvis://session/macbook/" + RICH +
        ")。这个不会打开：[脚本](javascript:alert(1))。"))]
    INBOX.insert(0, {"id": "mail:links:1", "source": "mail", "kind": "mail", "account": "work", "account_name": "工作邮箱",
                     "title": "评审材料", "sender": "Lucy Wang", "preview": "材料在 https://example.org/ ，评审前看一下",
                     "count": 1, "started_at": ago(minutes=8), "updated_at": ago(minutes=8), "tier": "action",
                     "summary": "评审材料的链接：https://example.org/", "read": False})


def fold_work(items):
    """transcript.FoldWork: each run of tool calls (with their results) and thoughts becomes one fold item."""
    uses = {x.get("tool_use_id") for x in items if x["kind"] == "tool_use"}
    results = {x.get("tool_use_id") for x in items if x["kind"] == "tool_result"}

    def inside(x):
        if x["kind"] == "thinking":
            return True
        t = x.get("tool_use_id")
        return x["kind"] in ("tool_use", "tool_result", "image") and t and t in uses and t in results
    out, i = [], 0
    while i < len(items):
        if not inside(items[i]):
            out.append(items[i]); i += 1; continue
        j = i
        while j < len(items) and inside(items[j]):
            j += 1
        run = items[i:j]
        if len(run) < 3 and sum(len(x.get("text") or "") + len(json.dumps(x.get("tool_input") or "")) for x in run) < 4096:
            out += run
        else:
            out.append({"id": "fold:" + run[0]["id"], "seq": run[0]["seq"], "ts": run[0]["ts"], "role": "assistant", "kind": "fold",
                        "fold": {"items": len(run), "tools": sum(x["kind"] == "tool_use" for x in run),
                                 "thoughts": sum(x["kind"] == "thinking" and bool((x.get("text") or "").strip()) for x in run),
                                 "until": run[-1]["ts"], "through_seq": run[-1]["seq"], "through_id": run[-1]["id"]}})
        i = j
    return out


def served(sid, via, payload):
    if sid == BIG:
        SERVED.append({"via": via, "items": len(payload["items"]), "bytes": len(json.dumps(payload, ensure_ascii=False).encode())})


def all_sessions():
    return [decorated_session(s) for s in (SESSIONS + ASSISTANT)]


def find_items(sid):
    items = ITEMS.get(sid, [])
    if GAP and sid == SESSIONS[0]["session_id"] and not GAP_STATE["grown"]:
        return items[:-6]  # what the transcript held before the lines whose push gets lost
    return items


# Every items batch says where it sits in the transcript (docs/protocol.md): one history per session here, to_seq the
# last seq, from_seq for batches that continue from a seq.
HISTORY = "mock-1"
# MOCK_GAP=1: the first conversation's subscribe backlog leaves out its last items, then a live push brings only the
# final two, as if a batch in between had been lost; the app should notice the hole and fetch it (after_seq).
PHOTO = "a1f3c2d4-0000-4000-8000-00000000000c"
if os.environ.get("MOCK_PHOTO") == "1":  # a screenshot in the chat, to save to the photo library (PhotoSaveTests)
    import struct, zlib

    def _png(w, h, rgb):
        def chunk(t, d):
            return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
        raw = b"".join(b"\0" + bytes(rgb) * w for _ in range(h))
        return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
                + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))

    SESSIONS.append(sess("macbook", PHOTO, "看看这张截图", "/Users/me/code/weather", "done", "read", "weather", 1, "shot", 6, 3,
                         "截图里的布局没问题。"))
    _shot = {"mime": "image/png", "data": base64.b64encode(_png(64, 48, (52, 120, 246))).decode(), "width": 64, "height": 48}
    ITEMS[PHOTO] = [it("user", "text", 4, text="看看这张截图"), it("user", "image", 4, image=_shot),
                    it("assistant", "text", 3, text="截图里的布局没问题。")]


GAP = os.environ.get("MOCK_GAP") == "1"

# ---------- files (docs/ios.md「文件查看器」) ----------
# MOCK_FILES=1: a made-up checkout of the weather app under /Users/me/code/weather that GET …/file and …/stat answer
# from, with big files for timing the viewer (a 3 MB JSON, a 20 000-line Go file, a 5 000-line README, a 5 MB log),
# one small file per language for screenshots, and a last message in the widget conversation that names some of them
# and two that are not there (another machine's path, a deleted file).
FILES = {}
FILE_PAGE = 2 << 20
if os.environ.get("MOCK_FILES"):
    import random
    rnd = random.Random(7)
    ROOT = "/Users/me/code/weather"

    def big_json():
        cities = ["上海", "北京", "Tokyo", "Lisbon", "Reykjavík", "São Paulo", "Nairobi", "Seattle"]
        rows = []
        for i in range(6000):
            rows.append({"id": i, "city": cities[i % len(cities)], "at": "2026-10-%02dT%02d:00:00Z" % (1 + i % 28, i % 24),
                         "temperature": {"c": round(rnd.uniform(-8, 34), 1), "feels_like": round(rnd.uniform(-10, 36), 1)},
                         "rain": {"chance": rnd.randint(0, 100), "mm": round(rnd.uniform(0, 12), 2)},
                         "wind": {"speed": round(rnd.uniform(0, 40), 1), "direction": rnd.choice(["N", "NE", "E", "SE", "S", "SW", "W", "NW"])},
                         "alerts": [] if i % 9 else [{"kind": "storm", "level": 2, "note": "雷阵雨，注意出行"}],
                         "stale": i % 13 == 0, "source": None if i % 17 == 0 else "model-v4"})
        return json.dumps({"version": 3, "generated": "2026-10-31T09:00:00Z", "count": len(rows), "forecasts": rows},
                          ensure_ascii=False, indent=2)

    def long_go():
        out = ["// Package server serves the weather API (made-up code for the viewer's timing).", "package server", "",
               "import (", '\t"context"', '\t"encoding/json"', '\t"fmt"', '\t"net/http"', '\t"time"', ")", ""]
        n = 0
        while len(out) < 20000:
            n += 1
            out += [
                "// handleForecast%d answers GET /v1/forecast/%d with the next hours for a city." % (n, n),
                "func (s *Server) handleForecast%d(w http.ResponseWriter, r *http.Request) {" % n,
                "	ctx, cancel := context.WithTimeout(r.Context(), %d*time.Second)" % (2 + n % 5),
                "	defer cancel()",
                '	city := r.URL.Query().Get("city")',
                '	if city == "" {',
                '		http.Error(w, "missing city", http.StatusBadRequest)',
                "		return",
                "	}",
                "	hours, err := s.store.Hourly(ctx, city, %d)" % (6 + n % 18),
                "	if err != nil {",
                '		s.log.Printf("forecast %%s: %%v", city, err) /* logged, not shown */',
                '		http.Error(w, fmt.Sprintf("no forecast for %%q", city), http.StatusNotFound)',
                "		return",
                "	}",
                '	w.Header().Set("Content-Type", "application/json; charset=utf-8")',
                "	_ = json.NewEncoder(w).Encode(map[string]any{\"city\": city, \"hours\": hours, \"ok\": true, \"rate\": %d.5})" % n,
                "}",
                "",
            ]
        return "\n".join(out[:20000]) + "\n"

    def long_markdown():
        out = ["# 天气 App 开发手册", "", "这份文档是编造的，用来测文件查看器打开长 Markdown 的速度。", "",
               "[api]: https://example.com/api", ""]
        for i in range(1, 140):
            out += ["## %d. 第 %d 部分：小组件和时间线" % (i, i), "",
                    "小组件每小时刷新一次（**时间线**由 `Provider` 生成），降雨概率取接下来 *6 小时* 的预报。详见 [API 说明][api] 和 <https://example.com/docs/%d>。" % i, "",
                    "- 小号：只显示温度", "- 中号：温度 + 6 小时降雨条", "  - 降雨条用 `Theme.Hue.blue`", "1. 先读缓存", "2. 再请求网络", "",
                    "> 注意：后台刷新有配额，别在时间线里发网络请求。", "",
                    "```swift", "func timeline(for config: Config) async -> Timeline<Entry> {",
                    "    let hours = await store.hourly(limit: 6) // cached", '    return Timeline(entries: hours.map(Entry.init), policy: .after(.now + 3600))', "}", "```", "",
                    "| 尺寸 | 内容 | 刷新 |", "| --- | --- | --- |", "| 小号 | 温度 | 每小时 |", "| 中号 | 温度、降雨 | 每小时 |", "",
                    "```json", '{"city": "上海", "hours": [0.1, 0.4, 0.8], "ok": true}', "```", "",
                    "这一节的最后一段比较长：" + "小组件的数据来自 App Group 里共享的缓存文件，主 App 每次拉到新预报都会写一份，小组件只读不写，避免两边同时写坏文件。" * 2, ""]
        return "\n".join(out[:5000]) + "\n"

    def log():
        lines = []
        size = 0
        n = 0
        while size < 5 * (1 << 20):
            n += 1
            l = "2026-10-31T09:%02d:%02d.%03dZ INFO  request id=%06d path=/v1/forecast/%d city=%s status=200 took=%dms" % (
                n // 3600 % 60, n // 60 % 60, n % 1000, n, n % 400, ["上海", "Tokyo", "Lisbon"][n % 3], 3 + n % 90)
            lines.append(l)
            size += len(l.encode()) + 1
        return "\n".join(lines) + "\n"

    SMALL = {
        "Sources/App.swift": """import SwiftUI\n\n/// The app's entry point.\n@main\nstruct WeatherApp: App {\n    @State private var store = WeatherStore()\n\n    var body: some Scene {\n        WindowGroup {\n            ContentView()\n                .environment(store)\n                .task { await store.refresh(every: .seconds(3600)) } // hourly\n        }\n    }\n}\n\nextension WeatherStore {\n    func refresh(every interval: Duration) async {\n        while !Task.isCancelled {\n            try? await load(city: \"上海\", hours: 6)\n            try? await Task.sleep(for: interval)\n        }\n    }\n}\n""",
        "scripts/deploy.sh": """#!/usr/bin/env bash\n# Deploy the API to the server (made up).\nset -euo pipefail\n\nHOST=${DEPLOY_HOST:-api.example.com}\nfor svc in api worker; do\n  echo \"deploying $svc to $HOST\"\n  ssh \"$HOST\" \"systemctl restart weather-$svc\" || exit 1\ndone\n""",
        "config/app.yaml": """# Runtime config (made up)\nserver:\n  port: 8080\n  timeout: 30s\n  cities: [上海, Tokyo, Lisbon]\ncache: &cache\n  ttl: 3600\n  enabled: true\nworker:\n  <<: *cache\n  queue: \"forecasts\"\n""",
        "Cargo.toml": """[package]\nname = \"weather-core\"\nversion = \"0.4.1\"\nedition = \"2021\"\n\n[dependencies]\nserde = { version = \"1\", features = [\"derive\"] }\ntokio = \"1.41\" # async runtime\n""",
        "web/index.html": """<!DOCTYPE html>\n<html lang=\"zh\">\n<head>\n  <meta charset=\"utf-8\">\n  <title>天气 &amp; 预报</title>\n  <link rel=\"stylesheet\" href=\"styles.css\">\n</head>\n<body>\n  <!-- the forecast list -->\n  <ul id=\"hours\" class=\"forecast\"></ul>\n  <script src=\"app.js\" defer></script>\n</body>\n</html>\n""",
        "web/styles.css": """/* Forecast list */\n.forecast > li:hover {\n  color: #0a84ff;\n  margin: 0 1.5rem;\n  font-weight: 600 !important;\n}\n@media (max-width: 600px) {\n  .forecast { padding: 4px; }\n}\n""",
        "Dockerfile": """FROM golang:1.23 AS build\nWORKDIR /src\nCOPY . .\n# build a static binary\nRUN CGO_ENABLED=0 go build -o /out/api ./cmd/api && \\\n    echo \"built $(date)\"\n\nFROM gcr.io/distroless/static\nCOPY --from=build /out/api /api\nENTRYPOINT [\"/api\"]\n""",
        "db/schema.sql": """-- Forecast storage (made up)\nCREATE TABLE forecasts (\n  id BIGSERIAL PRIMARY KEY,\n  city TEXT NOT NULL,\n  at TIMESTAMPTZ NOT NULL,\n  rain_chance INT DEFAULT 0\n);\nSELECT city, count(*) FROM forecasts WHERE at > now() - interval '1 day' GROUP BY city;\n""",
        "tools/fetch.py": """#!/usr/bin/env python3\n\"\"\"Fetch forecasts for the cities (made up).\"\"\"\nimport json\nfrom dataclasses import dataclass\n\n@dataclass\nclass Hour:\n    at: str\n    rain: float = 0.0\n\ndef fetch(city: str, hours: int = 6) -> list[Hour]:\n    print(f\"fetching {city}\")  # progress\n    return [Hour(at=str(h), rain=h / 10) for h in range(hours)] if city else None\n""",
        "web/app.ts": """// Renders the forecast list (made up).\ninterface Hour { at: string; rain: number }\n\nexport async function render(city: string): Promise<void> {\n  const res = await fetch(`/v1/forecast?city=${encodeURIComponent(city)}`);\n  const hours: Hour[] = await res.json();\n  document.querySelector('#hours')!.innerHTML = hours.map(h => `<li>${h.at}: ${Math.round(h.rain * 100)}%</li>`).join('');\n}\n""",
        "core/src/lib.rs": """//! Rain chance smoothing (made up).\n#[derive(Debug, Clone)]\npub struct Hour<'a> { pub at: &'a str, pub rain: f32 }\n\npub fn smooth<'a>(hours: &[Hour<'a>]) -> Vec<f32> {\n    println!(\"smoothing {} hours\", hours.len());\n    hours.windows(3).map(|w| w.iter().map(|h| h.rain).sum::<f32>() / 3.0).collect()\n}\n""",
        "android/Main.kt": """package app.weather\n\nimport androidx.compose.runtime.Composable\n\n@Composable\nfun Forecast(hours: List<Hour>) {\n    val rainy = hours.filter { it.rain > 0.5 } // likely rain\n    Text(text = \"${rainy.size} 小时有雨\")\n}\n""",
        "native/buffer.c": """#include <stdio.h>\n#include <stdlib.h>\n\n/* A ring buffer of readings (made up). */\ntypedef struct { float *data; size_t size, head; } ring_t;\n\nint ring_push(ring_t *r, float v) {\n    if (r == NULL) return -1;\n    r->data[r->head++ % r->size] = v;\n    printf(\"pushed %.1f\\n\", v);\n    return 0;\n}\n""",
        "Makefile": """# Build and test (made up)\nGO ?= go\nBIN := bin/api\n\n.PHONY: build test\nbuild:\n\t$(GO) build -o $(BIN) ./cmd/api\n\ntest: build\n\t$(GO) test ./... -count=1\n""",
    }
    for rel, text in SMALL.items():
        FILES[ROOT + "/" + rel] = text
    FILES[ROOT + "/data/forecast.json"] = big_json()
    FILES[ROOT + "/server/handlers.go"] = long_go()
    FILES[ROOT + "/README.md"] = long_markdown()
    FILES[ROOT + "/logs/api.log"] = log()
    ITEMS[WIDGET] = ITEMS[WIDGET] + [it("assistant", "text", 0, text=(
        "hub 上的 `~/.jarvis/personas/doudou` 不在这台电脑上，`/Users/me/code/weather/old/Removed.swift` 也已经删掉了。"
        "其余的都在：`Sources/App.swift`、`server/handlers.go`、`data/forecast.json`、`/Users/me/code/weather/README.md`、"
        "`config/app.yaml` 和 `/Users/me/code/weather/logs/api.log`。"))]


def file_path(path, cwd):
    path = path.strip()
    if path == "~" or path.startswith("~/"):
        path = "/Users/me" + path[1:]
    elif not path.startswith("/"):
        if not cwd:
            return ""
        path = cwd.rstrip("/") + "/" + path
    return os.path.normpath(path)


def file_kind(path):
    if path in FILES:
        return "file"
    prefix = path.rstrip("/") + "/"
    return "dir" if any(f.startswith(prefix) for f in FILES) else "missing"


def file_read(path, cwd, offset):
    p = file_path(path, cwd)
    kind = file_kind(p) if p else "missing"
    if kind == "missing":
        return None
    name = os.path.basename(p.rstrip("/"))
    if kind == "dir":
        prefix = p.rstrip("/") + "/"
        names = sorted({f[len(prefix):].split("/")[0] + ("/" if "/" in f[len(prefix):] else "") for f in FILES if f.startswith(prefix)})
        return {"path": p, "name": name, "size": 0, "is_dir": True, "entries": names, "truncated": False}
    data = FILES[p].encode()
    page = data[offset:offset + FILE_PAGE]
    more = offset + len(page) < len(data)
    if more:
        page = page[:page.rfind(b"\n") + 1]
    ext = os.path.splitext(name)[1].lower()
    mime = {".json": "application/json", ".md": "text/markdown"}.get(ext, "text/plain; charset=utf-8")
    return {"path": p, "name": name, "size": len(data), "mime": mime, "text": page.decode(), "truncated": more,
            "is_dir": False, "offset": offset, "next": offset + len(page) if more else 0}


# ---------- personas (docs/persona.md) ----------
# MOCK_PERSONAS=<dir>: the packages in it, as the hub lists and serves ~/.jarvis/personas. MOCK_PERSONAS=demo: the
# original placeholder (scripts/assets/make_persona.py demo), made into a temporary directory at start.
PERSONAS_DIR = os.environ.get("MOCK_PERSONAS", "")
if PERSONAS_DIR == "demo":
    import subprocess, tempfile
    PERSONAS_DIR = tempfile.mkdtemp(prefix="mock-personas-")
    subprocess.run([sys.executable, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "assets", "make_persona.py"),
                    "demo", "--out", PERSONAS_DIR], check=True, stdout=sys.stderr)


def personas():
    out = []
    if not PERSONAS_DIR or not os.path.isdir(PERSONAS_DIR):
        return out
    for pid in sorted(os.listdir(PERSONAS_DIR)):
        root = os.path.join(PERSONAS_DIR, pid)
        mpath = os.path.join(root, "persona.json")
        if pid.startswith(".") or not os.path.isfile(mpath):
            continue
        files = sorted(os.path.relpath(os.path.join(r, f), root) for r, ds, fs in os.walk(root) for f in fs
                       if not f.startswith(".") and not any(x.startswith(".") for x in os.path.relpath(r, root).split(os.sep) if x != "."))
        h, total = hashlib.sha256(), 0
        for f in files:
            b = open(os.path.join(root, f), "rb").read()
            h.update(("%s\0%d\0" % (f, len(b))).encode()); h.update(b)
            total += len(b)
        out.append({"id": pid, "rev": h.hexdigest()[:16], "bytes": total, "manifest": json.load(open(mpath))})
    return out


def persona_file(rest):
    """<id>/<file> inside the directory, or None."""
    if not PERSONAS_DIR:
        return None
    pid, _, f = rest.partition("/")
    root = os.path.realpath(os.path.join(PERSONAS_DIR, pid))
    p = os.path.realpath(os.path.join(root, f))
    if not p.startswith(root + os.sep) or not os.path.isfile(p) or os.path.basename(p).startswith("."):
        return None
    return p
# MOCK_STREAM=1: the first conversation keeps working after it is opened: every 2 s a thought, a tool call or a reply
# arrives (a reply then folds into the work line), 12 times. For checking that reading history holds still.
STREAM = os.environ.get("MOCK_STREAM") == "1"
STREAM_STATE = {"started": False}
GAP_STATE = {"started": False, "grown": False}


def span(items, frm=None, to=None):
    out = {"history": HISTORY, "to_seq": to if to is not None else (items[-1]["seq"] if items else 0)}
    if frm is not None:
        out["from_seq"] = frm
    return out


# ---------- a bad network, on demand ----------
# MOCK_DELAY=<seconds>: every request and pong is that late (a weak network). At runtime, for UI tests:
#   GET /mock/delay?s=3   the same, from now on (0 turns it off)
#   GET /mock/down?s=8    the hub is gone for that long: open WebSockets drop, new connections are closed unanswered
NET = {"delay": float(os.environ.get("MOCK_DELAY", "0")), "down_until": 0.0}
SOCKETS = set()


# --- Files & Git (read-only browser: /machines/{mid}/file, fs, git/*). Made-up tree under each machine's home. ---
def mock_png(w=48, h=32):
    """A small gradient PNG for the image viewer (built here: no binary in the repo)."""
    import zlib
    rows = b"".join(b"\x00" + bytes(c for x in range(w) for c in (x * 255 // w, y * 255 // h, 160)) for y in range(h))
    chunk = lambda t, d: struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
    return base64.b64encode(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)) +
                            chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b"")).decode()


MOCK_PNG = mock_png()

# MOCK_CHAT_IMAGES=1: the blog conversation gets a screenshot the agent took (an image item of its tool's result) and
# one it showed before its answer, for the chat's thumbnails and the image viewer. Made-up page, inline bytes; seqs
# renumbered so the list stays in order. Off by default so other screenshots keep their transcripts.
_shot = "toolu_mock_screenshot"
if os.environ.get("MOCK_CHAT_IMAGES"):
    ITEMS[BLOG] = ITEMS[BLOG][:-1] + [
        it("assistant", "tool_use", 13, tool_use_id=_shot, tool_name="mcp__browser__screenshot", tool_input={"url": "http://localhost:4321/"}),
        it("user", "tool_result", 13, tool_use_id=_shot, text="截图已保存"),
        it("user", "image", 13, tool_use_id=_shot, image={"mime": "image/png", "data": MOCK_PNG, "width": 48, "height": 32}),
        it("assistant", "image", 12, image={"mime": "image/png", "data": MOCK_PNG, "width": 48, "height": 32}),
    ] + ITEMS[BLOG][-1:]
    for _i, _x in enumerate(ITEMS[BLOG]): _x["seq"] = ITEMS[BLOG][0]["seq"] + _i
MOCK_TREE = {
    "code/": None,
    "code/weather/": None,
    "code/weather/.gitignore": ".build/\n",
    "code/weather/README.md": "# Weather\n\nA small sample app.\n\n<p align=\"center\"><img src=\"docs/icon.png\" alt=\"icon\"></p>\n\n- 今天的天气\n- `swift build`\n",
    "code/weather/Package.swift": "// swift-tools-version:6.0\nimport PackageDescription\n\nlet package = Package(\n    name: \"Weather\",\n    targets: [.executableTarget(name: \"App\")]\n)\n",
    "code/weather/Sources/": None,
    "code/weather/Sources/App/": None,
    "code/weather/Sources/App/main.swift": "import Foundation\n\nlet city = CommandLine.arguments.dropFirst().first ?? \"Paris\"\nprint(\"Weather for \\(city): sunny, 21°C\")\n",
    "code/weather/docs/": None,
    "code/weather/docs/icon.png": ("image/png", MOCK_PNG),
    "notes.txt": "Remember to water the plants.\n",
}
MOCK_REPO = "code/weather"
MOCK_COMMITS = [{"hash": "%040x" % (0xa1b2c3d0 + i), "short": "%07x" % (0xa1b2c3d0 + i),
                 "subject": s, "author": "Sample Dev", "date": ago(hours=3 * i + 1),
                 **({"refs": "HEAD -> main, origin/main"} if i == 0 else {})}
                for i, s in enumerate(["Show the city name", "Add README", "Package skeleton"] +
                                      ["Tweak #%d" % n for n in range(40)])]
MOCK_DIFFS = {
    "Sources/App/main.swift": "diff --git a/Sources/App/main.swift b/Sources/App/main.swift\n--- a/Sources/App/main.swift\n+++ b/Sources/App/main.swift\n"
                              "@@ -1,3 +1,4 @@\n import Foundation\n \n-print(\"Weather: sunny\")\n+let city = CommandLine.arguments.dropFirst().first ?? \"Paris\"\n+print(\"Weather for \\(city): sunny, 21°C\")",
    "README.md": "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1,2 +1,4 @@\n # Weather\n \n+A small sample app.\n+",
    "docs/icon.png": "diff --git a/docs/icon.png b/docs/icon.png\nnew file mode 100644\nBinary files /dev/null and b/docs/icon.png differ",
}


def mock_home(mid):
    return next((m.get("home") for m in MACHINES if m["id"] == mid), None) or "/home/me"


def mock_abs(mid, path, cwd):
    home = mock_home(mid)
    path = (path or "").strip() or (cwd or "")
    if path == "~" or path.startswith("~/"): path = home + path[1:]
    elif not path.startswith("/") and cwd: path = cwd.rstrip("/") + "/" + path
    return home, "/" + "/".join(x for x in path.split("/") if x and x != ".")


def mock_rel(home, path):
    """The tree key of an absolute path (dirs end with /), or None outside the tree."""
    if path == home: return ""
    if not path.startswith(home + "/"): return None
    rel = path[len(home) + 1:]
    return rel + "/" if rel + "/" in MOCK_TREE else rel if rel in MOCK_TREE else None


def mock_children(rel):
    out = []
    for k in MOCK_TREE:
        if k.startswith(rel) and k != rel:
            rest = k[len(rel):]
            if rest.count("/") == 0 or (rest.count("/") == 1 and rest.endswith("/")): out.append(k)
    return sorted(out, key=lambda k: (not k.endswith("/"), k.lower()))


def mock_size(v):
    return 0 if v is None else len(base64.b64decode(v[1])) if isinstance(v, tuple) else len(v.encode())


def mock_files(p, q):
    """(json, status) for the files / Git endpoints, or None when p is not one of them."""
    parts = p.split("/")
    if not (p.startswith("/api/v1/machines/") and len(parts) >= 6): return None
    mid, what = parts[4], "/".join(parts[5:])
    if what not in ("file", "fs", "git/status", "git/diff", "git/show", "git/log"): return None
    if not machine_online(mid): return {"error": "%s: machine is offline" % mid}, 503
    arg = lambda k: (q.get(k) or [""])[0]
    if what in ("file", "fs"):
        home, path = mock_abs(mid, arg("path"), arg("cwd"))
        rel = mock_rel(home, path)
        if rel is None: return {"error": "open %s: no such file or directory" % path}, 502
        is_dir = rel == "" or rel.endswith("/")
        if what == "fs":
            if not is_dir: return {"error": "readdir %s: not a directory" % path}, 502
            ents = [{"name": k[len(rel):].rstrip("/"), "dir": k.endswith("/"), "size": mock_size(MOCK_TREE[k]),
                     "mtime": ago(hours=i + 1)} for i, k in enumerate(mock_children(rel))]
            return {"path": path, "entries": ents, "truncated": False}, 200
        name = path.rsplit("/", 1)[-1]
        if is_dir:
            return {"path": path, "name": name, "size": 0, "mime": "", "is_dir": True, "truncated": False,
                    "entries": [k[len(rel):] for k in mock_children(rel)]}, 200
        v = MOCK_TREE[rel]
        if isinstance(v, tuple):
            return {"path": path, "name": name, "size": mock_size(v), "mime": v[0], "data": v[1], "is_dir": False, "truncated": False}, 200
        mime = "text/markdown" if name.endswith(".md") else "text/plain"
        return {"path": path, "name": name, "size": mock_size(v), "mime": mime, "text": v, "is_dir": False, "truncated": False}, 200
    home, cwd = mock_abs(mid, arg("cwd"), "")
    root = home + "/" + MOCK_REPO
    if not (cwd == root or cwd.startswith(root + "/")):
        return ({"is_repo": False, "files": [], "commits": []}, 200) if what == "git/status" else ({"error": "not a git repository"}, 502)
    if what == "git/status":
        return {"is_repo": True, "root": root, "branch": "main", "head": MOCK_COMMITS[0], "upstream": "origin/main",
                "ahead": 1, "behind": 0, "stashes": 1, "commits": MOCK_COMMITS[:30], "files": [
                    {"path": "Sources/App/main.swift", "index": "M", "worktree": "M", "added": 2, "deleted": 1, "binary": False},
                    {"path": "README.md", "index": ".", "worktree": "M", "added": 2, "deleted": 0, "binary": False},
                    {"path": "docs/icon.png", "index": "?", "worktree": "?", "added": 0, "deleted": 0, "binary": True}]}, 200
    if what == "git/diff":
        return {"path": arg("path"), "diff": MOCK_DIFFS.get(arg("path"), ""), "truncated": False}, 200
    if what == "git/log":
        skip, limit = int(arg("skip") or 0), int(arg("limit") or 50)
        return {"commits": MOCK_COMMITS[skip:skip + limit]}, 200
    c = next((c for c in MOCK_COMMITS if c["hash"] == arg("hash")), None)
    if not c: return {"error": "unknown revision"}, 502
    return {"commit": dict(c, body="Made-up commit for the mock hub."),
            "files": [{"path": "Sources/App/main.swift", "index": "M", "worktree": ".", "added": 2, "deleted": 1, "binary": False},
                      {"path": "README.md", "index": "M", "worktree": ".", "added": 2, "deleted": 0, "binary": False}],
            "diff": MOCK_DIFFS["Sources/App/main.swift"] + "\n" + MOCK_DIFFS["README.md"], "truncated": False}, 200
# --- end of Files & Git ---


def net_control(path, q):
    """Handles /mock/delay and /mock/down; True if it was one of them."""
    if path == "/mock/delay":
        NET["delay"] = float(q.get("s", ["0"])[0])
        return True
    if path == "/mock/down":
        NET["down_until"] = time.time() + float(q.get("s", ["5"])[0])
        for sock in list(SOCKETS):
            try: sock.shutdown(2)
            except OSError: pass
        return True
    return False


# ---------- live churn (LiveUpdateTests) ----------
# GET /mock/churn?n=20&ms=300&steps=1: n rounds, ms apart, of what a busy hub pushes: every machine's sessions again (the
# working ones with a new last line and time, two of them trading working / idle so the order of spaces moves), the
# machine list (a new last_seen), and `steps` new steps in the turn of each conversation a connection has open. GET
# /mock/churned tells how many rounds have gone out. &final=1: then the turn ends (a last reply, the working agents
# done; counted as one more round). For checking that what the user is doing survives the updates.
# &inbox=1: each round also changes an inbox item (a new Feishu message: the inbox's 需要你 count changes every other
# round) and pushes `inbox`, and pushes the pending interactions again; &waiting=1: one more interaction comes and goes
# (the waiting count changes every round, its card shows and goes on the Agents list). The real hub does both all day;
# the tab bar's and the shell's badges read them, the mock's other pushes never touched them.
# &real=1: what else the real hub's pushes change (recorded 2026-11-01: a push of some machine's agents every 2 s, nearly
# half of them in a new order): the working agents' last line alternates between one line and two (the row's height),
# and one of them finishes and starts again every three rounds (its space changes rank). ScrollUnderPushTests.
# MOCK_CHURN=1 also puts a screenshot (a tool result image) in the middle of the first conversation's work.
# MOCK_HUB_UPDATE=available|waiting|notice|held|docker|docker-pinned: the hub's version and its updating itself
# (docs/hub-update.md): a newer release to install (auto-update off), an update waiting for two busy agents, 「自动更新已开启」
# told for the first time, a newer release auto-update holds back (its code does not contain the hub's commit, with
# the held line in the history), or a Docker hub with a newer image (on :latest, or on a pinned version tag). Unset:
# GET /api/v1/hub/update is 404 (a hub from before), no line anywhere.
HUB_UPDATE_MODE = os.environ.get("MOCK_HUB_UPDATE", "")
HUB_UPDATE = {}


def hub_update_state():
    if not HUB_UPDATE:
        latest = {"version": "v0.3.1-4-gdef5678-202611030900", "build": 202611030900, "released": ago(minutes=40),
                  "notes": "会话列表改成增量推送，更省电；灵动岛偶尔不更新的问题修好了。", "newer": True, "bad": False}
        HUB_UPDATE.update({"version": "v0.3.0-12-gabc1234-202611021500", "build": 202611021500, "channel": "release",
                           "can_update": True, "latest": latest, "state": "available", "busy": [], "auto": False,
                           "auto_notice": False, "last_check": ago(minutes=20), "confirmed": True})
        if HUB_UPDATE_MODE == "waiting":
            HUB_UPDATE.update(state="waiting_idle", auto=True, busy=[
                {"machine_id": "macbook", "machine": "MacBook", "session_id": "a1f3c2d4-0000-4000-8000-000000000001", "title": "给天气 App 加主屏幕小组件"},
                {"machine_id": "server", "machine": "构建服务器", "session_id": "b2", "title": "整理发布脚本"}])
        elif HUB_UPDATE_MODE == "held":
            HUB_UPDATE.update(auto=True)
            HUB_UPDATE["latest"] = dict(latest, held=HELD_REASON)
        elif HUB_UPDATE_MODE == "notice":
            HUB_UPDATE.update(state="idle", auto=True, auto_notice=True)
            HUB_UPDATE["latest"] = dict(latest, version=HUB_UPDATE["version"], build=HUB_UPDATE["build"], newer=False)
        elif HUB_UPDATE_MODE in ("docker", "docker-pinned"):
            pinned = HUB_UPDATE_MODE == "docker-pinned"
            HUB_UPDATE.update(version="0.3.0", build=0, channel="docker", can_update=False, auto=False,
                              why_not="Docker 里的 hub 由新镜像更新（docker compose pull && docker compose up -d）",
                              image={"name": "ghcr.io/owner/jarvis", "tag": "0.3.0" if pinned else "latest", "pinned": pinned})
            HUB_UPDATE["latest"] = {"version": "0.3.1", "build": 0, "newer": True, "bad": False}
    return HUB_UPDATE


HELD_REASON = "新版本（提交 def56789）不包含这个 hub 正在运行的提交 abc12345：发布用的代码比这里的旧，或在另一条分支上，自动装了会退回"


def hub_update_history():
    held = [{"from_version": "v0.3.0-12-gabc1234-202611021500", "to_version": "v0.3.1-4-gdef5678-202611030900", "to_build": 202611030900,
             "started": ago(minutes=20), "ended": ago(minutes=20), "result": "held", "reason": HELD_REASON, "auto": True}]
    return (held if HUB_UPDATE_MODE == "held" else []) + HUB_UPDATE_HISTORY  # newest first, like the hub


def set_hub_update_mode(mode):
    global HUB_UPDATE_MODE
    HUB_UPDATE_MODE = mode
    HUB_UPDATE.clear()


HUB_UPDATE_HISTORY = [
    {"from_version": "v0.2.9-30-g1111111-202610311200", "to_version": "v0.3.0-12-gabc1234-202611021500", "to_build": 202611021500,
     "started": ago(hours=20), "ended": ago(hours=20), "result": "updated", "auto": True},
    {"from_version": "v0.2.9-30-g1111111-202610311200", "to_version": "v0.3.0-9-g2222222-202611011800", "to_build": 202611011800,
     "started": ago(hours=40), "ended": ago(hours=40), "result": "rolled_back", "reason": "这些机器 2 分钟内没连回来：构建服务器", "auto": True},
]

MOCK_SKILLS = [{"name": "lark-connector", "description": "Feishu / Lark through lark-connector's agent API: messages, docs, calendar, tasks.", "source": "user"},
               {"name": "weekly-report", "description": "Draft the week's report from the calendar, docs and chats.", "source": "user"}]

LIVE = []  # (send, subscribed session ids) of each open WebSocket


def sessions_for_delta_app(obj, have):
    """A machine's whole list as an app on the delta protocol gets it (hub/sessions_push.go): the first one whole,
    without the Codex history; then the sessions new or different and the ids gone; None when nothing changed."""
    m = obj["machine_id"]
    old = have.get(m)
    shown = [s for s in obj["sessions"] if s.get("source_kind") != "codex_history" or (old is not None and s["session_id"] in old)]
    enc = {s["session_id"]: json.dumps(s, sort_keys=True, ensure_ascii=False) for s in shown}
    have[m] = enc
    if old is None:
        return dict(obj, sessions=shown)
    upsert = [s for s in shown if old.get(s["session_id"]) != enc[s["session_id"]]]
    remove = sorted(i for i in old if i not in enc)
    if not upsert and not remove:
        return None
    return {"type": "sessions_delta", "machine_id": m, "upsert": upsert, "remove": remove}
CHURN = {"rounds": 0}
CHURN_ITEMS = {}  # each conversation's items before churn added to them (/mock/reset puts them back)
CHURN_INBOX = []  # the inbox before churn changed it (/mock/reset puts it back)


def png(w, h, rgb):
    """A plain w×h PNG of one color (stdlib only)."""
    import zlib
    raw = b"".join(b"\0" + bytes(rgb) * w for _ in range(h))
    def chunk(t, d): return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))


def churn(rounds, ms, steps=1, final=False, inbox=False, waiting=False, real=False):
    import time
    flips = [s for s in SESSIONS if s["state"] == "working"][-1:] if real else []
    for k in range(rounds):
        time.sleep(ms / 1000)
        now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
        if inbox and INBOX:
            if not CHURN_INBOX: CHURN_INBOX.extend(copy.deepcopy(INBOX))
            first = INBOX[0]
            first.update(preview="第 %d 条新消息：下午的会改到四点了" % (k + 1), updated_at=now, count=first.get("count", 1) + 1,
                         tier="action" if k % 2 else "fyi")
        if real:  # the agent that finishes and starts again: its space changes rank, now and then, as on the real hub
            for s in flips:
                if k % 3 == 1: s.update(state="unread", status="done", state_since=now)
                elif k % 3 == 2: s.update(state="working", status="working", state_since=now)
        working = [s for s in SESSIONS if s["state"] == "working"]
        for s in working:
            text = "第 %d 轮更新：还在跑，刚看完一处。" % (k + 1)
            if real and k % 2:  # a longer last line: the row grows to its two lines, then shrinks back
                text += "接下来把剩下的几个模块的测试都跑一遍，看看还有没有别的地方用到了这个旧接口，顺手把文档也改了。"
            s.update(updated_at=now, last_text=text)
        if len(working) >= 2 and k % 2 == 1:  # the busiest space is now a different one
            working[0]["updated_at"] = ago(minutes=30)
        for m in MACHINES:
            m["last_seen"] = now
        if waiting and INTERACTIONS:  # the hub's own list follows what is pushed below (/mock/truth reads it)
            INTERACTIONS[:] = [x for x in INTERACTIONS if x["id"] != "ix_churn"]
            if k % 2 == 0: INTERACTIONS.append(dict(INTERACTIONS[0], id="ix_churn", created_at=now))
        by = {}
        for s in all_sessions(): by.setdefault(s["machine_id"], []).append(s)
        for send, subscribed in list(LIVE):
            try:
                send({"type": "machines", "machines": MACHINES})
                for m, ss in by.items(): send({"type": "sessions", "machine_id": m, "sessions": ss})
                if inbox:
                    send({"type": "inbox"})
                    for ix in INTERACTIONS: send({"type": "interaction", "interaction": ix})
                if waiting and INTERACTIONS:  # another agent starts waiting, then is answered elsewhere
                    if k % 2 == 0:
                        send({"type": "interaction", "interaction": dict(INTERACTIONS[0], id="ix_churn", created_at=now)})
                    else:
                        send({"type": "interaction_resolved", "id": "ix_churn"})
                for sid in list(subscribed):
                    items = ITEMS.get(sid)
                    if not items: continue
                    CHURN_ITEMS.setdefault(sid, list(items))
                    before = items[-1]["seq"]
                    new = [x for j in range(steps) for x in tool("Read", {"file_path": "/Users/me/code/weather/Step%d_%d.swift" % (k, j)},
                                                                   "struct Step%d {}" % k, 0)]
                    if k % 4 == 3:  # a word between the steps: it folds into the work line, the turn goes on
                        new = [it("assistant", "text", 0, text="第 %d 处看完了，接着看下一处。" % (k + 1))] + new
                    ITEMS[sid] = items + new
                    mid = next((x["machine_id"] for x in all_sessions() if x["session_id"] == sid), "macbook")
                    send({"type": "items", "machine_id": mid, "session_id": sid, "items": new, **span(new, before)})
            except Exception:
                pass
        CHURN["rounds"] = k + 1
    if final:  # the turn ends: a last reply, and the agents that were working are done
        time.sleep(ms / 1000)
        for s in SESSIONS:
            if s["state"] == "working":
                s.update(state="unread", status="done", last_text="都看完了，改动已经提交。")
        by = {}
        for s in all_sessions(): by.setdefault(s["machine_id"], []).append(s)
        for send, subscribed in list(LIVE):
            try:
                for sid in list(subscribed):
                    items = ITEMS.get(sid)
                    if not items: continue
                    before = items[-1]["seq"]
                    new = [it("assistant", "text", 0, text="都看完了，改动已经提交。")]
                    ITEMS[sid] = items + new
                    mid = next((x["machine_id"] for x in all_sessions() if x["session_id"] == sid), "macbook")
                    send({"type": "items", "machine_id": mid, "session_id": sid, "items": new, **span(new, before)})
                for m, ss in by.items(): send({"type": "sessions", "machine_id": m, "sessions": ss})
            except Exception:
                pass
        CHURN["rounds"] += 1


# ---------- truth and single changes (ScreenTruthTests, RenderBudgetTests; docs/ios-state.md) ----------
# GET /mock/truth: what the hub holds now, shaped like the app's probe.agents-list (`states` and `last` by
# "<machine>/<session>", `asks` the questions about agents): the tests compare the screen with it.
# GET /mock/bump?what=rev|last|needs|ask|resolve: one change, pushed like the real hub (the machine's whole session list,
# or one interaction): `rev` a working agent's transcript grew (rev, updated_at: most real pushes), `last` its last line, `needs` a working agent starts needing the user, `ask` a
# question comes for an agent, `resolve` it is answered. Returns what changed. /mock/reset undoes them.
BUMPED = {"asks": [], "n": 0}


def truth():
    agents = [s for s in all_sessions() if not s.get("assistant")]
    keys = {s["session_id"]: s["machine_id"] + "/" + s["session_id"] for s in agents}
    return {"states": {keys[s["session_id"]]: s["state"] for s in agents},
            "last": {keys[s["session_id"]]: s.get("last_text") or "" for s in agents},
            "asks": sorted(ix["id"] for ix in INTERACTIONS if ix["session_id"] in keys),
            "titles": {keys[s["session_id"]]: s.get("title") or "" for s in agents}}


def push_machine(mid):
    ss = [s for s in all_sessions() if s["machine_id"] == mid]
    for send, _ in list(LIVE):
        try: send({"type": "sessions", "machine_id": mid, "sessions": ss})
        except Exception: pass


def bump(what):
    now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    BUMPED["n"] += 1
    n = BUMPED["n"]
    working = [s for s in SESSIONS if s["state"] == "working" and not s.get("assistant")]
    if what in ("last", "needs", "rev") and working:
        s = working[0] if what != "needs" else working[-1]
        if what == "rev":  # what most of the real hub's pushes are: the transcript grew, nothing shown changed
            s.update(rev=(s.get("rev") or 1000) + 517, updated_at=now)
        elif what == "last":
            s.update(last_text="第 %d 次更新：刚改完一处。" % n, updated_at=now)
        else:
            s.update(state="needs_you", status="blocked", state_since=now, updated_at=now)
        push_machine(s["machine_id"])
        return {"key": s["machine_id"] + "/" + s["session_id"], "state": s["state"], "last": s.get("last_text") or ""}
    if what == "ask":
        s = working[0] if working else SESSIONS[0]
        ix = dict(INTERACTIONS[0], id="ix_bump_%d" % n, machine_id=s["machine_id"], session_id=s["session_id"], created_at=now)
        INTERACTIONS.append(ix)
        BUMPED["asks"].append(ix["id"])
        for send, _ in list(LIVE):
            try: send({"type": "interaction", "interaction": ix})
            except Exception: pass
        return {"ask": ix["id"]}
    if what == "resolve" and BUMPED["asks"]:
        ix_id = BUMPED["asks"].pop()
        INTERACTIONS[:] = [x for x in INTERACTIONS if x["id"] != ix_id]
        for send, _ in list(LIVE):
            try: send({"type": "interaction_resolved", "id": ix_id})
            except Exception: pass
        return {"resolved": ix_id}
    return {"error": "nothing to bump"}


def say(text, user=None, stream=False, steps=10, ms=300):
    """A reply (after `user`'s message, if given) in every conversation an app has open (/mock/say). `stream`: the
    message first, then the reply arrives as growing `delta` text over about 3 s, like the Jarvis assistant's."""
    import time
    for send, subscribed in list(LIVE):
        for sid in list(subscribed):
            items = ITEMS.get(sid)
            if not items: continue
            CHURN_ITEMS.setdefault(sid, list(items))  # /mock/reset takes the replies away again
            mid = next((x["machine_id"] for x in all_sessions() if x["session_id"] == sid), "macbook")
            if stream:
                if user:
                    before = ITEMS[sid][-1]["seq"]
                    asked = [it("user", "text", 0, text=user)]
                    ITEMS[sid] = ITEMS[sid] + asked
                    send({"type": "items", "machine_id": mid, "session_id": sid, "items": asked, **span(asked, before)})
                    user = None
                for k in range(1, steps + 1):
                    time.sleep(ms / 1000)
                    send({"type": "delta", "machine_id": mid, "session_id": sid, "text": text[:len(text) * k // steps]})
                items = ITEMS[sid]
            before = items[-1]["seq"]
            new = ([it("user", "text", 0, text=user)] if user else []) + [it("assistant", "text", 0, text=text)]
            ITEMS[sid] = items + new
            LOG.write("  SAY: %d items to %s after seq %d\n" % (len(new), sid, before)); LOG.flush()
            try:
                send({"type": "items", "machine_id": mid, "session_id": sid, "items": new, **span(new, before)})
            except Exception:
                pass


if os.environ.get("MOCK_CHURN") == "1":
    # Machines enough that the ⋯ menu scrolls, as it does with six on a phone.
    for _i, _n in enumerate(["构建机", "测试机", "Mac mini", "Mac mini 2", "训练机"]):
        MACHINES.append({"id": "extra%d" % _i, "name": _n, "kind": "ssh", "icon": "server", "ssh_host": "extra%d" % _i,
                         "online": True, "last_seen": ago()})
    _shot = tool("Bash", {"command": "xcrun simctl io booted screenshot /tmp/widget.png", "description": "截一张小组件的图"}, "", 6)
    _img = it("user", "image", 6, tool_use_id=_shot[0]["tool_use_id"],
              image={"mime": "image/png", "data": base64.b64encode(png(320, 240, (64, 140, 220))).decode()})
    _w = ITEMS[WIDGET]
    ITEMS[WIDGET] = _w[:-3] + _shot + [_img] + _w[-3:]  # before the last tool call (its use, its result) and the reply
    for _n, _x in enumerate(ITEMS[WIDGET]):  # seqs in transcript order again (the new ones were numbered last)
        _x["seq"] = _w[0]["seq"] + _n


# Every fixture, including machines added by optional modes, follows the engine contract.
for _m in MACHINES:
    _m["engine"] = "herdr"
    _m["engines"] = ([{"id": "herdr", "name": "herdr", "available": True, "version": "0.9.5"},
                       {"id": "herdr-core", "name": "Herdr App", "available": _m["id"] == "macbook"},
                       {"id": "orca", "name": "Orca", "available": _m["id"] == "studio", "version": "1.0",
                        **({"note": "Orca 协议版本不兼容，请先更新 Orca。"} if _m["id"] == "server" else {})}]
                      if _m.get("online") else [])

# ---------- HTTP ----------
class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def bad_network(self):
        """True when the hub is "down" (the connection is closed without an answer); otherwise waits out the delay."""
        if time.time() < NET["down_until"]:
            self.close_connection = True
            return True
        if NET["delay"] > 0:
            time.sleep(NET["delay"])
        return False

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
        HITS["GET " + p] = HITS.get("GET " + p, 0) + 1
        if net_control(p, q): return self.send_json({"ok": True, **NET})
        if p == "/mock/churn":
            CHURN["rounds"] = 0
            threading.Thread(target=churn, args=(int(q.get("n", ["20"])[0]), int(q.get("ms", ["300"])[0]),
                                                 int(q.get("steps", ["1"])[0]), q.get("final", ["0"])[0] == "1",
                                                 q.get("inbox", ["0"])[0] == "1", q.get("waiting", ["0"])[0] == "1",
                                                 q.get("real", ["0"])[0] == "1"), daemon=True).start()
            return self.send_json({"ok": True})
        if p == "/mock/churned": return self.send_json(CHURN)
        if p == "/mock/truth": return self.send_json(truth())
        if p == "/mock/bump": return self.send_json(bump(q.get("what", ["last"])[0]))
        if p == "/mock/mark": return self.send_json({"ok": True})  # a test's marker in the request log (explore)
        if p == "/mock/say":  # ChatFollowTests: a reply arrives in every open conversation (?text=…, ?user=… a message before it)
            # &steps=50&ms=80: the hub's own pace (a delta every 80 ms, node/assistant.go).
            args = (q.get("text", ["好了。"])[0], q["user"][0] if "user" in q else None, q.get("stream", ["0"])[0] == "1",
                    int(q.get("steps", ["10"])[0]), int(q.get("ms", ["300"])[0]))
            threading.Thread(target=say, args=args, daemon=True).start()
            return self.send_json({"ok": True})
        if MOCK_LARK and (p.startswith("/api/v1/lark/") or p == "/mock/lark") and fs_get(self, p, q): return  # area: feishu chat page
        if not p.startswith("/mock/") and self.bad_network(): return
        if p == "/api/v1/ws":
            return self.websocket()
        if p == "/mock/ports": return self.send_json({"connects": PORT_LOG})
        if p.startswith("/api/v1/machines/") and "/ports/" in p:
            parts = p.split("/")  # ['', 'api', 'v1', 'machines', mid, 'ports', port, ('connect')]
            port = int(parts[6]) if parts[6].isdigit() else 0
            if len(parts) == 8 and parts[7] == "connect": return self.port_connect(parts[4], port)
            return self.send_json({"port": port, "listening": port_listening(port), "http": port_listening(port)})
        if p == "/api/v1/health": return self.send_json({"ok": True, "version": "mock", "channel": "release", "build": 202611021500, "protocol": 1, "min_app_build": 0})
        if p == "/api/v1/hub/update":
            return self.send_json(hub_update_state()) if HUB_UPDATE_MODE else self.send_json({"error": "not found"}, 404)
        if p == "/api/v1/hub/update/history": return self.send_json({"history": hub_update_history()})
        if p == "/api/v1/skills": return self.send_json({"machine_id": "macbook", "machine": "MacBook", "skills": MOCK_SKILLS})
        if p == "/api/v1/machines": return self.send_json({"machines": MACHINES})
        if p == "/api/v1/metrics":
            return self.send_json({"machines": [machine_metrics(m["id"], 3600, 60) for m in MACHINES], "step": 60})
        if p.startswith("/api/v1/machines/") and p.endswith("/metrics"):
            r = q.get("range", ["1h"])[0]
            secs, step = {"1h": (3600, 10), "6h": (6 * 3600, 60), "24h": (86400, 240)}.get(r, (3600, 10))
            return self.send_json({"metrics": machine_metrics(p.split("/")[4], secs, step), "range": r, "step": step})
        if p == "/api/v1/sessions": return self.send_json({"sessions": all_sessions()})
        if p == "/api/v1/todos": return self.send_json({"todos": sorted_todos()})
        if p.startswith("/api/v1/todos/") and p.count("/") == 4:
            tid = p.split("/")[4]
            t = next((x for x in TODOS if x["id"] == tid), None)
            if not t: return self.send_json({"error": "待办不存在"}, 404)
            return self.send_json({"todo": format_todo(t)})
        if p == "/api/v1/interactions": return self.send_json({"interactions": INTERACTIONS})
        if p == "/api/v1/interactions/history": return self.send_json({"interactions": []})
        if p == "/mock/resolved": return self.send_json({"resolved": RESOLVED})
        if p == "/api/v1/auth/tokens": return self.send_json({"tokens": TOKENS, "current": "dev_phone"})
        if p == "/mock/sent": return self.send_json({"sent": SENT})
        if p == "/mock/hits": return self.send_json({"hits": HITS})
        if p == "/mock/hub_update":  # HubNoticeUITests: ?mode=<a MOCK_HUB_UPDATE mode>, from the start
            set_hub_update_mode((q.get("mode") or [""])[0])
            return self.send_json({"ok": True})
        if p == "/mock/reset":  # MachinePagesTests: each test starts from the same machines
            for m in MACHINES:
                m["engine"] = "herdr"
                if m["id"] == "lab":
                    for k in ("hostname", "os", "arch", "home", "herdr_version", "claude_version"): m.pop(k, None)
                    m.update(online=False, error="ssh: connect to host lab port 22: Connection refused")
            SESSIONS[:] = [x for x in SESSIONS if not x["session_id"].startswith("d4e6f8a0-")]
            del RECONNECTS[:], STARTED[:], SEEN[:]
            for sid, items in CHURN_ITEMS.items(): ITEMS[sid] = list(items)  # LiveUpdateTests, ChatFollowTests: what churn / say added goes
            if CHURN_INBOX: INBOX[:] = copy.deepcopy(CHURN_INBOX)
            for x in SESSIONS:
                if x["session_id"] in ORIG_STATE:
                    x["state"], x["status"] = ORIG_STATE[x["session_id"]]
            INTERACTIONS[:] = [x for x in INTERACTIONS if not x["id"].startswith(("ix_bump_", "ix_churn"))]
            BUMPED["asks"].clear()
            TODOS[:] = copy.deepcopy(TODOS_INIT)
            return self.send_json({"ok": True})
        if p == "/mock/seen": return self.send_json({"seen": SEEN})
        if p == "/mock/reconnects": return self.send_json({"reconnects": RECONNECTS})
        if p == "/mock/started": return self.send_json({"started": STARTED})
        if p == "/mock/uploads": return self.send_json({"uploads": UPLOADS})
        if p == "/mock/keys": return self.send_json({"keys": KEYS})
        if p == "/mock/terminal_input": return self.send_json({"input": TERMINAL_INPUT})
        if p == "/mock/kb_updates": return self.send_json({"updates": KB_UPDATES})
        if p.startswith("/api/v1/devices/") and p.endswith("/prefs"): return self.send_json(PREFS)
        if p in ("/api/v1/pins",): return self.send_json({"spaces": ["macbook/weather"], "sessions": []})
        if p == "/api/v1/mutes": return self.send_json({"spaces": [], "sessions": [], "allowed": []})
        if p == "/api/v1/assistant":
            return self.send_json({"machine_id": "server", "launcher": "claude", "cwd": "/home/me/.jarvis/assistant",
                                   "timezone": "Asia/Shanghai", "notes_effective": "", "conversations": ASSISTANT,
                                   "main": ASK1})
        if p == "/api/v1/model-config": return self.send_json(model_configuration())
        if p.startswith("/api/v1/model-config/purposes/"):
            catalog = model_configuration()
            purpose = next((x for x in catalog["purposes"] if x["id"] == unquote(p.rsplit("/", 1)[1])), None)
            if not purpose: return self.send_json({"error": "模型用途不存在"}, 404)
            return self.send_json({"purpose": purpose, "providers": catalog["providers"]})
        if p == "/api/v1/assistant/tasks": return self.send_json({"tasks": SCHEDULE})
        # The two lists the Android app still reads (the hub's views of the same tasks, assistant_schedule.go):
        # routines are the daily tasks, follow-ups the enabled once / interval ones by their next run.
        if p == "/api/v1/assistant/routines": return self.send_json({"routines": [t for t in SCHEDULE if t.get("repeat") == "daily"]})
        if p == "/api/v1/assistant/followups":
            due = [{**t, "at": t.get("next_run")} for t in SCHEDULE if t.get("repeat") != "daily" and t.get("enabled")]
            return self.send_json({"followups": sorted(due, key=lambda t: t["at"] or "")})
        if p.startswith("/api/v1/machines/") and p.endswith("/stat"):
            cwd = q.get("cwd", [""])[0]
            paths = q.get("path", []) + [x for x in q.get("paths", [""])[0].split("\n") if x]
            return self.send_json({"stats": [{"path": x, "kind": file_kind(file_path(x, cwd)) if file_path(x, cwd) else "missing"} for x in paths]})
        if p.startswith("/api/v1/machines/") and p.endswith("/file") and FILES:
            f = file_read(q.get("path", [""])[0], q.get("cwd", [""])[0], int(q.get("offset", ["0"])[0] or 0))
            if not f: return self.send_json({"error": "stat %s: no such file or directory" % q.get("path", [""])[0]}, 404)
            return self.send_json(f)
        if p == "/api/v1/personas": return self.send_json({"personas": personas()})
        if p.startswith("/api/v1/personas/"):
            f = persona_file(p[len("/api/v1/personas/"):])
            if not f: return self.send_json({"error": "no such file"}, 404)
            body = open(f, "rb").read()
            self.send_response(200)
            self.send_header("Content-Type", "application/json" if f.endswith(".json") else "image/png")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        if p == "/api/v1/assistant/runs":
            ref = parse_qs(urlparse(self.path).query).get("ref", [""])[0]
            return self.send_json({"runs": [r for r in RUNS if not ref or r["ref_id"] == ref]})
        if p == "/api/v1/assistant/memory": return self.send_json({"files": MEMORY, "dir": "/home/me/.jarvis/assistant/memory"})
        if p == "/mock/schedule": return self.send_json({"ran": SCHEDULE_RAN})
        if p == "/mock/renamed": return self.send_json({"renamed": RENAMED})
        if p == "/mock/served": return self.send_json({"served": SERVED})
        if p == "/api/v1/inbox": return self.send_json({"items": inbox_for(q)})
        if p == "/api/v1/lark/moments/posts/mp1": return self.send_json(MOMENT_POST)
        if p == "/api/v1/lark/moments/posts/mp1/comments": return self.send_json({"comments": MOMENT_COMMENTS})
        if p.startswith("/api/v1/inbox/"):
            iid = p[len("/api/v1/inbox/"):]
            for x in INBOX:
                if x["id"] == iid: return self.send_json(dict(x, context=x.get("context") or x["preview"]))
        if mail_area(self, "GET", p, q): return
        if p == "/api/v1/mail/accounts": return self.send_json({"accounts": MAIL_ACCOUNTS})
        if p.startswith("/api/v1/mail/messages/") and p.count("/") == 6:
            uid = int(p.rsplit("/", 1)[1])
            it = next((x for x in INBOX if (x.get("mail") or {}).get("uids") == [uid]), None)
            if it: return self.send_json({"message": {"account_id": "acc1", "mailbox": "INBOX", "uid": uid, "subject": it["title"],
                "from": it["mail"]["from"], "to": [{"name": "Me", "email": "me@example.com"}], "date": it["updated_at"],
                "text": MAIL_BODIES.get(uid, it["preview"]), "seen": False}})
        if p == "/api/v1/mail/messages": return self.send_json({"messages": [], "has_more": False})
        if p == "/api/v1/lark/unread-count": return self.send_json({"count": 4})
        if p == "/api/v1/connectors":  # 设置 › 连接器 › GitHub; MOCK_GITHUB=signed_out: its gh login has expired
            return self.send_json({"connectors": [GITHUB_CONNECTOR, {k: v for k, v in IMESSAGE_CONNECTOR.items() if v is not None}]})
        if p == "/api/v1/imessage":  # 设置 › 连接器 › 信息; MOCK_IMESSAGE=denied: no Full Disk Access on the Mac
            return self.send_json(IMESSAGE_STATUS)
        if p == "/api/v1/imessage/chats" or p.startswith("/api/v1/imessage/chats/") and p.endswith("/messages"):
            if IMESSAGE_DENIED:
                return self.send_json({"error": IMESSAGE_STATUS["error"], "code": "no_permission"}, 403)
            if p == "/api/v1/imessage/chats":
                return self.send_json({"chats": IMESSAGE_CHATS})
            cid = p.split("/")[5]
            chat = next((c for c in IMESSAGE_CHATS if c["id"] == cid), None)
            if not chat:
                return self.send_json({"error": "没有这个会话", "code": "no_chat"}, 404)
            msgs = IMESSAGE_MESSAGES.get(cid) or [{"id": cid[-4:], "at": chat["last"], "kind": "message", "text": chat["preview"],
                                                   "from_me": chat.get("from_me", False), "sender": None if chat.get("from_me") else chat["members"][0]}]
            return self.send_json({"chat": chat, "messages": msgs})
        if p == "/api/v1/lark/status":  # 设置 › 连接器 › 飞书
            return self.send_json({"configured": True, "connected": True, "name": "我", "last_event_at": int(time.time()) - 95})
        lark_read = p in ("/api/v1/lark/conversations", "/api/v1/lark/pinned") or (
            p.startswith(("/api/v1/lark/conversations/", "/api/v1/lark/messages/")) and p.endswith(("/messages", "/thread")))
        if lark_read and os.environ.get("MOCK_LARK_LOGGED_OUT"):
            return self.send_json({"error": LARK_LOGGED_OUT, "code": "lark_not_logged_in"}, 502)
        if p in ("/api/v1/lark/conversations", "/api/v1/lark/pinned"):
            return self.send_json({"conversations": LARK_CHATS if p.endswith("conversations") else [], "next_cursor": ""})
        if p.startswith("/api/v1/lark/conversations/") and p.endswith("/messages"):
            return self.send_json(lark_window(LARK_MESSAGES.get(p.split("/")[5], []), q))
        if p.startswith("/api/v1/lark/messages/") and p.endswith("/thread"):
            return self.send_json(lark_window(LARK_THREADS.get(p.split("/")[5], []), q))
        if p.startswith("/api/v1/lark/media/") and p.split("/")[5] in LARK_MEDIA:
            body = base64.b64decode(mock_png(*LARK_MEDIA[p.split("/")[5]]))
            self.send_response(200); self.send_header("Content-Type", "image/png")
            self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
            return
        if p.startswith("/api/v1/lark/conversations/") and p.endswith("/members"):  # the @ picker
            arg = lambda k: (q.get(k) or [""])[0]
            limit = int(arg("limit") or 0)
            limit = 50 if limit <= 0 else min(limit, 200)
            return self.send_json({"members": lark_members(p.split("/")[5], arg("q").strip(), limit)})
        if p == "/api/v1/lark/labels": return self.send_json({"labels": []})
        if p == "/api/v1/lark/flags": return self.send_json({"conversations": [], "messages": []})
        if p == "/api/v1/lark/emojis": return self.send_json({"emojis": LARK_EMOJIS})
        if p.startswith("/api/v1/machines/") and not machine_online(p.split("/")[4]) and p.split("/")[5] in ("launchers", "dirs"):
            return self.send_json({"error": "%s: machine is offline" % p.split("/")[4]}, 503)
        if p.startswith("/api/v1/machines/") and p.endswith("/dirs"):
            return self.send_json(mock_dirs(p.split("/")[4], (q.get("path") or [""])[0]))
        if p == "/api/v1/launchers": return self.send_json({"launchers": [dict(l) for l in LAUNCHERS]})
        if p.startswith("/api/v1/launchers/") and p.endswith("/quota"):
            n = p.split("/")[4]
            q = LAUNCHER_QUOTAS.get(n)
            return self.send_json({"machine": "macbook", "checked_at": ago(minutes=0), **({"quota": q} if q else
                {"error": "这个网关没有能查的额度（只支持 relay-history 和 FAKEMODEL）", "unsupported": True})})
        if p.startswith("/api/v1/launchers/"):
            n = p.split("/")[4]
            l = next((x for x in LAUNCHERS if x["name"] == n), None)
            if l: return self.send_json({"launcher": l})
        if p.startswith("/api/v1/machines/") and p.endswith("/launchers"):
            return self.send_json({"launchers": [{"name": "claude", "model": "opus"}, {"name": "sonnetcc", "model": "sonnet"},
                                                 {"name": "codex", "model": "gpt-5.5", "agent": "codex"}]})  # Codex: 从手机新建
        if p.startswith("/api/v1/machines/") and p.endswith("/claude-settings"):
            return self.send_json({"settings": CLAUDE_SETTINGS.setdefault(p.split("/")[4], _claude_settings())})
        if p.startswith("/api/v1/machines/") and p.endswith("/codex-settings"):
            return self.send_json({"settings": CODEX_SETTINGS.setdefault(p.split("/")[4], _codex_settings())})
        if p.startswith("/api/v1/sessions/") and p.endswith("/items"):
            sid = p.split("/")[5]
            items = find_items(sid)
            last = items[-1]["seq"] if items else 0
            if "after_seq" in q:
                a = int(q["after_seq"][0] or 0)
                LOG.write("  items after_seq=%d\n" % a); LOG.flush()
                return self.send_json({"items": [x for x in items if x["seq"] > a], "more_after": False, **span(items, a, max(a, last))})
            if "through_seq" in q:  # opening a fold
                frm, thru = int(q.get("from_seq", ["0"])[0]), int(q["through_seq"][0])
                inside = [x for x in items if frm <= x["seq"] <= thru]
                out = {"items": inside[:1000], "more": len(inside) > 1000}
                served(sid, "range", out)
                return self.send_json(out)
            b = q.get("before_seq", [""])[0]
            if b: items = [x for x in items if x["seq"] < int(b)]
            out = {"items": fold_work(items) if q.get("fold_work") == ["1"] else items, "has_more": False,
                   **span(items, None, int(b) - 1 if b else last)}
            served(sid, "page", out)
            return self.send_json(out)
        if p.startswith("/api/v1/sessions/") and "/items/" in p and p.endswith("/full"):
            item = FULL.get(unquote(p.split("/")[7]))
            return self.send_json(item) if item else self.send_json({"error": "not found"}, 404)
        if p.startswith("/api/v1/sessions/") and p.endswith("/info"):  # the chat's 开发信息 card
            s = next((s for s in all_sessions() if s["session_id"] == p.split("/")[5]), {})
            return self.send_json({"session_id": s.get("session_id", ""), "cwd": s.get("cwd"), "model": "claude-opus-5",
                                   "launcher": s.get("launcher"), "git_branch": "main", "version": "2.1.320",
                                   "permission_mode": s.get("permission_mode"), "effort": s.get("effort"),
                                   "context_tokens": 48200, "output_tokens": 12800, "compactions": 0, "turns": 6,
                                   "started_at": ago(minutes=40), "last_activity": ago()})
        if p.startswith("/api/v1/sessions/") and p.endswith("/screen"):
            fail = os.environ.get("MOCK_SCREEN_FAIL_AFTER")
            if fail:
                SCREEN_FIRST[:] = SCREEN_FIRST or [datetime.now(timezone.utc)]
                t = (datetime.now(timezone.utc) - SCREEN_FIRST[0]).total_seconds()
                after, _, back = fail.partition(":")
                if t > float(after) and not (back and t > float(after) + float(back)):
                    return self.send_json({"error": "node macbook: not connected"}, 503)
            return self.send_json({"text": TERMINAL, "status": "idle"})
        if p.startswith("/api/v1/kb/"):  # the knowledge base (Jarvis › 知识库)
            parts = p.split("/")[4:]
            kind = parts[0]
            if len(parts) == 1:
                tag = (q.get("tag") or [""])[0]
                items = [e for e in KB.get(kind, []) if not tag or tag in (e.get("tags") or [])]
                return self.send_json({"items": items, "tags": KB_TAGS.get(kind, [])})
            if kind == "moments" and len(parts) == 2:  # one archived removed post
                return self.send_json({"post": {"id": parts[1]}, "markdown": "# removed · 帖子\n\n王五 · 2026-10-30 10:00 · 评论 2\n\n有人在用天气小组件吗？中号的降雨条看不太清\n\n## 评论（2）\n\n- 赵六: 同感，颜色太浅\n- 我: 在改了"})
            if kind == "chats" and len(parts) == 4 and parts[2] == "days":
                days = sorted(KB_DAYS.get(parts[1], {}))
                if parts[3] not in days: return self.send_json({"error": "not found"}, 404)
                i = days.index(parts[3])
                return self.send_json({"day": parts[3], "messages": [{"n": n, "text": t} for d, n, t in kb_messages(parts[1]) if d == parts[3]],
                                       "prev": days[i - 1] if i else "", "next": days[i + 1] if i + 1 < len(days) else ""})
            e = next((e for e in KB.get(kind, []) if e["id"] == parts[1]), None)
            if e is None: return self.send_json({"error": "not found"}, 404)
            d = dict({"kind": kind, "id": e["id"], "path": "/home/me/.jarvis/data/%s/%s" % (kind, e["id"]), "summary": e.get("one_line")},
                     **KB_DETAIL.get((kind, e["id"]), {}))
            out = {"detail": d}
            word = (q.get("q") or [""])[0]
            if word:
                ms = [{"path": "%s/%s/summary.md" % (kind, e["id"]), "n": i + 1, "line": l.strip()}
                      for i, l in enumerate((d.get("summary") or "").split("\n")) if l.strip() and kb_match(l, word)]
                msgs = kb_messages(e["id"]) if kind == "chats" else []
                for j in range(len(msgs) - 1, -1, -1):
                    day, n, t = msgs[j]
                    if kb_match(t, word):
                        ms.append({"path": "chats/%s/messages/%s.md" % (e["id"], day), "n": n, "line": t, "day": day,
                                   "before": [m[2] for m in msgs[max(0, j - 2):j] if m[0] == day],
                                   "after": [m[2] for m in msgs[j + 1:j + 3] if m[0] == day]})
                out["matches"] = ms
            if kind == "projects":
                dirs = [x.split(":", 1)[1] for x in (d.get("meta") or {}).get("dirs", [])]
                out["agents"] = [x for x in SESSIONS if x.get("cwd") in dirs]
            return self.send_json(out)
        if p == "/api/v1/kb-search":
            word = (q.get("q") or [""])[0].lower()
            if (q.get("results") or [""])[0] == "1":
                res = []
                for k, l in KB.items():
                    for e in l:
                        found = [{"path": "%s/%s/messages/%s.md" % (k, e["id"], d), "n": n, "line": t}
                                 for d, n, t in reversed(kb_messages(e["id"])) if kb_match(t, word)] if k == "chats" else []
                        named = kb_match(e["name"], word) or kb_match(e.get("one_line") or "", word)
                        if found or named:
                            res.append({"entry": e, "count": len(found), "hits": found[:3], "in_name": kb_match(e["name"], word)})
                return self.send_json({"results": res})
            hits = [{"path": "%s/%s/summary.md" % (k, e["id"]), "line": "- " + e["name"] + "：" + (e.get("one_line") or "")}
                    for k, l in KB.items() for e in l if word and (word in e["name"].lower() or word in (e.get("one_line") or "").lower())]
            return self.send_json({"hits": hits})
        files = mock_files(p, q)
        if files: return self.send_json(*files)
        LOG.write("  MISSING GET %s\n" % p); LOG.flush()
        self.send_json({"error": "not found"}, 404)

    def upload(self):
        """POST /api/v1/machines/{mid}/files?name=…: the body read slowly enough to watch the progress bar
        (MOCK_UPLOAD_KBPS, default 2048); MOCK_UPLOAD_OLD_NODE=1 answers like a node from before uploads."""
        q = parse_qs(urlparse(self.path).query)
        name = (q.get("name") or ["file"])[0]
        n = int(self.headers.get("Content-Length") or 0)
        if n > 200 << 20:
            return self.send_json({"error": "文件超过 200 MB", "code": "too_large", "limit": 200 << 20}, 413)
        if os.environ.get("MOCK_UPLOAD_OLD_NODE"):
            self.body()
            return self.send_json({"error": "MacBook 上的 jarvisd 太旧，不能接收文件，需要重新部署", "code": "node_outdated"}, 409)
        kbps = int(os.environ.get("MOCK_UPLOAD_KBPS", "2048"))
        left = n
        while left > 0:
            got = self.rfile.read(min(left, 64 << 10))
            if not got: return
            left -= len(got)
            time.sleep(len(got) / 1024 / kbps)
        stem, dot, ext = name.rpartition(".")
        if not stem: stem, dot, ext = ext, "", ""
        path = "/tmp/%s-%s%s%s" % (stem, datetime.now().strftime("%Y%m%d-%H%M%S"), dot, ext)
        UPLOADS.append({"name": name, "size": n, "path": path})
        return self.send_json({"path": path, "name": path.rsplit("/", 1)[1], "size": n})

    def do_POST(self):
        if self.command == "POST" and urlparse(self.path).path.startswith("/api/v1/machines/") and urlparse(self.path).path.endswith("/files"):
            return self.upload()
        self.last_body = self.body()
        p = urlparse(self.path).path
        if self.bad_network(): return
        if p.startswith("/api/v1/hub/update") and HUB_UPDATE_MODE:
            s, body = hub_update_state(), json.loads(self.last_body or b"{}")
            if p == "/api/v1/hub/update/settings" and self.command == "PUT":
                if "auto" in body: s.update(auto=bool(body["auto"]), auto_notice=False)
                if body.get("auto_notice_seen"): s["auto_notice"] = False
            elif p == "/api/v1/hub/update" and self.command == "POST":
                if not s["can_update"]: return self.send_json({"error": "这个 hub 不能自己更新：" + s.get("why_not", "")}, 409)
                s.update(state="restarting" if body.get("now") else "waiting_idle")
            return self.send_json(s)
        if mail_area(self, self.command, p, parse_qs(urlparse(self.path).query)): return
        if MOCK_LARK and fs_post(self, p, parse_qs(urlparse(self.path).query), self.last_body): return  # area: feishu chat page
        if p == "/api/v1/todos" and self.command == "POST":
            if is_read_only(self): return self.send_json({"error": "只读角色不能修改待办"}, 403)
            body = json.loads(self.last_body or b"{}")
            title = (body.get("title") or "").strip()
            if not title: return self.send_json({"error": "title 不能为空"}, 400)
            now_str = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
            new_todo = {
                "id": "todo_" + uuid.uuid4().hex[:8],
                "title": title,
                "notes": body.get("notes") or "",
                "done_at": None,
                "created_at": now_str,
                "updated_at": now_str,
                "agents": [],
            }
            TODOS.insert(0, new_todo)
            push_todos()
            return self.send_json({"todo": new_todo})
        if p.startswith("/api/v1/todos/") and "/agents/" in p:
            if is_read_only(self): return self.send_json({"error": "只读角色不能修改待办"}, 403)
            parts = p.split("/")  # ['', 'api', 'v1', 'todos', id, 'agents', mid, sid]
            if len(parts) >= 8:
                tid, mid, sid = parts[4], parts[6], parts[7]
                t = next((x for x in TODOS if x["id"] == tid), None)
                if not t: return self.send_json({"error": "待办不存在"}, 404)
                if self.command == "PUT":
                    if t.get("done_at"): return self.send_json({"error": "待办已完成"}, 409)
                    now_str = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
                    for other in TODOS:
                        if other["id"] != tid:
                            for a in other.get("agents", []):
                                if a["machine_id"] == mid and a["session_id"] == sid and not a.get("released_at"):
                                    a["released_at"] = now_str
                                    other["updated_at"] = now_str
                    target = next((a for a in t.get("agents", []) if a["machine_id"] == mid and a["session_id"] == sid), None)
                    s_obj = next((x for x in SESSIONS + ASSISTANT if x["machine_id"] == mid and x["session_id"] == sid), None)
                    s_title = s_obj.get("title", "") if s_obj else ""
                    s_cwd = s_obj.get("cwd", "") if s_obj else ""
                    s_agent = s_obj.get("agent", "claude") if s_obj else "claude"
                    if target:
                        target["released_at"] = None
                        target["linked_at"] = now_str
                        if s_title: target["title"] = s_title
                        if s_cwd: target["cwd"] = s_cwd
                        if s_agent: target["agent"] = s_agent
                    else:
                        t.setdefault("agents", []).append({
                            "machine_id": mid, "session_id": sid, "linked_at": now_str, "released_at": None,
                            "title": s_title, "cwd": s_cwd, "agent": s_agent, "start_error": "",
                        })
                    t["updated_at"] = now_str
                    push_machine(mid)
                    push_todos()
                    return self.send_json({"ok": True})
                elif self.command == "DELETE":
                    agent = next((a for a in t.get("agents", []) if a["machine_id"] == mid and a["session_id"] == sid), None)
                    if agent:
                        t["agents"].remove(agent)
                        t["updated_at"] = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
                        push_machine(mid)
                        push_todos()
                    return self.send_json({"ok": True})
        if p.startswith("/api/v1/todos/") and p.count("/") == 4:
            if is_read_only(self): return self.send_json({"error": "只读角色不能修改待办"}, 403)
            tid = p.split("/")[4]
            t = next((x for x in TODOS if x["id"] == tid), None)
            if not t: return self.send_json({"error": "待办不存在"}, 404)
            if self.command == "PATCH":
                body = json.loads(self.last_body or b"{}")
                now_str = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
                aff = set(a["machine_id"] for a in t.get("agents", []) if not a.get("released_at"))
                if "title" in body:
                    title = (body["title"] or "").strip()
                    if not title: return self.send_json({"error": "title 不能为空"}, 400)
                    t["title"] = title
                if "notes" in body:
                    t["notes"] = body["notes"] or ""
                if "done" in body:
                    if body["done"]:
                        if not t.get("done_at"): t["done_at"] = now_str
                    else:
                        t["done_at"] = None
                t["updated_at"] = now_str
                for mid in aff: push_machine(mid)
                push_todos()
                return self.send_json({"todo": format_todo(t)})
            elif self.command == "DELETE":
                aff = set(a["machine_id"] for a in t.get("agents", []) if not a.get("released_at"))
                TODOS.remove(t)
                for mid in aff: push_machine(mid)
                push_todos()
                return self.send_json({"ok": True})
        if p.startswith("/api/v1/model-config/purposes/") and self.command == "PATCH":
            pid, body = unquote(p.rsplit("/", 1)[1]), json.loads(self.last_body or b"{}")
            with MODEL_CONFIG_LOCK:
                catalog = model_configuration()
                purpose = next((x for x in catalog["purposes"] if x["id"] == pid), None)
                if purpose is None: return self.send_json({"error": "模型用途不存在"}, 404)
                if os.environ.get("MOCK_MODEL_CONFLICT") or body.get("revision") != purpose["revision"]:
                    return self.send_json({"error": "配置已在别处修改"}, 409)
                binding = dict(purpose["override"])
                for field in purpose["fields"]:
                    if field not in body: continue
                    if body[field] is None: binding.pop(field, None)
                    else: binding[field] = body[field]
                MODEL_BINDINGS[pid] = {"revision": purpose["revision"] + 1, "override": binding}
                catalog = model_configuration()
                purpose = next(x for x in catalog["purposes"] if x["id"] == pid)
                return self.send_json({"purpose": purpose, "providers": catalog["providers"]})
        if p.startswith("/api/v1/machines/") and p.endswith("/open") and "/ports/" in p:
            port = int(p.split("/")[6])
            if not port_listening(port): return self.send_json({"error": "nothing is listening on port %d" % port}, 404)
            return self.send_json({"port": port, "listening": True, "http": True})
        if self.command == "PUT" and p.startswith("/api/v1/machines/") and p.endswith("/engine"):
            mid = p.split("/")[4]
            machine = next((m for m in MACHINES if m["id"] == mid), None)
            if machine is None: return self.send_json({"error": "找不到机器"}, 404)
            body = json.loads(self.last_body or b"{}")
            engine = body.get("engine")
            if engine not in ("herdr", "herdr-core", "orca") and not any(
                    e["id"] == engine and e["available"] for e in machine["engines"]):
                return self.send_json({"error": "未知引擎"}, 400)
            if os.environ.get("MOCK_ENGINE_ERROR"): return self.send_json({"error": "暂时无法保存"}, 503)
            machine["engine"] = engine
            for send, _ in list(LIVE):
                try: send({"type": "machines", "machines": MACHINES})
                except Exception: pass
            return self.send_json(machine)
        if self.command == "PUT" and p.startswith("/api/v1/kb/"):  # 打标: kept over the automatic tagging
            kind, eid = p.split("/")[4:6]
            body = json.loads(self.last_body or b"{}")
            e = next((e for e in KB.get(kind, []) if e["id"] == eid), None)
            if e is None: return self.send_json({"error": "not found"}, 404)
            for k in ("importance", "relevance", "tags"):
                if k in body: e[k] = body[k]
            if kind == "projects" and ("status" in body or "project_kind" in body):
                pk, st = (e.get("extra") or "short · active").split(" · ")
                e["extra"] = "%s · %s" % (body.get("project_kind") or pk, body.get("status") or st)
            meta = KB_DETAIL.get((kind, eid), {}).get("meta")
            if meta is not None:
                meta.update({k: e[k] for k in ("importance", "relevance", "tags") if k in e})
                if kind == "projects": meta["kind"], meta["status"] = e["extra"].split(" · ")
            KB_UPDATES.append({"kind": kind, "id": eid, **body})
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/lark/conversations/") and p.endswith("/messages"):  # 飞书发消息: lark-connector takes a moment
            body, feed = json.loads(self.last_body or b"{}"), p.split("/")[5]
            if lark_check_mentions(body): return self.send_json({"error": lark_check_mentions(body)}, 400)
            time.sleep(float(os.environ.get("MOCK_LARK_SEND_SECONDS", "2")))
            if "#fail" in body.get("text", ""):
                return self.send_json({"error": "lark-connector: send timed out"}, 502)
            msgs = LARK_MESSAGES.setdefault(feed, [])
            pos = str(max([int(m["position"]) for m in msgs] or [0]) + 1)
            m = _lm(pos, "我", body.get("text", ""), mine=True, mins=0)
            if body.get("mentions"): m["parts"] = lark_mention_parts(m["text"], body["mentions"])
            m["created_at"] = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")  # now, not the mock's start
            msgs.append(m)
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/lark/conversations/") and p.endswith("/read-states"):  # 飞书已读回执 of my messages
            ids = json.loads(self.last_body or b"{}").get("message_ids") or []
            if len(ids) > 100: return self.send_json({"error": "at most 100 message_ids"}, 400)
            return self.send_json({"states": lark_read_states(p.split("/")[5], ids)})
        if p.startswith("/api/v1/lark/messages/") and p.endswith("/replies"):  # 飞书回复: in the thread or quoted in the chat
            body, mid = json.loads(self.last_body or b"{}"), p.split("/")[5]
            if lark_check_mentions(body): return self.send_json({"error": lark_check_mentions(body)}, 400)
            time.sleep(float(os.environ.get("MOCK_LARK_SEND_SECONDS", "2")))
            if "#fail" in body.get("text", ""):
                return self.send_json({"error": "lark-connector: reply timed out"}, 502)
            now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
            feed, target = next(((f, m) for f, ms in LARK_MESSAGES.items() for m in ms if m["id"] == mid), (None, None))
            if body.get("thread"):
                replies = LARK_THREADS.setdefault(mid, [])
                pos = str(max([int(m["position"]) for m in replies] or [0]) + 1)
                m = _lm(pos, "我", body.get("text", ""), mine=True, mins=0)
                m.update(id="%s-%s" % (mid, pos), created_at=now)
                replies.append(m)
                if target is not None:
                    target["reply_count"] = target.get("reply_count", 0) + 1
                    target["thread_reply_count"] = target.get("thread_reply_count", 0) + 1
            elif feed is not None:
                msgs = LARK_MESSAGES[feed]
                pos = str(max(int(x["position"]) for x in msgs) + 1)
                m = _lm(pos, "我", body.get("text", ""), mine=True, mins=0)
                m.update(created_at=now, reply_to={"id": mid, "sender_name": target["sender_name"], "text": target["text"]})
                msgs.append(m)
            else:
                return self.send_json({"error": "no such message"}, 404)
            if body.get("mentions"): m["parts"] = lark_mention_parts(m["text"], body["mentions"])
            return self.send_json({"ok": True, "message_id": m["id"]})
        if p == "/api/v1/assistant/conversations" and self.command == "POST":
            # As the hub does (#511, hub/assistant.go startAssistant, node/assistant.go assistantSend): the session the
            # hub returns, the machine's list pushed, and no transcript yet: the node answers a starting session's items
            # with none, and its watch sends the first line once claude has written it (MOCK_ASSISTANT_TRANSCRIPT_SECONDS
            # later, default 1). MOCK_ASSISTANT_START_SECONDS: the request itself takes that long (the hub prepares the
            # directory, the model config and the live snapshot first); meanwhile the app shows 正在准备.
            time.sleep(float(os.environ.get("MOCK_ASSISTANT_START_SECONDS", "0")))
            body = json.loads(self.last_body or b"{}")
            prompt = body.get("prompt", "")
            SENT.append({"text": prompt, "images": body.get("images")})
            now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
            sid = str(uuid.uuid4())
            session = assistant_sess(sid, "", 0, "")  # what the hub returns: no title, model or transcript rev yet
            for k in ("model", "rev", "seen_at", "agent"): session.pop(k)  # the hub sends agent only when not Claude Code
            session.update(status="working", state="working", updated_at=now, launcher=body.get("launcher") or "claude",
                           permission_mode=body.get("permission_mode") or "", effort=body.get("effort") or "")
            ASSISTANT.append(session)
            push_machine("server")

            def written():
                first = [it("user", "text", 0, text=prompt, ts=now)]
                ITEMS[sid] = first
                for send, subscribed in list(LIVE):
                    if sid in subscribed:
                        try: send({"type": "items", "machine_id": "server", "session_id": sid, "items": first, **span(first, 0)})
                        except Exception: pass
            threading.Timer(float(os.environ.get("MOCK_ASSISTANT_TRANSCRIPT_SECONDS", "1")), written).start()
            return self.send_json({"session": session})
        if p.endswith("/messages") and not os.environ.get("MOCK_SEND_REFUSE"):  # sent messages, once per client_msg_id
            body = json.loads(self.last_body or b"{}")
            todo_id = body.get("todo_id")
            if todo_id:
                if is_read_only(self): return self.send_json({"error": "只读角色不能修改待办"}, 403)
                t = next((x for x in TODOS if x["id"] == todo_id), None)
                if not t: return self.send_json({"error": "待办不存在"}, 404)
                if t.get("done_at"): return self.send_json({"error": "待办已完成"}, 409)
                parts = p.split("/")  # ['', 'api', 'v1', 'sessions', mid, sid, 'messages']
                if len(parts) >= 7:
                    mid, sid = parts[4], parts[5]
                    now_str = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
                    for other in TODOS:
                        if other["id"] != todo_id:
                            for a in other.get("agents", []):
                                if a["machine_id"] == mid and a["session_id"] == sid and not a.get("released_at"):
                                    a["released_at"] = now_str
                                    other["updated_at"] = now_str
                    target = next((a for a in t.get("agents", []) if a["machine_id"] == mid and a["session_id"] == sid), None)
                    s_obj = next((x for x in SESSIONS + ASSISTANT if x["machine_id"] == mid and x["session_id"] == sid), None)
                    s_title = s_obj.get("title", "") if s_obj else ""
                    s_cwd = s_obj.get("cwd", "") if s_obj else ""
                    s_agent = s_obj.get("agent", "claude") if s_obj else "claude"
                    if target:
                        target["released_at"] = None
                        target["linked_at"] = now_str
                        if s_title: target["title"] = s_title
                        if s_cwd: target["cwd"] = s_cwd
                        if s_agent: target["agent"] = s_agent
                    else:
                        t.setdefault("agents", []).append({
                            "machine_id": mid, "session_id": sid, "linked_at": now_str, "released_at": None,
                            "title": s_title, "cwd": s_cwd, "agent": s_agent, "start_error": "",
                        })
                    t["updated_at"] = now_str
                    push_machine(mid)
                    push_todos()
            if not any(m.get("client_msg_id") and m.get("client_msg_id") == body.get("client_msg_id") for m in SENT):
                SENT.append(body)
            return self.send_json({"ok": True})
        if p == "/api/v1/inbox/refresh": return self.send_json({"items": inbox_for(parse_qs(urlparse(self.path).query))})
        if p == "/api/v1/translate/mail":
            body = json.loads(self.last_body or b"{}")
            t = MAIL_TRANSLATIONS.get(body.get("uid"))
            return self.send_json(t) if t else self.send_json({"error": "no translation"}, 502)
        if p == "/api/v1/translate":  # marks the texts, enough to see where they show
            body = json.loads(self.last_body or b"{}")
            return self.send_json({"texts": ["【译】" + t for t in body.get("texts", [])]})
        if p.startswith("/api/v1/inbox/") and p.endswith("/doc/status"):
            iid, ch = p[len("/api/v1/inbox/"):-len("/doc/status")], json.loads(self.last_body or b"{}")
            for x in INBOX:
                for r in (x.get("doc") or {}).get("rows") or []:
                    for st in r.get("status") or []:
                        if x["id"] == iid and r["ref"] == ch.get("ref") and st["field"] == ch.get("field"):
                            st["value"] = ch.get("value")
                            return self.send_json(x)
            return self.send_json({"error": "no such row"}, 400)
        if p.startswith("/api/v1/inbox/") and p.endswith("/draft"):  # AI 草拟回复
            return self.send_json({"text": "看过了，退避时间没问题，可以合。"})
        if p.startswith("/api/v1/inbox/") and p.endswith("/reply"):
            iid, body = p[len("/api/v1/inbox/"):-len("/reply")], json.loads(self.last_body or b"{}")
            SENT.append(dict(body, inbox=iid))
            for x in INBOX:
                if x["id"] == iid:
                    for ch in body.get("changes") or []:
                        for r in (x.get("doc") or {}).get("rows") or []:
                            for st in r.get("status") or []:
                                if r["ref"] == ch["ref"] and st["field"] == ch["field"]: st["value"] = ch["value"]
                    x["done_at"], x["done_by"] = ago(minutes=0), "replied"
                    return self.send_json(x)
        if p == "/api/v1/assistant/tasks" and self.command == "POST":
            t = save_task({"id": "task_" + uuid.uuid4().hex[:8], "created_at": ago()}, json.loads(self.last_body or b"{}"))
            SCHEDULE.append(t)
            return self.send_json({"task": t})
        if p.startswith("/api/v1/assistant/tasks/") and p.endswith("/fresh"):  # 下次开新对话
            t = next((t for t in SCHEDULE if t["id"] == p.split("/")[5]), None)
            if t is None: return self.send_json({"error": "这个定时任务已经不在了"}, 404)
            for k in ("run_machine_id", "run_session_id", "run_launcher"): t.pop(k, None)
            return self.send_json({"task": t})
        if p == "/api/v1/assistant/routines" and self.command == "PUT":  # the Android app replaces the daily ones
            old = {t["id"]: t for t in SCHEDULE if t.get("repeat") == "daily"}
            new = [save_task(old.get(r.get("id")) or {"id": "rt_" + uuid.uuid4().hex[:8], "created_at": ago()}, {**r, "repeat": "daily"})
                   for r in json.loads(self.last_body or b"{}").get("routines") or []]
            SCHEDULE[:] = [t for t in SCHEDULE if t.get("repeat") != "daily"] + new
            return self.send_json({"routines": new})
        if p.startswith("/api/v1/assistant/followups/") and self.command == "DELETE":
            SCHEDULE[:] = [t for t in SCHEDULE if t["id"] != p.split("/")[5]]
            return self.send_json({"ok": True})
        if p.startswith(("/api/v1/assistant/tasks/", "/api/v1/assistant/routines/", "/api/v1/assistant/followups/")) and p.endswith("/run"):
            tid = p.split("/")[5]
            SCHEDULE_RAN.append(tid)
            t = next((t for t in SCHEDULE if t["id"] == tid), {})
            if t.get("repeat") == "once": SCHEDULE[:] = [x for x in SCHEDULE if x["id"] != tid]
            elif not t.get("session_id") and not t.get("run_session_id"):  # the run starts the task's own conversation
                t.update({"run_machine_id": "server", "run_session_id": ASK2, "run_launcher": "claude"})
            return self.send_json({"session_id": t.get("session_id") or t.get("run_session_id") or ASK2, "machine_id": "server"})
        if p.startswith("/api/v1/launchers/") and (self.command == "DELETE" or p.endswith("/restore")):  # hide / bring back
            l = next((l for l in LAUNCHERS if l["name"] == p.split("/")[4]), None)
            if l is None: return self.send_json({"error": "没有这个启动器"}, 404)
            if l["source"] == "jarvis" and self.command == "DELETE": LAUNCHERS.remove(l)  # a Jarvis one is deleted
            else: l["hidden"] = self.command == "DELETE"
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/assistant/tasks/"):
            tid = p.split("/")[5]
            i = next((i for i, t in enumerate(SCHEDULE) if t["id"] == tid), None)
            if i is None: return self.send_json({"error": "这个定时任务已经不在了"}, 404)
            if self.command == "DELETE":
                del SCHEDULE[i]
                return self.send_json({"ok": True})
            SCHEDULE[i] = save_task(SCHEDULE[i], json.loads(self.last_body or b"{}"))
            return self.send_json({"task": SCHEDULE[i]})
        if p.startswith("/api/v1/sessions/") and p.endswith("/rename"):  # the hub keeps an assistant conversation's title
            body = json.loads(self.last_body or b"{}")
            sid = p.split("/")[5]
            RENAMED.append({"session_id": sid, **body})
            for s in ASSISTANT + SESSIONS:
                if s["session_id"] == sid and body.get("title"): s["title"] = body["title"]
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/sessions/") and p.endswith("/seen"):
            mark_seen(p.split("/")[5])
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/machines/") and p.endswith("/deploy"):  # 重新部署: deploying for 3 s, then the hub's build
            mid = p.split("/")[4]
            m = next((m for m in MACHINES if m["id"] == mid), None)
            if not m or not m.get("node_mismatch"):
                return self.send_json({"error": "这台电脑已经是 hub 这一版"}, 409)
            m["deploying"] = True
            def done(m=m):
                time.sleep(3)
                m.pop("deploying", None); m.pop("node_mismatch", None)
                for send, _ in list(LIVE):
                    try: send({"type": "machines", "machines": MACHINES})
                    except Exception: pass
            threading.Thread(target=done, daemon=True).start()
            for send, _ in list(LIVE):
                try: send({"type": "machines", "machines": MACHINES})
                except Exception: pass
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/machines/") and p.endswith("/reconnect"):
            mid = p.split("/")[4]
            RECONNECTS.append(mid)
            for m in MACHINES:
                if m["id"] == mid and not m.get("online"):  # the ssh comes back
                    m.update(online=True, error=None, hostname="lab", os="linux", arch="amd64", home="/home/me",
                             herdr_version="0.9.5", claude_version="2.1.320", last_seen=ago())
            return self.send_json({"ok": True})
        if p == "/api/v1/sessions" and self.command == "POST":  # 新建会话
            body = json.loads(self.last_body or b"{}")
            todo_id = body.get("todo_id")
            if todo_id:
                if is_read_only(self): return self.send_json({"error": "只读角色不能修改待办"}, 403)
                t = next((x for x in TODOS if x["id"] == todo_id), None)
                if not t: return self.send_json({"error": "待办不存在"}, 404)
                if t.get("done_at"): return self.send_json({"error": "待办已完成"}, 409)
            STARTED.append(body)
            if not machine_online(body.get("machine_id", "")):
                return self.send_json({"error": "machine is offline"}, 503)
            if body.get("cwd", "").endswith("/broken"):  # a launcher that never gets ready
                return self.send_json({"error": "claude did not become ready within 1m0s\nzsh: command not found: claude"}, 502)
            codex = body.get("launcher") == "codex"  # the node starts Codex with its first message and answers with its thread
            if codex and (body.get("async") or not (body.get("prompt") or "").strip()):
                return self.send_json({"error": "新建 Codex 会话要连第一条消息一起发：Codex 收到第一条消息才建会话。", "code": "codex_needs_prompt"}, 400)
            n = len(STARTED)
            START_IDS[0] += 1
            s = sess(body["machine_id"], "d4e6f8a0-0000-4000-8000-%012d" % START_IDS[0], (body.get("prompt") or "新会话").split("\n")[0], body.get("cwd", ""),
                     "working", "working", body.get("cwd", "").rsplit("/", 1)[-1] or "new", 9, body.get("tab_name") or "agent-%d" % n, n, 0, "",
                     agent="codex" if codex else "claude")
            s["launcher"] = body.get("launcher") or "claude"
            if codex:  # the node answers once the first message is in the thread's rollout
                ITEMS[s["session_id"]] = [it("user", "text", 0, text=body["prompt"])]
            if body.get("async"):  # answered at once, claude still starting (node/starting.go); MOCK_START_SECONDS later it is up
                s.update(status="idle", state="idle", starting=True, title=body.get("name_hint") or s["title"])
                def started(s=s):
                    s.pop("starting", None)
                threading.Timer(float(os.environ.get("MOCK_START_SECONDS", "6")), started).start()
            SESSIONS.append(s)
            if todo_id:
                now_str = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
                t = next((x for x in TODOS if x["id"] == todo_id), None)
                if t:
                    t.setdefault("agents", []).append({
                        "machine_id": s["machine_id"], "session_id": s["session_id"], "linked_at": now_str, "released_at": None,
                        "title": s["title"], "cwd": s["cwd"], "agent": s["agent"], "start_error": "",
                    })
                    t["updated_at"] = now_str
                    push_machine(s["machine_id"])
                    push_todos()
            return self.send_json({"session": decorated_session(s)})
        if p == "/api/v1/auth/tokens" and self.command == "POST":  # pairing trades the token for the device's own
            body = json.loads(self.last_body or b"{}")
            t = {"id": "dev_" + uuid.uuid4().hex[:6], "name": body.get("name", ""), "kind": body.get("kind", "phone"),
                 "role": "full", "created_at": ago(), "last_seen": ago()}
            TOKENS.append(t)
            return self.send_json({"id": t["id"], "token": "mock", "role": "full", "name": t["name"]})
        if p.startswith("/api/v1/auth/tokens/") and self.command == "DELETE":
            for t in TOKENS:
                if t["id"] == p.split("/")[-1]:
                    t["revoked_at"] = ago()
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/interactions/") and p.endswith("/resolve"):  # notification buttons (UI test reads it back)
            RESOLVED.append({"id": p.split("/")[4], "resolution": json.loads(self.last_body or b"{}")})
            if os.environ.get("MOCK_CODEX") and p.split("/")[4] == "cxa_orders_curl" and \
                    sum(r["id"] == "cxa_orders_curl" for r in RESOLVED) == 1:
                return self.send_json({"error": "电脑上 Codex 的输入框里有没发出去的内容：「先看下慢查询日志」。这次什么都没输入，免得和它拼在一起：在电脑上发出去或清空后再答。",
                                       "code": "input_draft", "delivered": "none"}, 409)
            if os.environ.get("MOCK_PROMPT_CHANGED") and p.split("/")[4].startswith("scr_"):
                return self.send_json({"error": "终端上的对话框已经变了（可能在电脑上答过了），这次什么都没按：看看新的再答。",
                                       "code": "prompt_changed", "delivered": "none"}, 409)
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/machines/") and (p.endswith("/claude-settings") or p.endswith("/codex-settings")) and self.command == "PUT":
            body, mid = json.loads(self.last_body or b"{}"), p.split("/")[4]
            if not machine_online(mid):
                return self.send_json({"error": "machine is offline"}, 503)
            store, make = (CODEX_SETTINGS, _codex_settings) if p.endswith("/codex-settings") else (CLAUDE_SETTINGS, _claude_settings)
            for st in store.setdefault(mid, make()):
                if st["key"] == body.get("key"):
                    st["value"] = body.get("value")
            return self.send_json({"settings": store[mid]})
        if p.startswith("/api/v1/devices/") and p.endswith("/prefs"):  # 设置 › 通知
            body = json.loads(self.last_body or b"{}")
            PREFS.update({k: v for k, v in body.items() if k in ("input", "done", "live_activity", "lock_details")})
            PREFS["island"].update({k: v for k, v in (body.get("island") or {}).items() if k in ("preset", "left", "right", "machine")})
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
        if p.startswith("/api/v1/sessions/") and p.endswith("/screen/before"):  # the terminal's scrollback, one page
            E = "\x1b["
            return self.send_json({"has_more": False, "at_limit": False, "text": "\r\n".join(
                ["$ git log --oneline -3", E + "33m3f9a1c2" + E + "0m Widget: hourly timeline",
                 E + "33m8b20e7d" + E + "0m Widget: small and medium sizes", E + "33m1c4d5a9" + E + "0m Forecast client",
                 "$ swift build", E + "32mBuild complete!" + E + "0m (8.31s)"])})
        if p.startswith("/api/v1/sessions/") and p.endswith("/keys"):
            KEYS.append(json.loads(self.last_body or b"{}"))
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/sessions/") and p.endswith("/modes"):  # permission mode / effort chips
            body = json.loads(self.last_body or b"{}")
            for s in all_sessions():
                if s["session_id"] == p.split("/")[5]:
                    s.update({k: v for k, v in body.items() if k in ("permission_mode", "effort") and v})
            return self.send_json({"ok": True})
        self.send_json({"ok": True})

    do_PUT = do_POST
    do_DELETE = do_POST
    do_PATCH = do_POST

    # ---------- minimal WebSocket ----------
    def port_connect(self, mid, port):
        """…/ports/{port}/connect: the WebSocket's binary messages are one TCP connection to the port (docs/ports.md)."""
        import socket
        listed = any(pp.get("port") == port for x in all_sessions() for pp in (x.get("ports") or []))
        if not listed: return self.send_json({"error": "port %d is not an agent's and was not opened" % port}, 403)
        try:
            tcp = socket.create_connection(("127.0.0.1", port), timeout=3)
        except OSError:
            return self.send_json({"error": "nothing is listening on port %d" % port}, 404)
        PORT_LOG.append({"machine_id": mid, "port": port})
        key = self.headers.get("Sec-WebSocket-Key", "")
        accept = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
        self.send_response(101, "Switching Protocols")
        self.send_header("Upgrade", "websocket"); self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accept); self.end_headers(); self.wfile.flush()
        lock = threading.Lock()

        def frame(op, data):
            n = len(data)
            hdr = bytes([0x80 | op]) + (bytes([n]) if n < 126 else bytes([126]) + struct.pack(">H", n) if n < 65536 else bytes([127]) + struct.pack(">Q", n))
            with lock:
                self.wfile.write(hdr + data); self.wfile.flush()

        def machine_to_phone():
            try:
                while True:
                    b = tcp.recv(32768)
                    if not b: break
                    frame(0x2, b)
            except OSError:
                pass
            try: frame(0x8, struct.pack(">H", 1000))
            except OSError: pass

        threading.Thread(target=machine_to_phone, daemon=True).start()
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
                    frame(0xA, payload); continue
                if op in (0, 1, 2): tcp.sendall(payload)
        except OSError:
            pass
        finally:
            try: tcp.shutdown(socket.SHUT_RDWR)
            except OSError: pass
            tcp.close()
        self.close_connection = True

    def websocket(self):
        # An app that asks for sessions_delta (?features=) gets each machine's list once, then deltas, like the hub
        # (hub/sessions_push.go); MOCK_WHOLE_LISTS=1 is an older hub that ignores the question.
        features = parse_qs(urlparse(self.path).query).get("features", [""])[0].split(",")
        delta = "sessions_delta" in features and not os.environ.get("MOCK_WHOLE_LISTS")
        have = {}  # machine -> {session id: its JSON}: what this app has
        key = self.headers.get("Sec-WebSocket-Key", "")
        accept = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
        self.send_response(101, "Switching Protocols")
        self.send_header("Upgrade", "websocket"); self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accept); self.end_headers(); self.wfile.flush()
        lock = threading.Lock()

        def send(obj):
            with lock:
                if delta and obj.get("type") == "sessions":
                    obj = sessions_for_delta_app(obj, have)
                    if obj is None: return
                data = json.dumps(obj, ensure_ascii=False).encode()
                n = len(data)
                hdr = bytes([0x81]) + (bytes([n]) if n < 126 else bytes([126]) + struct.pack(">H", n) if n < 65536 else bytes([127]) + struct.pack(">Q", n))
                self.wfile.write(hdr + data); self.wfile.flush()

        SOCKETS.add(self.connection)
        subscribed = set()
        LIVE.append((send, subscribed))
        send({"type": "hello", "server_version": "mock", "client_id": uuid.uuid4().hex[:8], "channel": "release",
              "build": 202611021500, "protocol": 1, "min_app_build": 0,
              "features": ["file_upload", "connectors"] + (["sessions_delta"] if delta else []) + (["terminal_stream"] if TERMINAL_STREAM else []) + (["terminal_control", "terminal_scroll", "terminal_click"] if TERMINAL_CONTROL else [])})
        drive = {}  # the terminal this connection drives: cols, rows, typed, prompt row
        send({"type": "machines", "machines": MACHINES})
        by = {}
        for s in all_sessions(): by.setdefault(s["machine_id"], []).append(s)
        for m, ss in by.items(): send({"type": "sessions", "machine_id": m, "sessions": ss})
        for ix in INTERACTIONS: send({"type": "interaction", "interaction": ix})  # as the real hub does on connect
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
                if op == 9:  # a pong carries the ping's payload back (URLSession ignores an empty one)
                    if NET["delay"] > 0: time.sleep(NET["delay"])
                    with lock: self.wfile.write(bytes([0x8A, len(payload)]) + payload); self.wfile.flush()
                    continue
                if op != 1: continue
                msg = json.loads(payload or b"{}")
                LOG.write("  WS %s\n" % msg.get("type")); LOG.flush()
                if msg.get("type") == "seen":
                    mark_seen(msg.get("session_id"))
                    continue
                if msg.get("type") == "terminal_input" and drive:
                    TERMINAL_INPUT.append(msg)
                    ctl = msg.get("control")
                    for ch in msg.get("text", ""):
                        if ch == "\x7f": drive["typed"] = drive["typed"][:-1]
                        elif ch == "\r": drive["typed"] = ""
                        elif ch >= " ": drive["typed"] += ch
                    if ctl: drive.update(cols=ctl["cols"], rows=ctl["rows"])
                    drive["send"](full=bool(ctl))
                    continue
                if msg.get("type") == "terminal_subscribe" and TERMINAL_STREAM and TERMINAL_CONTROL and msg.get("control"):
                    sid, mid, ctl = msg.get("session_id"), msg.get("machine_id"), msg["control"]
                    if os.environ.get("MOCK_TERMINAL_BUSY") and not msg.get("takeover"):
                        send({"type": "terminal_control", "machine_id": mid, "session_id": sid, "state": "busy",
                              "error": "电脑上另一个程序正在使用这个终端"})
                        continue
                    drive.update(cols=ctl["cols"], rows=ctl["rows"], typed=drive.get("typed", ""), seq=0, tick=0)

                    def frame(full=False, sid=sid, mid=mid):
                        lines, at = phone_screen(drive["cols"], drive["rows"], drive["typed"], drive["tick"])
                        E = "\x1b["
                        body = E + "2J" + "".join(E + "%d;1H" % (i + 1) + l for i, l in enumerate(lines)) if full else \
                            E + "%d;1H" % (at + 1) + lines[at] + E + "K" + "".join(E + "%d;1H" % (i + 1) + l + E + "K" for i, l in enumerate(lines) if "Brewing" in l)
                        col = 3 + sum(cell_width(c) for c in drive["typed"])
                        data = E + "?2026h" + E + "?25l" + body + E + "%d;%dH" % (at + 1, col) + E + "?25h" + E + "?2026l"
                        drive["seq"] += 1
                        send({"type": "terminal", "machine_id": mid, "session_id": sid, "epoch": "mock-ctl", "seq": drive["seq"],
                              "full": full, "cols": drive["cols"], "rows": drive["rows"], "data": base64.b64encode(data.encode()).decode()})
                    drive["send"] = frame
                    frame(full=True)
                    send({"type": "terminal_control", "machine_id": mid, "session_id": sid, "state": "active",
                          "cols": ctl["cols"], "rows": ctl["rows"]})

                    def ticks(mine=frame):
                        import time
                        while drive.get("send") is mine:
                            time.sleep(float(os.environ.get("MOCK_TERMINAL_INTERVAL", "1")))
                            if drive.get("send") is not mine: break
                            drive["tick"] += 1
                            mine()
                    threading.Thread(target=ticks, daemon=True).start()
                    continue
                if msg.get("type") == "unsubscribe":
                    subscribed.discard(msg.get("session_id"))
                    continue
                if msg.get("type") == "terminal_unsubscribe":
                    if drive: TERMINAL_INPUT.append(msg)  # letting go (the UI test looks for it)
                    drive.clear()
                    continue
                if msg.get("type") == "terminal_subscribe" and TERMINAL_STREAM:
                    sid, mid = msg.get("session_id"), msg.get("machine_id")

                    def term(sid=sid, mid=mid, after=msg.get("after_seq") or 0, epoch=msg.get("epoch")):
                        import time
                        frames = terminal_frames()
                        start = after if epoch == "mock" else 0
                        for f in frames[start:]:
                            send({"type": "terminal", "machine_id": mid, "session_id": sid, "epoch": "mock", **f})
                            time.sleep(float(os.environ.get("MOCK_TERMINAL_INTERVAL", "1")))
                    threading.Thread(target=term, daemon=True).start()
                    continue
                if msg.get("type") == "subscribe":
                    sid, mid = msg.get("session_id"), msg.get("machine_id")
                    subscribed.add(sid)
                    items = find_items(sid)
                    since = msg.get("since_seq") or 0
                    if GAP and sid == SESSIONS[0]["session_id"] and not GAP_STATE["started"]:
                        # The backlog below holds the head; then 6 more items are written, only the last 2 get pushed.
                        GAP_STATE["started"] = True

                        def later_push(sid=sid, mid=mid):
                            import time
                            time.sleep(2)
                            full = ITEMS[sid]
                            lost, tail = full[-6:-2], full[-2:]
                            GAP_STATE["grown"] = True
                            LOG.write("  GAP: pushing %d items from seq %d, %d left out\n" % (len(tail), lost[-1]["seq"], len(lost))); LOG.flush()
                            send({"type": "items", "machine_id": mid, "session_id": sid, "items": tail, **span(tail, lost[-1]["seq"])})
                        threading.Thread(target=later_push, daemon=True).start()
                    if STREAM and sid == SESSIONS[0]["session_id"] and not STREAM_STATE["started"]:
                        STREAM_STATE["started"] = True

                        def stream(sid=sid, mid=mid):
                            import time
                            for n in range(12):
                                time.sleep(2)
                                before = ITEMS[sid][-1]["seq"]
                                new = ([it("assistant", "thinking", 0, text="再检查一遍第 %d 处。" % n)] if n % 3 == 0 else
                                       tool("Read", {"file_path": "/Users/me/code/weather/WeatherWidget/Step%d.swift" % n}, "struct Step%d {}" % n, 0) if n % 3 == 1 else
                                       [it("assistant", "text", 0, text="第 %d 步做完了，接着看下一处。" % n)])
                                ITEMS[sid] = ITEMS[sid] + new
                                LOG.write("  STREAM: push %d items after seq %d\n" % (len(new), before)); LOG.flush()
                                # To every socket that has it open now: the app reconnects soon after launch, and the
                                # first socket going away used to end the stream after one push.
                                for out, subs in list(LIVE):
                                    if sid in subs:
                                        try:
                                            out({"type": "items", "machine_id": mid, "session_id": sid, "items": new, **span(new, before)})
                                        except Exception:
                                            pass
                        threading.Thread(target=stream, daemon=True).start()
                    if since:
                        newer = [x for x in items if x["seq"] > since]
                        send({"type": "items", "machine_id": mid, "session_id": sid, "items": newer, "backlog": True,
                              **span(items, since, max(since, items[-1]["seq"] if items else 0))})
                    else:
                        out = {"type": "items", "machine_id": mid, "session_id": sid, "backlog": True, "has_more": False, **span(items),
                               "items": fold_work(items) if msg.get("fold_work") else items}
                        served(sid, "backlog", out)
                        send(out)
        except Exception as e:
            LOG.write("  WS closed %r\n" % e); LOG.flush()
        SOCKETS.discard(self.connection)
        LIVE[:] = [x for x in LIVE if x[0] is not send]
        self.close_connection = True


if __name__ == "__main__":
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), H)
    srv.daemon_threads = True
    print("mock hub on 127.0.0.1:%d" % PORT, flush=True)
    srv.serve_forever()
