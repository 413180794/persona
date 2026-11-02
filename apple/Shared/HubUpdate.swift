import Foundation

/// The hub's version and its updating itself (docs/hub-update.md, GET /api/v1/hub/update and the `hub_update` event).
nonisolated struct HubUpdateStatus: Codable, Equatable, Sendable {
    var version: String
    var build: Int64
    var channel: String // release | docker | dev
    var canUpdate: Bool
    var whyNot: String?
    var latest: Latest?
    var state: String // idle | checking | available | downloading | waiting_idle | restarting | confirming | updated | rolled_back | failed
    var progress: String?
    var busy: [Busy]
    var error: String?
    var auto: Bool
    var autoNotice: Bool
    var lastCheck: Date?
    var confirmed: Bool

    nonisolated struct Latest: Codable, Equatable, Sendable {
        var version: String
        var build: Int64
        var released: Date?
        var notes: String?
        var newer: Bool
        var bad: Bool // went back before: not installed automatically
    }

    /// Something the restart would interrupt.
    nonisolated struct Busy: Codable, Equatable, Sendable, Identifiable {
        var machineID: String
        var machine: String
        var sessionID: String?
        var title: String
        var id: String { machineID + "/" + (sessionID ?? title) }

        enum CodingKeys: String, CodingKey {
            case machineID = "machine_id", machine, sessionID = "session_id", title
        }
    }

    enum CodingKeys: String, CodingKey {
        case version, build, channel, canUpdate = "can_update", whyNot = "why_not", latest, state, progress, busy, error, auto
        case autoNotice = "auto_notice", lastCheck = "last_check", confirmed
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.decodeIfPresent(String.self, forKey: .version) ?? ""
        build = try c.decodeIfPresent(Int64.self, forKey: .build) ?? 0
        channel = try c.decodeIfPresent(String.self, forKey: .channel) ?? "dev"
        canUpdate = try c.decodeIfPresent(Bool.self, forKey: .canUpdate) ?? false
        whyNot = try c.decodeIfPresent(String.self, forKey: .whyNot)
        latest = try c.decodeIfPresent(Latest.self, forKey: .latest)
        state = try c.decodeIfPresent(String.self, forKey: .state) ?? "idle"
        progress = try c.decodeIfPresent(String.self, forKey: .progress)
        busy = try c.decodeIfPresent([Busy].self, forKey: .busy) ?? []
        error = try c.decodeIfPresent(String.self, forKey: .error)
        auto = try c.decodeIfPresent(Bool.self, forKey: .auto) ?? false
        autoNotice = try c.decodeIfPresent(Bool.self, forKey: .autoNotice) ?? false
        lastCheck = try c.decodeIfPresent(Date.self, forKey: .lastCheck)
        confirmed = try c.decodeIfPresent(Bool.self, forKey: .confirmed) ?? true
    }

    /// Between swapping the binary and the new version confirming: the socket going away is expected.
    var restarting: Bool { state == "restarting" || state == "confirming" }
    /// Doing something the app shows progress for.
    var working: Bool { ["downloading", "waiting_idle", "restarting", "confirming"].contains(state) }
    /// A newer release the user could install now.
    var installable: Bool { canUpdate && latest?.newer == true }

    /// The channel, for people.
    var channelTitle: String {
        switch channel {
        case "release": "发布版"
        case "docker": "Docker"
        default: "开发版"
        }
    }

    /// What the progress row says.
    var stateTitle: String {
        switch state {
        case "checking": "正在检查"
        case "downloading": "正在下载"
        case "waiting_idle": busy.isEmpty ? "等助手和 Agent 空闲" : "等 \(busy.count) 个在忙的结束"
        case "restarting": "正在重启，会断开一会儿"
        case "confirming": "新版本正在确认"
        case "updated": "已更新"
        case "rolled_back": "已回滚"
        case "failed": "没更新成功"
        case "available": "有新版本"
        default: "已是最新"
        }
    }
}

/// One update in the hub's history (GET /api/v1/hub/update/history).
nonisolated struct HubUpdateRecord: Codable, Equatable, Sendable, Identifiable {
    var fromVersion: String
    var toVersion: String
    var toBuild: Int64
    var started: Date?
    var ended: Date?
    var result: String // updated | rolled_back | failed
    var reason: String?
    var auto: Bool
    var id: String { "\(toBuild)/\(started?.timeIntervalSince1970 ?? 0)" }

    var resultTitle: String {
        switch result {
        case "updated": "已更新"
        case "rolled_back": "已回滚"
        default: "没更新成功"
        }
    }

    enum CodingKeys: String, CodingKey {
        case fromVersion = "from_version", toVersion = "to_version", toBuild = "to_build", started, ended, result, reason, auto
    }
}

