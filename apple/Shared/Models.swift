import Foundation

// Wire types mirroring docs/protocol.md. Explicit CodingKeys everywhere: key strategies would also rewrite
// arbitrary JSON keys inside tool inputs and answer dictionaries.

nonisolated struct Machine: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String
    var kind: String
    var icon: String?
    var sshHost: String?
    var online: Bool
    var error: String?
    var hostname: String?
    var os: String?
    var arch: String?
    var home: String?
    var herdrVersion: String?
    var claudeVersion: String?
    var lastSeen: Date?
    /// The machine's jarvisd is not from the hub's build (newer hubs): some answers may be refused until it is deployed.
    var nodeMismatch: Bool?
    /// The machine's jarvisd build.
    var nodeVersion: String?
    /// The hub is installing its own build there (重新部署); deployError: why the last try did not work.
    var deploying: Bool?
    var deployError: String?
    /// OS and hardware (docs/machines.md); nil from hubs before machine monitoring, or a machine never connected.
    var system: MachineSystem?
    var engine: MachineEngine = .herdr
    var engines: [DetectedEngine] = []

    enum CodingKeys: String, CodingKey {
        case id, name, kind, icon, online, error, hostname, os, arch, home, system, engine, engines
        case sshHost = "ssh_host", herdrVersion = "herdr_version", claudeVersion = "claude_version", lastSeen = "last_seen"
        case nodeMismatch = "node_mismatch", nodeVersion = "node_version", deploying, deployError = "deploy_error"
    }

    var symbol: String {
        switch icon {
        case "server": Theme.Icon.server
        case "desktop": Theme.Icon.machine
        default: Theme.Icon.laptop
        }
    }
}

// Keep memberwise construction while decoding older hubs and future engines safely.
extension Machine {
    nonisolated init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        kind = try c.decode(String.self, forKey: .kind)
        icon = try c.decodeIfPresent(String.self, forKey: .icon)
        sshHost = try c.decodeIfPresent(String.self, forKey: .sshHost)
        online = try c.decode(Bool.self, forKey: .online)
        error = try c.decodeIfPresent(String.self, forKey: .error)
        hostname = try c.decodeIfPresent(String.self, forKey: .hostname)
        os = try c.decodeIfPresent(String.self, forKey: .os)
        arch = try c.decodeIfPresent(String.self, forKey: .arch)
        home = try c.decodeIfPresent(String.self, forKey: .home)
        herdrVersion = try c.decodeIfPresent(String.self, forKey: .herdrVersion)
        claudeVersion = try c.decodeIfPresent(String.self, forKey: .claudeVersion)
        lastSeen = try c.decodeIfPresent(Date.self, forKey: .lastSeen)
        nodeMismatch = try c.decodeIfPresent(Bool.self, forKey: .nodeMismatch)
        nodeVersion = try c.decodeIfPresent(String.self, forKey: .nodeVersion)
        deploying = try c.decodeIfPresent(Bool.self, forKey: .deploying)
        deployError = try c.decodeIfPresent(String.self, forKey: .deployError)
        system = try c.decodeIfPresent(MachineSystem.self, forKey: .system)
        engine = try c.decodeIfPresent(MachineEngine.self, forKey: .engine) ?? .herdr
        engines = try c.decodeIfPresent([DetectedEngine].self, forKey: .engines) ?? []
    }
}

nonisolated struct MachineEngine: RawRepresentable, Codable, Hashable, CaseIterable, Identifiable, Sendable {
    var rawValue: String
    init(rawValue: String) { self.rawValue = rawValue }
    static let herdr = Self(rawValue: "herdr")
    static let herdrApp = Self(rawValue: "herdr-core")
    static let orca = Self(rawValue: "orca")
    static let allCases: [Self] = [.herdr, .herdrApp, .orca]
    var id: String { rawValue }
    var name: String {
        switch self {
        case .herdr: "herdr"
        case .herdrApp: "Herdr App"
        case .orca: "Orca"
        default: rawValue
        }
    }
    var detail: String? {
        switch self {
        case .herdr: "终端里的 herdr"
        case .herdrApp: "桌面 App 里开的终端"
        case .orca: "Orca 里的终端"
        default: nil
        }
    }

    init(from decoder: Decoder) throws {
        rawValue = try decoder.singleValueContainer().decode(String.self)
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        try c.encode(rawValue)
    }
}

nonisolated struct DetectedEngine: Codable, Hashable, Identifiable, Sendable {
    var id: String
    var name: String
    var available: Bool
    var version: String?
    var note: String?
}

extension Machine {
    var engineName: String { engines.first { $0.id == engine.rawValue }?.name ?? engine.name }
}

nonisolated struct Launcher: Codable, Identifiable, Hashable, Sendable {
    var name: String
    var baseURL: String?
    var model: String?
    /// "codex" for plain codex or an alias running it (docs/codex.md「从手机新建」); nil: Claude Code.
    var agent: String? = nil
    var id: String { name }
    /// A Codex start sends its first message with it and waits for the thread Codex makes.
    var isCodex: Bool { agent == "codex" }

    enum CodingKeys: String, CodingKey {
        case name, model, agent
        case baseURL = "base_url"
    }
}

