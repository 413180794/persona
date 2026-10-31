#!/usr/bin/env python3
"""A fake Jarvis hub serving made-up data, for README / App Store screenshots. Nothing here is real.

    python3 scripts/mock_hub.py            # listens on 127.0.0.1:18760 (PORT=… to change)

Pair the app with it: -pairURL "jarvis://pair?url=http%3A%2F%2F127.0.0.1%3A18760&token=mock" (any token works).
It serves machines, agents, conversations, interactions and the inbox, enough for the main screens; requests it
does not know are logged to stderr as MISSING. Timestamps are relative to when it starts. See docs/testing.md.
"""
import base64, unicodedata
import hashlib, json, os, struct, sys, threading, time, uuid
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
    sess("studio", BLOG, "博客迁移到 Astro", "/Users/me/code/blog", "done", "unread", "blog", 2, "1", 1, 12,
         "迁移完成：32 篇文章都能正常构建，旧链接全部加了 301 跳转，Lighthouse 性能分从 71 提到 98。"),
    sess("studio", PHOTOS, "照片同步脚本支持 HEIC", "/Users/me/code/photo-sync", "blocked", "needs_you", "photo-sync", 3, "1", 1, 6,
         "计划写好了，等你批准后开始改。", pending=1, permission_mode="plan", effort="xhigh"),
    sess("server", ORDERS, "排查 /orders 接口变慢", "/home/me/code/shop-api", "working", "working", "shop-api", 1, "perf", 1, 1,
         "慢在订单列表的 N+1 查询，正在改成一次 JOIN 并补索引。", agent="codex"),
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
                                  "links": [{"kind": "people", "id": "p_han", "name": "韩梅梅", "one_line": "产品经理，周五约了午饭", "importance": "P1"},
                                            {"kind": "chats", "id": "oc_team", "name": "天气 App 项目群", "one_line": "今天在讨论降雨条的样式", "importance": "P1"},
                                            {"kind": "docs", "id": "doc_widget", "name": "小组件设计稿 v3", "one_line": "小号、中号两种布局", "importance": "P2"}],
                                  "meta": {"name": "天气 App", "importance": "P0", "relevance": 92, "tags": ["iOS", "小组件"], "kind": "short",
                                           "status": "active", "dirs": ["macbook:/Users/me/code/weather"]}},
}

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
    if os.environ.get("MOCK_TERMINAL_FIXTURE"):  # frames recorded from herdr (ios/JarvisTests/Fixtures/terminal-observe.jsonl)
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


CLAUDE_SETTINGS = {}  # machine id → its settings, made on first use
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

# Mail bodies (GET /api/v1/mail/messages/{aid}/{uid}); 504 is in English, for 设置 › 翻译.
INBOX.insert(next(i for i, x in enumerate(INBOX) if x["kind"] == "mail"), {"id": "mail:acc1:t4:504", "source": "mail", "kind": "mail", "account": "acc1", "account_name": "个人邮箱",
     "title": "Can we move the design review?", "sender": "Lucy Wang",
     "preview": "Hi, I'm travelling on Thursday. Could we move the design review to Tuesday afternoon?", "count": 1,
     "started_at": ago(hours=1), "updated_at": ago(hours=1), "tier": "action", "summary": "Lucy 想把设计评审挪到周二下午",
     "reason": "要你确认时间", "read": False,
     "mail": {"account_id": "acc1", "mailbox": "INBOX", "uids": [504], "from": {"name": "Lucy Wang", "email": "lucy@example.com"}, "direct": True}})
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
               "updated_at": ago(minutes=30)}]
def _lm(pos, who, text, mine=False, mins=60):
    return {"id": "m" + pos, "position": pos, "sender_id": "u-" + who, "sender_name": who, "is_mine": mine, "type": "TEXT", "text": text,
            "created_at": ago(minutes=mins), "recalled": False, "reply_count": 0}
LARK_MESSAGES = {"f2": [
    _lm("200", "Lucy Wang", "The new icon set is in the shared folder. Could you take a look before the review this afternoon?", mins=58),
    _lm("201", "李雷", "我先看了一下，标签栏的图标偏细", mins=57),
    _lm("202", "Lucy Wang", "Agreed. I'll make the tab bar icons one weight heavier and send an update by 3 pm.", mins=56),
    _lm("203", "我", "Sounds good, thanks!", mine=True, mins=55),
]}

