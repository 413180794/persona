import SwiftUI

// Multi-agent workflows an agent runs in the background (Claude Code's Workflow tool, `WorkflowRun`, #2935): one line
// for the Agents list's rows and task cards, a section in the chat's header: a card per running one with its phases,
// progress and agents, a line per ended one (#4532), the whole a capped share of the page that scrolls inside.

/// "Background workflow “parity-audit” · Verify 2/2 · 169/185 · 14 min" ("and N more" when more run): under a row's last line.
struct WorkflowLine: View {
    var runs: [WorkflowRun]
    var font: Font = Theme.Font.caption

    var body: some View {
        if let run = runs.first {
            HStack(alignment: .firstTextBaseline, spacing: Theme.Space.xs) {
                Image(systemName: Theme.Icon.workflow)
                    .font(font)
                    .foregroundStyle(Theme.ai)
                // Every minute: many rows tick at once.
                TimelineView(.periodic(from: .now, by: 60)) { ctx in
                    Text(text(run, now: ctx.date))
                        .font(font)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            .accessibilityElement(children: .combine)
        }
    }

    private func text(_ run: WorkflowRun, now: Date) -> String {
        var s = run.line
        if let t = run.elapsed(now: now) { s += " · " + WorkflowRun.duration(t, seconds: false) }
        if runs.count > 1 { s += String(localized: " · 等 \(runs.count - 1) 个") }
        return s
    }
}

/// The chat header's 「后台 workflow」 section, above the subagents (#4532): the running ones as cards, the ended ones
/// one line each under them. At most `maxHeight` tall (a share of the page, `Theme.Size.workflowShare`); past that it
/// scrolls inside, so open agents never push the conversation off the screen.
struct WorkflowSection: View {
    var runs: [WorkflowRun]
    /// The most it may take; nil: as tall as it is.
    var maxHeight: CGFloat?
    /// Opens one of a workflow's agents in the chat (its transcript, read as a subagent).
    var open: (WorkflowAgent) -> Void
    @State private var contentHeight: CGFloat = 0

    var body: some View {
        ScrollView(.vertical) {
            content
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { contentHeight = $0 }
        }
        .scrollBounceBehavior(.basedOnSize)
        .frame(height: height)
        .accessibilityIdentifier("workflow.section")
    }

    /// Its content's height up to the cap; nil until measured.
    private var height: CGFloat? {
        guard contentHeight > 0 else { return nil }
        guard let maxHeight, maxHeight > 0 else { return contentHeight }
        return min(contentHeight, maxHeight)
    }

    private var content: some View {
        let live = runs.filter(\.isRunning)
        let ended = runs.filter { !$0.isRunning }
        return VStack(alignment: .leading, spacing: Theme.Space.xs) {
            ForEach(live) { run in
                WorkflowCard(run: run, open: open)
            }
            if !ended.isEmpty {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(ended) { run in
                        WorkflowEndedRow(run: run, open: open)
                        if run.id != ended.last?.id { Divider() }
                    }
                }
                .padding(.horizontal, Theme.Space.m)
                .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            }
        }
    }
}

/// A running workflow: its name, time and state; the phases with the current one marked beside done / started (a tap
/// there opens its agents); its error.
struct WorkflowCard: View {
    var run: WorkflowRun
    var open: (WorkflowAgent) -> Void
    @State private var expanded = false

