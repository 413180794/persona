import MarkdownUI
import PhotosUI
import SwiftUI

enum ChatStyle {
    /// 开发: every step visible, tab bar hidden.
    case dev
    /// 对话 (assistant): thoughts hidden, tool runs folded into one line, lives at the root of its tab.
    case assistant
}

struct ChatView: View {
    @Environment(AppModel.self) private var model
    /// The session on screen; the sibling strip can switch it to another agent in the same space.
    @State private var key: SessionKey
    var style: ChatStyle = .dev
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var atBottom = true
    @State private var showTerminal = false
    @State private var file: FileRef?
    @State private var debugImages: [ImagePayload]?
    @State private var showInfo = false
    @State private var pane: SessionPane = .chat
    @State private var git: GitStatus?
    @State private var menu = MessageMenu()
    /// Turns the user folded / unfolded (default: running ones open, finished ones folded).
    @State private var openTurns: [String: Bool] = [:]

    init(key: SessionKey, style: ChatStyle = .dev) {
        _key = State(initialValue: key)
        self.style = style
    }

    private var store: ChatStore { model.chat(key) }
    private var session: Session? { model.session(key) }
    private var status: SessionStatus { session?.status ?? .unknown }

    var body: some View {
        paneContent
        .background(Theme.pageBackground)
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .overlay(alignment: .top) {
            if showInfo {
                DevInfoCard(key: key, session: session)
                    .padding(.horizontal, 12)
                    .padding(.top, 6)
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .toolbar {
            // A custom title: tapping it toggles the developer info card (instead of the system scroll-to-top).
            ToolbarItem(placement: .principal) {
                Button {
                    Haptics.tap()
                    withAnimation(.snappy) { showInfo.toggle() }
                } label: {
                    VStack(spacing: 1) {
                        Text(title).font(.headline).lineLimit(1)
                        HStack(spacing: 3) {
                            Text(subtitle).lineLimit(1)
                            Image(systemName: "chevron.down")
                                .font(.system(size: 8, weight: .bold))
                                .rotationEffect(.degrees(showInfo ? 180 : 0))
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: 260)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(title)，\(showInfo ? "收起" : "查看")会话信息")
            }
            if style == .assistant {
                ToolbarItem(placement: .topBarTrailing) { AssistantModelMenu(key: key) }
            }
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    if style == .dev {
                        Button { withAnimation(.snappy) { pane = .terminal } } label: { Label("终端", systemImage: "terminal") }
                    }
                    if status.isActive {
                        Button { Task { await model.interrupt(key) } } label: { Label("中断", systemImage: "stop.circle") }
                    }
                    Divider()
                    if let s = session {
                        Button { UIPasteboard.general.string = "claude --resume \(s.sessionID)" } label: {
                            Label("拷贝恢复命令", systemImage: "doc.on.doc")
                        }
                        Button { UIPasteboard.general.string = s.cwd } label: { Label("拷贝目录", systemImage: "folder") }
                        if s.startedByJarvis && s.status.isLive {
                            Button(role: .destructive) { Task { await model.close(key) } } label: {
                                Label("退出 Claude 并关闭标签页", systemImage: "xmark.circle")
                            }
                        }
                    }
                } label: {
                    Image(systemName: "ellipsis")
                }
            }
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            if style == .dev {
                VStack(spacing: 0) {
                    SiblingStrip(current: key) { next in switchTo(next) }
                    SessionPaneBar(pane: $pane, panes: panes, badges: paneBadges)
                }
                .background(.bar)
            }
        }
        .task(id: "\(key.machineID)/\(key.sessionID)|\(session?.cwd ?? "")") { await loadGit() }
        .onChange(of: pane) { _, p in
            // The chat's scroll view is rebuilt when you come back to it: start at the latest message.
            if p == .chat { position = ScrollPosition(id: Self.bottomID, anchor: .bottom); atBottom = true }
        }
        .onChange(of: status) { _, st in if !st.isActive { Task { await loadGit() } } }
        .toolbar(style == .dev ? .hidden : .automatic, for: .tabBar)
        .fullScreenCover(isPresented: $showTerminal) { TerminalSheet(key: key) }
        .sheet(item: $file) { ref in FileViewer(ref: ref) }
        #if DEBUG
        // Simulator automation: `-openFirstImage YES` opens the viewer on the first image once it loads.
        .onChange(of: store.rows.count) { _, _ in
            guard UserDefaults.standard.bool(forKey: "openFirstImage"), debugImages == nil else { return }
            let imgs = store.items.compactMap(\.image)
            if !imgs.isEmpty { debugImages = imgs }
        }
        .fullScreenCover(isPresented: Binding(get: { debugImages != nil }, set: { if !$0 { debugImages = [] } })) {
            if let imgs = debugImages, !imgs.isEmpty { ImageViewer(images: imgs, index: 0) }
        }
        #endif
        .environment(\.openFile) { path in
            file = FileRef(machineID: key.machineID, path: path, cwd: session?.cwd)
        }
        .environment(\.loadImage) { [model, key] path in
            guard let f = try? await model.api?.file(machine: key.machineID, path: path, cwd: nil), let data = f.data else { return nil }
            return ImagePayload(mime: f.mime ?? "image/jpeg", data: data)
        }
        .onAppear(perform: appeared)
        .onDisappear {
            model.unsubscribe(key)
            if model.visibleSession == key { model.visibleSession = nil }
        }
        .onChange(of: session?.state) { _, st in
            if st == .unread && model.visibleSession == key { model.markSeen(key) }
        }
    }

    /// The chat as a flat list of lines (see `ChatLine`).
    private func chatLines(_ blocks: [ChatBlock]) -> [ChatLine] {
        let liveID = liveTurnID
        var out: [ChatLine] = []
        for block in blocks {
            switch block {
            case .row(let r):
                out.append(.row(r))
            case .turn(let t):
                let live = t.id == liveID
                let open = openTurns[t.id] ?? live
                // Every message of the turn first (also ones sent while it runs), then the timer line, then the work.
                out += t.users.map { .row($0) }
                if live || !t.process.isEmpty { out.append(.header(t, live: live, open: open)) }
                if open {
                    out += Self.workLines(live ? t.steps : t.process, live: live)
                    if !live, let a = t.answer { out.append(.row(a)) }
                } else if let a = t.answer {
                    out.append(.row(a))
                }
                out += t.trailing.map { .row($0) }
            }
        }
        return out
    }

    /// The work of a turn: the agent's words in full; everything between them — tool calls and thinking (newer
    /// Claude Code writes its narration as thinking) — as one line "调用了 3 个工具，思考 2 次" to tap open.
    private static func workLines(_ rows: [ChatRow], live: Bool) -> [ChatLine] {
        var out: [ChatLine] = []
        var run: [ToolStep] = []
        func flush(last: Bool) {
            guard let first = run.first else { return }
            out.append(.tools(id: first.id, steps: run, running: live && last))
            run = []
        }
        for r in rows {
            switch r {
            case .thinking(let i):
                if !(i.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { run.append(.thought(i)) }
            case .tools(let steps):
                run += steps
            default:
                flush(last: false)
                out.append(.row(r))
            }
        }
        flush(last: true)
        return out
    }

    @ViewBuilder private func lineView(_ line: ChatLine) -> some View {
        switch line {
        case .row(let r): rowView(r, isLatest: false, live: true)
        case .header(let t, let live, let open):
            WorkHeader(live: live, start: t.startedAt, end: t.endedAt, open: open,
                       openTerminal: live && style == .dev ? { showTerminal = true } : nil) {
                Haptics.tap()
                withAnimation(.snappy) { openTurns[t.id] = !open }
            }
        case .tools(_, let steps, let running): ToolCountRow(steps: steps, running: running)
        }
    }

    static let bottomID = "chat-bottom"

    /// Scrolls to the sentinel after the last row by id: SwiftUI lays that row out and positions around it, unlike
    /// `scrollTo(edge: .bottom)`, which trusts the lazy stack's estimated height and can land in unrendered space.
    private func scrollToEnd() {
        position.scrollTo(id: Self.bottomID, anchor: .bottom)
    }

    private var liveTurnID: String? {
        guard status.isActive, case .turn(let t) = store.blocks.last else { return nil }
        return t.id
    }

    private var lastPromptID: String? { store.items.last { $0.role == "user" && $0.kind == .text }?.id }

    @ViewBuilder private var paneContent: some View {
        switch pane {
        case .chat: chatPane
        case .terminal: TerminalPanel(key: key).padding(.horizontal, 12).padding(.bottom, 8)
        case .git: GitPane(machineID: key.machineID, cwd: session?.cwd ?? "", status: $git)
        case .files: FilesPane(machineID: key.machineID, cwd: session?.cwd ?? "", git: git)
        }
    }

    /// Git only for repositories, files only when we know the directory; the assistant has neither.
    private var panes: [SessionPane] {
        var p: [SessionPane] = [.chat, .terminal]
        if git?.isRepo == true { p.append(.git) }
        if !(session?.cwd ?? "").isEmpty { p.append(.files) }
        return p
    }

    private var paneBadges: [SessionPane: String] {
        guard let n = git?.files.count, n > 0 else { return [:] }
        return [.git: n > 99 ? "99+" : String(n)]
    }

    private func loadGit() async {
        guard style == .dev, let cwd = session?.cwd, !cwd.isEmpty, let api = model.api else { return }
        if let s = try? await api.gitStatus(machine: key.machineID, cwd: cwd) { git = s }
    }

    @ViewBuilder private var chatPane: some View {
        let store = store
        let rows = store.rows
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 14) {
                if store.hasMore {
                    // Reaching the top loads the page before it; runs again whenever older rows were prepended.
                    ProgressView()
                        .controlSize(.small)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 6)
                        .task(id: rows.first?.id) { await model.loadOlder(key) }
                }
                if !store.loadedOnce && rows.isEmpty {
                    ChatSkeleton()
                } else if rows.isEmpty {
                    EmptyStateView(symbol: "text.bubble", title: "还没有消息", message: "发一句话开始吧。")
                }
                // Finished turns: the user's messages (queued ones gathered), one folded "process" row, the answer.
                // The turn still running shows every step in arrival order.
                // Every message, timer line, narration, tool count and answer is its own lazy row: a running turn can
                // hold hundreds of steps and laying them out in one stack froze the page (blank for seconds).
                ForEach(chatLines(Self.debugTrim(store.blocks))) { line in
                    lineView(line)
                        .id(line.id)
                        .zIndex(menu.selected != nil && line.rowID == menu.selected ? 1 : 0)
                }
                // Sent from here, not in the transcript yet (ones Claude queued show in the queue strip instead).
                let queued = Set((session?.queued ?? []).map(PendingMessage.normalize))
                ForEach(store.pending.filter { !queued.contains(PendingMessage.normalize($0.text)) }) { msg in
                    PendingBubble(msg: msg, retry: { model.retry(key, msg) }, discard: { model.discard(key, msg) })
                        .id(msg.id.uuidString)
                }
                if style == .assistant {
                    let live = store.liveText
                    let lastText: String? = { if case .assistant(let i) = rows.last { return i.text } else { return nil } }()
                    if !live.isEmpty && !(lastText.map { $0.hasPrefix(live) || live == $0 } ?? false) {
                        StreamingText(text: live).id("live")
                    } else if status == .working && live.isEmpty {
                        TypingIndicator(text: "Jarvis 正在思考…").id("typing")
                    }
                }
                // Bottom sentinel: while it is on screen we are at the end and the "jump to bottom" arrow hides
                // (geometry math missed the composer / queue insets and kept the arrow up at the very end).
                Color.clear
                    .frame(height: 1)
                    .id(Self.bottomID)
                    .onScrollVisibilityChange(threshold: 0.01) { visible in atBottom = visible }
                if let err = store.error {
                    Label(err, systemImage: "exclamationmark.triangle").font(.caption).foregroundStyle(Theme.danger)
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 12)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .scrollDismissesKeyboard(.interactively)
        .simultaneousGesture(TapGesture().onEnded { menu.dismiss() })
        // Only the user's own scrolling closes the bar; content streaming in also moves the offset (.animating).
        .onScrollPhaseChange { _, phase in if phase == .tracking || phase == .interacting { menu.dismiss() } }
        .environment(menu)
        .sheet(item: $menu.selecting) { t in SelectableTextSheet(text: t.text) }

        .onChange(of: rows.last?.id) { _, _ in
            if atBottom { withAnimation(.snappy) { scrollToEnd() } }
        }
        // A turn finishing folds its work away (the content gets much shorter) and a new one starts: without a
        // jump the old offset lands in empty space below the content. Your own new message always goes to the end.
        .onChange(of: liveTurnID) { _, _ in if atBottom { scrollToEnd() } }
        .onChange(of: lastPromptID) { old, _ in if old != nil { scrollToEnd() } }
        .onChange(of: store.pending.count) { old, new in if new > old { withAnimation(.snappy) { scrollToEnd() } } }
        // A message absorbed mid-turn is inserted above the timer, far up: re-anchor on the end so the viewport does
        // not end up in a stretch the lazy stack never laid out (blank page).
        .onChange(of: store.items.count) { _, _ in if atBottom { scrollToEnd() } }
        .onChange(of: store.liveText.count / 40) { _, _ in
            if atBottom { scrollToEnd() }
        }
        .overlay(alignment: .bottomTrailing) {
            if !atBottom {
                Button {
                    withAnimation(.snappy) { scrollToEnd() }
                } label: {
                    Image(systemName: "arrow.down").font(.callout.weight(.semibold)).frame(width: 40, height: 40)
                }
                .themedButton()
                .clipShape(.circle)
                .padding(16)
                .transition(.scale.combined(with: .opacity))
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(spacing: 10) {
                ForEach(model.interactions(for: key)) { ix in InteractionPanel(interaction: ix) }
                if let loop = session?.loop, style == .dev {
                    LoopStrip(loop: loop) { model.stopLoop(key) }
                }
                if let bg = session?.background, !bg.isEmpty, style == .dev {
                    BackgroundTasksStrip(tasks: bg) { t in
                        if let out = t.outputFile { file = FileRef(machineID: key.machineID, path: out, cwd: session?.cwd) }
                    }
                }
                if let queued = session?.queued, !queued.isEmpty, style == .dev {
                    QueueStrip(key: key, queued: queued)
                }
                Composer(key: key, status: status, placeholder: style == .assistant ? "问 Jarvis…" : nil)
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .animation(.snappy, value: status)
            .animation(.snappy, value: model.interactions(for: key).map(\.id))
        }
    }

    private func appeared() {
            bindMenu()
            model.subscribe(key)
            model.visibleSession = key
            model.markSeen(key)
            if style == .dev { AppModel.rememberChat(key) }
            #if DEBUG
            if UserDefaults.standard.bool(forKey: "openTerminal") { showTerminal = true }
            if UserDefaults.standard.bool(forKey: "showInfo") { showInfo = true }
            if let p = UserDefaults.standard.string(forKey: "openPane").flatMap(SessionPane.init(rawValue:)) { pane = p }
            if let f = UserDefaults.standard.string(forKey: "openFile") { file = FileRef(machineID: key.machineID, path: f, cwd: session?.cwd) }
            if UserDefaults.standard.bool(forKey: "showMenu") { // the long-press bar on the last answer, for screenshots
                Task {
                    try? await Task.sleep(for: .seconds(UserDefaults.standard.bool(forKey: "loadAll") ? 40 : 6))
                    let from = UserDefaults.standard.integer(forKey: "fromSeq")
                    let answers = model.chat(key).blocks.compactMap { b -> ChatRow? in if case .turn(let t) = b { t.answer } else { nil } }
                    let last = from > 0
                        ? answers.first { if case .assistant(let i) = $0 { i.seq > from } else { false } }
                        : answers.last
                    menu.anchor = CGPoint(x: 160, y: 30)
                    menu.selected = last?.id
                }
            }
            if UserDefaults.standard.bool(forKey: "loadAll") {
                Task {
                    try? await Task.sleep(for: .seconds(3))
                    while model.chat(key).hasMore { await model.loadOlder(key) }
                    try? await Task.sleep(for: .seconds(1))
                    position.scrollTo(edge: .top)
                }
            }
            #endif
    }

    /// Simulator automation: `-fromSeq N` renders from the turn holding the user message with that seq
    /// (scrolling a long lazy history to a turn drifts while rows above size themselves).
    private static func debugTrim(_ blocks: [ChatBlock]) -> [ChatBlock] {
        #if DEBUG
        let seq = UserDefaults.standard.integer(forKey: "fromSeq")
        if seq > 0, let i = blocks.firstIndex(where: { b in
            if case .turn(let t) = b { return t.users.contains { if case .user(let u) = $0 { u.seq == seq } else { false } } }
            return false
        }) {
            var out = Array(blocks[i...])
            // `-dropUsers YES`: hide that turn's messages so its process row and answer fit one screenshot.
            if UserDefaults.standard.bool(forKey: "dropUsers"), case .turn(var t) = out[0] { t.users = []; out[0] = .turn(t) }
            return out
        }
        #endif
        return blocks
    }

    /// 引用 puts the message as a quote in front of the draft; 编辑 replaces the draft with your old message.
    private func bindMenu() {
        menu.quote = { [model, key] text in
            let quoted = text.split(separator: "\n", omittingEmptySubsequences: false).map { "> " + $0 }.joined(separator: "\n")
            let draft = model.drafts[key] ?? ""
            model.drafts[key] = quoted + "\n\n" + draft
        }
        menu.edit = { [model, key] text in model.drafts[key] = text }
    }

    private func switchTo(_ next: SessionKey) {
        guard next != key else { return }
        Haptics.tap()
        model.unsubscribe(key)
        key = next
        bindMenu()
        menu.selected = nil
        if pane == .git || pane == .files { pane = .chat } // the next agent may live in another directory
        git = nil
        position = ScrollPosition(id: Self.bottomID, anchor: .bottom)
        model.subscribe(next)
        model.visibleSession = next
        model.markSeen(next)
        if style == .dev { AppModel.rememberChat(next) }
    }

    private var title: String {
        let t = session?.title ?? ""
        if style == .assistant { return t.isEmpty || t == "Claude Code" ? "Jarvis" : t }
        return t.isEmpty ? "对话" : t
    }

    private var subtitle: String {
        guard let s = session else { return "" }
        if style == .assistant {
            if !model.interactions(for: key).isEmpty { return "等你确认" }
            let launcher = model.assistantLaunchers[key] ?? s.launcher
            return s.status.isActive ? "正在处理…" : ([launcher, Fmt.model(s.model)].compactMap { $0 }.joined(separator: " · "))
        }
        let machine = model.machine(s.machineID)?.name ?? s.machineID
        let place = [s.spaceName, s.slotLabel].compactMap { $0 }.joined(separator: " › ")
        return [machine, place.isEmpty ? nil : place, s.state.label].compactMap { $0 }.joined(separator: " · ")
    }

    @ViewBuilder private func rowView(_ row: ChatRow, isLatest: Bool, live: Bool) -> some View {
        switch row {
        case .user(let i):
            if (i.text ?? "").hasPrefix("/loop") { LoopTickRow(item: i) } else { UserBubble(item: i) }
        case .assistant(let i): AssistantText(item: i)
        case .thinking(let i):
            if style == .dev && live { ThinkingRow(item: i) }
        case .tools(let steps):
            if style == .dev && live { ToolGroupView(steps: steps, isLatest: isLatest) } else { AssistantToolsRow(steps: steps) }
        case .notice(let i): NoticeRow(item: i)
        case .recap(let i): RecapCard(item: i)
        case .image(let i):
            if let p = i.image {
                HStack { if i.role == "user" { Spacer(minLength: 60) }; Base64Image(payload: p).frame(maxWidth: 240) }
            }
        }
    }
}

struct ChatSkeleton: View {
    @State private var on = false
    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(0..<5, id: \.self) { i in
                RoundedRectangle(cornerRadius: 8)
                    .fill(Color.primary.opacity(on ? 0.08 : 0.04))
                    .frame(width: [220, 300, 180, 260, 140][i], height: 14)
                    .frame(maxWidth: .infinity, alignment: i == 0 ? .trailing : .leading)
            }
        }
        .onAppear { withAnimation(.easeInOut(duration: 0.9).repeatForever()) { on = true } }
    }
}

/// Message input: text, photos, quick slash commands; turns into a stop button while Claude works.
struct Composer: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var status: SessionStatus
    var placeholder: String?
    private var text: String {
        get { model.drafts[key] ?? "" }
        nonmutating set { model.drafts[key] = newValue }
    }
    private var textBinding: Binding<String> { Binding(get: { text }, set: { text = $0 }) }
    @State private var picks: [PhotosPickerItem] = []
    @State private var showPhotos = false
    @State private var showCamera = false
    @State private var images: [(UIImage, ImagePayload)] = []
    @FocusState private var focused: Bool

    private var canSend: Bool { !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.isEmpty }

    var body: some View {
        VStack(spacing: 8) {
            if !images.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack {
                        ForEach(Array(images.enumerated()), id: \.offset) { idx, pair in
                            Image(uiImage: pair.0).resizable().scaledToFill().frame(width: 56, height: 56)
                                .clipShape(.rect(cornerRadius: 12, style: .continuous))
                                .overlay(alignment: .topTrailing) {
                                    Button { images.remove(at: idx) } label: {
                                        Image(systemName: "xmark.circle.fill").symbolRenderingMode(.palette)
                                            .foregroundStyle(.white, .black.opacity(0.6))
                                    }
                                    .offset(x: 5, y: -5)
                                }
                        }
                    }
                    .padding(.top, 6)
                }
            }
            HStack(alignment: .bottom, spacing: 8) {
                Menu {
                    // A PhotosPicker inside a Menu never presents (the menu dismisses first): present it from here.
                    Button { showPhotos = true } label: { Label("照片", systemImage: "photo.on.rectangle") }
                    if UIImagePickerController.isSourceTypeAvailable(.camera) {
                        Button { showCamera = true } label: { Label("拍照", systemImage: "camera") }
                    }
                    Section("快捷指令") {
                        ForEach(SlashCommand.all) { cmd in
                            Button { Task { await sendText(cmd.text) } } label: { Label(cmd.title, systemImage: cmd.symbol) }
                        }
                    }
                } label: {
                    Image(systemName: "plus")
                        .font(.body.weight(.semibold))
                        .frame(width: 36, height: 36)
                }
                .themedButton()
                .clipShape(.circle)

                TextField(status.isActive ? "补充说明（会排队发送）" : (placeholder ?? "给 Claude 发消息"), text: textBinding, axis: .vertical)
                    .lineLimit(1...6)
                    .focused($focused)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 9)
                    .surface(interactive: true, in: .rect(cornerRadius: 20, style: .continuous))

                if canSend || !status.isActive {
                    Button {
                        Task { await sendText(text) }
                    } label: {
                        Group {
                            Image(systemName: "arrow.up").font(.body.weight(.bold))
                        }
                        .frame(width: 36, height: 36)
                    }
                    .themedButton(prominent: true)
                    .clipShape(.circle)
                    .disabled(!canSend)
                    .keyboardShortcut(.return, modifiers: .command)
                } else {
                    Button {
                        Task { await model.interrupt(key) }
                    } label: {
                        Image(systemName: "stop.fill").font(.body.weight(.bold)).frame(width: 36, height: 36)
                    }
                    .themedButton(prominent: true)
                    .tint(Theme.danger)
                    .clipShape(.circle)
                    .accessibilityLabel("中断")
                }
            }
        }
        .photosPicker(isPresented: $showPhotos, selection: $picks, maxSelectionCount: 4, matching: .images)
        .fullScreenCover(isPresented: $showCamera) {
            CameraPicker { img in add(img) }.ignoresSafeArea()
        }
        .onChange(of: picks) { _, items in
            guard !items.isEmpty else { return }
            Task { await load(items) }
        }
    }

    private func sendText(_ raw: String) async {
        let t = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty || !images.isEmpty else { return }
        // On screen at once; the request runs in the background (a failure turns the bubble into "重发").
        model.sendNow(key, text: t.isEmpty ? "请看图片" : t, images: images.map(\.1))
        if raw == text { text = "" }
        images = []
    }

    private func load(_ items: [PhotosPickerItem]) async {
        var failed = 0
        for item in items {
            // iCloud-only photos are downloaded here; HEIC and others are re-encoded to JPEG below.
            guard let data = try? await item.loadTransferable(type: Data.self), let img = UIImage(data: data) else {
                failed += 1
                continue
            }
            add(img)
        }
        picks = []
        if failed > 0 {
            model.toast = Toast("有 \(failed) 张照片读不出来（可能还在 iCloud 里没下载完）", style: .error)
        }
    }

    private func add(_ img: UIImage) {
        guard images.count < 4 else {
            model.toast = Toast("一次最多 4 张", style: .error)
            return
        }
        let scaled = img.scaledDown(maxSide: 1600)
        if let jpeg = scaled.jpegData(compressionQuality: 0.75) {
            images.append((scaled, ImagePayload(mime: "image/jpeg", data: jpeg.base64EncodedString())))
            Haptics.tap()
        }
    }
}