extension Launcher {
    /// Runs Claude Code or Codex: what the Jarvis assistant can use (headless `claude -p` or `codex app-server`).
    /// Other agents (TRAEX…) are left out; the node refuses them with a reason too.
    var runsAssistant: Bool { baseURL != "(traex)" }
}

nonisolated struct DirEntry: Codable, Hashable, Sendable, Identifiable {
    var name: String
    var path: String
    var isGit: Bool
    var id: String { path }
    enum CodingKeys: String, CodingKey { case name, path; case isGit = "is_git" }
}

nonisolated struct RecentDir: Codable, Hashable, Sendable, Identifiable {
    var path: String
    var name: String
    var lastUsed: Date?
    var id: String { path }
    enum CodingKeys: String, CodingKey { case path, name; case lastUsed = "last_used" }
}

nonisolated struct DirList: Codable, Sendable {
    var path: String
    var parent: String?
    var entries: [DirEntry]
    var recent: [RecentDir]?
}

nonisolated enum SessionStatus: String, Codable, Sendable {
    case idle, working, blocked, done, unknown, offline

    init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = SessionStatus(rawValue: raw) ?? .unknown
    }

    var isActive: Bool { self == .working || self == .blocked }
    var isLive: Bool { self != .offline }
}

/// New sources opt in to every operation; a partial capability object never grants missing permissions.
nonisolated struct SessionCapabilities: Codable, Hashable, Sendable {
    var read = false
    var send = false
    var interrupt = false
    var approve = false
    var terminal = false
    var attach = false

    enum CodingKeys: String, CodingKey { case read, send, interrupt, approve, terminal, attach }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        read = try c.decodeIfPresent(Bool.self, forKey: .read) ?? false
        send = try c.decodeIfPresent(Bool.self, forKey: .send) ?? false
        interrupt = try c.decodeIfPresent(Bool.self, forKey: .interrupt) ?? false
        approve = try c.decodeIfPresent(Bool.self, forKey: .approve) ?? false
        terminal = try c.decodeIfPresent(Bool.self, forKey: .terminal) ?? false
        attach = try c.decodeIfPresent(Bool.self, forKey: .attach) ?? false
    }
}

