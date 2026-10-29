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

/// The session's terminal at any time (not only while Claude works). The latest 200 lines refresh live; scrolling up
/// loads the 200 before them, again and again, back to the start of what herdr keeps (about 1000 lines — older output
/// lives in the chat). Each page is found by the lines it must end before, so output arriving meanwhile shifts
/// nothing; reading history pauses the live refresh, 回到最新 resumes it. A refresh that fails or hangs never leaves
/// old output looking live: a bar says how old it is and when the next try is, with a button to try now.
struct TerminalPanel: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    @State private var tail = ""
    @State private var older: [String] = []
    @State private var hasOlder = true
    @State private var atLimit = false
    @State private var paused = false
    @State private var loading = false
    @State private var loaded = false
    @State private var offline = false
    @State private var error: String?
    @State private var input = ""
    /// The key bar's ctrl / alt / shift: they also apply to what is typed in the text field (ctrl, then c → ctrl+c).
    @State private var mods = TerminalModifiers()
    /// The last send: each waits for the one before, so keys pressed quickly reach the terminal in order.
    @State private var sending: Task<Void, Never>?
    /// When the screen last came back, the latest refresh's failure and how many in a row, and when to try next
    /// (backing off to every 15 seconds while it keeps failing).
    @State private var lastOK: Date?
    @State private var failure: String?
    @State private var failures = 0
    @State private var nextTry = Date.distantPast

    static let page = 200
    /// Older than this without a fresh screen counts as stale even before the request fails (it may hang for a minute).
    static let staleAfter: TimeInterval = 4

    private var text: String { older.isEmpty ? tail : older.joined(separator: "\n") + "\n" + tail }

    var body: some View {
        VStack(spacing: 10) {
            screen
            TerminalKeyBar(mods: $mods) { keys in send(keys) }
            HStack(spacing: 8) {
                TextField(Self.placeholder(mods), text: $input)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .padding(10)
                    .background(Color.primary.opacity(0.06), in: .rect(cornerRadius: 12, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous)
                        .stroke(mods.active ? Theme.accent.opacity(0.6) : .clear, lineWidth: 1.5))
                    .onSubmit(sendInput)
                    .onChange(of: input) { old, new in typed(old: old, new: new) }
                Button(action: sendInput) {
                    Image(systemName: "return").frame(width: 24, height: 24)
                }
                .themedButton(prominent: true)
                .disabled(input.isEmpty)
                .accessibilityLabel("发送并回车")
            }
        }
        .task(id: key) {
            tail = ""; older = []; hasOlder = true; atLimit = false; paused = false; loaded = false
            lastOK = nil; failure = nil; failures = 0; nextTry = .distantPast
            #if DEBUG
            // `-terminalPages N`: load N pages of history after the first refresh (what scrolling up does).
            let pages = UserDefaults.standard.integer(forKey: "terminalPages")
            if pages > 0 {
                await refresh()
                for _ in 0..<pages { await loadOlder() }
            }
            #endif
            while !Task.isCancelled {
                if !paused, Date() >= nextTry { await refresh() }
                try? await Task.sleep(for: .milliseconds(250))
            }
        }
    }

    @ViewBuilder private var screen: some View {
        if offline && tail.isEmpty {
            ContentUnavailableView("没有终端", systemImage: "terminal",
                                   description: Text("这个会话没有在 herdr 里运行，恢复会话后就能看到终端。"))
                .frame(maxHeight: .infinity)
        } else if !loaded {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Theme.terminalBackground, in: .rect(cornerRadius: 12, style: .continuous))
        } else {
            VStack(alignment: .leading, spacing: 0) {
                if !paused {
                    TerminalLinkStatus(lastOK: lastOK, failure: failure, nextTry: nextTry) { retryNow() }
                }
                historyStatus.padding(.horizontal, 10).padding(.top, 8)
                // Unwrapped and scrollable both ways; keeps its distance from the end, so pages added on top and
                // output added below never move what you read.
                MonoTextView(text: MonoText.plain(text, color: UIColor(Theme.terminalText)), followsEnd: true) {
                    Task { await loadOlder() }
                }
            }
            .background(Theme.terminalBackground, in: .rect(cornerRadius: 12, style: .continuous))
            .overlay(alignment: .bottom) {
                if paused {
                    Button {
                        Haptics.tap()
                        older = []; hasOlder = true; atLimit = false; paused = false
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

    @ViewBuilder private var historyStatus: some View {
        Group {
            if loading {
                HStack(spacing: 6) { ProgressView().controlSize(.mini).tint(Theme.terminalText); Text("加载更早的输出…").lineLimit(1) }
            } else if atLimit {
                Text("— herdr 只保留最近约 1000 行，更早的过程在「对话」里 —").lineLimit(1).minimumScaleFactor(0.8)
            } else if !hasOlder {
                Text("— 已到最前面 —")
            } else {
                Text("上滑加载更早的输出").lineLimit(1)
            }
        }
        .font(.caption.weight(.medium))
        .foregroundStyle(Theme.terminalText.opacity(0.55))
    }

    private func refresh() async {
        guard let api = model.api else { return }
        do {
            let r = try await api.screen(key, lines: Self.page, recent: true)
            offline = r.status == .offline
            if r.text != tail { tail = r.text }
            if Self.lines(r.text).count < Self.page - 1 { hasOlder = false }
            lastOK = Date(); failure = nil; failures = 0
            nextTry = Date().addingTimeInterval(1)
        } catch {
            guard !error.isCancellation else { return }
            failure = error.shown
            failures += 1
            nextTry = Date().addingTimeInterval(Self.backoff(failures))
        }
        loaded = true
    }

    /// 1, 2, 4, 8 and then every 15 seconds.
    static func backoff(_ failures: Int) -> TimeInterval { min(15, pow(2, Double(max(0, failures - 1)))) }

    private func retryNow() {
        Haptics.tap()
        nextTry = .distantPast
        Task { await refresh() }
    }

    /// The page before the first lines on screen.
    private func loadOlder() async {
        guard !loading, hasOlder, loaded, let api = model.api else { return }
        loading = true
        paused = true // the tail must stay put while pages are placed above it
        defer { loading = false }
        let all = Self.lines(text)
        let anchor = Array(all.prefix(20))
        do {
            let r = try await api.screen(key, before: anchor, loaded: all.count, lines: Self.page)
            if !r.text.isEmpty { older = Self.lines(r.text) + older }
            hasOlder = r.hasMore
            atLimit = r.atLimit
        } catch {
            self.error = error.shown
        }
    }

    private static func lines(_ s: String) -> [String] {
        s.isEmpty ? [] : s.trimmingCharacters(in: CharacterSet(charactersIn: "\n")).components(separatedBy: "\n")
    }

    /// The return after the text takes the modifiers that are down (shift+enter: a new line in Claude's box).
    private func sendInput() {
        let t = input
        guard !t.isEmpty else { return }
        input = ""
        let enter = mods.apply("enter")
        mods.pressed()
        send([enter], text: t)
    }

    /// Keys (and text typed before them) in the order they were pressed; the screen refreshes right after.
    private func send(_ keys: [String], text: String = "") {
        let before = sending
        sending = Task {
            await before?.value
            await model.keys(key, keys, text: text)
            nextTry = .distantPast
        }
    }

    /// With a modifier down, a character typed in the field goes out as that key at once (ctrl, c → ctrl+c) instead of
    /// joining the text; the rest of a paste stays in the field.
    private func typed(old: String, new: String) {
        guard mods.active, new.count > old.count, new.hasPrefix(old), let ch = new.dropFirst(old.count).first else { return }
        let (k, text) = mods.typed(ch)
        mods.pressed()
        let rest = String(new.dropFirst(old.count + 1))
        input = old + rest
        Haptics.tap()
        if let k { send([k]) } else if let text { send([], text: text) }
    }

    static func placeholder(_ mods: TerminalModifiers) -> String {
        guard mods.active else { return "发送文字到终端" }
        let down = mods.apply("").dropLast()
        return "下一个字符按 \(down) 发送"
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