struct SlashCommand: Identifiable {
    var title: String
    var text: String
    var symbol: String
    var id: String { text }

    static let all: [SlashCommand] = [
        SlashCommand(title: "继续", text: "继续", symbol: "forward"),
        SlashCommand(title: "压缩上下文 /compact", text: "/compact", symbol: "rectangle.compress.vertical"),
        SlashCommand(title: "查看上下文 /context", text: "/context", symbol: "chart.pie"),
        SlashCommand(title: "提交并推送", text: "把当前改动按规范提交并推送", symbol: "arrow.up.doc"),
        SlashCommand(title: "汇报进展", text: "简要汇报一下目前的进展、还剩什么", symbol: "list.bullet.rectangle"),
    ]
}

extension UIImage {
    func scaledDown(maxSide: CGFloat) -> UIImage {
        let side = max(size.width, size.height)
        guard side > maxSide else { return self }
        let scale = maxSide / side
        let target = CGSize(width: size.width * scale, height: size.height * scale)
        return UIGraphicsImageRenderer(size: target).image { _ in draw(in: CGRect(origin: .zero, size: target)) }
    }
}

/// Full-screen live terminal of the herdr pane, for anything the chat cannot express.
struct TerminalSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let key: SessionKey

    var body: some View {
        NavigationStack {
            TerminalPanel(key: key)
                .padding(.horizontal, 12)
                .padding(.bottom, 8)
                .navigationTitle(model.session(key)?.slotLabel.map { "终端 · \($0)" } ?? "终端")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } }
        }
    }
}