/// One of the assistant's skills (GET /api/v1/skills): the user's own in ~/.jarvis/skills, or built in.
nonisolated struct AgentSkill: Codable, Equatable, Sendable, Identifiable {
    var name: String
    var description: String?
    var source: String // user | builtin
    var id: String { name }
}

nonisolated struct SkillsResponse: Codable, Sendable {
    var machine: String?
    var skills: [AgentSkill]
}

/// What the hub said it is in its hello (docs/protocol.md): its build and protocol, for the compatibility notices.
nonisolated struct HubHello: Equatable, Sendable {
    var channel: String?
    var build: Int64?
    var protocolVersion: Int?
    var minAppBuild: Int?
}

/// Whether this app and the hub go together (docs/hub-update.md「兼容」).
nonisolated enum HubCompat: Equatable, Sendable {
    case ok
    case hubTooOld // its protocol is older than this app needs (a hub from before protocols says none)
    case appTooOld // the hub needs a newer app build than this one

    /// The oldest hub protocol this app works with.
    static let minHubProtocol = 1

    static func of(_ hello: HubHello?, appBuild: Int) -> HubCompat {
        guard let hello else { return .ok } // not connected yet: nothing to say
        if let min = hello.minAppBuild, min > 0, appBuild > 0, appBuild < min { return .appTooOld }
        if (hello.protocolVersion ?? 0) < minHubProtocol { return .hubTooOld }
        return .ok
    }
}

/// The one quiet line over the Agents list about the hub (docs/hub-update.md「App 里」), the most pressing first.
nonisolated enum HubNotice: Equatable, Sendable {
    case appTooOld
    case hubTooOld
    case updating(String) // what it is doing
    case rolledBack(String) // why, until dismissed
    case autoOn // tell once that it updates itself
    case available(version: String, build: Int64)

    /// What `dismissed` holds once the user closed the 「hub 太旧」 line.
    static let hubTooOldDismissed: Int64 = -1

    /// `dismissed`: the build (or problem) the user closed the line for.
    static func of(_ status: HubUpdateStatus?, compat: HubCompat, dismissed: Int64) -> HubNotice? {
        switch compat {
        case .appTooOld: return .appTooOld
        case .hubTooOld: return dismissed == HubNotice.hubTooOldDismissed ? nil : .hubTooOld
        case .ok: break
        }
        guard let s = status else { return nil }
        if s.working { return .updating(s.stateTitle) }
        if s.state == "rolled_back", let e = s.error, let b = s.latest?.build, dismissed != b { return .rolledBack(e) }
        if s.autoNotice { return .autoOn }
        if let l = s.latest, s.installable, !s.auto, !l.bad, dismissed != l.build {
            return .available(version: l.version, build: l.build)
        }
        return nil
    }
}

extension APIClient {
    func hubUpdate() async throws -> HubUpdateStatus { try await get("/api/v1/hub/update") }

    func checkHubUpdate() async throws -> HubUpdateStatus { try await post("/api/v1/hub/update/check") }

    /// Starts the update; `now` skips waiting for idle (the second confirmation's 现在就更新).
    func startHubUpdate(now: Bool) async throws -> HubUpdateStatus {
        try await post("/api/v1/hub/update", body: ["now": now])
    }

    func setHubUpdate(auto: Bool? = nil, noticeSeen: Bool? = nil) async throws -> HubUpdateStatus {
        struct Body: Encodable {
            var auto: Bool?
            var autoNoticeSeen: Bool?
            enum CodingKeys: String, CodingKey { case auto, autoNoticeSeen = "auto_notice_seen" }
        }
        return try JSON.decoder.decode(HubUpdateStatus.self, from: try await request("PUT", "/api/v1/hub/update/settings",
                                                                                     body: Body(auto: auto, autoNoticeSeen: noticeSeen)))
    }

    func hubUpdateHistory() async throws -> [HubUpdateRecord] {
        struct R: Decodable { var history: [HubUpdateRecord] }
        return try await get("/api/v1/hub/update/history", as: R.self).history
    }

    func skills() async throws -> SkillsResponse { try await get("/api/v1/skills") }
}
