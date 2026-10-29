import Foundation

/// The "Fleet" Live Activity's content (hub/liveactivity.go FleetState, field for field): every agent that needs you,
/// is working, or finished unseen, summed up for the lock screen and the Dynamic Island. The hub computes it and
/// pushes it over APNs; the open app gets the same thing as the `fleet` WebSocket event.
///
/// Times are Unix seconds, not `Date`: ActivityKit decodes pushed JSON with a plain JSONDecoder, which reads a Date
/// as seconds since 2001. Every field decodes leniently so an older or newer hub never kills the activity.
nonisolated struct FleetState: Codable, Hashable, Sendable {
    var phase: Phase = .none
    var needsYou = 0
    var working = 0
    var unread = 0
    var items: [Item] = []
    var workingSince: Double?
    /// The lock screen may show agent names (设置 › 通知 › 锁屏显示详情); off, only counts.
    var details = false

    nonisolated enum Phase: String, Codable, Hashable, Sendable {
        case needsYou, working, done
        case none = ""

        init(from decoder: Decoder) throws {
            self = Phase(rawValue: (try? decoder.singleValueContainer().decode(String.self)) ?? "") ?? .none
        }
    }

    nonisolated struct Item: Codable, Hashable, Sendable, Identifiable {
        var id: String // "machine/session"
        var title = ""
        var place = ""
        var kind: Kind = .working
        var detail = ""
        /// The interaction 允许 / 拒绝 / 批准 resolve.
        var interaction: String?
        var since: Double?

        nonisolated enum Kind: String, Codable, Hashable, Sendable {
            case permission, plan, question, screen, working, unread

            init(from decoder: Decoder) throws {
                self = Kind(rawValue: (try? decoder.singleValueContainer().decode(String.self)) ?? "") ?? .screen
            }
        }

        init(id: String, title: String, place: String = "", kind: Kind, detail: String = "", interaction: String? = nil, since: Double? = nil) {
            self.id = id
            self.title = title
            self.place = place
            self.kind = kind
            self.detail = detail
            self.interaction = interaction
            self.since = since
        }

        init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.decode(String.self, forKey: .id)
            title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
            place = try c.decodeIfPresent(String.self, forKey: .place) ?? ""
            kind = try c.decodeIfPresent(Kind.self, forKey: .kind) ?? .screen
            detail = try c.decodeIfPresent(String.self, forKey: .detail) ?? ""
            interaction = try c.decodeIfPresent(String.self, forKey: .interaction)
            since = try c.decodeIfPresent(Double.self, forKey: .since)
        }

        var needsYou: Bool { kind != .working && kind != .unread }
        var sinceDate: Date? { since.map { Date(timeIntervalSince1970: $0) } }

        /// machine id and session id, for the deep link.
        var key: (machine: String, session: String)? {
            guard let slash = id.firstIndex(of: "/") else { return nil }
            return (String(id[..<slash]), String(id[id.index(after: slash)...]))
        }
    }

    init(phase: Phase = .none, needsYou: Int = 0, working: Int = 0, unread: Int = 0, items: [Item] = [], workingSince: Double? = nil, details: Bool = false) {
        self.phase = phase
        self.needsYou = needsYou
        self.working = working
        self.unread = unread
        self.items = items
        self.workingSince = workingSince
        self.details = details
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.decodeIfPresent(Phase.self, forKey: .phase) ?? .none
        needsYou = try c.decodeIfPresent(Int.self, forKey: .needsYou) ?? 0
        working = try c.decodeIfPresent(Int.self, forKey: .working) ?? 0
        unread = try c.decodeIfPresent(Int.self, forKey: .unread) ?? 0
        items = try c.decodeIfPresent([Item].self, forKey: .items) ?? []
        workingSince = try c.decodeIfPresent(Double.self, forKey: .workingSince)
        details = try c.decodeIfPresent(Bool.self, forKey: .details) ?? false
    }

    var isEmpty: Bool { phase == .none }
    var workingSinceDate: Date? { workingSince.map { Date(timeIntervalSince1970: $0) } }
    /// Agents left out when only the first `showing` items are drawn ("+2 个").
    func hidden(showing: Int) -> Int { max(0, needsYou + working + unread - min(showing, items.count)) }
    /// The first request that can be settled with a button (permission or plan).
    var firstActionable: Item? { items.first { ($0.kind == .permission || $0.kind == .plan) && $0.interaction != nil } }
}

/// The activity's wording, in one place so tests pin it and the widget and the app say the same thing.
nonisolated enum FleetText {
    static func headline(_ s: FleetState) -> String {
        switch s.phase {
        case .needsYou: "\(s.needsYou) 个需要你"
        case .working: "\(s.working) 个在工作"
        case .done: "\(s.unread) 个做完了"
        case .none: "都处理完了"
        }
    }

    /// "授权：Bash: go test" / "计划等你批准" / "Claude 在问你" … for one item.
    static func line(_ item: FleetState.Item) -> String {
        switch item.kind {
        case .permission: item.detail.isEmpty ? "等你授权" : "授权：" + item.detail
        case .plan: item.detail.isEmpty ? "计划等你批准" : "计划：" + item.detail
        case .question: item.detail.isEmpty ? "在问你" : item.detail
        case .screen: item.detail.isEmpty ? "终端等你处理" : item.detail
        case .working: "工作中"
        case .unread: item.detail.isEmpty ? "做完了" : item.detail
        }
    }

    /// What a hidden (lock screen without details) request says instead of its title.
    static func anonymous(_ item: FleetState.Item) -> String {
        switch item.kind {
        case .permission: "有个授权等你"
        case .plan: "有个计划等你批准"
        case .question: "有个提问等你回答"
        case .screen: "有个终端等你处理"
        case .working: "工作中"
        case .unread: "做完了"
        }
    }

    static func more(_ n: Int) -> String { "+\(n) 个" }
}
