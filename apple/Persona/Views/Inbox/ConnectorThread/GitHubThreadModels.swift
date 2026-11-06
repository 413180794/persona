import Foundation

/// A GitHub pull request or issue as GitHub's thread (GitHubThreadView): what the page works out from a thread's page
/// before drawing it.
nonisolated enum GitHubThread {
    /// One entry of the timeline: a message, or the checks that ran one after another, as one card.
    enum Entry: Identifiable, Equatable {
        case message(ThreadMessage)
        case checks([ThreadMessage])

        var id: String {
            switch self {
            case .message(let m): m.id
            case .checks(let ms): "checks-" + (ms.first?.id ?? "")
            }
        }
    }

    /// The timeline's entries, oldest first, consecutive checks together.
    static func entries(_ messages: [ThreadMessage]) -> [Entry] {
        var out: [Entry] = []
        for m in messages {
            if m.kind == "check" {
                if case .checks(let run)? = out.last {
                    out[out.count - 1] = .checks(run + [m])
                } else {
                    out.append(.checks([m]))
                }
            } else {
                out.append(.message(m))
            }
        }
        return out
    }

    /// "example-org/example-repo · #12" from the thread's id ("example-org/example-repo#12").
    static func reference(_ thread: String) -> String {
        guard let hash = thread.lastIndex(of: "#") else { return thread }
        return thread[..<hash] + " · " + thread[hash...]
    }

    /// The pull request's or issue's state in a word (state pill).
    static func stateTitle(_ state: String?) -> String {
        switch state {
        case "merged": String(localized: "Merged")
        case "closed": String(localized: "github.closed", defaultValue: "Closed")
        case "draft": String(localized: "Draft")
        default: String(localized: "github.open", defaultValue: "Open")
        }
    }

    /// What a review did, in a few words.
    static func reviewTitle(_ review: String?) -> String {
        switch review {
        case "approved": String(localized: "approved")
        case "changes_requested": String(localized: "requested changes")
        case "dismissed": String(localized: "review dismissed")
        default: String(localized: "reviewed")
        }
    }

    /// The checks in one line: 3 checks · 1 failed · 1 in progress.
    static func checksSummary(_ checks: [ThreadMessage]) -> String {
        let states = checks.compactMap { $0.check?.state }
        var parts = [String(localized: "\(states.count) checks")]
        let failed = states.filter { $0 == "failure" }.count
        let pending = states.filter { $0 == "pending" }.count
        let passed = states.filter { $0 == "success" }.count
        if failed > 0 { parts.append(String(localized: "\(failed) failed")) }
        if pending > 0 { parts.append(String(localized: "\(pending) in progress")) }
        if failed == 0 && pending == 0 && passed == states.count { parts.append(String(localized: "all passed")) }
        return parts.joined(separator: " · ")
    }

    /// A reply that quotes a comment, as GitHub's Quote Reply does: its words as a Markdown quote, a blank line, the reply.
    static func quoting(_ quoted: ThreadMessage?, reply: String) -> String {
        guard let quoted else { return reply }
        let said = (quoted.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        guard !said.isEmpty else { return reply }
        let lines = said.components(separatedBy: "\n").prefix(6).map { "> " + $0 }
        return lines.joined(separator: "\n") + "\n\n" + reply // no @name: GitHub would notify them
    }

    /// A diff hunk's lines and what each is: added, removed, or context (the header too).
    static func hunkLines(_ hunk: String) -> [(text: String, mark: Character?)] {
        hunk.components(separatedBy: "\n").map { line in
            let mark = line.first.flatMap { $0 == "+" || $0 == "-" ? $0 : nil }
            return (line, mark)
        }
    }
}
