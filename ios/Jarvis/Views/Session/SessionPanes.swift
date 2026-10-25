import SwiftUI

/// The tabs of a dev session page: the conversation, its terminal, the project's git state and its files.
enum SessionPane: String, CaseIterable, Identifiable {
    case chat, terminal, git, files
    var id: String { rawValue }

    var title: String {
        switch self {
        case .chat: "对话"
        case .terminal: "终端"
        case .git: "Git"
        case .files: "文件"
        }
    }

    var symbol: String {
        switch self {
        case .chat: "bubble.left.and.text.bubble.right"
        case .terminal: "terminal"
        case .git: "arrow.triangle.branch"
        case .files: "folder"
        }
    }
}

/// One row of equal segments under the navigation bar; labels never wrap.
struct SessionPaneBar: View {
    @Binding var pane: SessionPane
    var panes: [SessionPane]
    var badges: [SessionPane: String] = [:]
    @Namespace private var ns

    var body: some View {
        HStack(spacing: 4) {
            ForEach(panes) { p in
                Button {
                    guard p != pane else { return }
                    Haptics.tap()
                    withAnimation(.snappy(duration: 0.25)) { pane = p }
                } label: {
                    PaneSegment(pane: p, on: p == pane, badge: badges[p], ns: ns)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(p.title)
                .accessibilityAddTraits(p == pane ? .isSelected : [])
            }
        }
        .padding(3)
        .background(Color.primary.opacity(0.05), in: .capsule)
        .padding(.horizontal, 16)
        .padding(.vertical, 6)
    }
}

private struct PaneSegment: View {
    var pane: SessionPane
    var on: Bool
    var badge: String?
    var ns: Namespace.ID

    var body: some View {
        HStack(spacing: 5) {
            Image(systemName: pane.symbol).font(.system(size: 12, weight: .semibold))
            Text(pane.title).lineLimit(1).minimumScaleFactor(0.8)
            if let badge { badgeView(badge) }
        }
        .font(.footnote.weight(on ? .semibold : .medium))
        .foregroundStyle(on ? Theme.accent : Color.secondary)
        .frame(maxWidth: .infinity)
        .frame(height: 30)
        .background { if on { Capsule().fill(Theme.accent.opacity(0.13)).matchedGeometryEffect(id: "pane", in: ns) } }
        .contentShape(.capsule)
    }

    private func badgeView(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 10, weight: .bold).monospacedDigit())
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .background(on ? Theme.accent.opacity(0.18) : Color.primary.opacity(0.08), in: .capsule)
    }
}

// MARK: - Terminal

