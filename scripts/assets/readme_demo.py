"""Small, language-specific README fixtures. No accounts, people or real computer identities."""

MAIN = "hub/demo-main"
TESTS = "laptop/demo-tests"
WIDGET = "laptop/demo-widget"
PLAN = "desktop/demo-photos"

COPY = {
    "en": {
        "hub": "Hub", "laptop": "Laptop", "desktop": "Desktop",
        "main_title": "What needs my attention?",
        "main_ask": "What needs my attention today?",
        "main_reply": "There are **3 things** to follow up on:\n\n1. **Network tests** — choose a test framework.\n2. **Photo export** — review the HEIC support plan.\n3. **Design review** — a draft is ready in the Inbox.\n\nYou can make the decisions here; the agents will continue on your computers.",
        "widget_title": "Add a weather widget",
        "widget_ask": "Add a home screen widget with the temperature and the next six hours of rain. Support small and medium sizes.",
        "widget_thinking": "I will reuse the hourly forecast and check both widget sizes.",
        "widget_reply": "The hourly timeline is ready and the sample tests pass. I am finishing the rain chart and checking both widget sizes.",
        "tests_title": "Add network tests",
        "tests_ask": "Cover retry and timeout behavior with network unit tests.",
        "tests_reply": "Which test framework should I use?",
        "question_header": "Test framework",
        "question": "Which framework should I use for the network tests?",
        "swift_detail": "Use parameterized tests for the sample project.",
        "xctest_detail": "Use the same framework as the UI tests.",
        "photos_title": "Support HEIC export",
        "photos_ask": "Support HEIC photos, converting them to JPEG only during export.",
        "photos_reply": "The plan is ready for your review. The original files will stay untouched.",
        "plan": "## Support HEIC export\n\n1. Convert HEIC to JPEG at export time.\n2. Keep the original file and its metadata.\n3. Skip files already exported.\n4. Test portrait and Live Photos.",
        "design_title": "Design review", "design_sender": "Design team",
        "design_preview": "Please review the sample layout before the next build.",
        "design_summary": "A layout decision is ready for your review.",
        "design_draft": "The layout looks good. Please keep the secondary controls neutral.",
        "release_title": "Release summary", "release_sender": "Project updates",
        "release_preview": "The sample release notes are ready.",
        "planning_title": "Next steps", "planning_sender": "Team updates",
        "planning_preview": "The next tasks are organized by priority.",
        "notes_title": "Weekly notes", "notes_sender": "Sample feed",
        "notes_preview": "A short summary of the sample project.",
        "chat_source": "Lark", "mail_source": "Mail",
    },
    "zh-CN": {
        "hub": "hub 所在的机器", "laptop": "笔记本", "desktop": "台式电脑",
        "main_title": "今天要处理的事",
        "main_ask": "今天有什么需要我处理的？",
        "main_reply": "今天有 **3 件事**需要跟进：\n\n1. **网络层测试**：选一下测试框架。\n2. **照片导出**：确认支持 HEIC 的计划。\n3. **设计评审**：收件箱里有一条待确认的草稿。\n\n你在这里拍板，Agent 会在电脑上接着做。",
        "widget_title": "添加天气小组件",
        "widget_ask": "添加主屏幕小组件：显示温度和接下来六小时的降雨，支持小号和中号。",
        "widget_thinking": "我会复用逐小时预报，检查两种小组件尺寸。",
        "widget_reply": "逐小时的时间线已经做好，示例测试通过了。正在补齐降雨图，并检查两种尺寸的布局。",
        "tests_title": "补齐网络层测试",
        "tests_ask": "给网络层补单元测试，覆盖重试和超时。",
        "tests_reply": "先确认一下用哪个测试框架。",
        "question_header": "测试框架",
        "question": "网络层的测试用哪个框架？",
        "swift_detail": "使用参数化测试，覆盖示例项目的几种情况。",
        "xctest_detail": "和现有 UI 测试使用相同的框架。",
        "photos_title": "支持 HEIC 导出",
        "photos_ask": "支持 HEIC 照片，只在导出时转换成 JPEG。",
        "photos_reply": "计划写好了，等你确认。原文件不会修改。",
        "plan": "## 支持 HEIC 导出\n\n1. 导出时将 HEIC 转成 JPEG。\n2. 保留原文件和元数据。\n3. 跳过已经导出的文件。\n4. 补齐竖拍与实况照片的测试。",
        "design_title": "设计评审", "design_sender": "设计团队",
        "design_preview": "请在下次构建前确认一下示例布局。",
        "design_summary": "有一项布局需要你确认。",
        "design_draft": "布局可以，次要操作请保持中性样式。",
        "release_title": "发布摘要", "release_sender": "项目动态",
        "release_preview": "示例项目的发布说明已经整理好。",
        "planning_title": "下一步", "planning_sender": "团队动态",
        "planning_preview": "后续任务已按优先级整理。",
        "notes_title": "每周笔记", "notes_sender": "示例订阅",
        "notes_preview": "示例项目的一份简短摘要。",
        "chat_source": "飞书", "mail_source": "邮件",
    },
}