    var body: some View {
        let agents = run.sortedAgents
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                Image(systemName: Theme.Icon.workflow)
                    .font(Theme.Font.labelHeavy)
                    .foregroundStyle(Theme.ai)
                Text(run.title)
                    .font(Theme.Font.labelStrong)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if run.startedAt != nil {
                    TimelineView(.periodic(from: .now, by: 60)) { ctx in
                        if let t = run.elapsed(now: ctx.date) {
                            Text(WorkflowRun.duration(t, seconds: false))
                                .font(Theme.Font.label).foregroundStyle(.secondary).monospacedDigit().lineLimit(1).fixedSize()
                        }
                    }
                }
                Chip(text: WorkflowLook.word(run), look: .tint(WorkflowLook.tint(run))).fixedSize()
            }
            // One line: where it is and how far (the count opens the agents).
            HStack(alignment: .center, spacing: Theme.Space.s) {
                Group {
                    if let phases = run.phases, phases.count > 1 {
                        WorkflowPhases(phases: phases, current: run.phaseIndex ?? 0, finished: run.outcome == .completed)
                    } else if let p = run.phase, !p.isEmpty {
                        Text(p).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if !agents.isEmpty {
                    Button {
                        Haptics.tap()
                        withAnimation(Theme.Motion.reveal) { expanded.toggle() }
                    } label: {
                        HStack(spacing: Theme.Space.xs) {
                            if let pr = run.progress { Text(progressText(pr)).foregroundStyle(.secondary).monospacedDigit() }
                            Image(systemName: expanded ? Theme.Icon.collapse : Theme.Icon.expand).foregroundStyle(Theme.accent)
                        }
                        .font(Theme.Font.labelStrong)
                        .lineLimit(1)
                        .fixedSize()
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel([run.progress.map(progressText), expanded ? String(localized: "收起 agent") : String(localized: "展开 agent（\(agents.count)）")]
                        .compactMap { $0 }.joined(separator: String(localized: "，", comment: "List separator")))
                    .accessibilityIdentifier("workflow.agents")
                } else if let pr = run.progress {
                    Text(progressText(pr)).font(Theme.Font.labelStrong).foregroundStyle(.secondary).monospacedDigit()
                        .lineLimit(1).fixedSize()
                }
            }
            if let e = run.error, !e.isEmpty, run.outcome == .failed || run.outcome == .stopped {
                Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).lineLimit(1)
            }
            if expanded, !agents.isEmpty { WorkflowAgentList(agents: agents, open: open) }
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        // A plain fill as GoalBar's: the folded header fades the open one out, and glass does not fade with it.
        .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(WorkflowLook.spoken(run))
    }

    /// 「169/185 · 16 个在跑」: done / started, and how many run.
    private func progressText(_ pr: (done: Int, started: Int)) -> String {
        let running = run.runningAgents
        let done = "\(pr.done)/\(pr.started)"
        return run.isRunning && running > 0 ? done + String(localized: " · \(running) 个在跑") : done
    }
}

/// A workflow that ended (completed, stopped, failed, gone quiet): one line, its name, done / started, how long it ran
/// and how it ended, with a stopped or failed one's error under it; a tap opens its agents when it has any.
struct WorkflowEndedRow: View {
    var run: WorkflowRun
    var open: (WorkflowAgent) -> Void
    @State private var expanded = false

    var body: some View {
        let agents = run.sortedAgents
        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
            if agents.isEmpty {
                line(agents: 0)
            } else {
                Button {
                    Haptics.tap()
                    withAnimation(Theme.Motion.reveal) { expanded.toggle() }
                } label: { line(agents: agents.count).contentShape(.rect) }
                .buttonStyle(.plain)
                .accessibilityIdentifier("workflow.ended.agents")
            }
            if let e = run.error, !e.isEmpty, run.outcome == .failed || run.outcome == .stopped {
                Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).lineLimit(1)
            }
            if expanded, !agents.isEmpty { WorkflowAgentList(agents: agents, open: open) }
        }
        .padding(.vertical, Theme.Space.s)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(WorkflowLook.spoken(run))
    }

    private func line(agents: Int) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
            Image(systemName: Theme.Icon.workflow)
                .font(Theme.Font.label)
                .foregroundStyle(WorkflowLook.tint(run))
            Text(run.title)
                .font(Theme.Font.labelMedium)
                .foregroundStyle(.primary)
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let meta { Text(meta).font(Theme.Font.label).foregroundStyle(.secondary).monospacedDigit().lineLimit(1).fixedSize() }
            Text(WorkflowLook.word(run))
                .font(Theme.Font.labelStrong)
                .foregroundStyle(WorkflowLook.tint(run))
                .lineLimit(1)
                .fixedSize()
            if agents > 0 {
                Image(systemName: expanded ? Theme.Icon.collapse : Theme.Icon.expand)
                    .font(Theme.Font.label)
                    .foregroundStyle(.tertiary)
            }
        }
    }

    /// 「3/6 · 14 分钟」.
    private var meta: String? {
        let parts = [run.progress.map { "\($0.done)/\($0.started)" }, run.elapsed().map { WorkflowRun.duration($0, seconds: false) }]
            .compactMap { $0 }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }
}