nonisolated struct Session: Codable, Identifiable, Hashable, Sendable {
    var machineID: String
    var sessionID: String
    var title: String
    var cwd: String
    var status: SessionStatus
    var paneID: String?
    var tabID: String?
    var workspaceID: String?
    var agentName: String?
    /// The coding agent in the pane: "claude" (or nil, from older hubs), "codex" or "traex" (TRAEX, an in-house Codex fork).
    var agent: String? = nil
    var sourceKind: String? = nil
    var sourceThreadID: String? = nil
    var sourceInstanceID: String? = nil
    var parentThreadID: String? = nil
    var readOnlyReason: String? = nil
    var capabilities: SessionCapabilities? = nil
    var attached: Bool? = nil
    /// How Jarvis reaches a Codex in a terminal (docs/codex.md「守护进程上的会话」): "app_server" (joined on its shared
    /// app-server) or "terminal" (its screen and session record); the note says why it is the terminal.
    var codexRoute: String? = nil
    var codexRouteNote: String? = nil
    var launcher: String?
    var model: String?
    /// Claude Code's permission mode and effort level right now (`SessionModes`); nil while unknown and for Codex.
    var permissionMode: String? = nil
    var effort: String? = nil
    var startedByJarvis: Bool
    var assistant: Bool
    /// An assistant conversation moved out of the sidebar (listed under 已归档; nothing is deleted).
    var archived: Bool? = nil
    var spaceKind: String?
    var spaceID: String?
    var spaceName: String?
    var spaceIndex: Int?
    var slotID: String?
    var slotName: String?
    var slotIndex: Int?
    var state: AgentState
    var seenAt: Date?
    /// When the hub saw it enter its current state (`state_since`).
    var stateSince: Date? = nil
    var queued: [String]?
    /// Shells / subagents Claude left running in the background.
    var background: [BackgroundTask]?
    /// The /loop driving this agent (it wakes itself up on a schedule).
    var loop: LoopInfo?
    /// Agents it launched (the Agent tool); each reads like a session ("<id>@<agent id>").
    var subagents: [Subagent]?
    /// The ports its processes listen on that answer HTTP (docs/ports.md): its dev servers, opened through the hub.
    var ports: [ListeningPort]? = nil
    var updatedAt: Date?
    var lastText: String?
    var pendingInteractions: Int
    /// Changes whenever the transcript does: the app pulls sessions whose rev it has not synced (`AgentSync`).
    var rev: Int64?
    /// The next message Claude Code proposes after a turn (grey in its input box on the computer), until it works again.
    var suggestion: String?
    /// Jarvis is still opening its tab and starting claude: messages sent now go in once it is ready.
    var starting: Bool? = nil
    /// Starting it failed (shown in the chat; sends get the same error).
    var startError: String? = nil

    var id: String { machineID + "/" + sessionID }
    /// What lists sort by, newest first: a working agent's updated_at moves on every step, so working ones keep
    /// the time they started working and stay put.
    var sortDate: Date { (state == .working ? stateSince : nil) ?? updatedAt ?? .distantPast }
    var key: SessionKey { SessionKey(machineID: machineID, sessionID: sessionID) }
    var folderName: String { (cwd as NSString).lastPathComponent }

    var hasSource: Bool { !(sourceKind ?? "").isEmpty }
    var canRead: Bool { !hasSource || capabilities?.read == true }
    var canSend: Bool { !hasSource || capabilities?.send == true }
    var canInterrupt: Bool { !hasSource || capabilities?.interrupt == true }
    var canApprove: Bool { !hasSource || capabilities?.approve == true }
    var canTerminal: Bool { !hasSource || capabilities?.terminal == true }
    var canAttach: Bool { hasSource && capabilities?.attach == true && attached != true }
    var sourceLabel: String? {
        switch sourceKind {
        case "codex_app_server": "Codex 实时"
        case "identity_conflict": "身份冲突"
        case "codex_history": "Codex 历史"
        case .some(let kind) where !kind.isEmpty: kind
        default: nil
        }
    }
    var sourceSubtitle: String? {
        guard let sourceLabel else { return nil }
        let detail = sourceKind == "identity_conflict" ? nil : agentName.flatMap { $0.isEmpty ? nil : $0 }
        let label = detail.map { sourceLabel + " · " + $0 } ?? sourceLabel
        return canSend ? label : label + " · 仅查看：" + (readOnlyReason.flatMap { $0.isEmpty ? nil : $0 } ?? "此来源尚未允许发送")
    }

    var isCodex: Bool { agent == "codex" || sourceKind == "codex_app_server" || sourceKind == "codex_history" }
    var isTraex: Bool { agent == "traex" }
    /// Who the user talks to in this session: "Claude", "Codex" or "TRAEX".
    var agentLabel: String { isCodex ? "Codex" : isTraex ? "TRAEX" : "Claude" }
    /// How to pick the conversation up again on the computer.
    var resumeCommand: String { isCodex ? "codex resume" : isTraex ? "traex resume" : "claude --resume" }

    enum CodingKeys: String, CodingKey {
        case title, cwd, status, launcher, model, agent, effort, capabilities, attached
        case sourceKind = "source_kind", sourceThreadID = "source_thread_id", sourceInstanceID = "source_instance_id",
             parentThreadID = "parent_thread_id", readOnlyReason = "read_only_reason"
        case codexRoute = "codex_route", codexRouteNote = "codex_route_note"
        case permissionMode = "permission_mode"
        case machineID = "machine_id", sessionID = "session_id", paneID = "pane_id", tabID = "tab_id",
             workspaceID = "workspace_id", agentName = "agent_name", startedByJarvis = "started_by_jarvis", assistant, archived, state,
             spaceKind = "space_kind", spaceID = "space_id", spaceName = "space_name", spaceIndex = "space_index",
             slotID = "slot_id", slotName = "slot_name", slotIndex = "slot_index", seenAt = "seen_at", stateSince = "state_since", queued, background, loop, subagents, ports,
             updatedAt = "updated_at", lastText = "last_text", pendingInteractions = "pending_interactions", rev, suggestion,
             starting, startError = "start_error"
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        machineID = try c.decodeIfPresent(String.self, forKey: .machineID) ?? ""
        sessionID = try c.decode(String.self, forKey: .sessionID)
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        cwd = try c.decodeIfPresent(String.self, forKey: .cwd) ?? ""
        status = try c.decodeIfPresent(SessionStatus.self, forKey: .status) ?? .unknown
        paneID = try c.decodeIfPresent(String.self, forKey: .paneID)
        tabID = try c.decodeIfPresent(String.self, forKey: .tabID)
        workspaceID = try c.decodeIfPresent(String.self, forKey: .workspaceID)
        agentName = try c.decodeIfPresent(String.self, forKey: .agentName)
        agent = try c.decodeIfPresent(String.self, forKey: .agent)
        sourceKind = try c.decodeIfPresent(String.self, forKey: .sourceKind)
        sourceThreadID = try c.decodeIfPresent(String.self, forKey: .sourceThreadID)
        sourceInstanceID = try c.decodeIfPresent(String.self, forKey: .sourceInstanceID)
        parentThreadID = try c.decodeIfPresent(String.self, forKey: .parentThreadID)
        readOnlyReason = try c.decodeIfPresent(String.self, forKey: .readOnlyReason)
        capabilities = try c.decodeIfPresent(SessionCapabilities.self, forKey: .capabilities)
        attached = try c.decodeIfPresent(Bool.self, forKey: .attached)
        codexRoute = try c.decodeIfPresent(String.self, forKey: .codexRoute)
        codexRouteNote = try c.decodeIfPresent(String.self, forKey: .codexRouteNote)
        launcher = try c.decodeIfPresent(String.self, forKey: .launcher)
        model = try c.decodeIfPresent(String.self, forKey: .model)
        permissionMode = try c.decodeIfPresent(String.self, forKey: .permissionMode)
        effort = try c.decodeIfPresent(String.self, forKey: .effort)
        startedByJarvis = try c.decodeIfPresent(Bool.self, forKey: .startedByJarvis) ?? false
        assistant = try c.decodeIfPresent(Bool.self, forKey: .assistant) ?? false
        archived = try c.decodeIfPresent(Bool.self, forKey: .archived)
        spaceKind = try c.decodeIfPresent(String.self, forKey: .spaceKind)
        spaceID = try c.decodeIfPresent(String.self, forKey: .spaceID)
        spaceName = try c.decodeIfPresent(String.self, forKey: .spaceName)
        spaceIndex = try c.decodeIfPresent(Int.self, forKey: .spaceIndex)
        slotID = try c.decodeIfPresent(String.self, forKey: .slotID)
        slotName = try c.decodeIfPresent(String.self, forKey: .slotName)
        slotIndex = try c.decodeIfPresent(Int.self, forKey: .slotIndex)
        seenAt = try c.decodeIfPresent(Date.self, forKey: .seenAt)
        stateSince = try c.decodeIfPresent(Date.self, forKey: .stateSince)
        queued = try c.decodeIfPresent([String].self, forKey: .queued)
        background = try c.decodeIfPresent([BackgroundTask].self, forKey: .background)
        loop = try c.decodeIfPresent(LoopInfo.self, forKey: .loop)
        subagents = try c.decodeIfPresent([Subagent].self, forKey: .subagents)
        ports = try c.decodeIfPresent([ListeningPort].self, forKey: .ports)
        state = try c.decodeIfPresent(AgentState.self, forKey: .state) ?? AgentState(status: status)
        updatedAt = try c.decodeIfPresent(Date.self, forKey: .updatedAt)
        lastText = try c.decodeIfPresent(String.self, forKey: .lastText)
        pendingInteractions = try c.decodeIfPresent(Int.self, forKey: .pendingInteractions) ?? 0
        rev = try c.decodeIfPresent(Int64.self, forKey: .rev)
        suggestion = try c.decodeIfPresent(String.self, forKey: .suggestion)
        starting = try c.decodeIfPresent(Bool.self, forKey: .starting)
        startError = try c.decodeIfPresent(String.self, forKey: .startError)
    }
}

