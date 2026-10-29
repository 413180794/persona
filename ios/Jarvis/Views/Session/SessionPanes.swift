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

/// The session's terminal, to look at (2026-10-29: no keys, no typing, no takeover — the computer keeps its size):
/// the hub streams herdr's frames of the pane (docs/terminal.md) — colors, cursor, wide characters — or, where it
/// cannot, the colored screen is read once a second. Drawn like a native terminal: edge to edge on the theme's
/// background, no card or toolbar. Long rows fold at the phone's width unless 设置 › 终端 turns 换行 off. Scrolling up
/// keeps loading the 200 rows before, page after page without a button, back to the start of what herdr keeps (about
/// 1000 lines — older output lives in the chat); output arriving meanwhile shifts nothing. Reading history holds the
/// screen still, the arrow lets it follow again. A link that fails never leaves old output looking live: a bar says
/// how old it is and when the next try is.
struct TerminalPanel: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openURL) private var openURL
    @Environment(\.colorScheme) private var scheme
    let key: SessionKey
    @State private var feed: TerminalFeed
    @AppStorage(TerminalPrefs.wrap) private var wrap = true
    @AppStorage(TerminalPrefs.fontSize) private var fontSize = TerminalPrefs.defaultFontSize
    @AppStorage(TerminalPrefs.font) private var fontID = TerminalFont.jetbrains.rawValue
    @AppStorage(TerminalPrefs.theme) private var themeID = TerminalTheme.defaultID
    /// The size a pinch is at, shown while it changes.
    @State private var pinchHUD: Double?
    /// Switches the page to another session (two-finger swipes); nil where the panel cannot (the full-screen sheet).
    var switchTo: ((SessionKey) -> Void)?

    /// Older than this without a fresh screen counts as stale even before anything fails (a request may hang).
    static let staleAfter: TimeInterval = 4

    init(key: SessionKey, switchTo: ((SessionKey) -> Void)? = nil) {
        self.key = key
        self.switchTo = switchTo
        _feed = State(initialValue: TerminalFeed(key: key))
    }

    private var theme: TerminalTheme { .named(themeID) }
    private var side: TerminalTheme.Side { theme.side(scheme == .dark) }
    private var font: TerminalFont { TerminalFont(rawValue: fontID) ?? .jetbrains }

    var body: some View {
        screen
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color(hex: side.background).ignoresSafeArea())
            // What floats on the terminal takes its look: dark on the dark theme whatever the app's appearance.
            .environment(\.colorScheme, side.isDark ? .dark : .light)
            .task(id: key) {
                if feed.key != key { feed = TerminalFeed(key: key) }
                feed.wantsControl = false // watch only: the pane stays at the computer's size
                #if DEBUG
                // `-terminalPages N`: load N pages of history once the screen is there (what scrolling up does).
                let pages = UserDefaults.standard.integer(forKey: "terminalPages")
                var paged = pages == 0, ticks = 0
                #endif
                while !Task.isCancelled {
                    await feed.tick(model)
                    #if DEBUG
                    if !paged, feed.loaded {
                        ticks += 1
                        if ticks >= 8 { // after the hub's hello: the stream replaces the first reads
                            paged = true
                            for _ in 0..<pages { await feed.loadOlder(model) }
                        }
                    }
                    #endif
                    try? await Task.sleep(for: .milliseconds(250))
                }
                feed.stop(model)
            }
    }

    // MARK: Screen

    @ViewBuilder private var screen: some View {
        if feed.offline && feed.screen.isEmpty {
            ContentUnavailableView("没有终端", systemImage: "terminal",
                                   description: Text("这个会话没有在 herdr 里运行，恢复会话后就能看到终端。"))
                .foregroundStyle(Color(hex: side.foreground))
        } else {
            live.overlay {
                if !feed.loaded {
                    ProgressView().tint(Color(hex: side.foreground)).frame(maxWidth: .infinity, maxHeight: .infinity)
                        .allowsHitTesting(false)
                }
            }
        }
    }

    private var live: some View {
        VStack(spacing: 0) {
            if !feed.paused {
                TerminalLinkStatus(lastOK: feed.lastOK, failure: feed.failure, nextTry: feed.nextTry) {
                    Haptics.tap()
                    feed.retryNow(model)
                }
            }
            TerminalView(content: feed.content, wrap: wrap, fontSize: fontSize, font: font, theme: theme,
                         focused: nil,
                         onInput: nil,
                         onNearTop: { Task { await feed.loadOlder(model) } },
                         onOpenURL: { openURL($0) },
                         onCopy: { model.toast = Toast("已拷贝", style: .success) },
                         onFontSize: pinched,
                         onSwipe: swipe,
                         onDoubleTap: nil,
                         onGrid: nil)
        }
        .overlay(alignment: .top) { historyStatus }
        .overlay(alignment: .bottomTrailing) { followButton }
        .overlay { pinchOverlay }
        .overlay(alignment: .top) {
            if let error = feed.error {
                Text(error).font(.caption).foregroundStyle(.white).padding(8)
                    .background(Theme.danger.opacity(0.85), in: .capsule).padding(8)
            }
        }
        .animation(.snappy, value: feed.paused)
        .animation(.snappy, value: feed.loading)
        .animation(.snappy, value: pinchHUD == nil)
    }

    /// Only while something is to be said at the top: a page loading, or the start of what herdr keeps reached.
    @ViewBuilder private var historyStatus: some View {
        let text: String? = feed.loading ? "加载更早的输出…"
            : feed.paused && feed.atLimit ? "herdr 只保留最近约 1000 行，更早的过程在「对话」里"
            : feed.paused && !feed.hasOlder ? "已到最前面" : nil
        if let text {
            HStack(spacing: 6) {
                if feed.loading { ProgressView().controlSize(.mini) }
                Text(text).lineLimit(1).minimumScaleFactor(0.7)
            }
            .font(.caption2.weight(.medium))
            .foregroundStyle(Color(hex: side.foreground).opacity(0.7))
            .padding(.horizontal, 10).padding(.vertical, 5)
            .background(Color(hex: side.background).opacity(0.9), in: .capsule)
            .padding(.top, 6)
            .transition(.opacity)
            .allowsHitTesting(false)
        }
    }

    /// Reading history: back to the live end (as Terminal.app jumps back on new output, but on request).
    @ViewBuilder private var followButton: some View {
        if feed.paused {
            Button {
                Haptics.tap()
                feed.resume()
            } label: {
                Image(systemName: "arrow.down").font(.body.weight(.semibold)).frame(width: 40, height: 40)
            }
            .themedButton()
            .clipShape(.circle)
            .padding(12)
            .accessibilityLabel("回到最新")
            .transition(.scale.combined(with: .opacity))
        }
    }

    @ViewBuilder private var pinchOverlay: some View {
        if let size = pinchHUD {
            Text("字号 \(size, specifier: "%.1f")")
                .font(.subheadline.weight(.semibold).monospacedDigit())
                .lineLimit(1)
                .padding(.horizontal, 14).padding(.vertical, 8)
                .surface(in: .capsule)
                .transition(.opacity)
                .allowsHitTesting(false)
        }
    }

    // MARK: Gestures

    private func pinched(_ size: CGFloat) {
        fontSize = Double(size)
        pinchHUD = Double(size)
        let shown = Double(size)
        Task {
            try? await Task.sleep(for: .seconds(1))
            if pinchHUD == shown { pinchHUD = nil }
        }
    }

    /// Two fingers: left / right go to the next / previous agent of this space (herdr's tabs), up / down to the
    /// first agent of the next / previous space on this machine.
    private func swipe(_ d: UISwipeGestureRecognizer.Direction) {
        guard let switchTo else { return }
        let target: SessionKey?
        switch d {
        case .left, .right:
            let list = model.siblings(of: key).map(\.key)
            guard let i = list.firstIndex(of: key), list.count > 1 else { return }
            target = list[(i + (d == .left ? 1 : list.count - 1)) % list.count]
        default:
            let live = model.allSessions.filter { $0.machineID == key.machineID && $0.state.isLive && $0.spaceKey != nil }
            let spaces = Array(Set(live.compactMap(\.spaceKey))).sorted { $0.localizedStandardCompare($1) == .orderedAscending }
            guard let here = model.session(key)?.spaceKey, let i = spaces.firstIndex(of: here), spaces.count > 1 else { return }
            let next = spaces[(i + (d == .up ? 1 : spaces.count - 1)) % spaces.count]
            guard let there = live.first(where: { $0.spaceKey == next }) else { return }
            target = model.siblings(of: there.key).first?.key
        }
        if let target, target != key { switchTo(target) }
    }
}

