import SwiftUI

// Multi-agent workflows an agent runs in the background (Claude Code's Workflow tool, `WorkflowRun`, #2935): one line
// for the Agents list's rows and task cards, a section of cards in the chat's header, each with its phases, progress and
// agents. The same look as the web's and Android's (the issue's spec).

/// 「后台 workflow「parity-audit」· Verify 2/2 · 169/185 · 14 分钟」 (「等 N 个」 when more run): under a row's last line.
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
        if runs.count > 1 { s += " · 等 \(runs.count - 1) 个" }
        return s
    }
}

/// The chat header's 「后台 workflow」 section, above the subagents: one card per run, running ones first.
struct WorkflowSection: View {
    var runs: [WorkflowRun]
    /// Opens one of a workflow's agents in the chat (its transcript, read as a subagent).
    var open: (WorkflowAgent) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            ForEach(runs) { run in
                WorkflowCard(run: run, open: open)
            }
        }
    }
}

/// One workflow: its name, state and time; the phases with the current one marked; done / started and how many run;
/// its error; and its agents behind 「展开 agent」.
struct WorkflowCard: View {
    var run: WorkflowRun
    var open: (WorkflowAgent) -> Void
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
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
            if let phases = run.phases, phases.count > 1 {
                WorkflowPhases(phases: phases, current: run.phaseIndex ?? 0, finished: run.outcome == .completed)
            } else if let p = run.phase, !p.isEmpty {
                Text(p).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            if let pr = run.progress {
                Text(progressText(pr))
                    .font(Theme.Font.label)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .lineLimit(1)
            }
            if let e = run.error, !e.isEmpty, run.outcome == .failed || run.outcome == .stopped {
                Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).lineLimit(1)
            }
            let agents = run.sortedAgents
            if !agents.isEmpty {
                Button {
                    Haptics.tap()
                    withAnimation(Theme.Motion.reveal) { expanded.toggle() }
                } label: {
                    HStack(spacing: Theme.Space.xs) {
                        Text(expanded ? "收起 agent" : "展开 agent（\(agents.count)）").lineLimit(1)
                        Image(systemName: expanded ? Theme.Icon.collapse : Theme.Icon.expand)
                    }
                    .font(Theme.Font.labelStrong)
                    .foregroundStyle(Theme.accent)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("workflow.agents")
                if expanded {
                    VStack(alignment: .leading, spacing: 0) {
                        ForEach(agents) { a in
                            WorkflowAgentRow(agent: a, open: a.sessionID == nil ? nil : { open(a) })
                            if a.id != agents.last?.id { Divider() }
                        }
                    }
                }
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        // A plain fill as GoalBar's: the folded header fades the open one out, and glass does not fade with it.
        .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(WorkflowLook.spoken(run))
    }

    private func progressText(_ pr: (done: Int, started: Int)) -> String {
        let running = run.runningAgents
        return "\(pr.done)/\(pr.started) 个 agent 完成" + (run.isRunning && running > 0 ? " · \(running) 个在跑" : "")
    }
}

/// The phases in a row: the ones before the current one done, the current one marked, the rest quiet.
struct WorkflowPhases: View {
    var phases: [String]
    /// From 1; 0: not known.
    var current: Int
    var finished: Bool

    var body: some View {
        ScrollView(.horizontal) { row }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("阶段：" + phases.enumerated().map { i, p in p + (i + 1 == current ? "（当前）" : "") }.joined(separator: "、"))
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
        .padding(.vertical, Theme.Space.s)
        .contentShape(.rect)

        if let open {
            Button { Haptics.tap(); open() } label: { content }
                .buttonStyle(.plain)
                .accessibilityLabel("\(agent.title)，\(agent.running ? "在跑" : "已结束")，点按查看")
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
        case .running: "在跑"
        case .completed: "完成"
        case .failed: "失败"
        case .stopped: "已停止"
        case .stalled: "没动静了"
        }
    }

    /// VoiceOver: 「后台 workflow parity-audit，在跑，阶段 Verify 2/2，169/185 个 agent 完成」.
    static func spoken(_ run: WorkflowRun) -> String {
        var s = "后台 workflow \(run.title)，\(word(run))"
        if let p = run.phaseLabel { s += "，阶段 \(p)" }
        if let pr = run.progress { s += "，\(pr.done)/\(pr.started) 个 agent 完成" }
        return s
    }
}