/// A TCP port an agent's processes listen on (docs/ports.md).
nonisolated struct ListeningPort: Codable, Identifiable, Hashable, Sendable {
    var port: Int
    var addr: String?
    var pid: Int?
    var process: String?
    /// The <title> of its page.
    var title: String?

    var id: Int { port }
}

/// An agent a session launched (the Agent tool), with its own transcript under the session id "<parent>@<id>".
nonisolated struct Subagent: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var sessionID: String
    var type: String?
    var description: String?
    var toolUseID: String?
    var running: Bool
    var background: Bool?
    var startedAt: Date?
    var updatedAt: Date?
    var lastText: String?

    var title: String { (description?.isEmpty == false ? description : nil) ?? type ?? "子 Agent" }

    enum CodingKeys: String, CodingKey {
        case id, type, description, running, background
        case sessionID = "session_id", toolUseID = "tool_use_id", startedAt = "started_at", updatedAt = "updated_at",
             lastText = "last_text"
    }
}

/// A /loop: Claude re-runs a prompt on its own, scheduling each next wakeup (kind "wakeup") or by cron ("cron").
nonisolated struct LoopInfo: Codable, Hashable, Sendable {
    var kind: String
    var prompt: String?
    var reason: String?
    var intervalSeconds: Int?
    var nextAt: Date?
    var cron: String?
    var ticks: Int?

    /// The task, without the "/loop" command word.
    var task: String {
        let p = (prompt ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return p.hasPrefix("/loop") ? String(p.dropFirst(5)).trimmingCharacters(in: .whitespaces) : p
    }

    /// "每 30 分钟" / "按 cron 33 17 * * *".
    var cadence: String? {
        if kind == "cron", let cron { return "按计划 \(cron)" }
        guard let s = intervalSeconds, s > 0 else { return nil }
        if s < 120 { return "每 \(s) 秒" }
        if s < 7200 { return "每 \(s / 60) 分钟" }
        return "每 \(s / 3600) 小时"
    }

    enum CodingKeys: String, CodingKey {
        case kind, prompt, reason, cron, ticks
        case intervalSeconds = "interval_seconds", nextAt = "next_at"
    }
}

/// A Bash command or subagent running in the background (`run_in_background`), until it reports back.
nonisolated struct BackgroundTask: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var kind: String // "shell" | "agent"
    var description: String?
    var command: String?
    var outputFile: String?
    var startedAt: Date?

    var isAgent: Bool { kind == "agent" }
    /// What to call it in one line: the description Claude gave, else the command.
    var title: String {
        if let d = description, !d.isEmpty { return d }
        return command?.components(separatedBy: "\n").first ?? (isAgent ? "子代理" : "后台命令")
    }

    enum CodingKeys: String, CodingKey {
        case id, kind, description, command
        case outputFile = "output_file", startedAt = "started_at"
    }
}

