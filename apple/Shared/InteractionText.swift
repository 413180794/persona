import Foundation

/// The first heading line of an agent's plan: the first non-empty line without Markdown markers
/// ("### Plan" -> "Plan"), what every card and the Mac menu bar title shows. An empty plan is Plan.
nonisolated enum PlanText {
    static func title(_ plan: String) -> String {
        for line in plan.split(separator: "\n") {
            let t = line.trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "#* "))
            if !t.isEmpty { return t }
        }
        return String(localized: "执行计划")
    }
}

nonisolated extension Interaction {
    /// One line on what a needs-you card asks, chosen by its kind — the same wording on the chat card, the inbox's
    /// Needs You row and the Mac menu bar: a plan's first heading, a terminal dialog's question, a question card's
    /// first question, and a permission card's tool with what it runs.
    /// - Parameter includeToolName: a permission card shows `toolName: summary`; pass false to show the summary
    ///   only and fall back to the tool name, as the inbox row does (its next line already names the card kind).
    /// - Returns: nil when the card carries no such text; the caller falls back to its generic title.
    func askLine(includeToolName: Bool) -> String? {
        switch kind {
        case .question:
            return questions?.first?.question
        case .plan:
            return PlanText.title(plan ?? "")
        case .permission:
            if includeToolName {
                let line = [toolName, summary].compactMap { $0 }.joined(separator: permissionSeparator)
                return line.isEmpty ? nil : line
            }
            // The inbox row: trim whitespace-only summaries so it never draws a blank asks line.
            if let s = summary?.trimmingCharacters(in: .whitespacesAndNewlines), !s.isEmpty { return s }
            if let t = toolName?.trimmingCharacters(in: .whitespacesAndNewlines), !t.isEmpty { return t }
            return nil
        case .screen:
            return screenDialog?.question
        }
    }

    private var permissionSeparator: String {
        // Same separator on the chat card, the inbox row and the Mac menu bar.
        String(localized: "：", comment: "Separator between a tool name and what it runs, e.g. Bash：npm test")
    }
}