/// Input typed while Claude works and not yet taken, shown above the composer instead of in the chat.
/// Editing pulls it back (like ↑ in the terminal); the other queued messages are re-queued in order.
struct QueueStrip: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var queued: [String]
    @State private var busy = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: "tray.and.arrow.down")
                Text("排队中 \(queued.count) 条 · Claude 忙完会依次处理").lineLimit(1)
                Spacer(minLength: 0)
                if busy { ProgressView().controlSize(.mini) }
            }
            .font(.caption.weight(.semibold))
            .foregroundStyle(.secondary)
            ForEach(Array(queued.enumerated()), id: \.offset) { i, q in
                Menu {
                    Button { act(i, delete: false) } label: { Label("撤回编辑", systemImage: "pencil") }
                    Button { UIPasteboard.general.string = q } label: { Label("拷贝", systemImage: "doc.on.doc") }
                    Button(role: .destructive) { act(i, delete: true) } label: { Label("删除", systemImage: "trash") }
                } label: {
                    HStack(alignment: .top, spacing: 8) {
                        Text("\(i + 1)")
                            .font(.caption2.weight(.bold).monospacedDigit())
                            .foregroundStyle(Theme.accent)
                            .frame(width: 18, height: 18)
                            .background(Theme.accent.opacity(0.14), in: .circle)
                        Text(q).font(.subheadline).foregroundStyle(Color.primary).lineLimit(2).multilineTextAlignment(.leading)
                        Spacer(minLength: 0)
                        Image(systemName: "ellipsis").font(.caption).foregroundStyle(.secondary).padding(.top, 3)
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .background(Color.primary.opacity(0.05), in: .rect(cornerRadius: 12, style: .continuous))
                }
                .disabled(busy)
            }
        }
        .padding(10)
        .surface(in: .rect(cornerRadius: 18, style: .continuous))
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    private func act(_ i: Int, delete: Bool) {
        busy = true
        Task {
            await model.editQueued(key, index: i, delete: delete)
            busy = false
        }
    }
}