extension Session {
    /// Space this session belongs to, unique across machines (nil when it is not running in a host like herdr).
    var spaceKey: String? { spaceID.map { "\(machineID)/\($0)" } }

    /// Tab label, or "#n" when the label is just herdr's default number.
    var slotLabel: String? {
        guard let name = slotName, !name.isEmpty else { return slotIndex.map { "#\($0)" } }
        if let i = slotIndex, name == String(i) { return "#\(i)" }
        return name
    }

    var displayTitle: String { title.isEmpty || title == "Claude Code" ? folderName : title }

    /// Idle or unclassified: folded away in busy spaces.
    var isQuiet: Bool { state == .idle || state == .unknown }
}

/// Unified agent state (docs/agent-states.md), computed by the hub from herdr status, pending interactions and
/// whether the result has been seen.
nonisolated enum AgentState: String, Codable, Sendable, CaseIterable {
    case needsYou = "needs_you", working, unread, idle, offline, unknown

    init(from decoder: Decoder) throws {
        self = AgentState(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .unknown
    }

    /// Fallback for hubs older than the state field.
    init(status: SessionStatus) {
        switch status {
        case .blocked: self = .needsYou
        case .working: self = .working
        case .done: self = .unread
        case .idle: self = .idle
        case .offline: self = .offline
        case .unknown: self = .unknown
        }
    }

    /// Lower sorts first: what needs attention comes before what is quiet.
    var priority: Int {
        switch self {
        // Finished-unseen comes before working: it is waiting for you, a working agent is not (yet).
        case .needsYou: 0
        case .unread: 1
        case .working: 2
        case .idle: 3
        case .unknown: 4
        case .offline: 5
        }
    }

    var isLive: Bool { self != .offline }
}

nonisolated struct SessionKey: Hashable, Sendable, Codable {
    var machineID: String
    var sessionID: String
}

nonisolated struct ImagePayload: Codable, Hashable, Sendable {
    var mime: String
    /// Base64; empty when `lazy`: history pages leave the bytes out and each image is fetched when it is shown.
    var data: String
    var lazy: Bool? = nil
    var size: Int? = nil
    var width: Int? = nil
    var height: Int? = nil
    /// Where a lazy image lives: "<machine>/<session>/<item id>" (filled in by ChatStore).
    var ref: String? = nil

    init(mime: String, data: String) {
        self.mime = mime
        self.data = data
    }

    /// Tells images apart without comparing megabytes of base64.
    var identity: String { ref ?? (String(data.prefix(64)) + String(data.count)) }

    /// Width / height, when the hub told us (lazy images): lets a placeholder take the image's shape.
    var aspect: CGFloat? {
        guard let width, let height, width > 0, height > 0 else { return nil }
        return CGFloat(width) / CGFloat(height)
    }
}

nonisolated enum ItemKind: String, Codable, Sendable {
    case text, thinking, toolUse = "tool_use", toolResult = "tool_result", notice, image, recap, fold, unknown

    init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = ItemKind(rawValue: raw) ?? .unknown
    }
}

/// A run of the agent's work a page left out (`Item.fold`; docs/protocol.md「折叠的工作」): its counts for the folded
/// work line, and its range for fetching it when the line is opened.
nonisolated struct Fold: Codable, Hashable, Sendable {
    var items: Int
    var tools: Int
    var thoughts: Int
    var until: Date?
    var throughSeq: Int
    var throughID: String

    enum CodingKeys: String, CodingKey {
        case items, tools, thoughts, until
        case throughSeq = "through_seq", throughID = "through_id"
    }
}

/// A background task reporting back (`Item.task` on a notice): a command run in the background, a Monitor's event or
/// end, a subagent. The hub reads it from the notification (decoded); `TaskIndex` finds the call it came from.
nonisolated struct TaskNote: Codable, Hashable, Sendable {
    var kind: String // shell | monitor | agent | task
    var status: String?
    var title: String
    var taskID: String?
    var toolUseID: String?
    var exitCode: Int?
    var outputFile: String?
    /// A Monitor's event, a subagent's result, or the whole command when the title is its first line.
    var detail: String?

    enum CodingKeys: String, CodingKey {
        case kind, status, title, detail
        case taskID = "task_id", toolUseID = "tool_use_id", exitCode = "exit_code", outputFile = "output_file"
    }

    var failed: Bool { status == "failed" || (exitCode ?? 0) != 0 }
}

nonisolated struct Item: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var seq: Int
    var ts: Date?
    var role: String
    var kind: ItemKind
    var text: String?
    var toolUseID: String?
    var toolName: String?
    var toolInput: JSONValue?
    var isError: Bool?
    /// The hub cut `text` for the page (it ends in a "…（已截断，点开看全文，共 N 字节）" line): the whole is this many bytes,
    /// `APIClient.fullItem` fetches it.
    var fullSize: Int?
    var image: ImagePayload?
    /// User input typed while the agent worked and absorbed mid-turn.
    var queued: Bool?
    /// Sent by another Claude session (cross-session messaging), not typed by the user: its name.
    var from: String?
    /// A background task's notice that woke the idle agent: new work after a finished turn.
    var wake: Bool?
    /// kind == .fold: the work this item stands for.
    var fold: Fold?
    /// kind == .notice: a background task reporting back.
    var task: TaskNote?

    enum CodingKeys: String, CodingKey {
        case id, seq, ts, role, kind, text, image, queued, from, wake, fold, task
        case toolUseID = "tool_use_id", toolName = "tool_name", toolInput = "tool_input", isError = "is_error", fullSize = "full_size"
    }

    /// The text without the line the hub adds where it cut it (the app offers the whole text itself).
    var cutText: String {
        guard fullSize != nil, let text, let r = text.range(of: "\n…（已截断", options: .backwards) else { return text ?? "" }
        return String(text[..<r.lowerBound])
    }
}

