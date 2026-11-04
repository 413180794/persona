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
    /// Operating it through Cua Driver; nil while the hub's plugin cua is off or it is not a Mac.
    var computer: MachineComputer? = nil
    /// Its ego lite for the assistant; nil while the hub's plugin ego is off or it is not a Mac.
    var browser: MachineBrowser? = nil
    /// Hub mesh (docs/plan-hub-mesh.md 2.6): the hub that owns the computer in its group, its name, the computer's own
    /// id across hubs, and how the hub reaches it. nil from hubs before the mesh.
    var hubID: String? = nil
    var hubName: String? = nil
    var machineUID: String? = nil
    var route: MachineHubRoute? = nil
    /// The group of paired hubs this machine came from (docs/multi-hub.md); nil for the default group. The app's own,
    /// never sent by a hub.
    var hubGroup: String? = nil
    /// With several groups, the group's name, for lists to say where a machine is (`title`); nil with one group.
    var groupLabel: String? = nil

    enum CodingKeys: String, CodingKey {
        case id, name, kind, icon, online, error, hostname, os, arch, home, system, engine, engines, computer, browser, route
        case sshHost = "ssh_host", herdrVersion = "herdr_version", claudeVersion = "claude_version", lastSeen = "last_seen"
        case nodeMismatch = "node_mismatch", nodeVersion = "node_version", deploying, deployError = "deploy_error"
        case hubID = "hub_id", hubName = "hub_name", machineUID = "machine_uid", hubGroup = "app_hub_group",
             groupLabel = "app_group_label"
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
        computer = try? c.decodeIfPresent(MachineComputer.self, forKey: .computer)
        browser = try? c.decodeIfPresent(MachineBrowser.self, forKey: .browser)
        hubID = try? c.decodeIfPresent(String.self, forKey: .hubID)
        hubName = try? c.decodeIfPresent(String.self, forKey: .hubName)
        machineUID = try? c.decodeIfPresent(String.self, forKey: .machineUID)
        route = try? c.decodeIfPresent(MachineHubRoute.self, forKey: .route)
        hubGroup = try c.decodeIfPresent(String.self, forKey: .hubGroup)
        groupLabel = try? c.decodeIfPresent(String.self, forKey: .groupLabel)
    }
}

/// How the hub the app is connected to reaches a machine (docs/plan-hub-mesh.md 2.6): directly (`local`, `ssh`,
/// `tailcat`, `link`) or through another hub of its group (`peer`, `hops` links away).
nonisolated struct MachineHubRoute: Codable, Hashable, Sendable {
    var via: String
    var hops: Int = 0
    var rttMS: Int? = nil

    enum CodingKeys: String, CodingKey {
        case via, hops
        case rttMS = "rtt_ms"
    }

    init(via: String, hops: Int = 0, rttMS: Int? = nil) {
        self.via = via
        self.hops = hops
        self.rttMS = rttMS
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        via = (try? c.decodeIfPresent(String.self, forKey: .via)) ?? ""
        hops = (try? c.decodeIfPresent(Int.self, forKey: .hops)) ?? 0
        rttMS = try? c.decodeIfPresent(Int.self, forKey: .rttMS)
    }
}

nonisolated extension Machine {
    /// The group it came from ("" = the default group).
    var group: String {
        get { hubGroup ?? "" }
        set { hubGroup = newValue.isEmpty ? nil : newValue }
    }
    /// Which machine across groups: the same machine id can be in two groups.
    var ref: MachineRef { MachineRef(group: group, id: id) }
    /// What lists call it: its name, and with several groups the group's ("MacBook · 公司").
    var title: String { groupLabel.map { "\(name) · \($0)" } ?? name }
}

/// A machine across groups of hubs (docs/multi-hub.md): its group and the id its hub gives it.
nonisolated struct MachineRef: Hashable, Sendable, Codable {
    var group: String
    var id: String

    init(group: String = "", id: String) {
        self.group = group
        self.id = id
    }

    /// The machine id, or "group|machine id" for another group: what pickers and filters remember.
    var path: String { group.isEmpty ? id : group + "|" + id }

    init(path: String) {
        if let bar = path.firstIndex(of: "|") {
            self.init(group: String(path[..<bar]), id: String(path[path.index(after: bar)...]))
        } else {
            self.init(id: path)
        }
    }
}