/// System camera for "拍照" in the composer.
struct CameraPicker: UIViewControllerRepresentable {
    var onImage: (UIImage) -> Void
    @Environment(\.dismiss) private var dismiss

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let vc = UIImagePickerController()
        vc.sourceType = .camera
        vc.delegate = context.coordinator
        return vc
    }

    func updateUIViewController(_ vc: UIImagePickerController, context: Context) {}

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        let parent: CameraPicker
        init(_ parent: CameraPicker) { self.parent = parent }

        func imagePickerController(_ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
            if let img = info[.originalImage] as? UIImage { parent.onImage(img) }
            parent.dismiss()
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) { parent.dismiss() }
    }
}

/// Developer facts about a session, shown under the title: model, context in use, git branch, Claude Code version…
struct DevInfoCard: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var session: Session?
    @State private var info: APIClient.SessionInfo?
    @State private var error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let info {
                Grid(alignment: .leadingFirstTextBaseline, horizontalSpacing: 12, verticalSpacing: 8) {
                    row("cpu", "模型", [Fmt.model(info.model) ?? info.model, info.launcher].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                    row("gauge.with.dots.needle.33percent", "上下文", Self.tokens(info.contextTokens) + " tokens" + (info.compactions > 0 ? " · 压缩过 \(info.compactions) 次" : ""))
                    row("arrow.triangle.branch", "分支", info.gitBranch ?? "—")
                    row("folder", "目录", Fmt.path(info.cwd ?? session?.cwd ?? "", home: model.machine(key.machineID)?.home))
                    row("macbook", "位置", [model.machine(key.machineID)?.name, session?.spaceName, session?.slotLabel].compactMap { $0 }.joined(separator: " › "))
                    row("text.bubble", "对话", "\(info.turns) 轮 · 共生成 \(Self.tokens(info.outputTokens)) tokens")
                    row("lock.open", "权限", info.permissionMode ?? "—")
                    row("shippingbox", "Claude Code", info.version ?? "—")
                    if let started = info.startedAt, started.isMeaningful {
                        row("clock", "开始", started.formatted(date: .abbreviated, time: .shortened))
                    }
                }
                HStack(spacing: 8) {
                    Button {
                        UIPasteboard.general.string = info.sessionID
                        Haptics.success()
                        model.toast = Toast("已拷贝 session id", style: .success)
                    } label: { Label("拷贝 ID", systemImage: "number").lineLimit(1) }
                    Button {
                        UIPasteboard.general.string = "cd \(info.cwd ?? "") && claude --resume \(info.sessionID)"
                        Haptics.success()
                        model.toast = Toast("已拷贝恢复命令", style: .success)
                    } label: { Label("拷贝恢复命令", systemImage: "terminal").lineLimit(1) }
                }
                .themedButton()
                .controlSize(.small)
            } else if let error {
                Label(error, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger)
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(in: .rect(cornerRadius: 20, style: .continuous))
        .task(id: key) {
            do { info = try await model.api?.sessionInfo(key) } catch { self.error = error.shown }
        }
    }

    private func row(_ symbol: String, _ label: String, _ value: String) -> some View {
        GridRow {
            Label(label, systemImage: symbol)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .gridColumnAlignment(.leading)
            Text(value.isEmpty ? "—" : value)
                .font(.caption.monospaced())
                .lineLimit(2)
                .truncationMode(.middle)
                .textSelection(.enabled)
        }
    }

    static func tokens(_ n: Int) -> String {
        switch n {
        case 1_000_000...: String(format: "%.2fM", Double(n) / 1_000_000)
        case 1_000...: String(format: "%.1fk", Double(n) / 1_000)
        default: "\(n)"
        }
    }
}