nonisolated enum InteractionKind: String, Codable, Sendable {
    case question, plan, permission, screen
}

nonisolated struct QuestionOption: Codable, Hashable, Sendable {
    var label: String
    var description: String?

    /// "取最长的那段 (Recommended)" → ("取最长的那段", true): the words to show and whether Claude recommends it.
    /// The answer sent back keeps the original label. Phone and watch draw 「推荐」 instead of the suffix.
    static func display(_ label: String) -> (String, Bool) {
        for suffix in [" (Recommended)", "(Recommended)", "（推荐）", "(推荐)"] where label.hasSuffix(suffix) {
            return (String(label.dropLast(suffix.count)).trimmingCharacters(in: .whitespaces), true)
        }
        return (label, false)
    }
}

nonisolated struct Question: Codable, Hashable, Sendable, Identifiable {
    var question: String
    var header: String?
    var multiSelect: Bool
    var options: [QuestionOption]
    var id: String { question }

    enum CodingKeys: String, CodingKey { case question, header, options; case multiSelect = "multi_select" }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        question = try c.decode(String.self, forKey: .question)
        header = try c.decodeIfPresent(String.self, forKey: .header)
        multiSelect = try c.decodeIfPresent(Bool.self, forKey: .multiSelect) ?? false
        options = try c.decodeIfPresent([QuestionOption].self, forKey: .options) ?? []
    }

    init(question: String, header: String?, multiSelect: Bool, options: [QuestionOption]) {
        self.question = question
        self.header = header
        self.multiSelect = multiSelect
        self.options = options
    }
}

nonisolated struct Interaction: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var machineID: String
    var sessionID: String
    var sessionTitle: String?
    var cwd: String?
    var kind: InteractionKind
    var createdAt: Date?
    var expiresAt: Date?
    var toolName: String?
    var toolUseID: String?
    var questions: [Question]?
    var plan: String?
    var toolInput: JSONValue?
    var summary: String?
    var screen: String?
    /// The screen's dialog as the hub read it (newer hubs): answer with `Resolution.option`.
    var dialog: ScreenDialog?
    /// The terminal asks for a password: typed into a password field, not kept anywhere.
    var secret: Bool?
    /// Who asks when it is not Claude Code: "codex" / "traex" (a card read off a Codex TUI, docs/codex.md「提问和审批」).
    var agent: String?
    /// Why the agent asks for this permission, in its words.
    var reason: String?
    /// What "always allow" allows for this request ("本会话不再问这个命令"); a Codex card without it offers no such button.
    var alwaysLabel: String?

    var sessionKey: SessionKey { SessionKey(machineID: machineID, sessionID: sessionID) }

    /// The dialog to show: the hub's reading, else the app's own (older hubs).
    var screenDialog: ScreenParser.Dialog? { dialog?.parsed ?? screen.flatMap(ScreenParser.dialog) }

    enum CodingKeys: String, CodingKey {
        case id, kind, questions, plan, summary, screen, cwd, dialog, secret, agent, reason
        case machineID = "machine_id", sessionID = "session_id", sessionTitle = "session_title",
             createdAt = "created_at", expiresAt = "expires_at", toolName = "tool_name", toolUseID = "tool_use_id",
             toolInput = "tool_input", alwaysLabel = "always_label"
    }
}

nonisolated struct Resolution: Codable, Sendable {
    var answers: [String: String]?
    var approve: Bool?
    var mode: String?
    var feedback: String?
    var allow: Bool?
    var always: Bool?
    var message: String?
    var keys: [String]?
    var text: String?
    var dismiss: Bool?
    /// Keys before `text` (`keys` go after it): pick "Type something." by its number, then type.
    var preKeys: [String]?
    /// A screen dialog's option (Interaction.dialog): the node works out its keys, after checking the dialog is unchanged.
    var option: String?

    enum CodingKeys: String, CodingKey {
        case answers, approve, mode, feedback, allow, always, message, keys, text, dismiss, option
        case preKeys = "pre_keys"
    }

    static let dismissed = Resolution(dismiss: true)
}

/// One device's push settings, kept by the hub per device (设置 › 通知). The timings come back from the hub's
/// hub.json "notify" so the page can say what "long" means; they are not sent back.
/// 设置 › 翻译 as the hub keeps it with a device's prefs.
nonisolated struct TranslatePrefsBody: Codable, Equatable, Sendable {
    var enabled: Bool
    var target: String
    var mail: Bool
}