nonisolated struct MachineEngine: RawRepresentable, Codable, Hashable, CaseIterable, Identifiable, Sendable {
    var rawValue: String
    init(rawValue: String) { self.rawValue = rawValue }
    static let herdr = Self(rawValue: "herdr")
    static let herdrApp = Self(rawValue: "herdr-core")
    static let herdrBoth = Self(rawValue: "herdr-both")
    static let orca = Self(rawValue: "orca")
    static let allCases: [Self] = [.herdr, .herdrApp, .herdrBoth, .orca]
    var id: String { rawValue }
    var name: String {
        switch self {
        case .herdr: "herdr"
        case .herdrApp: "Herdr App"
        case .herdrBoth: "herdr + Herdr App"
        case .orca: "Orca"
        default: rawValue
        }
    }
    var detail: String? {
        switch self {
        case .herdr: "终端里的 herdr"
        case .herdrApp: "桌面 App 里开的终端"
        case .herdrBoth: "终端和桌面 App 的会话都显示"
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

/// A Mac the assistant may operate through Cua Driver (the hub's plugin cua, docs/computer-use.md). `enabled`: the user
/// turned it on for this machine; the rest is what its node found when last asked, and only for an enabled one.
nonisolated struct MachineComputer: Codable, Hashable, Sendable {
    var enabled = false
    var installed = false
    var version: String? = nil
    var daemon = false
    /// granted | missing | unknown
    var accessibility: String? = nil
    var screenRecording: String? = nil
    /// active | locked | away
    var desktop: String? = nil
    var ready = false
    /// What is missing and what to do, in Chinese.
    var note: String? = nil
    /// Installing Cua Driver or asking for its permissions (the page's buttons): the running one, or the last.
    var job: ComputerJob? = nil
    /// macOS's Screen Sharing answers on the Mac: its live screen can be shown (看画面).
    var screenSharing = false

    enum CodingKeys: String, CodingKey {
        case enabled, installed, version, daemon, accessibility, desktop, ready, note, job
        case screenRecording = "screen_recording", screenSharing = "screen_sharing"
    }

    init(enabled: Bool = false, installed: Bool = false, version: String? = nil, daemon: Bool = false, accessibility: String? = nil,
         screenRecording: String? = nil, desktop: String? = nil, ready: Bool = false, note: String? = nil, job: ComputerJob? = nil) {
        self.enabled = enabled
        self.installed = installed
        self.version = version
        self.daemon = daemon
        self.accessibility = accessibility
        self.screenRecording = screenRecording
        self.desktop = desktop
        self.ready = ready
        self.note = note
        self.job = job
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        enabled = try c.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
        installed = try c.decodeIfPresent(Bool.self, forKey: .installed) ?? false
        version = try c.decodeIfPresent(String.self, forKey: .version)
        daemon = try c.decodeIfPresent(Bool.self, forKey: .daemon) ?? false
        accessibility = try c.decodeIfPresent(String.self, forKey: .accessibility)
        screenRecording = try c.decodeIfPresent(String.self, forKey: .screenRecording)
        desktop = try c.decodeIfPresent(String.self, forKey: .desktop)
        ready = try c.decodeIfPresent(Bool.self, forKey: .ready) ?? false
        note = try c.decodeIfPresent(String.self, forKey: .note)
        job = try? c.decodeIfPresent(ComputerJob.self, forKey: .job)
        screenSharing = try c.decodeIfPresent(Bool.self, forKey: .screenSharing) ?? false
    }

    /// The node has said how it is (a never-asked one has neither a verdict nor a note).
    var checked: Bool { ready || !(note ?? "").isEmpty }

    /// The macOS permissions it still lacks, by their names in System Settings.
    var missingPermissions: [String] {
        [(accessibility, "辅助功能"), (screenRecording, "屏幕录制")].compactMap { $0.0 == "missing" ? $0.1 : nil }
    }
}

/// A Mac's ego lite, the browser the assistant drives (the hub's plugin ego, docs/plan-ego-browser.md). `enabled`: the
/// user turned it on for this machine; the rest is what its node found when last asked.
nonisolated struct MachineBrowser: Codable, Hashable, Sendable {
    var enabled = false
    var installed = false
    var version: String? = nil
    /// Its first-run setup is done; `imported`, that setup took another browser's data.
    var setUp = false
    var imported = false
    /// Chromium's usage statistics: on | off | unknown.
    var analytics: String? = nil
    var ready = false
    var note: String? = nil
    var screenSharing = false
    var job: ComputerJob? = nil

    enum CodingKeys: String, CodingKey {
        case enabled, installed, version, imported, analytics, ready, note, job
        case setUp = "set_up", screenSharing = "screen_sharing"
    }

    init(enabled: Bool = false, installed: Bool = false, version: String? = nil, setUp: Bool = false, imported: Bool = false,
         analytics: String? = nil, ready: Bool = false, note: String? = nil, screenSharing: Bool = false, job: ComputerJob? = nil) {
        self.enabled = enabled
        self.installed = installed
        self.version = version
        self.setUp = setUp
        self.imported = imported
        self.analytics = analytics
        self.ready = ready
        self.note = note
        self.screenSharing = screenSharing
        self.job = job
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        enabled = try c.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
        installed = try c.decodeIfPresent(Bool.self, forKey: .installed) ?? false
        version = try c.decodeIfPresent(String.self, forKey: .version)
        setUp = try c.decodeIfPresent(Bool.self, forKey: .setUp) ?? false
        imported = try c.decodeIfPresent(Bool.self, forKey: .imported) ?? false
        analytics = try c.decodeIfPresent(String.self, forKey: .analytics)
        ready = try c.decodeIfPresent(Bool.self, forKey: .ready) ?? false
        note = try c.decodeIfPresent(String.self, forKey: .note)
        screenSharing = try c.decodeIfPresent(Bool.self, forKey: .screenSharing) ?? false
        job = try? c.decodeIfPresent(ComputerJob.self, forKey: .job)
    }

    /// The node has said how it is.
    var checked: Bool { ready || installed || !(note ?? "").isEmpty }
}

/// One install or grant the node runs for the machine page (docs/computer-use.md「一键安装」).
/// An app the user always allowed the assistant to operate on a Mac (docs/computer-use.md「锁屏、防睡眠、按 App 批准」c).
nonisolated struct ComputerApp: Codable, Hashable, Sendable, Identifiable {
    var bundleID: String
    var name: String
    var id: String { bundleID }

    enum CodingKeys: String, CodingKey {
        case bundleID = "bundle_id", name
    }
}

nonisolated struct ComputerJob: Codable, Hashable, Sendable {
    /// install | grant
    var kind: String
    /// running | done | failed | cancelled
    var state: String
    var step: String? = nil
    var error: String? = nil

    var running: Bool { state == "running" }
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
    /// An agent runtime's launcher that can run the Persona assistant (DSH's, docs/runtimes.md「助手」).
    var assistant: Bool? = nil
    var id: String { name }
    /// A Codex start sends its first message with it and waits for the thread Codex makes.
    var isCodex: Bool { agent == "codex" }

    enum CodingKeys: String, CodingKey {
        case name, model, agent, assistant
        case baseURL = "base_url"
    }
}

extension Launcher {
    /// Runs Claude Code or Codex, or is an agent runtime's launcher that says it can: what the Persona assistant can
    /// use (headless `claude -p`, `codex app-server`, the runtime). Other agents (TRAEX…) are left out; the node
    /// refuses them with a reason too.
    var runsAssistant: Bool {
        assistant == true || (baseURL != "(traex)" && (agent == nil || agent == "" || agent == "claude" || agent == "codex"))
    }
}

/// What a launcher's agent offers in its own /model menu (`GET /api/v1/machines/{id}/launchers/{name}/models`,
/// docs/model-config.md「新建 Agent 选模型」): Claude Code's aliases and its gateway's models, or Codex's model/list.
nonisolated struct LauncherModels: Codable, Hashable, Sendable {
    /// What the launcher runs: claude, codex, traex, a runtime's.
    var agent: String
    /// Empty: its agent has no list to ask for.
    var models: [AgentModel]
    /// Why a picked model would not reach the agent (the launcher passes its own --model); nil: it would.
    var locked: String?
    /// 模型配置's list (#943): the machine whose CLI answered.
    var machine: String? = nil
}

nonisolated struct AgentModel: Codable, Hashable, Sendable, Identifiable {
    /// What a start sends as `model` (Claude's aliases too: opus, sonnet…).
    var id: String
    var name: String
    var description: String?
    /// The model an alias stands for with this launcher's settings (opus → its model id).
    var resolved: String?
    /// What the agent runs when no model is given: picking it is picking none.
    var isDefault: Bool = false
    /// The thinking efforts it takes, weakest first; nil or empty: none.
    var efforts: [String]?
    var defaultEffort: String?

    enum CodingKeys: String, CodingKey {
        case id, name, description, resolved, efforts
        case isDefault = "default", defaultEffort = "default_effort"
    }

    init(id: String, name: String, description: String? = nil, resolved: String? = nil, isDefault: Bool = false,
         efforts: [String]? = nil, defaultEffort: String? = nil) {
        self.id = id
        self.name = name
        self.description = description
        self.resolved = resolved
        self.isDefault = isDefault
        self.efforts = efforts
        self.defaultEffort = defaultEffort
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? id
        description = try c.decodeIfPresent(String.self, forKey: .description)
        resolved = try c.decodeIfPresent(String.self, forKey: .resolved)
        isDefault = try c.decodeIfPresent(Bool.self, forKey: .isDefault) ?? false
        efforts = try c.decodeIfPresent([String].self, forKey: .efforts)
        defaultEffort = try c.decodeIfPresent(String.self, forKey: .defaultEffort)
    }
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

nonisolated struct SessionTodoRef: Codable, Hashable, Sendable {
    var id: String
    var title: String
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
    /// The coding agent in the pane: "claude" (or nil, from older hubs), "codex" or "traex" (TRAEX, an in-house Codex fork);
    /// for a session a runtime lists (DSH…, docs/runtimes.md), the agent it runs.
    var agent: String? = nil
    var sourceKind: String? = nil
    var sourceThreadID: String? = nil
    var sourceInstanceID: String? = nil
    var parentThreadID: String? = nil
    var readOnlyReason: String? = nil
    var capabilities: SessionCapabilities? = nil
    var attached: Bool? = nil
    /// How Persona reaches a Codex in a terminal (docs/codex.md「守护进程上的会话」): "app_server" (joined on its shared
    /// app-server) or "terminal" (its screen and session record); the note says why it is the terminal.
    var codexRoute: String? = nil
    var codexRouteNote: String? = nil
    /// What the hub says to call an agent this app does not know (a runtime's own name, "DSH"); nil for the known ones.
    var agentLabelHint: String? = nil
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
    /// What it is doing when its latest step is a tool call no words came after (「在跑测试」「在读 tasks.go」), and since
    /// when; nil once it writes again. The hub's `activity` / `activity_at` (docs/protocol.md); older hubs send neither.
    var activity: String? = nil
    var activityAt: Date? = nil
    var pendingInteractions: Int
    /// Changes whenever the transcript does: the app pulls sessions whose rev it has not synced (`AgentSync`).
    var rev: Int64?
    /// The next message Claude Code proposes after a turn (grey in its input box on the computer), until it works again.
    var suggestion: String?
    /// Persona is still opening its tab and starting claude: messages sent now go in once it is ready.
    var starting: Bool? = nil
    /// Starting it failed (shown in the chat; sends get the same error).
    var startError: String? = nil
    /// The todo this session is linked to, filled by the hub (docs/protocol.md).
    var todo: SessionTodoRef? = nil
    /// Why it waits for the user, in its host's words (a botmux attention's reason); only while it needs you.
    var attentionReason: String? = nil
    /// Where it lives outside Persona (a botmux session's Feishu topic): 在飞书打开.
    var externalURL: String? = nil
    /// The channels messages came into it from, newest first ("wechat", "feishu"; docs/channels.md「来源标记」): the
    /// sidebar draws the latest one's mark. Nil from hubs without channels.
    var channels: [String]? = nil
    /// The group of paired hubs it came from (docs/multi-hub.md); nil for the default group. The app's own.
    var hubGroup: String? = nil

    /// Unique across groups; for the default group exactly what it always was.
    var id: String { (hubGroup.map { $0 + "|" } ?? "") + machineID + "/" + sessionID }
    /// What lists sort by, newest first: a working agent's updated_at moves on every step, so working ones keep
    /// the time they started working and stay put.
    var sortDate: Date { (state == .working ? stateSince : nil) ?? updatedAt ?? .distantPast }
    var key: SessionKey { SessionKey(group: group, machineID: machineID, sessionID: sessionID) }
    var machineRef: MachineRef { MachineRef(group: group, id: machineID) }
    /// The group it came from ("" = the default group).
    var group: String {
        get { hubGroup ?? "" }
        set { hubGroup = newValue.isEmpty ? nil : newValue }
    }
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
        case "runtime": agentLabel // an agent runtime's session (docs/runtimes.md): its own name, "DSH"
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
    /// Claude Code (or an older hub that does not name the agent): what the Claude-only controls (modes, /clear,
    /// /model) are for. Any other agent — named or not yet known to this app — does not get them.
    var isClaude: Bool { !isCodex && (agent == nil || agent == "" || agent == "claude") }
    /// Who the user talks to in this session: "Claude", "Codex", "TRAEX", or what the hub calls a runtime's agent ("DSH").
    var agentLabel: String {
        if isCodex { return "Codex" }
        if isTraex { return "TRAEX" }
        if isClaude { return "Claude" }
        return agentLabelHint ?? agent ?? "Claude"
    }
    /// How to pick the conversation up again on the computer (offered only with a terminal, canTerminal: a runtime's
    /// sessions are picked up in the runtime's own UI).
    var resumeCommand: String { isCodex ? "codex resume" : isTraex ? "traex resume" : "claude --resume" }

    enum CodingKeys: String, CodingKey {
        case title, cwd, status, launcher, model, agent, effort, capabilities, attached
        case sourceKind = "source_kind", sourceThreadID = "source_thread_id", sourceInstanceID = "source_instance_id",
             parentThreadID = "parent_thread_id", readOnlyReason = "read_only_reason"
        case codexRoute = "codex_route", codexRouteNote = "codex_route_note"
        case agentLabelHint = "agent_label"
        case permissionMode = "permission_mode"
        case machineID = "machine_id", sessionID = "session_id", paneID = "pane_id", tabID = "tab_id",
             workspaceID = "workspace_id", agentName = "agent_name", startedByJarvis = "started_by_jarvis", assistant, archived, state,
             spaceKind = "space_kind", spaceID = "space_id", spaceName = "space_name", spaceIndex = "space_index",
             slotID = "slot_id", slotName = "slot_name", slotIndex = "slot_index", seenAt = "seen_at", stateSince = "state_since", queued, background, loop, subagents, ports,
             updatedAt = "updated_at", lastText = "last_text", activity, activityAt = "activity_at", pendingInteractions = "pending_interactions", rev, suggestion,
             starting, startError = "start_error", todo, hubGroup = "app_hub_group",
             attentionReason = "attention_reason", externalURL = "external_url", channels
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
        agentLabelHint = try c.decodeIfPresent(String.self, forKey: .agentLabelHint)
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
        activity = try c.decodeIfPresent(String.self, forKey: .activity)
        activityAt = try c.decodeIfPresent(Date.self, forKey: .activityAt)
        pendingInteractions = try c.decodeIfPresent(Int.self, forKey: .pendingInteractions) ?? 0
        rev = try c.decodeIfPresent(Int64.self, forKey: .rev)
        suggestion = try c.decodeIfPresent(String.self, forKey: .suggestion)
        starting = try c.decodeIfPresent(Bool.self, forKey: .starting)
        startError = try c.decodeIfPresent(String.self, forKey: .startError)
        todo = try c.decodeIfPresent(SessionTodoRef.self, forKey: .todo)
        attentionReason = try c.decodeIfPresent(String.self, forKey: .attentionReason)
        externalURL = try c.decodeIfPresent(String.self, forKey: .externalURL)
        channels = try c.decodeIfPresent([String].self, forKey: .channels)
        hubGroup = try c.decodeIfPresent(String.self, forKey: .hubGroup)
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

nonisolated extension Session {
    /// The same apart from what ticks on every step of a working agent: `rev` (the transcript grew) and `updatedAt`.
    /// Views that show neither take a push of only those as no change (docs/ios-state.md「第 2 步」).
    static func sameApartFromTicks(_ a: Session?, _ b: Session?) -> Bool {
        guard var a, var b else { return a == nil && b == nil }
        a.rev = nil
        b.rev = nil
        a.updatedAt = nil
        b.updatedAt = nil
        return a == b
    }

    /// The same as far as views about this one session show it (`AppModel.session(_:)`: its chat, its top bar, its
    /// menus): apart from the ticks and the last line and step, which only the lists and the task cards show
    /// (docs/ios-state.md「第 3 步」).
    static func sameForItsPage(_ a: Session?, _ b: Session?) -> Bool {
        sameApartFromTicks(a.map(\.withoutLine), b.map(\.withoutLine))
    }

    private var withoutLine: Session {
        var x = self
        x.lastText = nil
        x.activity = nil
        x.activityAt = nil
        return x
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
    /// The line a list row shows under the title: why it waits for you when its host says so, else the last words.
    var listPreview: String? {
        if state == .needsYou, let r = attentionReason, !r.isEmpty { return r } // a stale reason (offline, seen) stays out
        return lastText.flatMap { $0.isEmpty ? nil : Fmt.plain($0) }
    }
    /// 在飞书打开: the session's place outside Persona, a web link only.
    var externalLink: URL? {
        guard let s = externalURL, let u = URL(string: s), u.scheme?.lowercased() == "https" else { return nil }
        return u
    }

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

/// A session across groups of hubs (docs/multi-hub.md): group, machine id, session id. The default group ("") has no
/// group in its JSON or its string forms, so everything keyed by its sessions stays byte for byte what it was.
nonisolated struct SessionKey: Hashable, Sendable, Codable {
    var machineID: String
    var sessionID: String
    /// nil for the default group.
    var hubGroup: String?

    init(group: String = "", machineID: String, sessionID: String) {
        self.machineID = machineID
        self.sessionID = sessionID
        hubGroup = group.isEmpty ? nil : group
    }

    var group: String {
        get { hubGroup ?? "" }
        set { hubGroup = newValue.isEmpty ? nil : newValue }
    }
    var machineRef: MachineRef { MachineRef(group: group, id: machineID) }

    /// "machine/session", or "group|machine/session" for another group: what launch arguments, remembered chats and
    /// deep links write.
    var path: String { (hubGroup.map { $0 + "|" } ?? "") + machineID + "/" + sessionID }

    /// Reads `path` back (also the old "machine/session").
    init?(path: String) {
        var rest = Substring(path)
        var group = ""
        if let bar = rest.firstIndex(of: "|"), rest[..<bar].firstIndex(of: "/") == nil {
            group = String(rest[..<bar])
            rest = rest[rest.index(after: bar)...]
        }
        guard let slash = rest.firstIndex(of: "/") else { return nil }
        let machine = String(rest[..<slash]), session = String(rest[rest.index(after: slash)...])
        guard !machine.isEmpty, !session.isEmpty else { return nil }
        self.init(group: group, machineID: machine, sessionID: session)
    }
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
    /// The images the run's calls looked at (docs/protocol.md「这一轮看过的图片」): its turn shows them before the answer
    /// while the run stays folded.
    var images: [FoldImage]? = nil

    enum CodingKeys: String, CodingKey {
        case items, tools, thoughts, until, images
        case throughSeq = "through_seq", throughID = "through_id"
    }
}

/// An image a folded run's call looked at (`Fold.images`): one its result carried (`itemID`), or a picture file Read
/// opened whose result carried none (only `path`).
nonisolated struct FoldImage: Codable, Hashable, Sendable {
    var itemID: String?
    var toolUseID: String
    var tool: String
    /// read, screenshot, browser, mcp or tool.
    var source: String
    var path: String?
    /// The machine a screenshot's or a browser's call named (an id or a name).
    var machine: String?
    var mime: String?
    var size: Int?
    var width: Int?
    var height: Int?
    /// Where its bytes are, like `ImagePayload.ref`: "<machine>/<session>/<item id>" (filled in by ChatStore).
    var ref: String? = nil

    enum CodingKeys: String, CodingKey {
        case tool, source, path, machine, mime, size, width, height, ref
        case itemID = "item_id", toolUseID = "tool_use_id"
    }
}

/// Where Claude Code compacted the context (`Item.compact`): from the transcript's compact_boundary line (`trigger`:
/// manual for /compact, auto otherwise; idle when the app saw the note of an idle compaction after it), and the notes
/// that came with it, shown when the divider is tapped.
nonisolated struct Compaction: Codable, Hashable, Sendable {
    var trigger: String?
    var preTokens: Int?
    /// The notices folded into the divider (the app's, never sent): 「上下文已压缩」「已从压缩的上下文继续」, the idle note.
    var notes: [String]?

    enum CodingKeys: String, CodingKey {
        case trigger, notes
        case preTokens = "pre_tokens"
    }

    /// 手动 / 自动 / 空闲时, for the divider; nil when the hub did not say.
    var source: String? {
        switch trigger {
        case "manual": "手动"
        case "auto": "自动"
        case "idle": "空闲时"
        default: nil
        }
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
    /// The id the app sent the message this item came from with (`PendingMessage.id`): on every item of it — its text,
    /// its images, a queued copy, or the notice it became (`/compact`). Nil from older hubs, or when the node could not
    /// tie it to one.
    var clientMsgID: String?
    /// kind == .notice: the context was compacted here (the hub's `compact`, or recognized by the app,
    /// `ChatStore.compactions`). Drawn as a divider across the chat (`CompactDivider`).
    var compact: Compaction?

    enum CodingKeys: String, CodingKey {
        case id, seq, ts, role, kind, text, image, queued, from, wake, fold, task, compact
        case toolUseID = "tool_use_id", toolName = "tool_name", toolInput = "tool_input", isError = "is_error", fullSize = "full_size",
             clientMsgID = "client_msg_id"
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
    /// The task (docs/unified-entry.md) the asking agent works for: filled in by the phone for the watch, not sent by
    /// the hub.
    var taskTitle: String?
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
    var fields: [InteractionField]?
    var requiresInput: Bool { !(fields ?? []).isEmpty }
    /// Who asks when it is not Claude Code: "codex" / "traex" (a card read off a Codex TUI, docs/codex.md「提问和审批」).
    var agent: String?
    /// Why the agent asks for this permission, in its words.
    var reason: String?
    /// What "always allow" allows for this request ("本会话不再问这个命令"); a Codex card without it offers no such button.
    var alwaysLabel: String?
    /// What the hub calls an agent this app does not know (a runtime's own name, "DSH"; docs/runtimes.md).
    var agentLabelHint: String?
    /// The group of paired hubs it came from (docs/multi-hub.md); nil for the default group. The app's own.
    var hubGroup: String?

    var sessionKey: SessionKey { SessionKey(group: group, machineID: machineID, sessionID: sessionID) }
    var machineRef: MachineRef { MachineRef(group: group, id: machineID) }

    var group: String {
        get { hubGroup ?? "" }
        set { hubGroup = newValue.isEmpty ? nil : newValue }
    }
    /// Unique across groups (`id` is the hub's, what resolve takes); for the default group just `id`.
    var uid: String { (hubGroup.map { $0 + "|" } ?? "") + id }

    /// The dialog to show: the hub's reading, else the app's own (older hubs).
    var screenDialog: ScreenParser.Dialog? { dialog?.parsed ?? screen.flatMap(ScreenParser.dialog) }

    enum CodingKeys: String, CodingKey {
        case id, kind, questions, plan, summary, screen, cwd, dialog, secret, fields, agent, reason
        case machineID = "machine_id", sessionID = "session_id", sessionTitle = "session_title", taskTitle = "task_title",
             createdAt = "created_at", expiresAt = "expires_at", toolName = "tool_name", toolUseID = "tool_use_id",
             toolInput = "tool_input", alwaysLabel = "always_label", agentLabelHint = "agent_label", hubGroup = "app_hub_group"
    }
}

nonisolated struct InteractionField: Codable, Hashable, Sendable, Identifiable {
    var key: String
    var label: String
    var placeholder: String?
    var secret: Bool?
    var optional: Bool?
    var id: String { key }
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
    /// A multi-select screen dialog: the options to have checked when it is sent (`text`: its free-text field).
    var options: [String]?
    var values: [String: String]?

    enum CodingKeys: String, CodingKey {
        case answers, approve, mode, feedback, allow, always, message, keys, text, dismiss, option, options, values
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

    /// The banner's line: 「Mac 上的 Persona 已登录」, or how many when there are several.
    static func bannerTitle(_ list: [DesktopDevice]) -> String {
        list.count > 1 ? "\(list.count) 台 Mac 上的 Persona 已登录" : "Mac 上的 Persona 已登录"
    }
}

/// The body that sets 手机静音 and nothing else (APIClient.setMuteWhileDesktop).
nonisolated struct MuteWhileDesktopPut: Encodable, Sendable {
    var on: Bool
    enum CodingKeys: String, CodingKey { case on = "mute_while_desktop" }
}

/// The Live Activity's persona alone, for the prefs PUT (it merges): FleetState.look.
nonisolated struct FleetLookPut: Encodable, Sendable {
    var look: FleetLook
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
        /// Ticked at the computer (a multi-select box), or an answered question's pick.
        var checked: Bool?
        /// What is typed into the free-text field (`type_something`).
        var text: String?
    }
    struct Step: Codable, Hashable, Sendable {
        var label: String
        var done: Bool
    }
    var header: String?
    var question: String
    var detail: String?
    var options: [Option]
    var rev: String
    /// A multi-select question: check boxes, sent together by its `submit` row ("Submit", or "Next" when another
    /// question follows). Steps are the questions of its tab bar; review is the tab that submits them all
    /// (incomplete: one has no answer).
    var multi: Bool?
    var submit: String?
    var steps: [Step]?
    var review: Bool?
    var incomplete: Bool?

    var parsed: ScreenParser.Dialog {
        var d = ScreenParser.Dialog(
            header: header.flatMap { $0.isEmpty ? nil : $0 }, question: question, detail: detail.flatMap { $0.isEmpty ? nil : $0 },
            options: options.map { o in
                let kind: ScreenParser.Dialog.Kind = o.kind == "type_something" ? .typeSomething : o.kind == "chat" ? .chat : .plain
                return .init(key: o.key, label: o.label, detail: o.detail.flatMap { $0.isEmpty ? nil : $0 }, kind: kind,
                             checked: o.checked ?? false, text: o.text.flatMap { $0.isEmpty ? nil : $0 })
            })
        if let steps, !steps.isEmpty { d.steps = steps.map { .init(label: $0.label, done: $0.done) } }
        d.multi = multi ?? false
        d.submit = submit
        d.review = review ?? false
        d.incomplete = incomplete ?? false
        return d
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
