import Foundation

/// Settings › Claude Code / Codex: an enum option's name, by the setting's key (node claudeconfig.go / codexconfig.go), the
/// same names as the web's (`web/src/lib/agent-settings.ts` `optionText`, #3835). Effort levels read as the session menu
/// calls them (`SessionModes`). An option with no name here (a new one) stays as the node wrote it. The names are in the
/// app's language (Localizable.xcstrings, the web's wording in en and ja, #4793).
nonisolated enum AgentSettingOptions {
    private static let names: [String: [String: String]] = [
        "effortLevel": Dictionary(uniqueKeysWithValues: SessionModes.effortLevels.map { ($0.id, $0.title) }),
        "autoUpdatesChannel": [
            "latest": String(localized: "最新版", comment: "Claude Code update channel"),
            "stable": String(localized: "稳定版", comment: "Claude Code update channel"),
        ],
        "model_reasoning_effort": Dictionary(uniqueKeysWithValues: SessionModes.codexEffortLevels.map { ($0.id, $0.title) }),
        "model_reasoning_summary": [
            "auto": String(localized: "自动", comment: "Codex reasoning summary"),
            "concise": String(localized: "简短", comment: "Codex reasoning summary; also a verbosity"),
            "detailed": String(localized: "详细", comment: "Codex reasoning summary; also a verbosity"),
            "none": String(localized: "不显示", comment: "Codex reasoning summary"),
        ],
        "model_verbosity": [
            "low": String(localized: "简短", comment: "Codex reasoning summary; also a verbosity"),
            "medium": String(localized: "适中", comment: "Codex verbosity"),
            "high": String(localized: "详细", comment: "Codex reasoning summary; also a verbosity"),
        ],
        "approval_policy": [
            "untrusted": String(localized: "没把握的命令都先问", comment: "Codex approval policy"),
            "on-failure": String(localized: "出错了再问", comment: "Codex approval policy"),
            "on-request": String(localized: "它觉得需要时问", comment: "Codex approval policy"),
            "never": String(localized: "一律不问", comment: "Codex approval policy"),
        ],
        "sandbox_mode": [
            "read-only": String(localized: "只能读", comment: "Codex sandbox mode"),
            "workspace-write": String(localized: "能改工作目录", comment: "Codex sandbox mode"),
            "danger-full-access": String(localized: "不设限（危险）", comment: "Codex sandbox mode"),
        ],
    ]

    static func name(_ option: String, key: String) -> String {
        names[key]?[option] ?? option
    }
}