/// A desktop app (the Mac app) connected to the hub, like WeChat's 「Mac 微信已登录」 (docs/protocol.md「桌面端」).
nonisolated struct DesktopDevice: Codable, Hashable, Sendable, Identifiable {
    /// Its device token's id (what 退出 revokes); empty when it connected with the pairing token.
    var device: String
    var name: String
    var since: Date?

    var id: String { device.isEmpty ? "name:" + name : device }

    /// The banner's line: 「Mac 上的 Jarvis 已登录」, or how many when there are several.
    static func bannerTitle(_ list: [DesktopDevice]) -> String {
        list.count > 1 ? "\(list.count) 台 Mac 上的 Jarvis 已登录" : "Mac 上的 Jarvis 已登录"
    }
}

/// The body that sets 手机静音 and nothing else (APIClient.setMuteWhileDesktop).
nonisolated struct MuteWhileDesktopPut: Encodable, Sendable {
    var on: Bool
    enum CodingKeys: String, CodingKey { case on = "mute_while_desktop" }
}

nonisolated struct NotifyPrefs: Codable, Equatable, Sendable {
    enum Done: String, Codable, CaseIterable, Sendable { case off, long, always }
    var input: Bool = true
    /// 手机静音 (WeChat's): no pushes to this phone while a desktop app is connected (hub/desktops.go).
    var muteWhileDesktop = false
    var done: Done = .long
    /// The Fleet Live Activity on the lock screen / Dynamic Island (docs/live-activity.md).
    var liveActivity: Bool = true
    /// Agent names on the lock screen activity; off, only counts.
    var lockDetails: Bool = false
    /// The compact Dynamic Island's two sides (FleetState.swift).
    var island = IslandPrefs()
    /// 设置 › 翻译, so the hub translates new mail before it is opened (Translator.swift). Kept as the hub sent it.
    var translate: TranslatePrefsBody?
    var longTurnSeconds: Int?
    var doneDelaySeconds: Int?

    enum CodingKeys: String, CodingKey {
        case input, done
        case liveActivity = "live_activity", lockDetails = "lock_details", island, translate
        case muteWhileDesktop = "mute_while_desktop"
        case longTurnSeconds = "long_turn_seconds", doneDelaySeconds = "done_delay_seconds"
    }

    init(input: Bool = true, done: Done = .long, liveActivity: Bool = true, lockDetails: Bool = false, island: IslandPrefs = IslandPrefs()) {
        self.input = input
        self.done = done
        self.liveActivity = liveActivity
        self.lockDetails = lockDetails
        self.island = island
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        input = try c.decodeIfPresent(Bool.self, forKey: .input) ?? true
        done = (try? c.decodeIfPresent(Done.self, forKey: .done)) ?? .long
        liveActivity = try c.decodeIfPresent(Bool.self, forKey: .liveActivity) ?? true
        lockDetails = try c.decodeIfPresent(Bool.self, forKey: .lockDetails) ?? false
        island = (try? c.decodeIfPresent(IslandPrefs.self, forKey: .island)) ?? IslandPrefs()
        longTurnSeconds = try c.decodeIfPresent(Int.self, forKey: .longTurnSeconds)
        doneDelaySeconds = try c.decodeIfPresent(Int.self, forKey: .doneDelaySeconds)
        muteWhileDesktop = try c.decodeIfPresent(Bool.self, forKey: .muteWhileDesktop) ?? false
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(input, forKey: .input)
        try c.encode(done, forKey: .done)
        try c.encode(liveActivity, forKey: .liveActivity)
        try c.encode(lockDetails, forKey: .lockDetails)
        try c.encode(island, forKey: .island)
        // muteWhileDesktop is not sent back: the banner writes it alone (MuteWhileDesktopPut).
    }

    /// "1 分钟" / "90 秒": how the page words a duration from the hub.
    static func duration(_ seconds: Int?) -> String {
        let s = seconds ?? 60
        if s >= 60, s % 60 == 0 { return "\(s / 60) 分钟" }
        return "\(s) 秒"
    }
}

nonisolated struct ToolSpec: Codable, Sendable {
    var name: String
    var description: String
    var inputSchema: JSONValue

    enum CodingKeys: String, CodingKey { case name, description; case inputSchema = "input_schema" }
}

nonisolated struct ToolContent: Codable, Sendable {
    var type: String
    var text: String?
    var data: String?
    var mimeType: String?

    enum CodingKeys: String, CodingKey { case type, text, data; case mimeType = "mime_type" }

    static func text(_ s: String) -> ToolContent { ToolContent(type: "text", text: s) }
    static func image(_ data: Data, mime: String) -> ToolContent {
        ToolContent(type: "image", data: data.base64EncodedString(), mimeType: mime)
    }
}

// MARK: - Mail

nonisolated struct MailAddress: Codable, Hashable, Sendable {
    var name: String?
    var email: String

    var display: String { (name?.isEmpty == false ? name! : email) }
}

