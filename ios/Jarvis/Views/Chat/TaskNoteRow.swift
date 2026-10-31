import SwiftUI

extension EnvironmentValues {
    /// The chat's background tasks and their calls (`ChatStore.tasks`).
    @Entry var taskIndex = TaskIndex()
    /// Brings the call with this tool_use_id into view (opening the work line it is folded in).
    @Entry var jumpToCall: ((String) -> Void)? = nil
}

/// What a background task's notice says, in words (`Item.task`).
enum TaskNoteText {
    /// "后台命令" / "Monitor 事件" / "Monitor 结束" / "子 Agent".
    static func label(_ n: TaskNote) -> String {
        switch n.kind {
        case "shell": "后台命令"
        case "monitor": n.status == "event" ? "Monitor 事件" : "Monitor 结束"
        case "agent": "子 Agent"
        default: "后台任务"
        }
    }

    /// The call's description when we know the call, else what the notice says (a Monitor event: the event's first line).
    static func subject(_ n: TaskNote, description: String?) -> String {
        if n.kind == "monitor", n.status == "event", let d = n.detail, let line = firstLine(d) { return line }
        return description.flatMap { $0.isEmpty ? nil : $0 } ?? n.title
    }

    /// The outcome at the end of the line: "✓", "✗ 退出码 1", "已停止", with how long it ran when known.
    static func outcome(_ n: TaskNote, startedAt: Date?, at: Date?) -> String? {
        var parts: [String] = []
        switch n.status {
        case "completed", "ended": parts.append("✓")
        case "failed": parts.append(n.exitCode.map { "✗ 退出码 \($0)" } ?? "✗")
        case "killed", "stopped": parts.append("已停止")
        case "waiting": parts.append("等待中")
        default: if let c = n.exitCode, c != 0 { parts.append("✗ 退出码 \(c)") }
        }
        if n.status != "event", let s = startedAt, let e = at, e > s { parts.append(took(Int(e.timeIntervalSince(s)))) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    /// How long it ran, short (the row keeps room for its description): "40 秒", "5 分", "2 小时 5 分".
    static func took(_ s: Int) -> String {
        if s < 60 { return "\(s) 秒" }
        if s < 3600 { return "\(s / 60) 分" }
        return s % 3600 / 60 == 0 ? "\(s / 3600) 小时" : "\(s / 3600) 小时 \(s % 3600 / 60) 分"
    }

    static func symbol(_ n: TaskNote) -> String {
        switch n.kind {
        case "shell": ToolPresentation.symbol("Bash")
        case "monitor": ToolPresentation.symbol("Monitor")
        case "agent": ToolPresentation.symbol("Agent")
        default: "bell"
        }
    }

    /// The badge on the call that started background work: "后台 · 运行中" / "后台 · 已完成" / "后台 · 3 个事件".
    static func badge(_ notes: [TaskNote]) -> (text: String, failed: Bool) {
        let events = notes.filter { $0.status == "event" }.count
        guard let last = notes.last(where: { $0.status != "event" }) else {
            return (events > 0 ? "后台 · \(events) 个事件" : "后台 · 运行中", false)
        }
        let state = switch last.status {
        case "failed": "失败"
        case "killed", "stopped": "已停止"
        case "waiting": "等待中"
        case "ended": "已结束"
        default: "已完成"
        }
        return ("后台 · " + state + (events > 0 ? " · \(events) 个事件" : ""), last.failed)
    }

    static func firstLine(_ s: String) -> String? {
        s.split(separator: "\n", omittingEmptySubsequences: true).lazy.map { $0.trimmingCharacters(in: .whitespaces) }.first { !$0.isEmpty }
    }
}

/// A background task reporting back, as one line in the tools' look: the task's icon, what it is, its description (a
/// Monitor event: the event), how it went. Tap for the whole command / event / result, the output file and the call it
/// came from. It used to be the summary in a centered capsule: a background heredoc filled screens.
struct TaskNoteRow: View {
    @Environment(\.taskIndex) private var index
    @Environment(\.jumpToCall) private var jumpToCall
    @Environment(\.openFile) private var openFile
    var item: Item
    /// A row of an open work line's card (no tile of its own), like `ToolCallRow`.
    var grouped = false
    @State private var open = false

    var body: some View {
        if let n = item.task {
            let call = index.calls[item.id]
            VStack(alignment: .leading, spacing: 0) {
                Button {
                    Haptics.tap()
                    withAnimation(.snappy(duration: 0.25)) { open.toggle() }
                } label: {
                    HStack(spacing: 10) {
                        ZStack {
                            Circle().fill(tint(n).opacity(0.14)).frame(width: 24, height: 24)
                            Image(systemName: TaskNoteText.symbol(n))
                                .font(Theme.Font.label.weight(.semibold)).dynamicTypeSize(...DynamicTypeSize.xLarge)
                                .foregroundStyle(tint(n))
                        }
                        Text(TaskNoteText.label(n)).font(Theme.Font.secondary.weight(.semibold)).lineLimit(1).fixedSize()
                        Text(TaskNoteText.subject(n, description: call.flatMap { index.descriptions[$0] }))
                            .font(Theme.Font.secondary)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.tail)
                        Spacer(minLength: 4)
                        if let o = TaskNoteText.outcome(n, startedAt: call.flatMap { index.startedAt[$0] }, at: item.ts) {
                            Text(o).font(Theme.Font.label.monospacedDigit())
                                .foregroundStyle(n.failed ? AnyShapeStyle(Theme.danger) : AnyShapeStyle(.tertiary))
                                .lineLimit(1).fixedSize()
                        }
                        Image(systemName: "chevron.right")
                            .font(Theme.Font.label.weight(.semibold))
                            .foregroundStyle(.tertiary)
                            .rotationEffect(.degrees(open ? 90 : 0))
                    }
                    .padding(.vertical, grouped ? 10 : 7)
                    .padding(.horizontal, pad)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("task-note")
                if open {
                    detail(n, call: call)
                        .padding(.horizontal, pad)
                        .padding(.bottom, 10)
                        .transition(.opacity.combined(with: .move(edge: .top)))
                }
            }
            .background(grouped ? Color.clear : Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        }
    }

    @ViewBuilder private func detail(_ n: TaskNote, call: String?) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            switch n.kind {
            case "shell": CodeBlock(text: "$ " + (n.detail ?? n.title), maxLines: 12)
            case "agent":
                if let r = n.detail { Text(r).font(Theme.Font.caption).foregroundStyle(.secondary).textSelection(.enabled) }
            default:
                if let d = n.detail { CodeBlock(text: d, maxLines: 12) }
            }
            if n.kind == "monitor", n.status == "event" || n.detail == nil {
                Text(n.title).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            HStack(spacing: 16) {
                if let f = n.outputFile, !f.isEmpty, let openFile {
                    Button { openFile(f) } label: { Label("查看输出", systemImage: "doc.text").lineLimit(1) }
                }
                if let call, let jumpToCall {
                    Button { jumpToCall(call) } label: { Label("跳到发起的这一步", systemImage: "arrow.up.forward").lineLimit(1) }
                }
            }
            .font(Theme.Font.caption.weight(.medium))
            .buttonStyle(.plain)
            .foregroundStyle(Theme.accent)
        }
    }

    private var pad: CGFloat { grouped ? WorkCardSlice.inset : 10 }

    private func tint(_ n: TaskNote) -> Color { n.failed ? Theme.danger : .secondary }
}
