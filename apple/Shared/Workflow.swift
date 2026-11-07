import Foundation

/// A multi-agent workflow an agent runs in the background (Claude Code's Workflow tool): its turn ends while the
/// workflow's agents work, and the workflow's completion wakes it again. The hub's `workflows` on a session
/// (docs/protocol.md); older hubs send none, and an odd entry never drops the session.
nonisolated struct WorkflowRun: Codable, Identifiable, Hashable, Sendable {
    var taskID: String
    var runID: String?
    var name: String?
    var summary: String?
    /// running / completed / failed / killed / stalled (no end notice and 30 minutes without a move: not running).
    var status: String?
    var running: Bool?
    /// The script's declared phases; `phase` is the one the latest running agent is in, `phaseIndex` from 1 (0: unknown).
    var phases: [String]?
    var phase: String?
    var phaseIndex: Int?
    /// The total is only known once it ends (the script launches as it goes): shown as done / started.
    var agentsStarted: Int?
    var agentsDone: Int?
    var agentsRunning: Int?
    var startedAt: Date?
    var endedAt: Date?
    var updatedAt: Date?
    /// A failed / killed one's first line.
    var error: String?
    /// Its agents in the order they started (at most 60, the newest kept).
    var agents: [WorkflowAgent]?

    var id: String { runID ?? taskID }

    enum CodingKeys: String, CodingKey {
        case name, summary, status, running, phases, phase, error, agents
        case taskID = "task_id", runID = "run_id", phaseIndex = "phase_index", agentsStarted = "agents_started",
             agentsDone = "agents_done", agentsRunning = "agents_running", startedAt = "started_at", endedAt = "ended_at",
             updatedAt = "updated_at"
    }

    init(taskID: String, runID: String? = nil, name: String? = nil, summary: String? = nil, status: String? = "running",
         running: Bool? = true, phases: [String]? = nil, phase: String? = nil, phaseIndex: Int? = nil, agentsStarted: Int? = nil,
         agentsDone: Int? = nil, agentsRunning: Int? = nil, startedAt: Date? = nil, endedAt: Date? = nil, error: String? = nil,
         agents: [WorkflowAgent]? = nil) {
        self.taskID = taskID
        self.runID = runID
        self.name = name
        self.summary = summary
        self.status = status
        self.running = running
        self.phases = phases
        self.phase = phase
        self.phaseIndex = phaseIndex
        self.agentsStarted = agentsStarted
        self.agentsDone = agentsDone
        self.agentsRunning = agentsRunning
        self.startedAt = startedAt
        self.endedAt = endedAt
        self.error = error
        self.agents = agents
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        runID = try? c.decodeIfPresent(String.self, forKey: .runID)
        // One with neither id is not one the app can tell apart: it is left out (`Lossy`).
        taskID = try (try? c.decodeIfPresent(String.self, forKey: .taskID)) ?? runID ?? { throw DecodingError.keyNotFound(CodingKeys.taskID, .init(codingPath: c.codingPath, debugDescription: "no task_id or run_id")) }()
        name = try? c.decodeIfPresent(String.self, forKey: .name)
        summary = try? c.decodeIfPresent(String.self, forKey: .summary)
        status = try? c.decodeIfPresent(String.self, forKey: .status)
        running = try? c.decodeIfPresent(Bool.self, forKey: .running)
        phases = try? c.decodeIfPresent([String].self, forKey: .phases)
        phase = try? c.decodeIfPresent(String.self, forKey: .phase)
        phaseIndex = try? c.decodeIfPresent(Int.self, forKey: .phaseIndex)
        agentsStarted = try? c.decodeIfPresent(Int.self, forKey: .agentsStarted)
        agentsDone = try? c.decodeIfPresent(Int.self, forKey: .agentsDone)
        agentsRunning = try? c.decodeIfPresent(Int.self, forKey: .agentsRunning)
        startedAt = try? c.decodeIfPresent(Date.self, forKey: .startedAt)
        endedAt = try? c.decodeIfPresent(Date.self, forKey: .endedAt)
        updatedAt = try? c.decodeIfPresent(Date.self, forKey: .updatedAt)
        error = try? c.decodeIfPresent(String.self, forKey: .error)
        // One agent the app cannot read leaves the others.
        agents = (try? c.decodeIfPresent([Lossy<WorkflowAgent>].self, forKey: .agents))?.compactMap(\.value)
    }

    /// The hub's `running`; from one without it, status running. An unknown status reads as completed.
    var isRunning: Bool { running ?? (status == "running") }

    enum Outcome: Sendable { case running, completed, failed, stopped, stalled }

    var outcome: Outcome {
        if isRunning { return .running }
        switch status {
        case "failed": return .failed
        case "killed": return .stopped
        case "stalled": return .stalled
        default: return .completed
        }
    }

    /// What to call it: its name, else its summary, else 「Workflow」.
    var title: String {
        if let n = name, !n.isEmpty { return n }
        if let d = summary, !d.isEmpty { return d }
        return "Workflow"
    }

    /// Done / started (the total is not known while it runs); counted from the agents when the hub sends no counts.
    var progress: (done: Int, started: Int)? {
        let listed = agents ?? []
        let done = agentsDone ?? listed.filter { !$0.running }.count
        let started = max(agentsStarted ?? listed.count, done)
        return started > 0 ? (done, started) : nil
    }

    var runningAgents: Int { agentsRunning ?? (agents ?? []).filter(\.running).count }

    /// The phase, and where it sits among the declared ones: 「Verify 2/2」.
    var phaseLabel: String? {
        guard let p = phase, !p.isEmpty else { return nil }
        let n = phases?.count ?? 0
        if let i = phaseIndex, i >= 1, n > 1 { return "\(p) \(i)/\(n)" }
        return p
    }

    /// The agents to list: running first, then the latest started first.
    var sortedAgents: [WorkflowAgent] {
        (agents ?? []).enumerated().sorted { a, b in
            if a.element.running != b.element.running { return a.element.running }
            return a.offset > b.offset
        }.map(\.element)
    }

    /// One line (list rows, task cards): "Background workflow “parity-audit” · Verify 2/2 · 169/185"; the time apart.
    var line: String {
        var parts = [isRunning ? String(localized: "Background workflow “\(title)”") : String(localized: "workflow「\(title)」")]
        if let p = phaseLabel { parts.append(p) }
        if let pr = progress { parts.append("\(pr.done)/\(pr.started)") }
        switch outcome {
        case .running, .completed: break
        case .failed: parts.append(String(localized: "Failed"))
        case .stopped: parts.append(String(localized: "Stopped [Port Chips f00645]"))
        case .stalled: parts.append(String(localized: "Stalled"))
        }
        return parts.joined(separator: " · ")
    }

    /// 「12 秒」「4 分 12 秒」「1 时 3 分」; minutes only when `seconds` is false (rows that tick every minute): 「14 分钟」.
    static func duration(_ t: TimeInterval, seconds: Bool = true) -> String {
        let s = max(0, Int(t))
        if !seconds {
            if s < 60 { return String(localized: "Under 1 min") }
            return s < 3600 ? String(localized: "\(s / 60) min [Task Views 135d12]") : String(localized: "\(s / 3600)h \(s % 3600 / 60)m [Machine Metrics 204e89]")
        }
        if s < 60 { return String(localized: "\(s) s") }
        if s < 3600 { return String(localized: "\(s / 60)m \(s % 60)s") }
        return String(localized: "\(s / 3600)h \(s % 3600 / 60)m [Session Panes c667a6]")
    }

    /// How long it has run (to now while running).
    func elapsed(now: Date = .now) -> TimeInterval? {
        guard let s = startedAt else { return nil }
        return max(0, (endedAt ?? (isRunning ? now : updatedAt ?? now)).timeIntervalSince(s))
    }
}