MAIL_ACCOUNTS = [{"id": "acc1", "name": "个人邮箱", "email": "me@example.com", "provider": "imap", "auth": "password",
                  "imap_host": "imap.example.com", "imap_port": 993, "smtp_host": "smtp.example.com", "smtp_port": 465}]


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
    return SESSIONS + ASSISTANT


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
# original placeholder (scripts/persona/make_persona.py demo), made into a temporary directory at start.
PERSONAS_DIR = os.environ.get("MOCK_PERSONAS", "")
if PERSONAS_DIR == "demo":
    import subprocess, tempfile
    PERSONAS_DIR = tempfile.mkdtemp(prefix="mock-personas-")
    subprocess.run([sys.executable, os.path.join(os.path.dirname(os.path.abspath(__file__)), "persona", "make_persona.py"),
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
# MOCK_CHURN=1 also puts a screenshot (a tool result image) in the middle of the first conversation's work.
LIVE = []  # (send, subscribed session ids) of each open WebSocket
CHURN = {"rounds": 0}
CHURN_ITEMS = {}  # each conversation's items before churn added to them (/mock/reset puts them back)


def png(w, h, rgb):
    """A plain w×h PNG of one color (stdlib only)."""
    import zlib
    raw = b"".join(b"\0" + bytes(rgb) * w for _ in range(h))
    def chunk(t, d): return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))


def churn(rounds, ms, steps=1, final=False):
    import time
    for k in range(rounds):
        time.sleep(ms / 1000)
        now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
        working = [s for s in SESSIONS if s["state"] == "working"]
        for s in working:
            s.update(updated_at=now, last_text="第 %d 轮更新：还在跑，刚看完一处。" % (k + 1))
        if len(working) >= 2 and k % 2 == 1:  # the busiest space is now a different one
            working[0]["updated_at"] = ago(minutes=30)
        for m in MACHINES:
            m["last_seen"] = now
        by = {}
        for s in all_sessions(): by.setdefault(s["machine_id"], []).append(s)
        for send, subscribed in list(LIVE):
            try:
                send({"type": "machines", "machines": MACHINES})
                for m, ss in by.items(): send({"type": "sessions", "machine_id": m, "sessions": ss})
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