/// Atop the terminal while what it shows may be old: how long since the last fresh screen, why, when the
/// next try is — and a button to try now. Hidden while refreshes keep arriving.
struct TerminalLinkStatus: View {
    var lastOK: Date?
    var failure: String?
    var nextTry: Date
    var retry: () -> Void

    private var tint: Color { failure != nil ? Theme.danger : Theme.busy }

    var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { ctx in
            let stale = lastOK.map { ctx.date.timeIntervalSince($0) > TerminalPanel.staleAfter } ?? false
            if failure != nil || stale {
                HStack(spacing: 8) {
                    Image(systemName: failure != nil ? "wifi.exclamationmark" : "clock.arrow.circlepath")
                        .font(.system(size: 12, weight: .semibold))
                    VStack(alignment: .leading, spacing: 1) {
                        Text(Self.headline(lastOK: lastOK, now: ctx.date)).font(.caption.weight(.semibold)).lineLimit(1)
                        Text(Self.detail(failure: failure, nextTry: nextTry, now: ctx.date))
                            .font(.caption2).opacity(0.8).lineLimit(1)
                    }
                    Spacer(minLength: 4)
                    Button(action: retry) {
                        Text("重试").font(.footnote.weight(.semibold)).lineLimit(1)
                            .foregroundStyle(tint)
                            .padding(.horizontal, 14)
                            .frame(height: 30)
                            .background(.white, in: .capsule)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("重试")
                }
                .foregroundStyle(.white)
                .padding(.leading, 12)
                .padding(.trailing, 8)
                .padding(.vertical, 7)
                .background(tint.opacity(0.92), in: .rect(cornerRadius: 10, style: .continuous))
                .padding([.horizontal, .top], 6)
                .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
    }

    /// "上次更新 12 秒前" / "还没有拿到终端".
    static func headline(lastOK: Date?, now: Date) -> String {
        guard let lastOK else { return "还没有拿到终端" }
        let s = max(0, Int(now.timeIntervalSince(lastOK)))
        return "上次更新 " + (s < 60 ? "\(s) 秒前" : s < 3600 ? "\(s / 60) 分钟前" : lastOK.formatted(date: .omitted, time: .shortened))
    }

    /// When it tries again, then why (the reason may be cut off, the countdown never): "4 秒后重试 · 连不上 hub",
    /// or "等待电脑回应…" while a request hangs.
    static func detail(failure: String?, nextTry: Date, now: Date) -> String {
        guard let failure else { return "等待电脑回应…" }
        let s = Int(ceil(nextTry.timeIntervalSince(now)))
        return (s > 0 ? "\(s) 秒后重试" : "正在重试…") + " · " + failure
    }
}

// MARK: - Background tasks

/// Commands and subagents Claude left running in the background ("2 shells still running" in the TUI), shown above
/// the composer even after the turn ended. Tap one to read its output file.
struct BackgroundTasksStrip: View {
    var tasks: [BackgroundTask]
    var openOutput: (BackgroundTask) -> Void
    @State var expanded = false

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

// MARK: - Activity chips

/// "⟳ 下次 11:57" and "⚙ 后台 2" under the chat's tabs: what keeps an agent going, in one small row.
struct ActivityChips: View {
    var loop: LoopInfo?
    var background: [BackgroundTask]
    var open: () -> Void

    var body: some View {
        Button {
            Haptics.tap()
            open()
        } label: {
            HStack(spacing: 6) {
                if let loop {
                    chip(Theme.done) {
                        Image(systemName: "arrow.triangle.2.circlepath").font(.system(size: 10, weight: .bold))
                        TimelineView(.periodic(from: .now, by: 30)) { ctx in
                            Text(Self.loopLabel(loop, now: ctx.date)).lineLimit(1)
                        }
                    }
                }
                if !background.isEmpty {
                    chip(Theme.busy) {
                        Image(systemName: "gearshape.2.fill").font(.system(size: 10, weight: .bold))
                            .symbolEffect(.rotate, options: .repeat(.continuous))
                        Text("后台 \(background.count)").lineLimit(1)
                        if background.count == 1, let t = background.first {
                            Text(t.title).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                }
                Spacer(minLength: 0)
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel([loop.map { "循环中，" + Self.loopLabel($0, now: .now) },
                             background.isEmpty ? nil : "\(background.count) 个后台任务"].compactMap { $0 }.joined(separator: "，") + "，点按查看")
    }

    private func chip(_ tint: Color, @ViewBuilder _ content: () -> some View) -> some View {
        HStack(spacing: 4) { content() }
            .font(.caption.weight(.semibold))
            .foregroundStyle(tint)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(tint.opacity(0.12), in: .capsule)
    }

    static func loopLabel(_ loop: LoopInfo, now: Date) -> String {
        if let next = loop.nextAt, next > now { return "下次 " + next.formatted(date: .omitted, time: .shortened) }
        if loop.nextAt != nil { return "循环执行中" }
        return loop.cadence ?? "循环中"
    }
}

/// The chips opened: the loop (with its stop button) and every background task (with its output).
struct ActivitySheet: View {
    @Environment(\.dismiss) private var dismiss
    var loop: LoopInfo?
    var background: [BackgroundTask]
    var stopLoop: () -> Void
    var openOutput: (BackgroundTask) -> Void

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if let loop { LoopStrip(loop: loop, stop: stopLoop, expanded: true) }
                    if !background.isEmpty { BackgroundTasksStrip(tasks: background, openOutput: openOutput, expanded: true) }
                }
                .padding(16)
            }
            .background(Theme.pageBackground)
            .navigationTitle(loop != nil && background.isEmpty ? "循环" : loop == nil ? "后台任务" : "循环与后台任务")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }
}

// MARK: - Loops

/// The /loop driving this agent: what it repeats, how often, when it wakes next — and a way to stop it.
struct LoopStrip: View {
    var loop: LoopInfo
    var stop: () -> Void
    @State var expanded = false
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


// MARK: - Subagents

extension EnvironmentValues {
    /// Opens the subagent launched by an Agent tool call (by its tool_use id; set by ChatView).
    @Entry var openSubagent: ((String) -> Void)? = nil
}

/// The agents in this conversation, under the tabs: 主 Agent and each subagent it launched (spinning while it
/// runs, a check when done). Tap one to read its conversation in the same chat UI.
struct AgentStrip: View {
    var agents: [Subagent]
    var viewing: String?
    var select: (Subagent?) -> Void

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    chip(title: "主 Agent", symbol: "person.fill", running: false, done: false, on: viewing == nil) { select(nil) }
                        .id("main")
                    ForEach(agents) { a in
                        chip(title: a.title, symbol: nil, running: a.running, done: !a.running, on: viewing == a.id) { select(a) }
                            .id(a.id)
                    }
                }
                .padding(.horizontal, 16)
                .padding(.bottom, 6)
            }
            // The one being read stays in sight — only when it changes, and only along the row (an initial scrollTo
            // also scrolled the page around it and shifted the whole header).
            .onChange(of: viewing) { _, id in
                guard let id else { return }
                withAnimation(.snappy) { proxy.scrollTo(id, anchor: .trailing) }
            }
        }
    }

    private func chip(title: String, symbol: String?, running: Bool, done: Bool, on: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 5) {
                if running {
                    ProgressView().controlSize(.mini)
                } else if done {
                    Image(systemName: "checkmark.circle.fill").font(.system(size: 11)).foregroundStyle(Theme.idle)
                } else if let symbol {
                    Image(systemName: symbol).font(.system(size: 10, weight: .semibold))
                }
                Text(title).lineLimit(1).frame(maxWidth: 160)
            }
            .font(.caption.weight(on ? .semibold : .medium))
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(on ? Theme.accent.opacity(0.15) : Color.primary.opacity(0.05), in: .capsule)
            .overlay(Capsule().stroke(on ? Theme.accent.opacity(0.45) : .clear, lineWidth: 1))
            .foregroundStyle(on ? Theme.accent : Color.primary)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title + (running ? "，运行中" : done ? "，已完成" : ""))
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

/// In place of the composer while a subagent is shown: what it is and the way back.
struct SubagentFooter: View {
    var agent: Subagent
    var back: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: agent.running ? "person.2.wave.2" : "person.2")
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(Theme.accent)
            VStack(alignment: .leading, spacing: 1) {
                Text("子 Agent · " + (agent.type ?? "Agent")).font(.footnote.weight(.semibold)).lineLimit(1)
                Text(agent.running ? "运行中，只读" : "已完成，只读").font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 6)
            Button(action: back) {
                Label("回到主 Agent", systemImage: "arrow.uturn.backward").font(.footnote.weight(.semibold)).lineLimit(1)
            }
            .themedButton()
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .surface(in: .rect(cornerRadius: 20, style: .continuous))
    }
}
