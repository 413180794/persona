import Foundation

/// The bounded list contract. Tokens are opaque and page cursors never stand in for delta checkpoints.
nonisolated struct SessionListQuery: Hashable, Sendable {
    static let pageSize = 50
    /// Client-only selection of the no-opt-in full collection; never sent as a made-up server filter.
    var legacy = false
    var state: String = ""
    var machine: String = ""
    var project: String = ""
    var group: String = ""
    var source: String = ""
    var provider: String = ""
    var text: String = ""

    var parameters: [String: String] {
        var out = ["summary": "1", "limit": String(Self.pageSize)]
        for (key, value) in [("state", state), ("machine_id", machine), ("project", project), ("group", group),
                             ("source", source), ("provider", provider), ("q", String(text.prefix(256)))] where !value.isEmpty {
            out[key] = value
        }
        return out
    }
}

nonisolated struct SessionListPage<Row: Decodable & Sendable>: Decodable, Sendable {
    var sessions: [Row]
    var remove: [String]?
    var hasMore: Bool?
    var nextCursor: String?
    var revision: String?
    var total: Int?
    var isPaged: Bool { revision != nil && hasMore != nil && total != nil }
    enum CodingKeys: String, CodingKey {
        case sessions, remove, revision, total
        case hasMore = "has_more", nextCursor = "next_cursor"
    }
}

nonisolated struct SessionListCounts: Decodable, Equatable, Sendable {
    var total: Int
    var machine: [String: Int]
    var project: [String: Int]
    var group: [String: Int]
    var state: [String: Int]
    var revision: String
}

extension APIClient {
    func sessionListPage(_ query: SessionListQuery, cursor: String? = nil, since: String? = nil) async throws -> SessionListPage<Session> {
        if query.legacy {
            return SessionListPage(sessions: try await sessions(), remove: [], hasMore: nil, nextCursor: nil, revision: nil, total: nil)
        }
        var parameters = query.parameters
        if let cursor { parameters["cursor"] = cursor }
        if let since { parameters["since"] = since }
        let data = try await request("GET", "/api/v1/sessions", query: parameters)
        var page = try await Task.detached(priority: .userInitiated) {
            try JSON.decoder.decode(SessionListPage<Session>.self, from: data)
        }.value
        page.sessions = stamp(page.sessions)
        return page
    }

    /// Complete metadata is fetched only when a chat opens. The existing bounded API has no id filter: narrow by
    /// machine/title, then inspect each bounded page by full identity (duplicate titles are not identities).
    func sessionListDetail(_ key: SessionKey, title: String) async throws -> Session {
        let prefix = String(title.unicodeScalars.prefix(256))
        // A title is mutable. If it changed before the hint's HTTP request, retry by stable machine/id rather than
        // reporting that an existing chat disappeared. Each lookup still uses bounded pages and discards other rows.
        for text in prefix.isEmpty ? [""] : [prefix, ""] {
            var query = SessionListQuery(machine: key.machineID, text: text).parameters
            query["summary"] = "0"
            for _ in 0..<160 {
                let data = try await request("GET", "/api/v1/sessions", query: query)
                let page = try await Task.detached(priority: .userInitiated) { try JSON.decoder.decode(SessionListPage<Session>.self, from: data) }.value
                if let row = page.sessions.first(where: { $0.machineID == key.machineID && $0.sessionID == key.sessionID }) { return stamp(row) }
                guard page.hasMore == true, let cursor = page.nextCursor else { break }
                query["cursor"] = cursor
                try Task.checkCancellation()
            }
        }
        throw APIError(status: 404, message: String(localized: "No matching sessions"))
    }

    func sessionListCounts(_ query: SessionListQuery, personal: Bool = false) async throws -> SessionListCounts {
        var parameters = query.parameters
        parameters["counts"] = "1"
        let data = try await request("GET", personal ? "/api/v1/agents/sessions" : "/api/v1/sessions", query: parameters)
        return try await Task.detached(priority: .utility) { try JSON.decoder.decode(SessionListCounts.self, from: data) }.value
    }
}

/// A runtime removal is a JSON tuple, not a slash-delimited path. Both components may contain slashes.
nonisolated enum SessionListIdentity {
    static func runtime(_ token: String, group: String) -> SessionKey? {
        guard let pair = try? JSON.decoder.decode([String].self, from: Data(token.utf8)), pair.count == 2 else { return nil }
        return SessionKey(group: group, machineID: pair[0], sessionID: pair[1])
    }
}
