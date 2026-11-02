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
    /// The assistant (对话 tab) is answering; pushed only to devices whose island shows it.
    var assistant = false
    /// The compact Dynamic Island's sides (设置 › 通知 › 灵动岛, IslandPrefs).
    var left: IslandSlot = .status
    var right: IslandSlot = .status
    /// The numbers the island shows, rounded to two significant digits by the hub.
    var stats: Stats?
    /// The persona this device's activity draws (设置 › 形象). The widget extension cannot read the app's settings, and
    /// on a phone it did not get them from the shared keychain either (build 98): the app stamps its own updates with
    /// it, and the hub echoes what the app told it (prefs `look`) in this device's pushes (docs/live-activity.md「形象」).
    /// Nil from an older hub: the extension falls back to the keychain.
    var look: FleetLook?

    nonisolated struct Stats: Codable, Hashable, Sendable {
        var tpm = 0
        var tokens = 0
        var requests = 0
        var loads: [Load] = []

        nonisolated struct Load: Codable, Hashable, Sendable {
            var id: String
            var name = ""
            var percent = 0
        }

        init(tpm: Int = 0, tokens: Int = 0, requests: Int = 0, loads: [Load] = []) {
            self.tpm = tpm
            self.tokens = tokens
            self.requests = requests
            self.loads = loads
        }

        init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            tpm = try c.decodeIfPresent(Int.self, forKey: .tpm) ?? 0
            tokens = try c.decodeIfPresent(Int.self, forKey: .tokens) ?? 0
            requests = try c.decodeIfPresent(Int.self, forKey: .requests) ?? 0
            loads = (try? c.decodeIfPresent([Load].self, forKey: .loads)) ?? []
        }
    }

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

    init(phase: Phase = .none, needsYou: Int = 0, working: Int = 0, unread: Int = 0, items: [Item] = [], workingSince: Double? = nil,
         details: Bool = false, assistant: Bool = false, left: IslandSlot = .status, right: IslandSlot = .status, stats: Stats? = nil,
         look: FleetLook? = nil) {
        self.phase = phase
        self.needsYou = needsYou
        self.working = working
        self.unread = unread
        self.items = items
        self.workingSince = workingSince
        self.details = details
        self.assistant = assistant
        self.left = left
        self.right = right
        self.stats = stats
        self.look = look
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
        assistant = try c.decodeIfPresent(Bool.self, forKey: .assistant) ?? false
        left = (try? c.decodeIfPresent(IslandSlot.self, forKey: .left)) ?? .status
        right = (try? c.decodeIfPresent(IslandSlot.self, forKey: .right)) ?? .status
        stats = try? c.decodeIfPresent(Stats.self, forKey: .stats)
        look = try? c.decodeIfPresent(FleetLook.self, forKey: .look)
    }

    /// The app's own copy for this device, like the hub's push (hub forDevice): its settings, the chosen load, and the
    /// persona it draws when given.
    func tailored(island: IslandPrefs, details: Bool, look: FleetLook? = nil) -> FleetState {
        var s = self
        s.details = details
        if let look { s.look = look }
        (s.left, s.right) = island.sides
        if var stats = s.stats, !stats.loads.isEmpty {
            let pick = stats.loads.first { $0.id == island.machine } ?? stats.loads.max { $0.percent < $1.percent }
            stats.loads = pick.map { [$0] } ?? []
            s.stats = stats
        }
        return s
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

    /// 980 / 1.2k / 12k / 4.2M / 42M: numbers that fit beside the camera.
    static func short(_ n: Int) -> String {
        func scaled(_ v: Double, _ unit: String) -> String {
            v < 10 ? String(format: "%.1f", v).replacingOccurrences(of: ".0", with: "") + unit : "\(Int(v.rounded()))" + unit
        }
        switch n {
        case ..<1000: return "\(n)"
        case ..<1_000_000: return scaled(Double(n) / 1000, "k")
        case ..<1_000_000_000: return scaled(Double(n) / 1_000_000, "M")
        default: return scaled(Double(n) / 1_000_000_000, "B")
        }
    }

    /// A count past 99 where the island has room for one short number (IslandGlyph).
    static let many = "99+"

    /// The words for a number on the expanded island / lock screen.
    static func stat(_ cell: IslandCell) -> String? {
        switch cell {
        case let .tpm(n): "\(short(n))/分"
        case let .tokens(n): "今日 \(short(n))"
        case let .requests(n): "今日 \(short(n)) 次"
        case let .assistant(on): on ? "助手在回答" : "助手空闲"
        case let .load(name, p): name.isEmpty ? "负载 \(p)%" : "\(name) \(p)%"
        default: nil
        }
    }
}

/// What one side of the compact Dynamic Island can show (hub notify.go, the same names).
nonisolated enum IslandSlot: String, Codable, Hashable, Sendable, CaseIterable {
    case status, tpm, tokens, requests, assistant, load, timer, none

    init(from decoder: Decoder) throws {
        self = IslandSlot(rawValue: (try? decoder.singleValueContainer().decode(String.self)) ?? "") ?? .status
    }

    var title: String {
        switch self {
        case .status: "状态数量"
        case .tpm: "每分钟 token"
        case .tokens: "今日 token"
        case .requests: "今日请求数"
        case .assistant: "Jarvis 助手"
        case .load: "机器负载"
        case .timer: "最久那一轮的计时"
        case .none: "不显示"
        }
    }
}