/// A finished exchange: messages, the folded process, the answer.
/// One line of the chat. A turn renders as: its messages (queued ones gathered), a thin "正在工作 39 秒 / 已工作 5 分"
/// line that folds the work, then — when open — the agent's words, narration in gray and each stretch of tool calls
/// as "调用了 N 个工具"; finished turns are folded and keep just the answer. Lines are separate lazy rows on purpose.
enum ChatLine: Identifiable {
    case row(ChatRow)
    case header(Turn, live: Bool, open: Bool)
    case tools(id: String, steps: [ToolStep], running: Bool)

    var id: String {
        switch self {
        case .row(let r): r.id
        case .header(let t, _, _): "hdr-" + t.id
        case .tools(let id, _, _): "tc-" + id
        }
    }

    /// The message row it is (for lifting the one whose action bar is open).
    var rowID: String? { if case .row(let r) = self { r.id } else { nil } }
}

/// "正在工作 39 秒 ——— ⌄" (ticking) or "已工作 5 分 12 秒 ——— ⌄"; tapping folds / unfolds the work.
struct WorkHeader: View {
    var live: Bool
    var start: Date?
    var end: Date?
    var open: Bool
    var openTerminal: (() -> Void)?
    var toggle: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Button(action: toggle) {
                HStack(spacing: 8) {
                    if live {
                        TimelineView(.periodic(from: .now, by: 1)) { ctx in
                            Text("正在工作 " + Self.duration(from: start, to: ctx.date))
                        }
                        .foregroundStyle(Theme.working)
                    } else {
                        Text(start != nil && end != nil ? "已工作 " + Self.duration(from: start, to: end ?? .now) : "已完成")
                    }
                    Rectangle().fill(Color.secondary.opacity(0.25)).frame(height: 0.5)
                    Image(systemName: "chevron.down")
                        .font(.caption2.weight(.semibold))
                        .rotationEffect(.degrees(open ? 0 : -90))
                }
                .font(.footnote.monospacedDigit())
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(open ? "收起过程" : "展开过程")
            if live, let openTerminal {
                Button(action: openTerminal) {
                    Image(systemName: "terminal").font(.footnote).foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("打开终端")
            }
        }
    }

