import Foundation

/// How Persona's own MCP tools (server `jarvis`: `mcp__jarvis__agent_read`…) are named in a run of steps.
///
/// Display only: the server keeps its old name `jarvis` on the wire and everything that matches tools by their full
/// name stays as it is. The common tools get a verb of their own ("Read agent chat"); the rest read like the web's rows,
/// "Call Persona · agent read", never the raw `agent_read`.
enum PersonaToolNames {
    /// The MCP server names that are Persona itself (`jarvis` today; `persona` if the server is ever renamed).
    nonisolated static let servers: Set<String> = ["jarvis", "persona"]

    /// The server of an MCP tool's full name ("mcp__jarvis__agent_read" → "jarvis"), nil for any other tool.
    nonisolated static func server(_ name: String) -> String? {
        guard name.hasPrefix("mcp__") else { return nil }
        let rest = name.dropFirst(5)
        guard let r = rest.range(of: "__", options: .backwards), r.lowerBound > rest.startIndex else { return nil }
        return String(rest[..<r.lowerBound])
    }

    nonisolated static func isPersona(_ name: String) -> Bool {
        server(name).map { servers.contains($0) } ?? false
    }

    /// The verb for one of Persona's tools, nil for any other tool and for the ones `ToolPresentation.verb` already
    /// names (conversations, computers, browsers, phone, mail).
    static func verb(_ name: String) -> String? {
        guard isPersona(name) else { return nil }
        let n = ToolPresentation.baseName(name)
        if let own = verbs(n) { return own }
        // Already named by the generic table: its bare name comes back as something other than itself.
        if ToolPresentation.verb(n) != n { return nil }
        return fallback(n)
    }

    /// "Call Persona · agent read".
    static func fallback(_ base: String) -> String {
        let label = base.replacingOccurrences(of: "_", with: " ")
        return String(localized: "调用 Persona · \(label)")
    }

    private static func verbs(_ n: String) -> String? {
        switch n {
        // Agents on the user's computers (docs/assistant.md「Agents 与飞书工具」).
        case "agent_read": String(localized: "读 Agent 对话")
        case "agent_send", "send_to_agent": String(localized: "给 Agent 发消息")
        case "agents_list", "list_agents": String(localized: "看 Agent 列表")
        case "agent_start", "task_plan": String(localized: "派任务")
        case "interaction_answer": String(localized: "替你回答")
        case "ask_jarvis": String(localized: "交给 Persona")
        // The knowledge base (docs/knowledge.md).
        case "kb_get", "kb_list", "kb_search", "search_knowledge": String(localized: "查知识库")
        case "kb_update": String(localized: "更新知识库")
        // Talking to the user.
        case "ask_user": String(localized: "问你")
        case "notify_user": String(localized: "通知你")
        case "schedule_followup": String(localized: "提醒")
        case "list_followups": String(localized: "看提醒")
        case "cancel_followup": String(localized: "取消提醒")
        case "schedule_routine": String(localized: "设定期任务")
        case "delete_routine": String(localized: "删定期任务")
        case "inbox_draft": String(localized: "起草回复")
        case "jarvis_settings": String(localized: "Persona 设置")
        // Live cards (docs/assistant-views.md).
        case "view_show": String(localized: "画卡片")
        case "view_get": String(localized: "看卡片")
        case "view_update": String(localized: "更新卡片")
        default: nil
        }
    }
}