/// The session's terminal at any time (not only while Claude works). Shows the latest screen live; "加载更早"
/// reaches back into the pane's scrollback page by page until its very first line, pausing the live refresh
/// while you read history.
struct TerminalPanel: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    @State private var text = ""
    @State private var lines = TerminalPanel.liveLines
    @State private var atTop = false
    @State private var paused = false
    @State private var loading = false
    @State private var loaded = false
    @State private var offline = false
    @State private var error: String?
    @State private var input = ""

    static let liveLines = 200
    static let page = 1000
    static let maxLines = 20000

    var body: some View {
        VStack(spacing: 10) {
            screen
            TerminalKeyBar { keys in Task { await model.keys(key, keys); await refresh() } }
            HStack(spacing: 8) {
                TextField("发送文字到终端", text: $input)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .padding(10)
                    .background(Color.primary.opacity(0.06), in: .rect(cornerRadius: 12, style: .continuous))
                    .onSubmit(sendInput)
                Button(action: sendInput) {
                    Image(systemName: "return").frame(width: 24, height: 24)
                }
                .themedButton(prominent: true)
                .disabled(input.isEmpty)
                .accessibilityLabel("发送并回车")
            }
        }
        .task(id: key) {
            text = ""; lines = Self.liveLines; atTop = false; paused = false; loaded = false
            while !Task.isCancelled {
                if !paused { await refresh() }
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    @ViewBuilder private var screen: some View {
        if offline && text.isEmpty {
            ContentUnavailableView("没有终端", systemImage: "terminal",
                                   description: Text("这个会话没有在 herdr 里运行，恢复会话后就能看到终端。"))
                .frame(maxHeight: .infinity)
        } else if !loaded {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Theme.terminalBackground, in: .rect(cornerRadius: 12, style: .continuous))
        } else {
            VStack(alignment: .leading, spacing: 0) {
                olderControl.padding(.horizontal, 10).padding(.top, 8)
                // Unwrapped and scrollable both ways; follows the last line unless you scrolled up to read.
                MonoTextView(text: MonoText.plain(text, color: UIColor(Theme.terminalText)), followsEnd: true)
            }
            .background(Theme.terminalBackground, in: .rect(cornerRadius: 12, style: .continuous))
            .overlay(alignment: .bottom) {
                if paused {
                    Button {
                        Haptics.tap()
                        lines = Self.liveLines; atTop = false; paused = false
                        Task { await refresh() }
                    } label: {
                        Label("回到最新", systemImage: "arrow.down.to.line").lineLimit(1).font(.footnote.weight(.semibold))
                    }
                    .themedButton()
                    .padding(10)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }
            .overlay(alignment: .top) {
                if let error {
                    Text(error).font(.caption).foregroundStyle(.white).padding(8)
                        .background(Theme.danger.opacity(0.85), in: .capsule).padding(8)
                }
            }
            .animation(.snappy, value: paused)
        }
    }

    @ViewBuilder private var olderControl: some View {
        Group {
            if atTop {
                Text("— 已到最前面 —")
            } else {
                Button {
                    Task { await loadOlder() }
                } label: {
                    HStack(spacing: 6) {
                        if loading { ProgressView().controlSize(.mini).tint(Theme.terminalText) }
                        Text("加载更早的输出").lineLimit(1)
                    }
                }
                .buttonStyle(.plain)
                .disabled(loading)
            }
        }
        .font(.caption.weight(.medium))
        .foregroundStyle(Theme.terminalText.opacity(0.55))
    }

    private func refresh() async {
        guard let api = model.api else { return }
        do {
            let r = try await api.screen(key, lines: lines, recent: true)
            error = nil
            offline = r.status == .offline
            if r.text != text { text = r.text }
            if Self.lineCount(r.text) < lines { atTop = true }
        } catch {
            if !loaded { self.error = error.shown }
        }
        loaded = true
    }

    private func loadOlder() async {
        Haptics.tap()
        paused = true
        loading = true
        lines = min(lines + Self.page, Self.maxLines)
        await refresh()
        if lines >= Self.maxLines { atTop = true }
        loading = false
    }

    private static func lineCount(_ s: String) -> Int { s.reduce(into: 1) { n, c in if c == "\n" { n += 1 } } }

    private func sendInput() {
        let t = input
        guard !t.isEmpty else { return }
        input = ""
        Task { await model.keys(key, ["enter"], text: t); await refresh() }
    }
}

// MARK: - Background tasks

/// Commands and subagents Claude left running in the background ("2 shells still running" in the TUI), shown above
/// the composer even after the turn ended. Tap one to read its output file.
struct BackgroundTasksStrip: View {
    var tasks: [BackgroundTask]
    var openOutput: (BackgroundTask) -> Void
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button {
                Haptics.tap()
                withAnimation(.snappy) { expanded.toggle() }
            } label: {
                HStack(spacing: 8) {
                    Image(systemName: "gearshape.2")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(Theme.busy)
                        .symbolEffect(.rotate, options: .repeat(.continuous))
                    Text("\(tasks.count) 个后台任务").font(.footnote.weight(.semibold)).lineLimit(1)
                    if !expanded, let first = tasks.first {
                        Text(first.title).font(.footnote).foregroundStyle(.secondary).lineLimit(1)
                    }
                    Spacer(minLength: 4)
                    Image(systemName: "chevron.up")
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(.secondary)
                        .rotationEffect(.degrees(expanded ? 0 : 180))
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(tasks.count) 个后台任务，\(expanded ? "收起" : "展开")")
            if expanded {
                ForEach(tasks) { t in row(t) }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .surface(tint: Theme.busy.opacity(0.06), in: .rect(cornerRadius: 20, style: .continuous))
    }

    private func row(_ t: BackgroundTask) -> some View {
        Button { if t.outputFile != nil { openOutput(t) } } label: {
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: t.isAgent ? "person.2" : "terminal")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 18, height: 18)
                VStack(alignment: .leading, spacing: 3) {
                    Text(t.title).font(.subheadline.weight(.medium)).foregroundStyle(Color.primary).lineLimit(1)
                    if let cmd = t.command, !cmd.isEmpty, cmd != t.title {
                        Text(cmd).font(.system(size: 11, design: .monospaced)).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
                Spacer(minLength: 4)
                VStack(alignment: .trailing, spacing: 3) {
                    if let start = t.startedAt {
                        TimelineView(.periodic(from: .now, by: 1)) { ctx in
                            Text(Self.elapsed(from: start, to: ctx.date))
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(.secondary)
                        }
                    }
                    if t.outputFile != nil {
                        Text("看输出").font(.caption2.weight(.semibold)).foregroundStyle(Theme.accent).lineLimit(1)
                    }
                }
            }
            .padding(10)
            .background(Color.primary.opacity(0.05), in: .rect(cornerRadius: 12, style: .continuous))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .contextMenu {
            if let cmd = t.command { Button { UIPasteboard.general.string = cmd } label: { Label("拷贝命令", systemImage: "doc.on.doc") } }
        }
    }

    static func elapsed(from start: Date, to now: Date) -> String {
        let s = max(0, Int(now.timeIntervalSince(start)))
        if s < 60 { return "\(s) 秒" }
        if s < 3600 { return "\(s / 60) 分 \(s % 60) 秒" }
        return "\(s / 3600) 时 \(s % 3600 / 60) 分"
    }
}

// MARK: - Loops

/// The /loop driving this agent: what it repeats, how often, when it wakes next — and a way to stop it.
struct LoopStrip: View {
    var loop: LoopInfo
    var stop: () -> Void
    @State private var expanded = false
    @State private var confirming = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: "arrow.triangle.2.circlepath")
                    .font(.system(size: 13, weight: .bold))
                    .foregroundStyle(Theme.done)
                    .symbolEffect(.rotate, options: .repeat(.periodic(delay: 2)))
                VStack(alignment: .leading, spacing: 1) {
                    Text("循环中" + (loop.cadence.map { " · " + $0 } ?? "")).font(.footnote.weight(.semibold)).lineLimit(1)
                    if let next = loop.nextAt {
                        TimelineView(.periodic(from: .now, by: 30)) { ctx in
                            Text(next > ctx.date ? "下次唤醒 \(next.formatted(date: .omitted, time: .shortened))（\(Self.until(next, now: ctx.date))）"
                                                 : "正在执行这一轮")
                                .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                }
                Spacer(minLength: 4)
                Button { confirming = true } label: {
                    Text("停止循环").font(.footnote.weight(.semibold)).lineLimit(1)
                }
                .themedButton()
                .tint(Theme.danger)
            }
            .contentShape(.rect)
            .onTapGesture { withAnimation(.snappy) { expanded.toggle() } }
            if expanded {
                Text(loop.task).font(.subheadline).foregroundStyle(.secondary).textSelection(.enabled)
                if let r = loop.reason, !r.isEmpty {
                    Label(r, systemImage: "text.bubble").font(.caption).foregroundStyle(.tertiary)
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .surface(tint: Theme.done.opacity(0.06), in: .rect(cornerRadius: 20, style: .continuous))
        .confirmationDialog("停止这个循环？", isPresented: $confirming, titleVisibility: .visible) {
            Button("停止循环", role: .destructive, action: stop)
        } message: {
            Text("会给 Agent 发一条消息，让它不再安排下一次唤醒。正在执行的这一轮会先做完。")
        }
    }
}

extension LoopStrip {
    /// "还有 4 分钟" / "还有 1 小时 5 分".
    static func until(_ date: Date, now: Date) -> String {
        let m = max(1, Int(ceil(date.timeIntervalSince(now) / 60)))
        return m < 60 ? "还有 \(m) 分钟" : "还有 \(m / 60) 小时" + (m % 60 > 0 ? " \(m % 60) 分" : "")
    }
}

/// A wakeup of a /loop (the transcript records each as the "/loop …" prompt again): one quiet line, not a bubble.
struct LoopTickRow: View {
    var item: Item

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "arrow.triangle.2.circlepath").font(.system(size: 10, weight: .bold))
            Text("循环唤醒").fontWeight(.semibold)
            if let ts = item.ts { Text(ts.formatted(date: .omitted, time: .shortened)) }
            Text(String((item.text ?? "").dropFirst(5)).trimmingCharacters(in: .whitespaces)).lineLimit(1).foregroundStyle(.tertiary)
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .padding(.horizontal, 10)
        .padding(.vertical, 5)
        .background(Color.primary.opacity(0.04), in: .capsule)
        .frame(maxWidth: .infinity, alignment: .trailing)
    }
}