def dataset(language):
    text = COPY[language]

    def machine(identifier, operating_system, icon):
        system = {"os": operating_system, "name": "macOS" if operating_system == "darwin" else "Linux",
                  "version": "26" if operating_system == "darwin" else "24", "arch": "arm64",
                  "cores": 8, "memory": 16 * 1024 ** 3, "boot_time": "@unix:-86400", "cpu": "Sample CPU",
                  "model": "Sample computer", "kernel": "sample", "distro": "macos" if operating_system == "darwin" else "linux"}
        return {"id": identifier, "name": text[identifier], "kind": "local" if identifier == "hub" else "ssh",
                "os": operating_system, "icon": icon, "online": True, "arch": "arm64", "home": "/home/demo",
                "hostname": identifier, "engine": "herdr", "herdr_version": "0.9", "claude_version": "2.1",
                "last_seen": "@rel:0", "system": system}

    def session(key, title, state, last_text, assistant=False):
        machine_id, session_id = key.split("/")
        return {"machine_id": machine_id, "session_id": session_id, "title": title, "agent": "claude",
                "model": "opus", "launcher": "claude", "effort": "high", "permission_mode": "default",
                "cwd": "/home/demo/projects/weather", "started_by_persona": True, "assistant": assistant,
                "state": state, "status": "working" if state == "working" else "idle", "state_since": "@rel:-840",
                "last_text": last_text, "pending_interactions": 1 if state == "needs_you" else 0,
                "updated_at": "@rel:-60", "rev": 1}

    def item(key, seq, role, kind, content="", **extra):
        return {"id": f"readme-{key.split('/')[-1]}-{seq}#0", "seq": seq, "role": role, "kind": kind,
                "text": content, "ts": f"@rel:{-840 + seq * 20}", **extra}

    main = session(MAIN, text["main_title"], "idle", text["main_reply"], assistant=True)
    sessions = [session(TESTS, text["tests_title"], "needs_you", text["tests_reply"]),
                session(PLAN, text["photos_title"], "needs_you", text["photos_reply"]),
                session(WIDGET, text["widget_title"], "working", text["widget_reply"]), main]
    question = {"id": "readme-question", "machine_id": "laptop", "session_id": "demo-tests", "kind": "question",
                "session_title": text["tests_title"], "tool_name": "AskUserQuestion", "created_at": "@rel:-120",
                "questions": [{"header": text["question_header"], "question": text["question"], "multi_select": False,
                               "options": [{"label": "Swift Testing (Recommended)", "description": text["swift_detail"]},
                                           {"label": "XCTest", "description": text["xctest_detail"]}]}]}
    plan = {"id": "readme-plan", "machine_id": "desktop", "session_id": "demo-photos", "kind": "plan",
            "session_title": text["photos_title"], "tool_name": "ExitPlanMode", "plan": text["plan"], "created_at": "@rel:-180"}
    items = {
        "demo-main": [item(MAIN, 1, "user", "text", text["main_ask"]), item(MAIN, 2, "assistant", "text", text["main_reply"])],
        "demo-tests": [item(TESTS, 1, "user", "text", text["tests_ask"]), item(TESTS, 2, "assistant", "text", text["tests_reply"])],
        "demo-photos": [item(PLAN, 1, "user", "text", text["photos_ask"]), item(PLAN, 2, "assistant", "text", text["photos_reply"])],
        "demo-widget": [item(WIDGET, 1, "user", "text", text["widget_ask"]), item(WIDGET, 2, "assistant", "thinking", text["widget_thinking"]),
                        item(WIDGET, 3, "assistant", "tool_use", tool_name="Grep", tool_input={"pattern": "hourly", "path": "Sources"}, tool_use_id="readme-grep"),
                        item(WIDGET, 4, "user", "tool_result", "Sources/Forecast.swift: hourly forecast is available.", tool_use_id="readme-grep", is_error=False),
                        item(WIDGET, 5, "assistant", "text", text["widget_reply"])],
    }
    inbox = []
    for index, prefix in enumerate(["design", "release", "planning", "notes"]):
        chat = prefix in ("design", "planning")
        entry = {"id": f"readme-inbox-{prefix}", "source": "lark" if chat else "mail", "account": "demo-chat" if chat else "demo-mail",
                 "account_name": text["chat_source" if chat else "mail_source"], "kind": "mention" if prefix == "design" else "group" if chat else "mail",
                 "title": text[prefix + "_title"], "sender": text[prefix + "_sender"], "preview": text[prefix + "_preview"],
                 "summary": text["design_summary"] if prefix == "design" else text[prefix + "_preview"], "count": 1,
                 "tier": "action" if prefix == "design" else "fyi", "read": False, "started_at": "@rel:-600", "updated_at": f"@rel:{-120 - index * 120}"}
        if chat: entry["lark"] = {"feed_id": f"readme-{prefix}", "is_group": True, "start_position": "0"}
        if prefix == "design": entry["agent"] = {"state": "drafted", "draft": text["design_draft"], "note": text["design_summary"], "task": text["design_title"]}
        inbox.append(entry)
    return {"machines": [machine("hub", "linux", "server"), machine("laptop", "darwin", "laptop"), machine("desktop", "darwin", "desktop")],
            "sessions": sessions, "items": items, "interactions": [question, plan], "inbox": inbox,
            "assistant": {"main": "demo-main", "machine_id": "hub", "cwd": "/home/demo/.persona/assistant", "launcher": "claude", "timezone": "UTC", "notes_effective": "", "conversations": [main]},
            "prefs": {"input": False, "done": "off", "live_activity": False, "lock_details": False},
            "pins": {"spaces": [], "sessions": []}, "mutes": {"spaces": [], "sessions": [], "allowed": []},
            "launchers": [{"name": "claude", "model": "opus"}, {"name": "codex", "model": "gpt-5.5"}],
            "todos": [], "tasks": [], "runs": [], "memory": [], "tokens": [], "mail_accounts": [], "mail": {}, "lark": {},
            "lark_conversations": [], "full": {}, "dirs": {"entries": []}, "kb": {}, "screen": "Sample terminal", "terminal_frames": ["Sample terminal"]}