/// 设置 › 通知 › 灵动岛, stored per device on the hub (prefs.island).
nonisolated struct IslandPrefs: Codable, Hashable, Sendable {
    enum Preset: String, Codable, CaseIterable, Sendable {
        case a, b, c, custom

        init(from decoder: Decoder) throws {
            self = Preset(rawValue: (try? decoder.singleValueContainer().decode(String.self)) ?? "") ?? .a
        }

        var title: String {
            switch self {
            case .a: "状态"
            case .b: "状态 + 速度"
            case .c: "状态 + 今日 token"
            case .custom: "自定义"
            }
        }
    }

    var preset: Preset = .a
    var left: IslandSlot?
    var right: IslandSlot?
    /// Whose load `load` shows; nil: the busiest machine.
    var machine: String?

    enum CodingKeys: String, CodingKey { case preset, left, right, machine }

    init(preset: Preset = .a, left: IslandSlot? = nil, right: IslandSlot? = nil, machine: String? = nil) {
        self.preset = preset
        self.left = left
        self.right = right
        self.machine = machine
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        preset = (try? c.decodeIfPresent(Preset.self, forKey: .preset)) ?? .a
        left = try? c.decodeIfPresent(IslandSlot.self, forKey: .left)
        right = try? c.decodeIfPresent(IslandSlot.self, forKey: .right)
        machine = (try? c.decodeIfPresent(String.self, forKey: .machine)).flatMap { $0?.isEmpty == false ? $0 : nil }
    }

    /// Every field, empty when unset: the hub merges what is sent, so leaving one out would keep the old value.
    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(preset, forKey: .preset)
        try c.encode(left?.rawValue ?? "", forKey: .left)
        try c.encode(right?.rawValue ?? "", forKey: .right)
        try c.encode(machine ?? "", forKey: .machine)
    }

    var sides: (left: IslandSlot, right: IslandSlot) {
        switch preset {
        case .a: (.status, .status)
        case .b: (.status, .tpm)
        case .c: (.status, .tokens)
        case .custom: (left ?? .status, right ?? .status)
        }
    }
}

/// One side of the compact island, worked out from the state: "needs you" always takes the left side; a `status` side
/// shows the most urgent count (left) or the next one (right).
nonisolated enum IslandCell: Hashable, Sendable {
    case needsYou(Int), working(Int), unread(Int)
    case tpm(Int), tokens(Int), requests(Int)
    case assistant(Bool)
    case load(name: String, percent: Int)
    case timer(Date)
    case empty

    var isStatus: Bool { count != nil }

    /// How many agents a status cell counts.
    var count: Int? {
        switch self {
        case let .needsYou(n), let .working(n), let .unread(n): n
        default: nil
        }
    }

    static func sides(_ s: FleetState) -> (left: IslandCell, right: IslandCell) {
        var counts: [IslandCell] = []
        if s.needsYou > 0 { counts.append(.needsYou(s.needsYou)) }
        if s.working > 0 { counts.append(.working(s.working)) }
        if s.unread > 0 { counts.append(.unread(s.unread)) }
        let left: IslandCell = s.needsYou > 0 ? counts[0] : cell(s.left, s, status: counts.first)
        var right: IslandCell
        if s.right == .status {
            // The count after the one on the left; with a number on the left, the most urgent count.
            let next = left.isStatus ? counts.firstIndex(of: left).map { $0 + 1 } ?? 0 : 0
            right = next < counts.count ? counts[next] : .empty
        } else {
            right = cell(s.right, s, status: nil)
        }
        if right == left { right = .empty }
        return (left, right)
    }

    /// The single circle when another app's activity shares the island: the most urgent count.
    static func minimal(_ s: FleetState) -> IslandCell {
        if s.needsYou > 0 { return .needsYou(s.needsYou) }
        if s.working > 0 { return .working(s.working) }
        if s.unread > 0 { return .unread(s.unread) }
        return sides(s).left
    }

    static func cell(_ slot: IslandSlot, _ s: FleetState, status: IslandCell?) -> IslandCell {
        switch slot {
        case .status: status ?? .empty
        case .tpm: .tpm(s.stats?.tpm ?? 0)
        case .tokens: .tokens(s.stats?.tokens ?? 0)
        case .requests: .requests(s.stats?.requests ?? 0)
        case .assistant: .assistant(s.assistant)
        case .load: s.stats?.loads.first.map { .load(name: $0.name, percent: $0.percent) } ?? .empty
        case .timer: s.workingSinceDate.map { .timer($0) } ?? .empty
        case .none: .empty
        }
    }
}

/// Which persona the Live Activity draws and in what color (docs/live-activity.md「形象」): the app's choice in
/// 设置 › 外观 › 形象 and the accent it draws that persona in. It rides in the activity's content (`FleetState.look`),
/// and the app also leaves it with a package's pictures in a keychain group it shares with the widget extension
/// (LiveActivity/FleetPersona.swift: drawing, saving, reading).
nonisolated struct FleetLook: Codable, Hashable, Sendable {
    /// PersonaFace.Design's raw value: a built-in's drawing; for a package, what is drawn while its faces cannot be.
    var design: String
    /// The accent as "0xRRGGBB" values, light and dark.
    var light: UInt32
    var dark: UInt32
    /// A hub package ("<id>/<rev>"): drawn from its faces (`FleetFaces`). Nil: a built-in.
    var package: String?

    init(design: String, light: UInt32, dark: UInt32, package: String? = nil) {
        self.design = design
        self.light = light
        self.dark = dark
        self.package = package
    }
}