def say(text, user=None, stream=False):
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
                for k in range(1, 11):
                    time.sleep(0.3)
                    send({"type": "delta", "machine_id": mid, "session_id": sid, "text": text[:len(text) * k // 10]})
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
                                                 int(q.get("steps", ["1"])[0]), q.get("final", ["0"])[0] == "1"), daemon=True).start()
            return self.send_json({"ok": True})
        if p == "/mock/churned": return self.send_json(CHURN)
        if p == "/mock/say":  # ChatFollowTests: a reply arrives in every open conversation (?text=…, ?user=… a message before it)
            args = (q.get("text", ["好了。"])[0], q["user"][0] if "user" in q else None, q.get("stream", ["0"])[0] == "1")
            threading.Thread(target=say, args=args, daemon=True).start()
            return self.send_json({"ok": True})
        if not p.startswith("/mock/") and self.bad_network(): return
        if p == "/api/v1/ws":
            return self.websocket()
        if p == "/api/v1/health": return self.send_json({"ok": True, "version": "mock"})
        if p == "/api/v1/machines": return self.send_json({"machines": MACHINES})
        if p == "/api/v1/metrics":
            return self.send_json({"machines": [machine_metrics(m["id"], 3600, 60) for m in MACHINES], "step": 60})
        if p.startswith("/api/v1/machines/") and p.endswith("/metrics"):
            r = q.get("range", ["1h"])[0]
            secs, step = {"1h": (3600, 10), "6h": (6 * 3600, 60), "24h": (86400, 240)}.get(r, (3600, 10))
            return self.send_json({"metrics": machine_metrics(p.split("/")[4], secs, step), "range": r, "step": step})
        if p == "/api/v1/sessions": return self.send_json({"sessions": all_sessions()})
        if p == "/api/v1/interactions": return self.send_json({"interactions": INTERACTIONS})
        if p == "/api/v1/interactions/history": return self.send_json({"interactions": []})
        if p == "/mock/resolved": return self.send_json({"resolved": RESOLVED})
        if p == "/api/v1/auth/tokens": return self.send_json({"tokens": TOKENS, "current": "dev_phone"})
        if p == "/mock/sent": return self.send_json({"sent": SENT})
        if p == "/mock/hits": return self.send_json({"hits": HITS})
        if p == "/mock/reset":  # MachinePagesTests: each test starts from the same machines
            for m in MACHINES:
                if m["id"] == "lab":
                    for k in ("hostname", "os", "arch", "home", "herdr_version", "claude_version"): m.pop(k, None)
                    m.update(online=False, error="ssh: connect to host lab port 22: Connection refused")
            SESSIONS[:] = [x for x in SESSIONS if not x["session_id"].startswith("d4e6f8a0-")]
            del RECONNECTS[:], STARTED[:], SEEN[:]
            for sid, items in CHURN_ITEMS.items(): ITEMS[sid] = list(items)  # LiveUpdateTests, ChatFollowTests: what churn / say added goes
            for x in SESSIONS:
                if x["session_id"] in ORIG_STATE:
                    x["state"], x["status"] = ORIG_STATE[x["session_id"]]
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
        if p == "/api/v1/assistant/tasks": return self.send_json({"tasks": SCHEDULE})
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
        if p == "/mock/served": return self.send_json({"served": SERVED})
        if p == "/api/v1/inbox": return self.send_json({"items": INBOX})
        if p.startswith("/api/v1/inbox/"):
            iid = p[len("/api/v1/inbox/"):]
            for x in INBOX:
                if x["id"] == iid: return self.send_json(dict(x, context=x["preview"]))
        if p == "/api/v1/mail/accounts": return self.send_json({"accounts": MAIL_ACCOUNTS})
        if p.startswith("/api/v1/mail/messages/") and p.count("/") == 6:
            uid = int(p.rsplit("/", 1)[1])
            it = next((x for x in INBOX if (x.get("mail") or {}).get("uids") == [uid]), None)
            if it: return self.send_json({"message": {"account_id": "acc1", "mailbox": "INBOX", "uid": uid, "subject": it["title"],
                "from": it["mail"]["from"], "to": [{"name": "Me", "email": "me@example.com"}], "date": it["updated_at"],
                "text": MAIL_BODIES.get(uid, it["preview"]), "seen": False}})
        if p == "/api/v1/mail/messages": return self.send_json({"messages": [], "has_more": False})
        if p == "/api/v1/lark/unread-count": return self.send_json({"count": 4})
        if p in ("/api/v1/lark/conversations", "/api/v1/lark/pinned"):
            return self.send_json({"conversations": LARK_CHATS if p.endswith("conversations") else [], "next_cursor": ""})
        if p.startswith("/api/v1/lark/conversations/") and p.endswith("/messages"):
            return self.send_json({"messages": LARK_MESSAGES.get(p.split("/")[5], []), "has_older": False, "has_newer": False})
        if p == "/api/v1/lark/labels": return self.send_json({"labels": []})
        if p == "/api/v1/lark/flags": return self.send_json({"conversations": [], "messages": []})
        if p == "/api/v1/lark/emojis": return self.send_json({"emojis": {}})
        if p.startswith("/api/v1/machines/") and not machine_online(p.split("/")[4]) and p.split("/")[5] in ("launchers", "dirs"):
            return self.send_json({"error": "%s: machine is offline" % p.split("/")[4]}, 503)
        if p.startswith("/api/v1/machines/") and p.endswith("/dirs"):
            return self.send_json(mock_dirs(p.split("/")[4], (q.get("path") or [""])[0]))
        if p.startswith("/api/v1/machines/") and p.endswith("/launchers"):
            return self.send_json({"launchers": [{"name": "claude", "model": "opus"}, {"name": "codex", "model": "gpt-5.5"}]})
        if p.startswith("/api/v1/machines/") and p.endswith("/claude-settings"):
            return self.send_json({"settings": CLAUDE_SETTINGS.setdefault(p.split("/")[4], _claude_settings())})
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
            from urllib.parse import unquote
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
            e = next((e for e in KB.get(kind, []) if e["id"] == parts[1]), None)
            if e is None: return self.send_json({"error": "not found"}, 404)
            d = dict({"kind": kind, "id": e["id"], "path": "/home/me/.jarvis/data/%s/%s" % (kind, e["id"]), "summary": e.get("one_line")},
                     **KB_DETAIL.get((kind, e["id"]), {}))
            out = {"detail": d}
            if kind == "projects":
                dirs = [x.split(":", 1)[1] for x in (d.get("meta") or {}).get("dirs", [])]
                out["agents"] = [x for x in SESSIONS if x.get("cwd") in dirs]
            return self.send_json(out)
        if p == "/api/v1/kb-search":
            word = (q.get("q") or [""])[0].lower()
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
            time.sleep(float(os.environ.get("MOCK_LARK_SEND_SECONDS", "2")))
            if "#fail" in body.get("text", ""):
                return self.send_json({"error": "lark-connector: send timed out"}, 502)
            msgs = LARK_MESSAGES.setdefault(feed, [])
            pos = str(max([int(m["position"]) for m in msgs] or [0]) + 1)
            msgs.append(_lm(pos, "我", body.get("text", ""), mine=True, mins=0))
            return self.send_json({"ok": True})
        if p.endswith("/messages") and not os.environ.get("MOCK_SEND_REFUSE"):  # sent messages, once per client_msg_id
            body = json.loads(self.last_body or b"{}")
            if not any(m.get("client_msg_id") and m.get("client_msg_id") == body.get("client_msg_id") for m in SENT):
                SENT.append(body)
            return self.send_json({"ok": True})
        if p == "/api/v1/inbox/refresh": return self.send_json({"items": INBOX})
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
        if p.startswith("/api/v1/assistant/tasks/") and p.endswith("/run"):
            tid = p.split("/")[5]
            SCHEDULE_RAN.append(tid)
            t = next((t for t in SCHEDULE if t["id"] == tid), {})
            if t.get("repeat") == "once": SCHEDULE[:] = [x for x in SCHEDULE if x["id"] != tid]
            return self.send_json({"session_id": t.get("session_id") or ASK2, "machine_id": "server"})
        if p.startswith("/api/v1/assistant/tasks/"):
            tid = p.split("/")[5]
            i = next((i for i, t in enumerate(SCHEDULE) if t["id"] == tid), None)
            if i is None: return self.send_json({"error": "这个定时任务已经不在了"}, 404)
            if self.command == "DELETE":
                del SCHEDULE[i]
                return self.send_json({"ok": True})
            SCHEDULE[i] = save_task(SCHEDULE[i], json.loads(self.last_body or b"{}"))
            return self.send_json({"task": SCHEDULE[i]})
        if p.startswith("/api/v1/sessions/") and p.endswith("/seen"):
            mark_seen(p.split("/")[5])
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
            STARTED.append(body)
            if not machine_online(body.get("machine_id", "")):
                return self.send_json({"error": "machine is offline"}, 503)
            if body.get("cwd", "").endswith("/broken"):  # a launcher that never gets ready
                return self.send_json({"error": "claude did not become ready within 1m0s\nzsh: command not found: claude"}, 502)
            n = len(STARTED)
            s = sess(body["machine_id"], "d4e6f8a0-0000-4000-8000-%012d" % n, body.get("prompt") or "新会话", body.get("cwd", ""),
                     "working", "working", body.get("cwd", "").rsplit("/", 1)[-1] or "new", 9, body.get("tab_name") or "agent-%d" % n, n, 0, "")
            if body.get("async"):  # answered at once, claude still starting (node/starting.go); MOCK_START_SECONDS later it is up
                s.update(status="idle", state="idle", starting=True, title=body.get("name_hint") or s["title"])
                def started(s=s):
                    s.pop("starting", None)
                threading.Timer(float(os.environ.get("MOCK_START_SECONDS", "6")), started).start()
            SESSIONS.append(s)
            return self.send_json({"session": s})
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
            if os.environ.get("MOCK_PROMPT_CHANGED") and p.split("/")[4].startswith("scr_"):
                return self.send_json({"error": "终端上的对话框已经变了（可能在电脑上答过了），这次什么都没按：看看新的再答。",
                                       "code": "prompt_changed", "delivered": "none"}, 409)
            return self.send_json({"ok": True})
        if p.startswith("/api/v1/machines/") and p.endswith("/claude-settings") and self.command == "PUT":
            body, mid = json.loads(self.last_body or b"{}"), p.split("/")[4]
            if not machine_online(mid):
                return self.send_json({"error": "machine is offline"}, 503)
            for st in CLAUDE_SETTINGS.setdefault(mid, _claude_settings()):
                if st["key"] == body.get("key"):
                    st["value"] = body.get("value")
            return self.send_json({"settings": CLAUDE_SETTINGS[mid]})
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

        SOCKETS.add(self.connection)
        subscribed = set()
        LIVE.append((send, subscribed))
        send({"type": "hello", "server_version": "mock", "client_id": uuid.uuid4().hex[:8],
              "features": ["file_upload"] + (["terminal_stream"] if TERMINAL_STREAM else []) + (["terminal_control", "terminal_scroll", "terminal_click"] if TERMINAL_CONTROL else [])})
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
