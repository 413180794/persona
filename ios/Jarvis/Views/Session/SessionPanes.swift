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
    /// 终端 comes up from the bottom over the page instead of taking its place (TerminalCover).
    var openTerminal: (() -> Void)?
    @Namespace private var ns

    var body: some View {
        HStack(spacing: 4) {
            ForEach(panes) { p in
                Button {
                    if p == .terminal, let openTerminal { Haptics.tap(); openTerminal(); return }
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
        .background(Theme.Fill.well, in: .capsule)
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
            Image(systemName: pane.symbol).font(.caption.weight(.semibold))
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
            .font(.caption2.weight(.bold).monospacedDigit())
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .background(on ? Theme.accent.opacity(0.18) : Theme.Fill.chip, in: .capsule)
    }
}

// MARK: - Terminal

/// The session's terminal at any time (not only while Claude works), live: the hub streams herdr's frames of the pane
/// (docs/terminal.md) — colors, cursor, wide characters — or, where it cannot, the colored screen is read once a
/// second. Long rows fold at the phone's width unless 不换行 is on (for TUI boxes). Scrolling up loads the 200 rows
/// before, again and again, back to the start of what herdr keeps (about 1000 lines — older output lives in the chat);
/// each page is found by the lines it must end before, so output arriving meanwhile shifts nothing. Reading history
/// holds the screen still, 回到最新 lets it follow again. A link that fails never leaves old output looking live: a
/// bar says how old it is and when the next try is, with a button to try now.
///
/// Typing is on the terminal itself: a tap brings the keyboard up, every key goes to herdr as it is pressed (batched
/// while a request is out), the key bar sits over the keyboard. 设置 › 输入方式 brings back a text field that sends
/// what is written in one go.
struct TerminalPanel: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openURL) private var openURL
    @Environment(\.colorScheme) private var scheme
    let key: SessionKey
    @State private var feed: TerminalFeed
    @State private var input = ""
    /// The key bar's ctrl / alt / shift: they also apply to what is typed (ctrl, then c → ctrl+c).
    @State private var mods = TerminalModifiers()
    /// Everything going to the terminal, in the order it was pressed.
    @State private var stream = KeyStream()
    @AppStorage(TerminalPrefs.wrap) private var wrap = true
    @AppStorage(TerminalPrefs.fontSize) private var fontSize = TerminalPrefs.defaultFontSize
    @AppStorage(TerminalPrefs.font) private var fontID = TerminalFont.jetbrains.rawValue
    @AppStorage(TerminalPrefs.theme) private var themeID = TerminalTheme.defaultID
    @AppStorage(TerminalPrefs.shortcuts) private var shortcutsJSON = ""
    @AppStorage(TerminalPrefs.keys) private var visibleKeys = TerminalPrefs.defaultKeys
    @AppStorage(TerminalPrefs.autoHide) private var autoHideKeys = false
    @AppStorage(TerminalPrefs.inputMode) private var inputMode = "direct"
    @AppStorage(TerminalPrefs.tipsSeen) private var tipsSeen = false
    @AppStorage(TerminalPrefs.size) private var sizeMode = "phone"
    /// A grid change waiting to go out (the keyboard animates, a pinch goes on: the last size is what counts).
    @State private var gridTask: Task<Void, Never>?
    /// The keyboard is up on the terminal (typing straight in).
    @State private var keyboardUp = false
    @FocusState private var typing: Bool
    @State private var showShortcuts = false
    @State private var showSettings = false
    @State private var suggestions: [String] = []
    @State private var pasting: String?
    /// The size a pinch is at, shown while it changes.
    @State private var pinchHUD: Double?
    /// Switches the page to another session (two-finger swipes); nil where the panel cannot (the full-screen sheet).
    var switchTo: ((SessionKey) -> Void)?
    /// The keyboard came up on the terminal or went down (the page folds its header meanwhile).
    var onKeyboard: ((Bool) -> Void)?
    /// Shown from the bottom over the page (TerminalCover): its header is a slim bar to drag it down by.
    var grab: TerminalGrab?

    /// Older than this without a fresh screen counts as stale even before anything fails (a request may hang).
    static let staleAfter: TimeInterval = 4

    init(key: SessionKey, switchTo: ((SessionKey) -> Void)? = nil, onKeyboard: ((Bool) -> Void)? = nil, grab: TerminalGrab? = nil) {
        self.key = key
        self.switchTo = switchTo
        self.onKeyboard = onKeyboard
        self.grab = grab
        _feed = State(initialValue: TerminalFeed(key: key))
    }

    private var theme: TerminalTheme { .named(themeID) }
    private var side: TerminalTheme.Side { theme.side(scheme == .dark) }
    private var direct: Bool { inputMode != "field" }
    private var font: TerminalFont { TerminalFont(rawValue: fontID) ?? .jetbrains }

    var body: some View {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("TerminalPanel.body")
        #endif
        VStack(spacing: 0) {
            screen
            controls
        }
        .background(Color(hex: side.background), in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        // The controls on the terminal take its look: dark keys on the dark theme whatever the app's appearance.
        .environment(\.colorScheme, side.isDark ? .dark : .light)
        .animation(.snappy, value: typing)
        .animation(.snappy, value: keyboardUp)
        .onChange(of: keyboardUp) { _, up in onKeyboard?(up) }
        .sheet(isPresented: $showSettings) { TerminalSettingsView().environment(\.colorScheme, scheme) }
        .alert("电脑上另一个程序正在使用这个终端", isPresented: Binding(get: { feed.control == .busy }, set: { shown in
            if !shown, feed.control == .busy { feed.watchOnly("只看：电脑上另一个程序在用这个终端") }
        })) {
            Button("接管") { feed.takeOver(model) }
            Button("只看", role: .cancel) { feed.watchOnly("只看：电脑上另一个程序在用这个终端") }
        } message: {
            Text("比如 Ovis 或另一个 herdr 客户端。接管后它会断开，终端改成手机的尺寸；离开终端时窗格回到电脑上的尺寸。")
        }
        .onChange(of: feed.controlNote) { _, note in
            if let note { model.toast = Toast(note, style: .info) }
        }
        .onChange(of: sizeMode) { _, mode in feed.setWantsControl(mode == "phone", model) }
        .confirmationDialog(pasting.map { "粘贴 \($0.split(separator: "\n", omittingEmptySubsequences: false).count) 行到终端？" } ?? "",
                            isPresented: Binding(get: { pasting != nil }, set: { if !$0 { pasting = nil } }),
                            titleVisibility: .visible) {
            Button("粘贴") { if let t = pasting { paste(t) } }
        } message: {
            Text("多行文字在 shell 里可能被逐行执行。")
        }
        #if DEBUG
        .onAppear {
            if UserDefaults.standard.bool(forKey: "focusTerminalInput") { if direct { keyboardUp = true } else { typing = true } }
            if UserDefaults.standard.bool(forKey: "openShortcutsPanel") { showShortcuts = true }
            if UserDefaults.standard.bool(forKey: "openTerminalSettings") { showSettings = true }
            if UserDefaults.standard.bool(forKey: "openTerminalTips") { tipsSeen = false }
        }
        #endif
        .task(id: key) {
            if feed.key != key { feed = TerminalFeed(key: key) }
            feed.wantsControl = sizeMode == "phone"
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

    // MARK: Controls

    /// The key bar (over the keyboard while it is up), and in 输入框 mode the text field under it.
    private var controls: some View {
        VStack(spacing: 8) {
            if !autoHideKeys || typing || keyboardUp || mods.active {
                TerminalKeyBar(mods: $mods, visible: Set(visibleKeys.split(separator: ",").map(String.init)),
                               shortcuts: { withAnimation(.spring(duration: 0.3, bounce: 0.25)) { showShortcuts.toggle() } },
                               shortcutsOn: showShortcuts,
                               paste: pasteClipboard,
                               keyboard: direct ? $keyboardUp : nil) { keys in send(keys) }
                    .overlay(alignment: .bottomLeading) {
                        if showShortcuts {
                            ShortcutsPanel(items: TerminalPrefs.decode(shortcutsJSON),
                                           hint: direct ? "快捷 · 单击输入，双击执行" : "快捷 · 单击填入，双击发送",
                                           fill: fillShortcut,
                                           send: { send(["enter"], text: $0); showShortcuts = false },
                                           edit: { showShortcuts = false; showSettings = true })
                                .padding(.bottom, 52)
                                .transition(.scale(scale: 0.6, anchor: .bottomLeading).combined(with: .opacity))
                        }
                    }
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            if !direct {
                if typing, !suggestions.isEmpty {
                    SuggestionStrip(items: suggestions) { input = $0 }
                }
                field
            }
        }
        .padding(.horizontal, 6)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

    /// 输入框 mode: write, then send with return.
    private var field: some View {
        HStack(spacing: 8) {
            TextField(Self.placeholder(mods), text: $input)
                .focused($typing)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .padding(10)
                .background(Theme.Fill.chip, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
                    .stroke(mods.active ? Theme.accent.opacity(0.6) : .clear, lineWidth: 1.5))
                .onSubmit(sendInput)
                .onChange(of: input) { old, new in
                    typed(old: old, new: new)
                    suggestions = TerminalCompletion.suggestions(for: input, rows: feed.content.rows)
                }
            Button(action: sendInput) {
                Image(systemName: "return").frame(width: 24, height: 24)
            }
            .themedButton(prominent: true)
            .disabled(input.isEmpty)
            .accessibilityLabel("发送并回车")
        }
    }

    // MARK: Screen

    @ViewBuilder private var screen: some View {
        if feed.offline && feed.screen.isEmpty {
            ContentUnavailableView("没有终端", systemImage: "terminal",
                                   description: Text("这个会话没有在 herdr 里运行，恢复会话后就能看到终端。"))
                .foregroundStyle(Color(hex: side.foreground))
                .frame(maxHeight: .infinity)
        } else {
            // The view is there while the first frame comes: its layout tells the phone's grid, which driving the
            // terminal waits for before it subscribes (a spinner in its place kept it loading forever).
            live.overlay {
                if !feed.loaded {
                    ProgressView().tint(Color(hex: side.foreground)).frame(maxWidth: .infinity, maxHeight: .infinity)
                        .allowsHitTesting(false)
                }
            }
        }
    }

    private var live: some View {
        liveContent
            .clipShape(.rect(cornerRadius: Theme.Radius.card, style: .continuous))
            .overlay(alignment: .bottom) { bottomOverlay }
            .overlay { pinchOverlay }
            .overlay(alignment: .top) {
                if let error = feed.error {
                    Text(error).font(.caption).foregroundStyle(.white).padding(8)
                        .background(Theme.danger.opacity(0.85), in: .capsule).padding(8)
                }
            }
            .animation(.snappy, value: feed.paused)
            .animation(.snappy, value: pinchHUD == nil)
    }

    private var liveContent: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !feed.paused {
                TerminalFeedLink(feed: feed)
            }
            header
            TerminalFeedFrame(feed: feed) { content in
                TerminalView(content: content, wrap: wrap, fontSize: fontSize, font: font, theme: theme,
                             focused: direct ? $keyboardUp : nil,
                             onInput: inputHandler,
                             onNearTop: { Task { await feed.loadOlder(model) } },
                             onOpenURL: { openURL($0) },
                             onCopy: { model.toast = Toast("已拷贝", style: .success) },
                             onFontSize: pinched,
                             onSwipe: swipe,
                             onDoubleTap: pasteClipboard,
                             onWheel: feed.drives && model.canScrollTerminal ? { model.terminalScroll(key, lines: $0) } : nil,
                             onClick: feed.drives && model.canClickTerminal ? { model.terminalClick(key, column: $0, row: $1) } : nil,
                             onGrid: gridChanged)
            }
        }
    }

    private var inputHandler: ((TerminalInputEvent) -> Void)? {
        guard direct else { return nil }
        return { e in typedOnTerminal(e) }
    }

    @ViewBuilder private var bottomOverlay: some View {
        if feed.paused {
            Button {
                Haptics.tap()
                feed.resume()
            } label: {
                Label("回到最新", systemImage: "arrow.down.to.line").lineLimit(1).font(.footnote.weight(.semibold))
            }
            .themedButton()
            .padding(10)
            .transition(.move(edge: .bottom).combined(with: .opacity))
        } else if !tipsSeen {
            TerminalTips(direct: direct, openSettings: { tipsSeen = true; showSettings = true }) {
                withAnimation(.snappy) { tipsSeen = true }
            }
            .padding(10)
            .transition(.move(edge: .bottom).combined(with: .opacity))
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

    @ViewBuilder private var header: some View {
        if let grab { grabBar(grab) } else { cardHeader }
    }

    /// Over the page (TerminalCover): a slim bar — its size on the left, the handle, settings on the right. Dragged
    /// down it takes the terminal with it (far or fast enough: closed); a tap on the handle closes it too.
    private func grabBar(_ g: TerminalGrab) -> some View {
        let fg = Color(hex: side.foreground)
        return ZStack {
            Capsule().fill(fg.opacity(0.35)).frame(width: 38, height: 5)
                .frame(width: 110, height: 30)
                .contentShape(.rect)
                .onTapGesture { Haptics.tap(); g.close() }
                .accessibilityElement()
                .accessibilityLabel("关闭终端")
                .accessibilityAddTraits(.isButton)
                .accessibilityIdentifier("terminal-close")
            HStack(spacing: 6) {
                if feed.wantsControl, feed.control != .off { sizeChip } else { wrapToggle }
                if feed.loading { ProgressView().controlSize(.mini).tint(fg) }
                Spacer(minLength: 0)
                Button { Haptics.tap(); showSettings = true } label: {
                    Image(systemName: "slider.horizontal.3").font(.footnote.weight(.semibold)).frame(width: 32, height: 26)
                }
                .buttonStyle(.plain)
                .foregroundStyle(fg.opacity(0.75))
                .accessibilityLabel("终端设置")
            }
        }
        .padding(.horizontal, 10)
        .frame(height: 34)
        .contentShape(.rect)
        .gesture(DragGesture(minimumDistance: 4, coordinateSpace: .global)
            .onChanged { g.dragged($0.translation.height) }
            .onEnded { g.released($0.translation.height, $0.predictedEndTranslation.height) })
    }

    private var cardHeader: some View {
        HStack(spacing: 8) {
            historyStatus
            Spacer(minLength: 8)
            if feed.wantsControl, feed.control != .off { sizeChip } else { wrapToggle }
            Button { Haptics.tap(); showSettings = true } label: {
                Label("设置", systemImage: "slider.horizontal.3")
                    .font(.caption2.weight(.semibold))
                    .lineLimit(1)
                    .fixedSize()
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(Color(hex: side.foreground).opacity(0.1), in: .capsule)
            }
            .buttonStyle(.plain)
            .foregroundStyle(Color(hex: side.foreground).opacity(0.75))
            .accessibilityLabel("终端设置")
        }
        .padding(.horizontal, 10)
        .padding(.top, 8)
    }

    @ViewBuilder private var historyStatus: some View {
        Group {
            if feed.loading {
                HStack(spacing: 6) { ProgressView().controlSize(.mini).tint(Color(hex: side.foreground)); Text("加载更早的输出…").lineLimit(1) }
            } else if feed.drives || feed.control == .starting {
                EmptyView() // driving: scrolling up is the terminal's own history since the takeover, nothing to say
            } else if feed.atLimit {
                Text("— herdr 只保留最近约 1000 行，更早的过程在「对话」里 —").lineLimit(1).minimumScaleFactor(0.7)
            } else if !feed.hasOlder {
                Text("— 已到最前面 —").lineLimit(1)
            } else {
                Text("上滑加载更早的输出").lineLimit(1).minimumScaleFactor(0.8)
            }
        }
        .font(.caption.weight(.medium))
        .foregroundStyle(Color(hex: side.foreground).opacity(0.55))
    }

    /// Driving the terminal: its size (the computer's pane is that now). Watching although it wanted to drive: a
    /// button to take it.
    @ViewBuilder private var sizeChip: some View {
        let fg = Color(hex: side.foreground)
        switch feed.control {
        case .active, .starting:
            let g = feed.phoneGrid
            Label(feed.drives ? "手机尺寸 \(g?.cols ?? 0)×\(g?.rows ?? 0)" : "接管中…", systemImage: "iphone")
                .font(.caption2.weight(.semibold).monospacedDigit())
                .lineLimit(1)
                .fixedSize()
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(fg.opacity(0.1), in: .capsule)
                .foregroundStyle(fg.opacity(0.75))
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("terminal-size")
        default:
            Button {
                Haptics.tap()
                feed.takeOver(model)
            } label: {
                Label("只看 · 接管", systemImage: "iphone.and.arrow.forward")
                    .font(.caption2.weight(.semibold))
                    .lineLimit(1)
                    .fixedSize()
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(fg.opacity(0.1), in: .capsule)
            }
            .buttonStyle(.plain)
            .foregroundStyle(fg.opacity(0.75))
            .accessibilityLabel("接管终端")
            .accessibilityIdentifier("terminal-size")
        }
    }

    /// The view's grid: the first one at once (the feed waits for it to subscribe), later ones once they settle.
    private func gridChanged(_ g: TerminalGridSize) {
        gridTask?.cancel()
        guard feed.phoneGrid != nil else { return feed.setGrid(g, model) }
        let feed = feed, model = model
        gridTask = Task {
            try? await Task.sleep(for: .milliseconds(250))
            guard !Task.isCancelled else { return }
            feed.setGrid(g, model)
        }
    }

    /// 换行 (rows fold at the phone's width) or 不换行 · 左右滑 (the pane's width, for TUI boxes).
    private var wrapToggle: some View {
        Button {
            Haptics.tap()
            wrap.toggle()
        } label: {
            Label(wrap ? "换行" : "不换行 · 左右滑", systemImage: wrap ? "arrow.turn.down.left" : "arrow.left.and.right")
                .font(.caption2.weight(.semibold))
                .lineLimit(1)
                .fixedSize()
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(Color(hex: side.foreground).opacity(0.1), in: .capsule)
        }
        .buttonStyle(.plain)
        .foregroundStyle(Color(hex: side.foreground).opacity(0.75))
        .accessibilityLabel("长行换行")
        .accessibilityValue(wrap ? "开" : "关，左右滑动看")
    }

    // MARK: Actions

    private func pinched(_ size: CGFloat) {
        fontSize = Double(size)
        pinchHUD = Double(size)
        let shown = Double(size)
        Task {
            try? await Task.sleep(for: .seconds(1))
            if pinchHUD == shown { pinchHUD = nil }
        }
    }

    /// A shortcut: typed into the terminal (or put in the field), not run.
    private func fillShortcut(_ s: String) {
        showShortcuts = false
        if direct {
            send([], text: s)
            keyboardUp = true
        } else {
            input = s
            typing = true
        }
    }

    /// Double tap: the clipboard goes to the terminal as typed (no return); several lines ask first.
    private func pasteClipboard() {
        guard let text = UIPasteboard.general.string, !text.isEmpty else {
            model.toast = Toast("剪贴板里没有文字", style: .error)
            return
        }
        if text.contains("\n") { pasting = text } else { paste(text) }
    }

    private func paste(_ text: String) {
        Haptics.tap()
        send([], text: text)
        model.toast = Toast("已粘贴", style: .success)
        pasting = nil
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

    /// A key on the keyboard: with ctrl / alt / shift down on the key bar it goes out as that combination.
    private func typedOnTerminal(_ e: TerminalInputEvent) {
        if feed.paused { feed.resume() }
        guard mods.active else { return push(e) }
        switch e {
        case .text(let t) where t.count == 1:
            let (k, text) = mods.typed(t.first!)
            mods.pressed()
            push(k.map { .key($0) } ?? .text(text ?? t))
        case .key(let k):
            let name = mods.apply(k)
            mods.pressed()
            push(.key(name))
        case .text:
            push(e)
        }
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

    /// Text, then keys — in order with everything typed before.
    private func send(_ keys: [String], text: String = "") {
        if !text.isEmpty { push(.text(text)) }
        for k in keys { push(.key(k)) }
    }

    private func push(_ e: TerminalInputEvent) {
        if feed.drives { // straight to the terminal, as its bytes
            switch e {
            case .text(let t): feed.type(TerminalKeyBytes.text(t), model)
            case .key(let k): if let b = TerminalKeyBytes.encode(k) { feed.type(b, model) }
            }
            return
        }
        let model = model, key = key, feed = feed
        stream.push(e) { keys, text in
            await model.keys(key, keys, text: text)
            feed.nudge()
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

private struct TerminalFeedLink: View {
    @Environment(AppModel.self) private var model
    let feed: TerminalFeed

    var body: some View {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("TerminalFeedLink.body")
        #endif
        TerminalLinkStatus(lastOK: feed.lastOK, failure: feed.failure, nextTry: feed.nextTry) {
            Haptics.tap()
            feed.retryNow(model)
        }
    }
}

private struct TerminalFeedFrame<Frame: View>: View {
    let feed: TerminalFeed
    @ViewBuilder var frame: (TerminalContent) -> Frame

    var body: some View {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("TerminalFeedFrame.body")
        #endif
        frame(feed.content)
    }
}

/// Sends what goes to the terminal in order, one request at a time; what is typed while one is out goes in the next
/// (KeyBatcher), so fast typing costs a few requests, not one per key, and nothing overtakes anything.
@MainActor final class KeyStream {
    private var batch = KeyBatcher()
    private var running = false

    func push(_ e: TerminalInputEvent, post: @escaping (_ keys: [String], _ text: String) async -> Void) {
        batch.push(e)
        guard !running else { return }
        running = true
        Task {
            while let next = batch.take() { await post(next.keys, next.text) }
            running = false
        }
    }
}

/// Shown on the terminal until dismissed: what can be done here that nothing on the screen says.
struct TerminalTips: View {
    var direct: Bool
    var openSettings: () -> Void
    var dismiss: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("终端小技巧").font(.subheadline.weight(.semibold))
            VStack(alignment: .leading, spacing: 7) {
                if direct { tip("keyboard", "键栏右边的键盘按钮弹出键盘直接打字；点终端是点在终端里") }
                tip("arrow.up.left.and.arrow.down.right", "双指捏合调字号")
                tip("hand.tap", "双击终端粘贴剪贴板")
                tip("bolt", "闪电：常用命令，单击输入、双击执行")
                tip("arrow.left.arrow.right", "双指左右滑：切换同一空间的 Agent")
            }
            .font(.footnote)
            HStack(spacing: 10) {
                Button("配色、字体、键栏", action: openSettings).lineLimit(1).themedButton()
                Spacer(minLength: 0)
                Button("知道了", action: dismiss).lineLimit(1).themedButton(prominent: true)
            }
            .font(.footnote.weight(.semibold))
        }
        .padding(14)
        .frame(maxWidth: 380, alignment: .leading)
        .surface(in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous)) // no shadow over the surface
    }

    private func tip(_ symbol: String, _ text: String) -> some View {
        Label { Text(text).lineLimit(1).minimumScaleFactor(0.8) } icon: {
            Image(systemName: symbol).foregroundStyle(Theme.accent).frame(width: 20)
        }
    }
}

/// Atop the terminal while what it shows may be old: how long since the last fresh screen, why, when the
/// next try is — and a button to try now. Hidden while refreshes keep arriving.
struct TerminalLinkStatus: View {
    var lastOK: Date?
    var failure: String?
    var nextTry: Date
    var retry: () -> Void

    private var tint: Color { failure != nil ? Theme.danger : Theme.caution }

    var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { ctx in
            let stale = lastOK.map { ctx.date.timeIntervalSince($0) > TerminalPanel.staleAfter } ?? false
            if failure != nil || stale {
                HStack(spacing: 8) {
                    Image(systemName: failure != nil ? "wifi.exclamationmark" : "clock.arrow.circlepath")
                        .font(.caption.weight(.semibold))
                    VStack(alignment: .leading, spacing: 1) {
                        Text(Self.headline(lastOK: lastOK, now: ctx.date)).font(.caption.weight(.semibold)).lineLimit(1)
                        Text(Self.detail(failure: failure, nextTry: nextTry, now: ctx.date))
                            .font(.caption2).opacity(0.8).lineLimit(1)
                    }
                    Spacer(minLength: 4)
                    Button(action: retry) {
                        Text("重试").font(.footnote.weight(.semibold)).lineLimit(1)
                            .foregroundStyle(.black)
                            .padding(.horizontal, 14)
                            .frame(height: 30)
                            .background(.white, in: .capsule)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("重试")
                }
                .foregroundStyle(.black)
                .padding(.leading, 12)
                .padding(.trailing, 8)
                .padding(.vertical, 7)
                .background(tint, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
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
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Theme.busy)
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
        .surface(tint: Theme.busy.opacity(0.06), in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous))
    }

    private func row(_ t: BackgroundTask) -> some View {
        Button { if t.outputFile != nil { openOutput(t) } } label: {
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: t.isAgent ? "person.2" : "terminal")
                    .font(.caption.weight(.semibold)).dynamicTypeSize(...DynamicTypeSize.xxLarge)
                    .foregroundStyle(.secondary)
                    .frame(width: 18, height: 18)
                VStack(alignment: .leading, spacing: 3) {
                    Text(t.title).font(.subheadline.weight(.medium)).foregroundStyle(Color.primary).lineLimit(1)
                    if let cmd = t.command, !cmd.isEmpty, cmd != t.title {
                        Text(cmd).font(.caption2.monospaced()).foregroundStyle(.secondary).lineLimit(2)
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
            .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
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
    /// Beside the agents menu in the header: the loop's next time and the task count only.
    var compact = false
    var open: () -> Void

    var body: some View {
        Button {
            Haptics.tap()
            open()
        } label: {
            HStack(spacing: 6) {
                if let loop {
                    chip(Theme.loop, text: Theme.loop) {
                        Image(systemName: "arrow.triangle.2.circlepath").font(.caption2.weight(.bold))
                        TimelineView(.periodic(from: .now, by: 30)) { ctx in
                            Text(compact ? Self.shortLoopLabel(loop, now: ctx.date) : Self.loopLabel(loop, now: ctx.date)).lineLimit(1)
                        }
                    }
                }
                if !background.isEmpty {
                    chip(Theme.busy, text: StatusPalette.working.text) {
                        Image(systemName: "gearshape.2.fill").font(.caption2.weight(.bold))
                        Text("后台 \(background.count)").lineLimit(1)
                        if !compact, background.count == 1, let t = background.first {
                            Text(t.title).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                }
                if !compact { Spacer(minLength: 0) }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel([loop.map { "循环中，" + Self.loopLabel($0, now: .now) },
                             background.isEmpty ? nil : "\(background.count) 个后台任务"].compactMap { $0 }.joined(separator: "，") + "，点按查看")
    }

    private func chip(_ tint: Color, text: Color, @ViewBuilder _ content: () -> some View) -> some View {
        HStack(spacing: 4) { content() }
            .font(.caption.weight(.semibold))
            .foregroundStyle(text)
            .padding(.horizontal, compact ? 10 : 8)
            .padding(.vertical, compact ? 10 : 3) // compact: as tall as the agents menu beside it
            .background(tint.opacity(0.12), in: .capsule)
    }

    /// The loop beside the arrows when room is short: the next time, or that it is running.
    static func shortLoopLabel(_ loop: LoopInfo, now: Date) -> String {
        if let next = loop.nextAt, next > now { return next.formatted(date: .omitted, time: .shortened) }
        return loop.nextAt != nil ? "执行中" : "循环"
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
                    .font(.footnote.weight(.bold))
                    .foregroundStyle(Theme.loop)
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
        .surface(tint: Theme.loop.opacity(0.06), in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous))
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
            Image(systemName: "arrow.triangle.2.circlepath").font(.caption2.weight(.bold))
            Text("循环唤醒").fontWeight(.semibold)
            if let ts = item.ts { Text(ts.formatted(date: .omitted, time: .shortened)) }
            Text(String((item.text ?? "").dropFirst(5)).trimmingCharacters(in: .whitespaces)).lineLimit(1).foregroundStyle(.tertiary)
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .padding(.horizontal, 10)
        .padding(.vertical, 5)
        .background(Theme.Fill.well, in: .capsule)
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
                    Image(systemName: "checkmark.circle.fill").font(.caption2).foregroundStyle(Theme.positive)
                } else if let symbol {
                    Image(systemName: symbol).font(.caption2.weight(.semibold))
                }
                Text(title).lineLimit(1).frame(maxWidth: 160)
            }
            .font(.caption.weight(on ? .semibold : .medium))
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(on ? Theme.accent.opacity(0.15) : Theme.Fill.well, in: .capsule)
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
                .font(.subheadline.weight(.semibold))
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
        .surface(in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous))
    }
}

/// How the terminal shown over the page (TerminalCover) is dragged down by its bar, and closed.
struct TerminalGrab {
    var dragged: (CGFloat) -> Void
    var released: (_ distance: CGFloat, _ predicted: CGFloat) -> Void
    var close: () -> Void
}