nonisolated struct MailAccount: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String?
    var email: String
    var provider: String?
    var auth: String?
    var username: String?
    var imapHost: String?
    var imapPort: Int?
    var smtpHost: String?
    var smtpPort: Int?
    var smtpSecurity: String?
    var status: String?
    var error: String?

    enum CodingKeys: String, CodingKey {
        case id, name, email, provider, auth, username, status, error
        case imapHost = "imap_host", imapPort = "imap_port", smtpHost = "smtp_host", smtpPort = "smtp_port",
             smtpSecurity = "smtp_security"
    }
}

nonisolated struct MailSummary: Codable, Identifiable, Hashable, Sendable {
    var accountID: String
    var mailbox: String
    var uid: UInt32
    var messageID: String?
    var subject: String?
    var from: MailAddress?
    var to: [MailAddress]?
    var date: Date?
    var snippet: String?
    var seen: Bool
    var flagged: Bool
    var hasAttachments: Bool

    var id: String { "\(accountID)/\(mailbox)/\(uid)" }

    enum CodingKeys: String, CodingKey {
        case mailbox, uid, subject, from, to, date, snippet, seen, flagged
        case accountID = "account_id", messageID = "message_id", hasAttachments = "has_attachments"
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        accountID = try c.decode(String.self, forKey: .accountID)
        mailbox = try c.decodeIfPresent(String.self, forKey: .mailbox) ?? "INBOX"
        uid = try c.decode(UInt32.self, forKey: .uid)
        messageID = try c.decodeIfPresent(String.self, forKey: .messageID)
        subject = try c.decodeIfPresent(String.self, forKey: .subject)
        from = try c.decodeIfPresent(MailAddress.self, forKey: .from)
        to = try c.decodeIfPresent([MailAddress].self, forKey: .to)
        date = try c.decodeIfPresent(Date.self, forKey: .date)
        snippet = try c.decodeIfPresent(String.self, forKey: .snippet)
        seen = try c.decodeIfPresent(Bool.self, forKey: .seen) ?? true
        flagged = try c.decodeIfPresent(Bool.self, forKey: .flagged) ?? false
        hasAttachments = try c.decodeIfPresent(Bool.self, forKey: .hasAttachments) ?? false
    }
}

nonisolated struct MailAttachment: Codable, Hashable, Sendable {
    var filename: String?
    var mime: String?
    var size: Int?
}

nonisolated struct MailMessage: Codable, Hashable, Sendable {
    var accountID: String
    var mailbox: String?
    var uid: UInt32
    var messageID: String?
    var subject: String?
    var from: MailAddress?
    var to: [MailAddress]?
    var cc: [MailAddress]?
    var date: Date?
    var text: String?
    var html: String?
    var attachments: [MailAttachment]?
    var inReplyTo: String?
    var references: [String]?
    var seen: Bool?
    var flagged: Bool?

    enum CodingKeys: String, CodingKey {
        case mailbox, uid, subject, from, to, cc, date, text, html, attachments, references, seen, flagged
        case accountID = "account_id", messageID = "message_id", inReplyTo = "in_reply_to"
    }
}

nonisolated struct Mailbox: Codable, Hashable, Sendable, Identifiable {
    var name: String
    var role: String?
    var unseen: Int?
    var total: Int?
    var id: String { name }
}

/// A terminal dialog read on the hub (server/internal/screen): what the card shows, and `rev`, which the node checks
/// before pressing anything for the answer.
nonisolated struct ScreenDialog: Codable, Hashable, Sendable {
    struct Option: Codable, Hashable, Sendable {
        var key: String
        var label: String
        var detail: String?
        var kind: String?
    }
    var header: String?
    var question: String
    var detail: String?
    var options: [Option]
    var rev: String

    var parsed: ScreenParser.Dialog {
        ScreenParser.Dialog(
            header: header.flatMap { $0.isEmpty ? nil : $0 }, question: question, detail: detail.flatMap { $0.isEmpty ? nil : $0 },
            options: options.map { o in
                let kind: ScreenParser.Dialog.Kind = o.kind == "type_something" ? .typeSomething : o.kind == "chat" ? .chat : .plain
                return .init(key: o.key, label: o.label, detail: o.detail.flatMap { $0.isEmpty ? nil : $0 }, kind: kind)
            })
    }
}

/// An assistant conversation's title in the hub's format 「MMDD | 类型 | 主题」 (docs/assistant.md「对话标题」), split so
/// the list can show the topic first; nil for any other title (the user's own, an older conversation's).
nonisolated struct ConversationTitle: Equatable, Sendable {
    var head: String   // "1101 | 修复 | "
    var topic: String  // "搜索框和顶栏重叠"

    init?(_ title: String) {
        let parts = title.components(separatedBy: " | ")
        guard parts.count >= 3, parts[0].count == 4, parts[0].allSatisfy(\.isASCIIDigit),
              (1...4).contains(parts[1].count) else { return nil }
        let topic = parts[2...].joined(separator: " | ").trimmingCharacters(in: .whitespaces)
        guard !topic.isEmpty else { return nil }
        head = parts[0] + " | " + parts[1] + " | "
        self.topic = topic
    }
}

nonisolated private extension Character {
    var isASCIIDigit: Bool { isASCII && isNumber }
}