/// One agent of a workflow, read as a subagent through `sessionID` ("<session>@<agent id>").
nonisolated struct WorkflowAgent: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var sessionID: String?
    var label: String?
    var phase: String?
    var running: Bool
    var startedAt: Date?
    var updatedAt: Date?
    var lastText: String?

    enum CodingKeys: String, CodingKey {
        case id, label, phase, running
        case sessionID = "session_id", startedAt = "started_at", updatedAt = "updated_at", lastText = "last_text"
    }

    init(id: String, sessionID: String? = nil, label: String? = nil, phase: String? = nil, running: Bool = false,
         startedAt: Date? = nil, updatedAt: Date? = nil, lastText: String? = nil) {
        self.id = id
        self.sessionID = sessionID
        self.label = label
        self.phase = phase
        self.running = running
        self.startedAt = startedAt
        self.updatedAt = updatedAt
        self.lastText = lastText
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        sessionID = try? c.decodeIfPresent(String.self, forKey: .sessionID)
        label = try? c.decodeIfPresent(String.self, forKey: .label)
        phase = try? c.decodeIfPresent(String.self, forKey: .phase)
        running = ((try? c.decodeIfPresent(Bool.self, forKey: .running)) ?? nil) ?? false
        startedAt = try? c.decodeIfPresent(Date.self, forKey: .startedAt)
        updatedAt = try? c.decodeIfPresent(Date.self, forKey: .updatedAt)
        lastText = try? c.decodeIfPresent(String.self, forKey: .lastText)
    }

    var title: String { (label?.isEmpty == false ? label : nil) ?? id }
}

