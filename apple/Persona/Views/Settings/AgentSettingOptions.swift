import Foundation

/// 设置 › Claude Code / Codex: an enum option's name, by the setting's key (node claudeconfig.go / codexconfig.go), the
/// same names as the web's (`web/src/lib/agent-settings.ts` `optionText`, #3835). Effort levels read as the session menu
/// calls them (`SessionModes`). An option with no name here (a new one) stays as the node wrote it.
nonisolated enum AgentSettingOptions {
    private static let names: [String: [String: String]] = [
        "effortLevel": Dictionary(uniqueKeysWithValues: SessionModes.effortLevels.map { ($0.id, $0.title) }),
        "autoUpdatesChannel": ["latest": "最新版", "stable": "稳定版"],
        "model_reasoning_effort": Dictionary(uniqueKeysWithValues: SessionModes.codexEffortLevels.map { ($0.id, $0.title) }),
        "model_reasoning_summary": ["auto": "自动", "concise": "简短", "detailed": "详细", "none": "不显示"],
        "model_verbosity": ["low": "简短", "medium": "适中", "high": "详细"],
        "approval_policy": ["untrusted": "没把握的命令都先问", "on-failure": "出错了再问", "on-request": "它觉得需要时问", "never": "一律不问"],
        "sandbox_mode": ["read-only": "只能读", "workspace-write": "能改工作目录", "danger-full-access": "不设限（危险）"],
    ]

    static func name(_ option: String, key: String) -> String {
        names[key]?[option] ?? option
    }
}
