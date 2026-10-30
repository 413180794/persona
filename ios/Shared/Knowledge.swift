import Foundation

/// The knowledge base under the hub's ~/.jarvis/data (docs/knowledge.md): projects, chats, people, docs, meetings and
/// events, each ranked by importance (P0–P3) and relevance to the user, with free tags.
nonisolated enum KBKind: String, CaseIterable, Identifiable, Sendable {
    case projects, chats, people, docs, meetings, events
    var id: String { rawValue }
    var title: String {
        switch self {
        case .projects: "项目"
        case .chats: "会话"
        case .people: "人"
        case .docs: "文档"
        case .meetings: "会议"
        case .events: "日程"
        }
    }
    var symbol: String {
        switch self {
        case .projects: "square.stack.3d.up"
        case .chats: "bubble.left.and.bubble.right"
        case .people: "person.2"
        case .docs: "doc.text"
        case .meetings: "video"
        case .events: "calendar"
        }
    }
}

nonisolated struct KBEntry: Codable, Identifiable, Hashable, Sendable {
    var kind: String
    var id: String
    var name: String
    var oneLine: String?
    var importance: String?
    var relevance: Int
    var tags: [String]?
    var updated: Date?
    var extra: String?

    enum CodingKeys: String, CodingKey {
        case kind, id, name, importance, relevance, tags, updated, extra
        case oneLine = "one_line"
    }

    /// Projects: "short · active" → 短期 / 长期 and the status.
    var projectKind: String? {
        guard kind == KBKind.projects.rawValue, let extra else { return nil }
        return extra.hasPrefix("long") ? "长期" : "短期"
    }
    var projectDone: Bool { kind == KBKind.projects.rawValue && (extra ?? "").hasSuffix("done") }
    /// Marked "not a project" (by hand, or because it only came from notifications).
    var projectIgnored: Bool { kind == KBKind.projects.rawValue && (extra ?? "").hasSuffix("ignored") }
}

nonisolated struct KBList: Decodable, Sendable {
    var items: [KBEntry]
    var tags: [String]?
}

nonisolated struct KBDetail: Decodable, Sendable {
    var kind: String
    var id: String
    var path: String
    var meta: JSONValue?
    var summary: String?
    var log: [String]?
    var recent: String?
    var links: [KBEntry]?
}

nonisolated struct KBDetailResponse: Decodable, Sendable {
    var detail: KBDetail
    var agents: [Session]?
}

/// A change made in the app; tagging set here is kept over the automatic one.
nonisolated struct KBUpdate: Encodable, Sendable {
    var importance: String?
    var relevance: Int?
    var tags: [String]?
    var status: String?
    var projectKind: String?
    var note: String?
    var log: String?
    enum CodingKeys: String, CodingKey {
        case importance, relevance, tags, status, note, log
        case projectKind = "project_kind"
    }
}

nonisolated struct KBHit: Decodable, Hashable, Sendable {
    var path: String
    var line: String

    /// The entry a hit is in: `chats/<id>/summary.md` → (chats, id), `docs/<id>.json` → (docs, id); nil for anything else.
    var entry: (kind: KBKind, id: String)? {
        let parts = path.split(separator: "/", omittingEmptySubsequences: true).map(String.init)
        guard parts.count >= 2, let kind = KBKind(rawValue: parts[0]) else { return nil }
        let id = parts.count == 2 && parts[1].hasSuffix(".json") ? String(parts[1].dropLast(5)) : parts[1]
        return id.isEmpty ? nil : (kind, id)
    }

    /// Which file the line is from, said plainly.
    var source: String {
        let file = (path as NSString).lastPathComponent
        if path.contains("/messages/") { return "聊天记录 · " + (file as NSString).deletingPathExtension }
        switch file {
        case "summary.md": return "档案"
        case "log.md": return "时间线"
        case "meta.json": return "资料"
        default: return file.hasSuffix(".json") ? "资料" : file
        }
    }

    /// The line without its markdown list marker, JSON key and quotes.
    var text: String {
        var t = line.trimmingCharacters(in: .whitespaces)
        for p in ["- ", "* ", "## ", "# "] where t.hasPrefix(p) { t = String(t.dropFirst(p.count)) }
        if t.hasPrefix("\""), let colon = t.range(of: "\": ") {
            t = String(t[colon.upperBound...])
        }
        t = t.trimmingCharacters(in: CharacterSet(charactersIn: " ,\""))
        return t
    }
}

/// A project's timeline line, `- 2026-10-30 23:16 · Agent · 标题 · 机器 · 说了什么`: when, from where, what.
nonisolated struct KBLogLine: Hashable, Sendable {
    var when: String
    var source: String
    var text: String

    init(_ raw: String) {
        var t = raw.trimmingCharacters(in: .whitespaces)
        if t.hasPrefix("- ") { t = String(t.dropFirst(2)) }
        let parts = t.components(separatedBy: " · ")
        if parts.count >= 2, parts[0].first?.isNumber == true {
            when = parts[0]
            text = parts.last ?? ""
            source = parts.count > 2 ? parts[1..<(parts.count - 1)].joined(separator: " · ") : ""
        } else {
            when = ""
            source = ""
            text = t
        }
    }

    /// "2026-10-30 23:16" → "10-30 23:16" (the year only when it is not this one).
    func shortWhen(now: Date = .now) -> String {
        let year = String(Calendar.current.component(.year, from: now))
        return when.hasPrefix(year + "-") ? String(when.dropFirst(5)) : when
    }
}

extension APIClient {
    func kbList(_ kind: KBKind, tag: String? = nil) async throws -> KBList {
        try await get("/api/v1/kb/\(kind.rawValue)", query: tag.map { ["tag": $0] } ?? [:])
    }
    func kbDetail(_ kind: String, id: String) async throws -> KBDetailResponse {
        try await get("/api/v1/kb/\(kind)/\(id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(["/"])) ?? id)")
    }
    func kbUpdate(_ kind: String, id: String, _ u: KBUpdate) async throws {
        _ = try await request("PUT", "/api/v1/kb/\(kind)/\(id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(["/"])) ?? id)", body: u)
    }
    func kbSearch(_ q: String) async throws -> [KBHit] {
        struct R: Decodable { var hits: [KBHit] }
        let r: R = try await get("/api/v1/kb-search", query: ["q": q])
        return r.hits
    }
}
