import BeautifulUI
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
        case "shell": String(localized: "后台命令")
        case "monitor": n.status == "event" ? String(localized: "Monitor 事件") : String(localized: "Monitor 结束")
        case "agent": String(localized: "子 Agent")
        default: String(localized: "后台任务")
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
        case "failed": parts.append(n.exitCode.map { String(localized: "✗ 退出码 \($0)") } ?? "✗")
        case "killed", "stopped": parts.append(String(localized: "已停止"))
        case "waiting": parts.append(String(localized: "等待中"))
        default: if let c = n.exitCode, c != 0 { parts.append(String(localized: "✗ 退出码 \(c)")) }
        }
        if n.status != "event", let s = startedAt, let e = at, e > s { parts.append(took(Int(e.timeIntervalSince(s)))) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    /// How long it ran, short (the row keeps room for its description): "40 秒", "5 分", "2 小时 5 分".
    static func took(_ s: Int) -> String {
        if s < 60 { return String(localized: "\(s) 秒") }
        if s < 3600 { return String(localized: "\(s / 60) 分") }
        return s % 3600 / 60 == 0 ? String(localized: "\(s / 3600) 小时") : String(localized: "\(s / 3600) 小时 \(s % 3600 / 60) 分")
    }

    /// The outcome as a status pill: done in green, failed in red, stopped plain, waiting in amber; with how long it ran.
    static func pill(_ n: TaskNote, startedAt: Date?, at: Date?) -> (text: String, tone: BUIPill.Tone)? {
        var ran: String?
        if n.status != "event", let s = startedAt, let e = at, e > s { ran = took(Int(e.timeIntervalSince(s))) }
        func with(_ word: String) -> String { ran.map { word + " · " + $0 } ?? word }
        switch n.status {
        case "completed", "ended": return (with(String(localized: "已完成")), .green)
        case "failed": return (with(n.exitCode.map { String(localized: "退出码 \($0)") } ?? String(localized: "失败")), .red)
        case "killed", "stopped": return (with(String(localized: "已停止")), .neutral)
        case "waiting": return (String(localized: "等待中"), .orange)
        default:
            if let c = n.exitCode, c != 0 { return (with(String(localized: "退出码 \(c)")), .red) }
            return nil
        }
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
            return (events > 0 ? String(localized: "后台 · \(events) 个事件") : String(localized: "后台 · 运行中"), false)
        }
        let state = switch last.status {
        case "failed": String(localized: "失败")
        case "killed", "stopped": String(localized: "已停止")
        case "waiting": String(localized: "等待中")
        case "ended": String(localized: "已结束")
        default: String(localized: "已完成")
        }
        return (events > 0 ? String(localized: "后台 · \(state) · \(events) 个事件") : String(localized: "后台 · \(state)"), last.failed)
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

    @Environment(\.chatPin) private var pin
    private var pinKey: String { "task-" + item.id }
    @State private var unfold = Unfolding()

    /// Its top is its header: opening or closing keeps it there in the chat (`ChatPin`).
    var body: some View { content.chatPinned(pinKey) }

    @ViewBuilder private var content: some View {
        if let n = item.task {
            let call = index.calls[item.id]
            VStack(alignment: .leading, spacing: 0) {
                Group {
                    if AppStyle.current.usesBeautifulUI {
                        let subject = TaskNoteText.subject(n, description: call.flatMap { index.descriptions[$0] })
                        // The tools' row: the task's icon, what it is, its description in a chip, how it went as a pill.
                        ToolChipRow(BeautifulUI.ToolCall(id: item.id, .symbol(TaskNoteText.symbol(n)), label: TaskNoteText.label(n), chip: subject,
                                                         monospacedChip: subject.unicodeScalars.allSatisfy(\.isASCII)),
                                    isOpen: open) {
                            Haptics.tap()
                            toggleInPlace(pin, pinKey, animation: Theme.Motion.state, unfold: $unfold, closing: open) { open.toggle() }
                        } accessory: {
                            if let p = TaskNoteText.pill(n, startedAt: call.flatMap { index.startedAt[$0] }, at: item.ts) {
                                BUIPill(p.text, tone: p.tone)
                            }
                        }
                        .padding(ToolCallRow.chipInsets(grouped: grouped))
                    } else {
                        Button {
                            Haptics.tap()
                            toggleInPlace(pin, pinKey, animation: Theme.Motion.state, unfold: $unfold, closing: open) { open.toggle() }
                        } label: {
                            HStack(spacing: Theme.Space.m) {
                                ZStack {
                                    Circle().fill(tint(n).opacity(0.14)).scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
                                    Image(systemName: TaskNoteText.symbol(n))
                                        .font(Theme.Font.labelStrong).dynamicTypeSize(...DynamicTypeSize.xLarge)
                                        .foregroundStyle(tint(n))
                                }
                                Text(TaskNoteText.label(n)).font(Theme.Font.secondaryStrong).lineLimit(1).fixedSize()
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
                                Image(systemName: Theme.Icon.disclosure)
                                    .font(Theme.Font.labelStrong)
                                    .foregroundStyle(.tertiary)
                                    .rotationEffect(.degrees(open ? 90 : 0))
                            }
                            .padding(.vertical, grouped ? 10 : 7)
                            .padding(.horizontal, pad)
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .accessibilityIdentifier("task-note")
                if open {
                    detail(n, call: call)
                        .padding(.horizontal, pad)
                        .padding(.bottom, Theme.Space.m)
                        .unfolding(unfold)
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
            HStack(spacing: Theme.Space.l) {
                if let f = n.outputFile, !f.isEmpty, let openFile {
                    Button { openFile(f) } label: { Label("查看输出", systemImage: Theme.Icon.document).lineLimit(1) }
                }
                if let call, let jumpToCall {
                    Button { jumpToCall(call) } label: { Label("跳到发起的这一步", systemImage: "arrow.up.forward").lineLimit(1) }
                }
            }
            .font(Theme.Font.captionMedium)
            .buttonStyle(.plain)
            .foregroundStyle(Theme.accent)
        }
    }

    private var pad: CGFloat { grouped ? WorkCardSlice.inset : 10 }
    private func tint(_ n: TaskNote) -> Color { n.failed ? Theme.danger : .secondary }

}
