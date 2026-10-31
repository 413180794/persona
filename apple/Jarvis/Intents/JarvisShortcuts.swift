import AppIntents

/// Siri / Spotlight shortcuts. Every phrase has to include `\(.applicationName)`; only AppEntity/AppEnum
/// parameters may be interpolated, which is why the message text is asked for after the phrase matches.
struct JarvisShortcuts: AppShortcutsProvider {
    nonisolated static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: AskClaudeIntent(),
            phrases: [
                "用 \(.applicationName) 问 Claude",
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