    static func duration(from start: Date?, to end: Date) -> String {
        guard let start else { return "" }
        let s = max(0, Int(end.timeIntervalSince(start)))
        if s < 60 { return "\(s) 秒" }
        if s < 3600 { return s % 60 == 0 ? "\(s / 60) 分" : "\(s / 60) 分 \(s % 60) 秒" }
        return "\(s / 3600) 小时 \(s % 3600 / 60) 分"
    }
}

/// The work between the agent's words as one quiet line, "调用了 3 个工具，思考 2 次 ›"; tap to see each step.
struct ToolCountRow: View {
    var steps: [ToolStep]
    var running: Bool
    @State private var expanded = false

    var body: some View {
        let calls = steps.reduce(0) { if case .call = $1 { $0 + 1 } else { $0 } }
        let thoughts = steps.count - calls
        let busy = running && steps.contains { if case .call(let c) = $0 { c.isRunning } else { false } }
        VStack(alignment: .leading, spacing: 10) {
            Button {
                Haptics.tap()
                withAnimation(.snappy) { expanded.toggle() }
            } label: {
                HStack(spacing: 6) {
                    if busy { ProgressView().controlSize(.mini) }
                    Text(Self.label(calls: calls, thoughts: thoughts, busy: busy))
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .rotationEffect(.degrees(expanded ? 90 : 0))
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if expanded {
                ToolGroupView(steps: steps, isLatest: running)
                    .transition(.opacity)
            }
        }
    }

    static func label(calls: Int, thoughts: Int, busy: Bool) -> String {
        if busy { return "正在调用工具 · 第 \(calls) 个" + (thoughts > 0 ? "，思考 \(thoughts) 次" : "") }
        var parts: [String] = []
        if calls > 0 { parts.append("调用了 \(calls) 个工具") }
        if thoughts > 0 { parts.append("思考 \(thoughts) 次") }
        return parts.joined(separator: "，")
    }
}