/// Decodes to nil instead of failing the array around it.
nonisolated struct Lossy<T: Decodable & Sendable>: Decodable, Sendable {
    var value: T?
    init(from decoder: Decoder) throws { value = try? T(from: decoder) }
}

nonisolated extension Session {
    /// Its workflows still running.
    var runningWorkflows: [WorkflowRun] { (workflows ?? []).filter(\.isRunning) }

    /// The chat header's cards: the running ones, and those that ended in the last hour (what happened to it), running
    /// first; only while one runs or one just ended.
    func shownWorkflows(now: Date = .now) -> [WorkflowRun] {
        (workflows ?? []).filter { w in
            w.isRunning || (w.endedAt ?? w.updatedAt).map { now.timeIntervalSince($0) < 3600 } == true
        }
        .enumerated().sorted { a, b in
            a.element.isRunning != b.element.isRunning ? a.element.isRunning : a.offset < b.offset
        }.map(\.element)
    }

    /// The workflow a single line shows: the running one the hub lists first (the newest).
    var leadWorkflow: WorkflowRun? { runningWorkflows.first }

    /// Working while its turn has ended, because a workflow runs (#2935; the hub counts it as working): 「工作中（后台）」.
    var isBackgroundWorking: Bool { !assistant && state == .working && !runningWorkflows.isEmpty }

    /// Background tasks to count apart from the workflows (the hub also lists a running workflow in `background`, for
    /// older apps): 「后台 N」 is these plus the running workflows.
    var backgroundCount: Int { (background ?? []).filter { !$0.isWorkflow }.count + runningWorkflows.count }
}

extension Session {
    /// The state's word with the background marked: 「工作中（后台）」 (`AgentState.label` is the main actor's). Never
    /// Awaiting Reply: that needs to know whether it is muted (`stateLabel(muted:)`).
    var stateLabel: String { isBackgroundWorking ? String(localized: "Working (Background)") : state.label }
    /// `stateLabel`, and "Awaiting Reply" for a seen one waiting on the user's reply that is not muted (#5096).
    func stateLabel(muted: Bool) -> String { awaitsYourReply(muted: muted) ? StateName.awaitsReply.text : stateLabel }
    /// The colors of `stateLabel(muted:)`: Awaiting Reply in its own tone (`StatusPalette.awaitingReply`), else the state's.
    func stateTone(muted: Bool) -> StatusTone { awaitsYourReply(muted: muted) ? StatusPalette.awaitingReply : state.tone }
}
