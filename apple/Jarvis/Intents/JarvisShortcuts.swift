import AppIntents

/// Siri / Spotlight shortcuts. Every phrase has to include `\(.applicationName)`; only AppEntity/AppEnum
/// parameters may be interpolated, which is why the message text is asked for after the phrase matches.
struct JarvisShortcuts: AppShortcutsProvider {
    nonisolated static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: AskJarvisIntent(),
            phrases: [
                "问 \(.applicationName)",
                "用 \(.applicationName) 问一下",
                "Ask \(.applicationName)",
                "Ask \(.applicationName) a question",
            ],
            shortTitle: "问 Jarvis",
            systemImageName: "sparkles"
        )
        AppShortcut(
            intent: AgentTaskIntent(),
            phrases: [
                "用 \(.applicationName) 让 Agent 去做",
                "让 \(.applicationName) 的 \(\.$agent) 去做",
                "\(.applicationName) 交给 Agent",
                "Have \(.applicationName) agent do something",
                "Send a task to \(\.$agent) with \(.applicationName)",
            ],
            shortTitle: "让 Agent 去做",
            systemImageName: "person.crop.circle.badge.checkmark"
        )
        AppShortcut(
            intent: AgentsStatusIntent(),
            phrases: [
                "\(.applicationName) 个人 Agent 未读",
                "\(.applicationName) Agent 有什么新消息",
                "Check \(.applicationName) agents",
            ],
            shortTitle: "Agent 未读",
            systemImageName: "tray.full"
        )
        AppShortcut(
            intent: AskClaudeIntent(),
            phrases: [
                "用 \(.applicationName) 问 Claude",
                "Ask Claude with \(.applicationName)",
                "\(.applicationName) 问 Claude",
                "用 \(.applicationName) 问一下 Claude",
            ],
            shortTitle: "问 Claude",
            systemImageName: "bubble.left.and.text.bubble.right"
        )
        AppShortcut(
            intent: JarvisStatusIntent(),
            phrases: [
                "\(.applicationName) 状态",
                "\(.applicationName) status",
                "\(.applicationName) 现在怎么样",
                "用 \(.applicationName) 看进度",
            ],
            shortTitle: "状态",
            systemImageName: "gauge.with.dots.needle.33percent"
        )
        AppShortcut(
            intent: ApproveFirstIntent(),
            phrases: [
                "用 \(.applicationName) 批准第一个",
                "\(.applicationName) 批准待处理",
            ],
            shortTitle: "批准第一个",
            systemImageName: "checkmark.shield"
        )
    }

    nonisolated static var shortcutTileColor: ShortcutTileColor { .tangerine }
}