/// A workflow's agents, running first: a tap on one opens its transcript.
struct WorkflowAgentList: View {
    var agents: [WorkflowAgent]
    var open: (WorkflowAgent) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(agents) { a in
                WorkflowAgentRow(agent: a, open: a.sessionID == nil ? nil : { open(a) })
                if a.id != agents.last?.id { Divider() }
            }
        }
    }
}

/// The phases in a row: the ones before the current one done, the current one marked, the rest quiet.
struct WorkflowPhases: View {
    /// VoiceOver: 「阶段：Plan、Build（当前）、Verify」.
    static func spokenPhases(_ phases: [String], current: Int) -> String {
        let list = phases.enumerated().map { i, p in i + 1 == current ? String(localized: "\(p)（当前）") : p }.joined(separator: "、")
        return String(localized: "阶段：\(list)")
    }
    var phases: [String]
    /// From 1; 0: not known.
    var current: Int
    var finished: Bool

    var body: some View {
        ScrollView(.horizontal) { row }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Self.spokenPhases(phases, current: current))
    }

    private var row: some View {
        HStack(spacing: Theme.Space.xs) {
            ForEach(Array(phases.enumerated()), id: \.offset) { i, p in
                if i > 0 { Image(systemName: Theme.Icon.disclosure).font(Theme.Font.caption).foregroundStyle(.tertiary) }
                let n = i + 1
                let done = finished || (current > 0 && n < current)
                let now = !finished && n == current
                HStack(spacing: Theme.Space.xxs) {
                    if done { Image(systemName: Theme.Icon.done).font(Theme.Font.caption) }
                    Text(p).lineLimit(1)
                }
                .font(now ? Theme.Font.labelStrong : Theme.Font.label)
                .foregroundStyle(now ? AnyShapeStyle(Theme.ai) : done ? AnyShapeStyle(.secondary) : AnyShapeStyle(.tertiary))
                .padding(.horizontal, Theme.Space.s)
                .padding(.vertical, Theme.Space.xxs)
                .background(now ? Theme.ai.opacity(0.14) : Color.clear, in: .capsule)
            }
        }
    }
}

/// An agent of a workflow: running dot, label, phase and its latest line; a tap opens its transcript.
struct WorkflowAgentRow: View {
    var agent: WorkflowAgent
    var open: (() -> Void)?

    var body: some View {
        let content = HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
            Circle()
                .fill(agent.running ? AgentState.working.tone.dot : AgentState.idle.tone.dot)
                .scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                    Text(agent.title).font(Theme.Font.secondaryMedium).foregroundStyle(.primary).lineLimit(1)
                    if let p = agent.phase, !p.isEmpty {
                        Text(p).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                if let t = agent.lastText, !t.isEmpty {
                    Text(t).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if open != nil {
                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.caption).foregroundStyle(.tertiary)
            }
        }
        .padding(.vertical, Theme.Space.sm)
        .contentShape(.rect)

        if let open {
            Button { Haptics.tap(); open() } label: { content }
                .buttonStyle(.plain)
                .accessibilityLabel("\(agent.title)，\(agent.running ? String(localized: "在跑") : String(localized: "已结束"))，点按查看")
        } else {
            content.accessibilityElement(children: .combine)
        }
    }
}

/// One word and tint per workflow outcome.
enum WorkflowLook {
    static func tint(_ run: WorkflowRun) -> Color {
        switch run.outcome {
        case .running: Theme.ai
        case .completed: Theme.positive
        case .failed, .stopped: Theme.danger
        case .stalled: Theme.cautionText
        }
    }

    static func word(_ run: WorkflowRun) -> String {
        switch run.outcome {
        case .running: String(localized: "在跑")
        case .completed: String(localized: "完成")
        case .failed: String(localized: "失败")
        case .stopped: String(localized: "已停止")
        case .stalled: String(localized: "没动静了")
        }
    }

    /// VoiceOver: 「后台 workflow parity-audit，在跑，阶段 Verify 2/2，169/185 个 agent 完成」.
    static func spoken(_ run: WorkflowRun) -> String {
        var s = String(localized: "后台 workflow \(run.title)，\(word(run))")
        if let p = run.phaseLabel { s += String(localized: "，阶段 \(p)") }
        if let pr = run.progress { s += String(localized: "，\(pr.done)/\(pr.started) 个 agent 完成") }
        if let e = run.error, !e.isEmpty, run.outcome == .failed || run.outcome == .stopped { s += "，" + e }
        return s
    }
}
