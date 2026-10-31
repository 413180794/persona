import BeautifulUI
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
    @Environment(\.scenePhase) private var scenePhase
    /// The session on screen; the sibling strip can switch it to another agent in the same space.
    @State private var key: SessionKey
    var style: ChatStyle = .dev
    @State private var position = ScrollPosition(edge: .top) // .top is the newest end of the flipped list
    /// The bottom sentinel is on screen. Starts false: if the first jump lands in unrendered space the arrow is the
    /// way out, and `settleAtEnd` retries until the sentinel has been seen.
    @State private var atBottom = false
    /// Hide the arrow for the first moment on entry (it would flash while the list settles at the end).
    @State private var settled = false
    @State private var showTerminal = false
    @State private var file: FileRef?
    @State private var showActivity = false
    @State private var debugImages: [ImagePayload]?
    @State private var showInfo = false
    @State private var showLink = false
    @State private var pane: SessionPane = .chat
    @State private var git: GitStatus?
    @State private var menu = MessageMenu()
    /// Work lines the user tapped open (by line id; kept here so lazy rows coming back keep their state).
    @State private var openWork: Set<String> = []
    /// Runs of tool calls (`ChatLine.toolRun`) opened into a row per call.
    @State private var openRuns: Set<String> = []
    /// While you read history, the chat is drawn from the items it had when you left the newest end (inserting at the
    /// newest end of the flipped list would push what you read); history loaded at the far end, and holes filled
    /// behind it, still show. Cleared when you are back at the newest. It used to keep the lines up to the newest
    /// line's id, but those ids change as a turn goes on (the answer folds into the work line, a finished turn's work
    /// lines merge), and losing the id let everything in at once.
    @State private var frozen: Frozen?

    struct Frozen: Equatable {
        var seq: Int
        var part: Int
        /// The turn that was running: its lines stay as they were drawn (live work line, answer not yet picked).
        var live: String?
    }
    /// The user dragged the list at least once: only then does reaching the top load older pages (a lazy stack may
    /// lay out the top row while settling at the bottom, which used to chain-load history on entry).
    @State private var userScrolled = false
    /// Stick to the latest message: on while you are at the end, off as soon as you drag the list (reading history is
    /// never interrupted), on again when you come back to the end, tap ↓ or send. Jumps and their retries only run
    /// while it is on — a lone jump into rows the lazy stack never laid out lands on an earlier message.
    @State private var followsEnd = true
    @State private var entered = false
    /// Where to land on entry, once the lines are there: the start of the latest answer (finished, unread) or where
    /// you left off (finished, read).
    @State private var entry: Entry?
    /// Lines on screen (for remembering where you were).
    /// Bookkeeping written while scrolling (visible rows, finger down) and the cached lines: kept out of SwiftUI's
    /// state so writing it never re-renders the page.
    @State private var scratch = ChatScratch()
    /// Keeps a tapped header in place while what it opens settles (`ChatPin`).
    @State private var pin = ChatPin()
    /// Work lines just opened, until when their steps reveal themselves as they appear (they arrive from the hub after
    /// the tap); and ones folding away before they close (`Unfolding`).
    @State private var revealUntil: [String: Date] = [:]
    @State private var concealing: Set<String> = []

    private enum Entry: Equatable { case latestAnswer, line(String) }
    /// The line being landed on, measured while landing (only that one row watches its frame).
    @State private var landing: String?

    private var readKey: String { "chat.read.\(shownKey.machineID)/\(shownKey.sessionID)" }
    @State private var renaming: Session?
    @State private var closing: Session?
    /// The turn whose "后台任务完成" drawer is open.
    @State private var wakeDrawer: String?
    @Environment(\.dismiss) private var dismissChat
    /// In the detail column of a split (iPhone Duo open): the list is beside the chat, the tab bar stays.
    @Environment(\.splitDetail) private var split
    /// The system presents bars down the side of the screen (iPhone Duo closed, or open in landscape).
    @State private var barsVertical = false
    /// The slim header's height while the actions are in the side column (`sideHeader`).
    @State private var sideHeaderHeight: CGFloat = 0
    /// Safari-style chrome (title row, agent strip, tabs): it follows the finger as the list scrolls and settles
    /// open or folded when the finger lifts. Its own object so the per-frame updates only redraw the header.
    @State private var chrome = ChatChrome()
    @Environment(\.openSpace) private var openSpace
    @State private var link: LinkTarget?
    /// The image viewer, shown by the page rather than the row it was opened from (`viewImages`).
    @State private var viewingImages: ImageViewing?
    @Namespace private var imageZoom
    /// Tool calls the user opened or closed (`openTools`).
    @State private var openTools = OpenTools()
    /// What the environment's actions call (`ChatActions`).
    @State private var actions = ChatActions()

    init(key: SessionKey, style: ChatStyle = .dev) {
        _key = State(initialValue: key)
        self.style = style
    }

    /// A subagent of this session being looked at instead of the main agent (read-only, same message UI).
    @State private var viewing: Subagent?
    /// This view's claim on its subscriptions and on `visibleSession` (another view of the same chat has its own).
    @State private var owner = UUID()

    /// What the chat shows: the main agent, or the subagent picked in the agent strip.
    private var shownKey: SessionKey { Self.shownKey(key, viewing: viewing) }

    /// The session whose items are on screen: the subagent's own transcript while one is open (its items, images and
    /// full texts are fetched from it, not from the main session).
    static func shownKey(_ key: SessionKey, viewing: Subagent?) -> SessionKey {
        viewing.map { SessionKey(machineID: key.machineID, sessionID: $0.sessionID) } ?? key
    }
    private var store: ChatStore { model.chat(shownKey) }
    private var session: Session? { model.session(key) }
    /// The latest state of the subagent being viewed (the list refreshes with the session).
    private var viewedAgent: Subagent? { viewing.flatMap { v in session?.subagents?.first { $0.id == v.id } ?? v } }
    private var status: SessionStatus {
        if let a = viewedAgent { return a.running ? .working : .idle }
        return session?.status ?? .unknown
    }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("ChatView")
        #endif
        let _ = actions.set(handlers)
        swipeablePanes
        // A readable measure on a wide page (iPhone Duo open, nothing beside the chat); a no-op on other iPhones.
        .frame(maxWidth: Self.readableWidth)
        // Where a message's action bar may go (MessageMenu): this chat, not the whole screen.
        .coordinateSpace(.named(MessageMenu.area))
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { menu.areaWidth = $0 }
        .frame(maxWidth: .infinity)
        .background(Theme.pageBackground)
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        // Agents chats draw their own header (devChrome) so it can follow the scroll; the assistant's floats over the
        // chat (AssistantTopBar, drawn by AssistantView over this page and the new-conversation page). Where the system
        // puts bars down the side (iPhone Duo), an Agents chat hands its actions to it instead: they go in that column.
        .toolbarVisibility(sideBars ? .visible : .hidden, for: .navigationBar)
        .navigationBarBackButtonHidden(sideBars)
        .toolbar { if sideBars { sideBarItems } }
        .readsVerticalBars($barsVertical)
        .overlay(alignment: .top) {
            if style == .dev { if sideBars { sideHeader } else { devChrome } }
        }
        // The session's details come up from the bottom (2026-10-31: the card that dropped from the title covered the
        // chat and needed a second tap on the title; a sheet closes with a swipe or a tap above it).
        #if os(iOS)
        .sheet(isPresented: $showInfo) {
            SessionInfoSheet(key: key, session: session)
                .presentationDetents([.medium, .large])
                .presentationDragIndicator(.visible)
        }
        #else
        // On the Mac the details are an inspector beside the chat (Agent › 显示会话信息 ⌥⌘I, or the title).
        .macInspector(isPresented: $showInfo) {
            SessionInfoSheet(key: key, session: session)
        }
        .onChange(of: MacShellState.shared.inspector) { _, t in
            if let t, t.key == key { showInfo.toggle() }
        }
        #endif
        .task(id: "\(key.machineID)/\(key.sessionID)|\(session?.cwd ?? "")") { await loadGit() }
        // Left alone for a few seconds, the open chrome folds away by itself (the page goes back to being all chat).
        .task(id: style == .dev && pane == .chat) {
            guard style == .dev, pane == .chat else { return }
            scratch.lastActivity = .now
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(500))
                if chrome.collapse == 0, !showInfo, !scratch.userDragging,
                   Date.now.timeIntervalSince(scratch.lastActivity) >= Self.chromeIdleSeconds {
                    hideChrome()
                }
            }
        }
        .onChange(of: pane) { _, p in
            showChrome()
            // The chat's scroll view is rebuilt when you come back to it: start at the latest message.
            if p == .chat {
                position = ScrollPosition(edge: .top)
                atBottom = false
                settled = false
                followsEnd = true
                settleAtEnd()
            }
        }
        .onChange(of: status) { _, st in if !st.isActive { Task { await loadGit() } } }
        // One stack: the chat takes the whole screen. Beside the list (split) the tab bar stays, or there is no way out.
        .hidesTabBarWhenShown(style == .dev && split == nil)
        .fullScreenCover(isPresented: $showTerminal) { TerminalCover(key: key, switchTo: switchTo).presentationBackground(.clear) }
        .renameSessionAlert($renaming)
        .confirmClose($closing)
        .sheet(item: $file) { ref in FileViewer(ref: ref) }
        #if DEBUG
        // Simulator automation: `-openFirstImage YES` opens the viewer on the first image once it loads.
        .onChange(of: store.rows.count) { _, _ in
            guard UserDefaults.standard.bool(forKey: "openFirstImage"), debugImages == nil else { return }
            let imgs = store.items.compactMap(\.image)
            if !imgs.isEmpty { debugImages = imgs }
        }
        .fullScreenCover(isPresented: Binding(get: { debugImages != nil }, set: { if !$0 { debugImages = [] } })) {
            if let imgs = debugImages, !imgs.isEmpty {
                ImageViewer(images: imgs, index: imgs.count - 1, conversation: imgs) { img in
                    model.composerInbox[key, default: []].append(img)
                }
            }
        }
        #endif
        .environment(\.openFile, actions.openFile)
        .environment(\.fileScope, FileScope(machine: key.machineID, cwd: session?.cwd ?? ""))
        .environment(\.chatImages, store.images)
        .environment(\.viewImages, actions.viewImages)
        .environment(\.imageZoom, imageZoom)
        .environment(\.openTools, openTools)
        .fullScreenCover(item: $viewingImages) { v in
            ImageViewer(images: v.images, index: v.index, conversation: v.conversation) { [model, key] img in
                model.composerInbox[key, default: []].append(img)
            }
            .navigationTransition(.zoom(sourceID: v.source, in: imageZoom))
        }
        .environment(\.openLink, actions.openLink)
        // Web links open in the in-app sheet (Feishu docs in the native reader); anything else as before.
        .environment(\.openURL, actions.openURL)
        #if os(macOS)
        .macLinkPanel($link, key: key) // the Mac's panel at the trailing edge (Mac/Link/MacLinkPanel.swift)
        #else
        .sheet(item: $link) { t in LinkSheet(target: t, key: key) }
        #endif
        .environment(\.attachImage, actions.attachImage)
        .environment(\.loadImage, actions.loadImage)
        .environment(\.loadFullText, actions.loadFullText)
        .onAppear(perform: appeared)
        // The same chat was already open (the app left on it) when its "完成" notification was tapped: no new entry.
        .onChange(of: model.latestAnswerFor) { _, k in
            guard style == .dev, k == key, entered, takeLatestAnswer() else { return }
            entry = .latestAnswer
            landEntry(currentLines())
        }
        // Going to the background is the last chance before iOS may end the app: a cold relaunch (the dev chat is
        // restored, AppModel.rememberChat) then opens at the same line, not only after leaving the chat.
        .onChange(of: scenePhase) { _, phase in if phase == .background { rememberReadPosition() } }
        .onDisappear {
            rememberReadPosition()
            if viewing != nil { model.unsubscribe(shownKey, by: owner) }
            model.unsubscribe(key, by: owner)
            model.hide(by: owner)
        }
        .onChange(of: session?.state) { _, st in
            if st == .unread && model.visibleSession == key { model.markSeen(key) }
        }
    }

    /// The chat as a flat list of lines (see `ChatLine`): every message of a turn opens its own exchange — the
    /// message, one work line for what the agent did after it, and the agent's last words before the next message.
    /// The chat's lines, rebuilt only when what they come from changed: the store's blocks, the running turn and
    /// which work lines are open. (Every re-render used to walk the whole history again.)
    private func currentLines() -> [ChatLine] {
        let items = store.items
        // Frozen: the items up to the snapshot (they are in transcript order); what comes after waits.
        let cut = frozen.map { f in Self.prefixCount(items) { ($0.seq, $0.part) <= (f.seq, f.part) } } ?? items.count
        let key = ChatScratch.LinesKey(store: "\(shownKey.machineID)/\(shownKey.sessionID)", version: frozen == nil ? store.version : cut,
                                       first: items.first?.id, live: frozen == nil ? liveTurnID : frozen?.live, open: openWork,
                                       runs: openRuns, frozen: frozen)
        if scratch.linesKey == key { return scratch.lines }
        var blocks = store.blocks
        if frozen != nil {
            let built = ChatStore.rebuild(Array(items[..<cut]), settled: scratch.frozenSettled)
            scratch.frozenSettled = built.settled
            blocks = built.blocks
        }
        let lines = lineBuilder.build(Self.debugTrim(blocks), live: key.live)
        scratch.lines = lines
        scratch.linesKey = key
        return lines
    }

    /// What builds the lines from the blocks, with what is open now.
    private var lineBuilder: ChatLines {
        ChatLines(openWork: openWork, openAll: Self.debugOpenAll, openRuns: openRuns, chipRuns: AppStyle.current.usesBeautifulUI,
                  foldsIntoRun: foldsIntoRun)
    }
    private func unfoldWork(_ lines: [ChatLine]) -> [ChatLine] { lineBuilder.unfold(lines) }
    private func isOpen(_ work: String) -> Bool { lineBuilder.isOpen(work) }
    private func isRunOpen(_ id: String) -> Bool { openRuns.contains(id) }
    private func runID(holding call: String, group: String, items: [WorkItem]) -> String? {
        lineBuilder.runID(holding: call, group: group, items: items)
    }
    private func foldsIntoRun(_ call: ToolCall) -> Bool {
        guard let id = call.use.toolUseID else { return true }
        return !store.tasks.started.contains(id) && store.tasks.notes[id] == nil
    }

    static func foldAPIErrors(_ lines: [ChatLine]) -> [ChatLine] { ChatLines.foldAPIErrors(lines) }

    /// How many leading elements satisfy `pred` (true for a prefix, then false): a binary search.
    static func prefixCount(_ items: [Item], _ pred: (Item) -> Bool) -> Int {
        var lo = 0, hi = items.count
        while lo < hi {
            let mid = (lo + hi) / 2
            if pred(items[mid]) { lo = mid + 1 } else { hi = mid }
        }
        return lo
    }

    #if DEBUG
    /// Opens and folds a work line from its header, then from 收起 under its steps, logging how far the header and the
    /// reply under the steps moved on screen (both should be 0).
    private func foldProbe() async {
        func pause() async { try? await Task.sleep(for: .seconds(1.5)) }
        // Puts a line's top at a fraction of the list on screen: jump near, then correct by what is measured.
        func bring(_ id: String, to f: CGFloat) async {
            position.scrollTo(id: id, anchor: UnitPoint(x: 0.5, y: 1 - f))
            for _ in 0..<6 {
                try? await Task.sleep(for: .milliseconds(250))
                guard let now = scratch.lineY[id], Set(scratch.visibleLines).contains(id) else { continue }
                let target = scratch.listFrame.minY + scratch.listFrame.height * f
                if abs(target - now) < 1 { break }
                scrollList(to: chrome.offset + (target - now))
            }
            await pause()
        }
        try? await Task.sleep(for: .seconds(6))
        var log: [String] = []
        for kind in ["w-", "wt-"] {
            // The newest work line of this kind with steps, brought into view.
            let newest = currentLines().last { if case .work(let id, let items, _, _, _) = $0.content { id.hasPrefix(kind) && !items.isEmpty } else { false } }
            guard let w = newest?.id else { log.append("no \(kind) work line"); continue }
            await bring(w, to: 0.3)
            guard let y0 = scratch.workHeadY[w], Set(scratch.visibleLines).contains(w) else { log.append("\(w) not on screen"); continue }
            toggleWork(w, fromHeader: true); await pause()
            let y1 = scratch.workHeadY[headLine(w, open: true)] ?? .nan
            toggleWork(w, fromHeader: true); await pause()
            let y2 = scratch.workHeadY[w] ?? .nan
            log.append("header \(w) at \(Int(y0)): opening moved it \(Int(y1 - y0)), folding \(Int(y2 - y0))")
            guard kind == "w-" else { continue }
            toggleWork(w, fromHeader: true); await pause()
            let lines = currentLines()
            guard let e = lines.firstIndex(where: { $0.id == w + "-end" }), e + 1 < lines.count else { log.append("no line after the steps"); continue }
            let reply = lines[e + 1].id
            await bring(w + "-end", to: 0.4)
            let r0 = scratch.lineY[reply] ?? .nan, end0 = scratch.lineY[w + "-end"] ?? .nan
            toggleWork(w, fromHeader: false); await pause()
            let r1 = scratch.lineY[reply] ?? .nan
            log.append("收起 at \(Int(end0)), reply at \(Int(r0)): folding moved the reply \(Int(r1 - r0)); header now \(Int(scratch.workHeadY[w] ?? .nan))")
        }
        if let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first {
            try? log.joined(separator: "\n").write(to: dir.appending(path: "fold.txt"), atomically: true, encoding: .utf8)
        }
        // Left open with 收起 in the middle of the screen, for screenshots.
        if let w = currentLines().last(where: { if case .work(let id, let items, _, _, _) = $0.content { id.hasPrefix("w-") && !items.isEmpty } else { false } })?.id {
            toggleWork(w, fromHeader: false); await pause()
            await bring(w + "-end", to: 0.5)
        }
    }
    #endif

    #if DEBUG
    /// Scrolls into the history while the agent keeps working (`MOCK_STREAM=1`), then logs, every 2 s, where a line on
    /// screen is and how many items have arrived (the line must not move); back at the newest, the new lines show.
    private func freezeProbe() async {
        try? await Task.sleep(for: .seconds(8)) // a big chat takes a moment to load and lay out
        userScrolled = true
        followsEnd = false
        scrollList(to: chrome.offset + 260)
        try? await Task.sleep(for: .seconds(1.5))
        var log: [String] = []
        defer {
            if let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first {
                try? log.joined(separator: "\n").write(to: dir.appending(path: "freeze.txt"), atomically: true, encoding: .utf8)
            }
        }
        let mid = scratch.listFrame.midY
        // The measured line nearest the middle of the list (rows report their position while on screen).
        let onScreen = scratch.lineY.filter { scratch.listFrame.minY < $0.value && $0.value < scratch.listFrame.maxY }.map(\.key)
        guard let id = onScreen.min(by: { abs((scratch.lineY[$0] ?? .infinity) - mid) < abs((scratch.lineY[$1] ?? .infinity) - mid) }),
              let y0 = scratch.lineY[id] else {
            log.append("no line on screen (visible \(scratch.visibleLines.count), measured \(scratch.lineY.count), list \(scratch.listFrame), lines \(currentLines().count))")
            return
        }
        let n0 = store.items.count
        log.append("watching \(id) at \(Int(y0)) with \(n0) items")
        var moved = 0
        for _ in 0..<8 {
            try? await Task.sleep(for: .seconds(2))
            let y = scratch.lineY[id] ?? .nan
            moved = max(moved, Int(abs(y - y0)))
            log.append("items \(store.items.count), line at \(Int(y)) (moved \(Int(y - y0)))")
        }
        followsEnd = true
        scrollToEnd()
        try? await Task.sleep(for: .seconds(2))
        let newest = currentLines().last?.id ?? ""
        let newestY = scratch.lineY[newest] ?? .nan
        log.append("RESULT: \(store.items.count - n0) items arrived while reading, line moved at most \(moved)pt; back at the newest: last line \(newest) on screen \(scratch.listFrame.minY < newestY && newestY < scratch.listFrame.maxY)")
    }
    #endif

    /// What this render's actions do (`ChatActions`).
    private var handlers: ChatActions.Handlers {
        ChatActions.Handlers(
            openFile: { [key, session] path in file = FileRef(machineID: key.machineID, path: path, cwd: session?.cwd) },
            openLink: { link = LinkTarget(url: $0) },
            viewImages: { viewingImages = $0 },
            attachImage: { [model, key] img in model.composerInbox[key, default: []].append(img) },
            loadImage: { [model, key] path in
                guard let f = try? await model.api?.file(machine: key.machineID, path: path, cwd: nil), let data = f.data else { return nil }
                return ImagePayload(mime: f.mime ?? "image/jpeg", data: data)
            },
            // The session on screen: a subagent's items live in its own transcript.
            loadFullText: { [model, shownKey] id in
                guard let api = model.api else { throw CancellationError() }
                return try await api.fullItem(shownKey, id: id).text ?? ""
            },
            openSubagent: { toolUseID in
                if let a = session?.subagents?.first(where: { $0.toolUseID == toolUseID }) { view(a) }
            },
            openFold: openFold,
            jumpToCall: { jumpToCall($0) })
    }

    /// Folds or opens a work line without moving what you were reading. In the upside-down list the newest end holds
    /// still on its own: folding from 收起 under the last step leaves the reply below it where it was. From the header,
    /// the header stays under the finger instead (what is above it holds still; the reply comes up to meet it).
    /// Scrolls to a vertical offset, keeping the horizontal one (see `ChatScratch.offsetX`).
    private func scrollList(to y: CGFloat) {
        position.scrollTo(point: CGPoint(x: scratch.offsetX, y: y))
    }

    /// A merged multi-message turn ("wt-…") opens into its messages under a "N 条消息 · 收起" line instead.
    private func headLine(_ id: String, open: Bool) -> String { id.hasPrefix("wt-") && open ? id + "-h" : id }

    /// Folds or opens a work line. From its header, the header stays under the finger (`ChatPin`), through the steps
    /// arriving from the hub after the tap; from 收起 under the last step, the flipped list keeps the reply below where
    /// it was on its own. No animation: hundreds of rows animating out left blank frames behind.
    private func toggleWork(_ id: String, fromHeader: Bool, run: Bool = false) {
        Haptics.tap()
        let wasOpen = run ? isRunOpen(id) : isOpen(id)
        guard run || !wasOpen else { return concealWork(id, fromHeader: fromHeader) }
        if !run { revealUntil[id] = .now.addingTimeInterval(Self.stepsArrive) }
        changeWork(id, fromHeader: fromHeader, run: run, wasOpen: wasOpen)
    }

    /// How long after opening a work line its steps may still arrive (and reveal themselves): the pin's hold.
    private static let stepsArrive: TimeInterval = 1.5

    /// Closing a work line: its steps fold away first, then go.
    private func concealWork(_ id: String, fromHeader: Bool) {
        concealing.insert(id)
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(Theme.Motion.concealTime))
            concealing.remove(id)
            revealUntil[id] = nil
            changeWork(id, fromHeader: fromHeader, run: false, wasOpen: true)
        }
    }

    private func changeWork(_ id: String, fromHeader: Bool, run: Bool, wasOpen: Bool) {
        if fromHeader { pin.hold(headLine(id, open: !wasOpen), from: headLine(id, open: wasOpen), slow: true) }
        var t = Transaction()
        t.disablesAnimations = true
        withTransaction(t) {
            if run {
                if openRuns.contains(id) { openRuns.remove(id) } else { openRuns.insert(id) }
            } else if openWork.contains(id) { openWork.remove(id) } else { openWork.insert(id) }
        }
        #if PERF_SIGNPOST
        PerfTrace.event("Work toggle", "open=\(!wasOpen) held=\(fromHeader)")
        #endif
    }

    /// A step of the work line `group`, as it opens and closes: steps appearing just after it opened reveal themselves.
    private func stepUnfolding(_ group: String) -> Unfolding {
        Unfolding(revealing: (revealUntil[group] ?? .distantPast) > .now, concealing: concealing.contains(group))
    }

    /// A work line's header moved (its top on screen): kept for the fold probe, and told to the pin. The wake
    /// drawer draws its own lines ("dw-…") with this too; the chat's list has none of those, so the pin never moves it.
    private func workHeadMoved(_ id: String, _ spot: ChatPin.Spot) {
        scratch.workHeadY[id] = spot.y
        pin.moved(id, to: spot)
    }

    /// Where a work line's header is, by its top: the open header is taller than the folded one, and held by its
    /// middle it pushed what is above it 10 pt up. The content is upside down: the top on screen is its far edge there.
    nonisolated private static func headSpot(_ g: GeometryProxy) -> ChatPin.Spot {
        ChatPin.Spot(y: g.frame(in: .global).minY, content: g.frame(in: .named(ChatPin.space)).maxY)
    }

    @ViewBuilder private func lineView(_ line: ChatLine) -> some View {
        switch line.content {
        case .row(let r): rowView(r, isLatest: false, live: true)
        case .apiError(let item, let info, let count):
            // 重试 only when the error is the last thing, the agent is not working (someone may have retried already) and
            // trying again can help (not a login or a bad request).
            APIErrorCard(info: info, count: count, at: item.ts,
                         retry: info.transient != false && !status.isActive && currentLines().last?.id == line.id
                             ? { model.sendNow(key, text: "继续") } : nil)
        case .userImages(_, let items):
            // Close to the bubble below it, like the photos on a message being sent (the list's spacing is 14).
            SentPhotos(images: items.compactMap(\.image)).padding(.bottom, -8)
        case .mergedUsers(_, let rows):
            MergedUserBubble(items: rows.compactMap { if case .user(let i) = $0 { i } else { nil } })
        case .splitHeader(let id, let count):
            Button {
                toggleWork(id, fromHeader: true)
            } label: {
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: "rectangle.split.1x2").font(Theme.Font.label)
                    Text("已按 \(count) 条消息拆开 · 收起").lineLimit(1).fixedSize()
                    Image(systemName: Theme.Icon.collapse).font(Theme.Font.labelStrong)
                }
                .font(Theme.Font.caption)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .trailing)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .onGeometryChange(for: ChatPin.Spot.self, of: Self.headSpot) { workHeadMoved(id + "-h", $0) }
        case .work(let id, let items, let live, let start, let end):
            WorkLine(items: items, live: live, start: start, end: end, open: isOpen(id),
                     openTerminal: live && style == .dev ? { showTerminal = true } : nil) {
                toggleWork(id, fromHeader: true)
            }
            .onGeometryChange(for: ChatPin.Spot.self, of: Self.headSpot) { workHeadMoved(id, $0) }
        case .workStep(let group, let item, let first):
            WorkStepView(item: item, first: first).equatable()
                .unfolding(stepUnfolding(group))
        case .toolRun(let id, let calls, let open, let first):
            ToolRunRow(calls: calls, open: open, first: first) { toggleWork(id, fromHeader: true, run: true) }
                .equatable()
                .unfolding(stepUnfolding(id.components(separatedBy: "/run-")[0])) // a run's id is "<work line>/run-<first call>"
                .onGeometryChange(for: ChatPin.Spot.self, of: Self.headSpot) { workHeadMoved(id, $0) }
        case .workEnd(let group, let count):
            // The foot of the open work line's card.
            Button { toggleWork(group, fromHeader: false) } label: {
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: Theme.Icon.collapse).font(Theme.Font.labelStrong)
                    Text("收起 \(count) 步").lineLimit(1).fixedSize()
                }
                .font(Theme.Font.caption)
                .foregroundStyle(.secondary)
                .padding(.leading, WorkCardSlice.contentLeading)
                .padding(.trailing, WorkCardSlice.inset)
                .padding(.vertical, Theme.Space.m)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background { WorkCardSlice(top: false, bottom: true) }
                .overlay(alignment: .top) { WorkCardSlice.separator }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("收起过程")
            .accessibilityIdentifier("work-fold-bottom")
            .unfolding(stepUnfolding(group))
        case .wakes(let t, let live):
            WakeChip(notices: t.wakes, live: live) {
                Haptics.tap()
                wakeDrawer = t.id
            }
        case .recaps(_, let items):
            RecapCard(items: items)
        }
    }

    /// The drawer's content: the results — each task's notice and what the agent answered it — in order; the steps in
    /// between (tools, thoughts, its narration) fold into one line at the end, closed. Opening the drawer's work used
    /// to unfold every task's steps at once, results and process mixed.
    /// The drawer's lines: each wake (its task reports, then what the agent answered to them), **newest wake first**
    /// (2026-10-31: with many reports the latest one sat at the bottom of a long drawer), then the folded work.
    private func wakeLines(_ t: Turn, live: Bool) -> [ChatLine] {
        var groups: [[ChatLine]] = []
        var process: [ChatRow] = []
        var chunk: [ChatRow] = []
        func flush() {
            let answer = chunk.lastIndex { if case .assistant = $0 { true } else { false } }
            var group: [ChatLine] = []
            for (i, r) in chunk.enumerated() where i != answer {
                if case .notice(let n) = r, n.task != nil || n.wake == true { group.append(.row(r)) } else { process.append(r) }
            }
            if let answer { group.append(.row(chunk[answer])) }
            if !group.isEmpty { groups.append(group) }
            chunk = []
        }
        for r in t.afterWake {
            if case .notice(let n) = r, n.wake == true { flush() } // each wake starts its own answer
            chunk.append(r)
        }
        flush()
        var results = Array(groups.reversed().joined())
        let items = ChatLines.workItems(process)
        if !items.isEmpty || live {
            results.append(.work(id: "dw-" + t.id, items: items, live: live,
                                 start: process.first?.firstTS, end: process.last?.lastTS))
        }
        return results
    }

    static let bottomID = "chat-bottom"

    /// Scrolls to the sentinel after the last row by id: SwiftUI lays that row out and positions around it, unlike
    /// `scrollTo(edge: .bottom)`, which trusts the lazy stack's estimated height and can land in unrendered space.
    /// Lands on the entry target once its line exists (the cache or the first page may not hold it yet).
    private func landEntry(_ lines: [ChatLine]) {
        guard let target = entry, !lines.isEmpty else { return }
        var id: String?
        switch target {
        case .latestAnswer:
            // The line right after the last input: the start of what the agent did and said for it.
            if let i = lines.lastIndex(where: \.isInput), i + 1 < lines.count { id = lines[i + 1].id }
        case .line(let saved):
            if lines.contains(where: { $0.id == saved }) {
                id = saved
            } else if store.loadedOnce {
                // Not in the latest page either: stay at the newest. Left waiting, it used to yank the list there
                // (and stop following) whenever paging up later happened to load that line.
                entry = nil
                return
            }
        }
        guard let id else { return }
        entry = nil
        followsEnd = false
        scratch.landingInFlight = true
        if target == .latestAnswer, let i = lines.lastIndex(where: \.isInput) {
            landUnderInput(lines[i].id)
            return
        }
        Task { @MainActor in
            await Task.yield()
            // Flipped: anchoring at the scroll view's bottom is the top of the screen — the line starts the page.
            position.scrollTo(id: id, anchor: .bottomLeading)
            try? await Task.sleep(for: .milliseconds(300))
            position.scrollTo(id: id, anchor: .bottomLeading)
            try? await Task.sleep(for: .milliseconds(100))
            landed()
        }
    }

    /// A landing is over. A line in the last screenful lands at the newest end (the list cannot go further): follow from
    /// there. The landing stopped following and the sentinel never left the screen, so nothing turned it back on: what
    /// arrived after it was held back, with no ↓ either (TestFlight 50/51, 2026-10-31).
    private func landed() {
        scratch.landingInFlight = false
        if atBottom { followsEnd = true }
    }

    /// New content arrived. At the newest end, untouched, but not following (anything that stopped following without
    /// moving the list): follow again, or the new lines wait behind a sentinel that is on screen, with no ↓. Then
    /// following goes to the newest, unless a tapped header is held in place (`ChatPin`).
    private func contentArrived(foldChrome: Bool) {
        if !followsEnd, atBottom, !scratch.userDragging, !scratch.landingInFlight { followsEnd = true }
        guard followsEnd, !pin.holding else { return }
        scrollToEnd()
        if foldChrome, userScrolled { hideChrome() }
    }

    /// How much of your last message stays on screen above the answer: about its last line (and the bubble's edge).
    static let inputTail: CGFloat = 34

    /// A finished turn you have not seen: your message's last line at the top of the page, the agent's answer under it.
    /// Jumps to the message, then moves by what is measured (rows not drawn yet only have estimated heights). The
    /// header folds to its slim bar at once (you came to read; it would fold a moment later anyway and uncover more of
    /// your message), so the page is laid out against the slim bar.
    private func landUnderInput(_ input: String) {
        landing = input
        scratch.landingMaxY = nil
        Task { @MainActor in
            await Task.yield()
            position.scrollTo(id: input, anchor: .bottomLeading)
            for n in 0..<8 {
                try? await Task.sleep(for: .milliseconds(60))
                if n == 1 { hideChrome() } // once the header has measured itself
                guard let bottom = scratch.landingMaxY, scratch.listFrame.height > 0 else { continue }
                let want = scratch.listFrame.minY + (style == .dev ? (sideBars ? sideHeaderHeight : ChatChrome.compactHeight) : AssistantTopBar.height) + Self.inputTail
                let off = want - bottom // > 0: too high; the flipped list scrolls toward older to lower it
                if abs(off) < 1 { break }
                scrollList(to: chrome.offset + off)
            }
            landing = nil
            landed()
        }
    }

    /// Remembers the top line on screen when leaving (nothing when at the newest: open at the newest again).
    private func rememberReadPosition() {
        guard viewing == nil else { return }
        if atBottom || scratch.visibleLines.isEmpty {
            UserDefaults.standard.removeObject(forKey: readKey)
            return
        }
        let order = currentLines().map(\.id)
        let top = scratch.visibleLines.min { (order.firstIndex(of: $0) ?? .max) < (order.firstIndex(of: $1) ?? .max) }
        UserDefaults.standard.set(top, forKey: readKey)
    }

    private func prefetchOlder() {
        let store = store
        guard userScrolled, store.hasMore, !store.isLoadingOlder else { return }
        Task { await model.loadOlder(shownKey) }
    }

    /// The newest message is at offset 0 of the flipped list: an exact jump, no estimate involved.
    private func scrollToEnd(animated: Bool = false) {
        #if DEBUG
        scratch.endJumps += 1
        #endif
        if animated { withAnimation(Theme.Motion.standard) { position.scrollTo(edge: .top) } } else { position.scrollTo(edge: .top) }
    }

    /// On entry / returning to the chat tab: at the newest (already the natural start of the flipped list).
    private func settleAtEnd() {
        Task { @MainActor in
            await Task.yield()
            if entry == nil && followsEnd { scrollToEnd() }
            try? await Task.sleep(for: .milliseconds(400))
            settled = true
        }
    }

    private func jumpToEnd() {
        Haptics.tap()
        followsEnd = true
        scrollToEnd(animated: true)
    }

    private var liveTurnID: String? {
        guard status.isActive, case .turn(let t) = store.blocks.last else { return nil }
        return t.id
    }

    private var lastPromptID: String? { store.items.last { $0.role == "user" && $0.kind == .text }?.id }

    /// A swipe to the left: from the chat to its files (and Git); to the right: back.
    private var swipeablePanes: some View {
        paneContent
            .gesture(SwipeTo(direction: .left) { swipePane(forward: true) })
            .gesture(SwipeTo(direction: .right) { swipePane(forward: false) })
    }

    @ViewBuilder private var paneContent: some View {
        switch pane {
        case .chat: chatPane.transition(.move(edge: .leading))
        case .terminal:
            TerminalPanel(key: key, switchTo: switchTo, onKeyboard: terminalKeyboard)
                .padding(.horizontal, Theme.Space.l).padding(.bottom, Theme.Space.s)
                .padding(.top, style == .dev ? chrome.height - min(chrome.collapse, chrome.maxCollapse) : 0)
        // Files and Git come in from the right (a swipe to the left), the chat back from the left.
        case .git: GitPane(machineID: key.machineID, cwd: session?.cwd ?? "", status: $git).padding(.top, headerRoom)
            .transition(.move(edge: .trailing))
        case .files: FilesPane(machineID: key.machineID, cwd: session?.cwd ?? "", git: git).padding(.top, headerRoom)
            .transition(.move(edge: .trailing))
        }
    }

    /// Typing on the terminal folds the header to its slim bar, so the rows over the keyboard go to the terminal; the
    /// keyboard going down opens it again.
    private func terminalKeyboard(_ up: Bool) {
        guard style == .dev else { return }
        withAnimation(Theme.Motion.layout) { chrome.collapse = up ? chrome.maxCollapse : 0 }
    }

    /// The custom header's full height (Agents; the assistant's floating bar): the other panes sit below it, the chat
    /// list scrolls under it.
    private var headerRoom: CGFloat { style == .dev ? (sideBars ? sideHeaderHeight : chrome.height) : AssistantTopBar.height }

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

    /// The chat is an upside-down list (newest first, flipped back on screen), the way chat apps do it: the newest
    /// message sits at scroll offset 0, so opening the page, following new messages and ↓ are exact jumps to 0 — a
    /// lazy stack never has to estimate its way to the bottom (that is what left pages blank). Older pages load at the
    /// far end and never push what you are reading.
    @ViewBuilder private var chatPane: some View {
        // The list above, the composer below — stacked, not overlaid: with the keyboard up the whole page gets
        // shorter and the newest message (offset 0 of the flipped list) stays right above the composer.
        VStack(spacing: 0) {
            // A hairline above the list: a scroll view touching the top safe area extends under it and adds its
            // inset — at the wrong end once flipped. Not touching it, the list gets no safe area at all (the
            // composer below keeps it off the bottom and the keyboard).
            Color.clear.frame(height: Theme.Size.line)
            ChatRegion(name: "ChatTranscript.body") {
                chatScroll()
                    .overlay(alignment: .bottomTrailing) { jumpArrow }
                    #if DEBUG
                    .overlay(alignment: .topLeading) { if Self.scrollProbe { scrollState } }
                    #endif
            }
            ChatRegion(name: "ChatFooter.body") { footer }
        }
        .environment(\.openSubagent, actions.openSubagent)
        .environment(\.openFold, actions.openFold)
        .environment(\.taskIndex, store.tasks)
        .environment(\.jumpToCall, actions.jumpToCall)
    }

    /// From a background task's notice to the call that started it: opens the work line it is folded in and brings
    /// the step into view (rows of the newest end hold still; the list stops following it).
    private func jumpToCall(_ id: String) {
        func holds(_ steps: [ToolStep]) -> String? {
            for case .call(let c) in steps where c.use.toolUseID == id { return c.id }
            return nil
        }
        var target: String?
        for line in currentLines() {
            switch line.content {
            case .work(let wid, let items, _, _, _):
                let steps = items.compactMap { if case .step(let s) = $0 { s } else { nil } }
                if let step = holds(steps) {
                    var t = Transaction()
                    t.disablesAnimations = true
                    if !isOpen(wid) {
                        withTransaction(t) { if Self.debugOpenAll { openWork.remove(wid) } else { openWork.insert(wid) } }
                    }
                    if AppStyle.current.usesBeautifulUI, let run = runID(holding: step, group: wid, items: items), !isRunOpen(run) {
                        withTransaction(t) { _ = openRuns.insert(run) }
                    }
                    target = wid + "/" + step
                }
            case .row(.tools(let steps)):
                if holds(steps) != nil { target = line.id }
            default: break
            }
            if target != nil { break }
        }
        guard let target else {
            Haptics.attention()
            return
        }
        wakeDrawer = nil
        userScrolled = true
        followsEnd = false
        Haptics.tap()
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(120)) // the opened work line's steps are laid out first
            withAnimation(Theme.Motion.standard) { position.scrollTo(id: target, anchor: .center) }
        }
    }

    /// Opening a work line the page folded fetches its steps (docs/protocol.md「折叠的工作」).
    private var openFold: (Item) async throws -> Void {
        { [model, shownKey] fold in try await model.unfold(shownKey, fold) }
    }

    @ViewBuilder private var footer: some View {
        if let a = viewedAgent {
            SubagentFooter(agent: a) { view(nil) }
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.s)
        } else {
            mainFooter.padding(.top, Theme.Space.s)
        }
    }

    #if DEBUG
    /// `-showScrollState YES`: where the list is, for UI tests (`ChatFollowTests`). XCUITest's frames of rows in the
    /// flipped list are wrong; the list itself knows which lines are on screen.
    static let scrollProbe = UserDefaults.standard.bool(forKey: "showScrollState") || scrollProbeVisible
    /// `-showScrollState visible`: the same, readable in a screenshot.
    static let scrollProbeVisible = UserDefaults.standard.string(forKey: "showScrollState") == "visible"

    private var scrollState: some View {
        TimelineView(.periodic(from: .now, by: 0.5)) { _ in
            let lines = currentLines()
            let newest = lines.last?.id ?? ""
            let shown = Set(scratch.visibleLines)
            let lastShown = lines.lastIndex { shown.contains($0.id) }.map { lines.count - 1 - $0 } ?? -1
            Text("bottom=\(atBottom ? 1 : 0) follows=\(followsEnd ? 1 : 0) frozen=\(frozen == nil ? 0 : 1) arrow=\(!atBottom && settled ? 1 : 0) newestShown=\(shown.contains(newest) ? 1 : 0) behind=\(lastShown) items=\(store.items.count) withheld=\(store.items.count - (frozen.map { f in Self.prefixCount(store.items) { ($0.seq, $0.part) <= (f.seq, f.part) } } ?? store.items.count))")
                .font(Theme.Font.label).opacity(Self.scrollProbeVisible ? 1 : 0.01).allowsHitTesting(false)
                .accessibilityIdentifier("chat.scrollState")
        }
    }
    #endif

    @ViewBuilder private var jumpArrow: some View {
            if !atBottom && settled {
                Button(action: jumpToEnd) {
                    Image(systemName: "arrow.down").font(Theme.Font.bodyStrong).scaledFrame(width: Theme.Size.avatar, height: Theme.Size.avatar)
                }
                .accessibilityLabel("回到最新")
                #if os(macOS)
                // ⌘↓ as in Safari / Mail; only while the arrow shows (scrolled up), so the composer keeps it otherwise.
                .keyboardShortcut(.downArrow, modifiers: .command)
                .help("回到最新 ⌘↓")
                #endif
                .themedButton()
                .clipShape(.circle)
                .padding(Theme.Space.l)
                .transition(.scale.combined(with: .opacity))
            }
    }

    private var mainFooter: some View {
            VStack(spacing: Theme.Space.m) {
                ForEach(model.interactions(for: key)) { ix in InteractionPanel(interaction: ix) }
                #if DEBUG
                if UserDefaults.standard.bool(forKey: "showSampleQuestion"), let ix = Interaction.sampleQuestion(key) {
                    InteractionPanel(interaction: ix) // `-showSampleQuestion YES`: the question UI, for screenshots
                }
                if UserDefaults.standard.bool(forKey: "showSampleScreen"), let ix = Interaction.sampleScreen(key) {
                    InteractionPanel(interaction: ix) // `-showSampleScreen YES`: a terminal dialog, for screenshots
                }
                #endif
                if let queued = session?.queued, !queued.isEmpty, style == .dev {
                    QueueStrip(key: key, queued: queued)
                }
                if viewing == nil, let recap = pendingRecap {
                    RecapStrip(item: recap) { dismissRecap(recap) }
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                }
                // The assistant takes modes on Codex too (the node maps them to Codex's sandbox / approvals).
                if viewing == nil, session?.assistant == true || (session?.isCodex != true && session?.isTraex != true) {
                    SessionModeBar(key: key, session: session).padding(.leading, Theme.Space.xs)
                }
                Composer(key: key, status: status, placeholder: style == .assistant ? "问 \(model.persona.name)…" : "给 \(session?.agentLabel ?? "Claude") 发消息",
                         onCleared: { switchTo($0) })
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.bottom, Theme.Space.s)
            .animation(Theme.Motion.standard, value: status)
            .animation(Theme.Motion.standard, value: model.interactions(for: key).map(\.id))
            .animation(Theme.Motion.standard, value: pendingRecap?.id)
            .onChange(of: model.drafts[key] ?? "") { _, draft in
                if !draft.isEmpty, let recap = pendingRecap { dismissRecap(recap) } // typing closes it
            }
    }

    /// The agent's "while you were away" recap above the composer, while it is the newest thing in the chat and has
    /// not been closed (a tap, or starting to type). The one-line marker in the chat stays.
    private var pendingRecap: Item? {
        guard style == .dev, case .row(.recap(let i))? = model.chat(key).blocks.last else { return nil }
        return closedRecap == i.id ? nil : i
    }

    @AppStorage("closedRecaps") private var closedRecapsData = Data()
    private var closedRecap: String? {
        (try? JSONDecoder().decode([String: String].self, from: closedRecapsData))?[key.machineID + "/" + key.sessionID]
    }

    private func dismissRecap(_ recap: Item) {
        var all = (try? JSONDecoder().decode([String: String].self, from: closedRecapsData)) ?? [:]
        all[key.machineID + "/" + key.sessionID] = recap.id
        if all.count > 300 { all = Dictionary(uniqueKeysWithValues: all.suffix(200).map { ($0.key, $0.value) }) }
        withAnimation(Theme.Motion.standard) { closedRecapsData = (try? JSONEncoder().encode(all)) ?? Data() }
    }

    /// Switches the chat to a subagent (nil: back to the main agent); its transcript streams like a session's.
    private func view(_ agent: Subagent?) {
        guard agent?.id != viewing?.id else { return }
        Haptics.tap()
        if viewing != nil { model.unsubscribe(shownKey, by: owner) }
        withAnimation(Theme.Motion.standard) { viewing = agent }
        if agent != nil { model.subscribe(shownKey, by: owner) }
        followsEnd = true
        frozen = nil
        scrollToEnd()
    }

    private func chatScroll() -> some View {
        let store = store
        let rows = store.rows
        let lines = currentLines()
        let queued = session?.queued ?? []
        let pending = store.pending.filter { p in !queued.contains { PendingMessage.same($0, p.text) } }
        return ScrollView {
            LazyVStack(alignment: .leading, spacing: Theme.Space.l) {
                // Newest first (the list is drawn upside down): the end sentinel, live text, unsent messages, lines.
                Color.clear
                    .frame(height: Theme.Size.line)
                    .id(Self.bottomID)
                    .onScrollVisibilityChange(threshold: 0.01) { visible in
                        atBottom = visible
                        if visible { followsEnd = true } // back at the newest: follow new messages again
                    }
                if let err = store.error {
                    Label(err, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger).flippedBack()
                }
                // Streamed text, the typing line and unsent messages sit at the newest end too: while you read
                // history they wait with the rest.
                if style == .assistant && frozen == nil {
                    ChatLiveText(store: store)
                }
                // Sent from here, not in the transcript yet (ones Claude queued show in the queue strip instead).
                ForEach(frozen == nil ? pending.reversed() : []) { msg in
                    PendingBubble(msg: msg, retry: { model.retry(key, msg) }, discard: { model.discard(key, msg) })
                        .flippedBack()
                        .id(msg.id.uuidString)
                }
                // Every message, timer line, tool count and answer is its own lazy row (a running turn can hold
                // hundreds of steps; one stack of them froze the page).
                // The oldest dozen lines start loading the page before them: by the time you reach the top it is
                // usually there (pages with screenshots run to megabytes and take seconds).
                let prefetch = Set(lines.prefix(12).map(\.id))
                ForEach(lines.reversed()) { line in
                    lineView(line)
                        .flippedBack()
                        .debugLineY(line.id, scratch)
                        .landingFrame(line.id == landing, scratch)
                        .id(line.id)
                        .onAppear { if prefetch.contains(line.id) { prefetchOlder() } }
                        .zIndex(menu.selected != nil && line.holds(menu.selected) ? 1 : 0)
                }
                if !store.loadedOnce && rows.isEmpty {
                    ChatSkeleton().flippedBack()
                } else if rows.isEmpty && store.pending.isEmpty { // a first message on its way is not 还没有消息
                    EmptyStateView(symbol: Theme.Icon.conversation, title: "还没有消息", message: "发一句话开始吧。").flippedBack()
                }
                if store.hasMore {
                    // Reaching the far (oldest) end loads the page before it — only once the user has dragged the list.
                    ProgressView()
                        .controlSize(.small)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, Theme.Space.sm)
                        .task(id: "\(rows.first?.id ?? "")|\(userScrolled)") { if userScrolled { await model.loadOlder(shownKey) } }
                }
            }
            .scrollTargetLayout()
            .coordinateSpace(.named(ChatPin.space))
            .background { ChatPin.Finder(pin: pin) }
        }
        // Side margins as scroll content margins, not padding: jumping to a row aligns its leading edge with the
        // margin instead of the screen edge (padding shifted the page 16pt sideways after landing on a row).
        .contentMargins(.horizontal, 16, for: .scrollContent)
        .onScrollTargetVisibilityChange(idType: String.self) { ids in scratch.visibleLines = ids }
        .flippedBack() // the scroll view itself upside down; every row above is flipped back
        // Safe areas would land on the wrong ends of a flipped scroll view: ignore them and add the room ourselves.
        // (Content top = the composer side on screen, content bottom = the navigation side.)
        // The header floats over the list (Agents): the oldest end leaves its full height free, so nothing hides
        // under it; the list's own frame never changes as it folds, so folding never moves the messages.
        .contentMargins(.top, 10, for: .scrollContent)
        .contentMargins(.bottom, headerRoom + 10, for: .scrollContent)
        .scrollIndicators(.hidden)
        // iOS 26's soft edge blur works out its region on the unflipped view and fogged the whole list.
        .scrollEdgeEffectHidden(true, for: .all)
        .scrollPosition($position)
        .onGeometryChange(for: CGRect.self, of: { $0.frame(in: .global) }) { scratch.listFrame = $0 }
        .scrollDismissesKeyboard(.interactively)
        .simultaneousGesture(TapGesture().onEnded { menu.dismiss() })
        // Only the user's own scrolling closes the bar; content streaming in also moves the offset (.animating).
        .onScrollGeometryChange(for: ListScroll.self, of: { ListScroll(offset: $0.contentOffset.y, height: $0.containerSize.height, x: $0.contentOffset.x) }) { old, new in
            scratch.offsetX = new.x
            chrome.offset = new.offset
            // A height change (keyboard) moves the offset too: that is not the user scrolling.
            if old.height != new.height { return }
            scrolled(by: new.offset - old.offset)
        }
        .onScrollPhaseChange { old, phase in
            // The user's own scrolling: a drag, and the glide after a flick. Deceleration alone is not proof — landing
            // on the entry line produced it with no finger down and folded the chrome on open.
            if phase != .idle { scratch.lastActivity = .now }
            let flick = phase == .decelerating && (old == .tracking || old == .interacting || scratch.userDragging)
            scratch.userDragging = phase == .tracking || phase == .interacting || flick
            if phase == .idle, old != .idle, old != .animating { settleChrome() } // the finger lifted, the glide ended
            if phase == .tracking || phase == .interacting {
                scratch.lastUserScroll = .now
                menu.dismiss()
                entry = nil // the user scrolls on their own: no landing on the saved place after this
                userScrolled = true
                followsEnd = false // the user has the list; nothing moves it until they return to the newest
            } else if phase == .idle, userScrolled, atBottom {
                followsEnd = true
            }
        }
        .environment(menu)
        .environment(\.chatPin, pin)
        .onAppear { pin.letGo = { [position = $position] in position.wrappedValue = ScrollPosition(idType: String.self) } }
        .sheet(item: $menu.selecting) { t in SelectableTextSheet(text: t.text) }
        .sheet(item: $menu.sharing) { m in MessageShareSheet(message: m) }
        .sheet(isPresented: Binding(get: { wakeDrawer != nil }, set: { if !$0 { wakeDrawer = nil } })) {
            // Looked up live: the drawer follows the work while the agent is still on it.
            let t = store.blocks.lazy.compactMap { b -> Turn? in if case .turn(let t) = b, t.id == wakeDrawer { t } else { nil } }.first
            WakeDrawer(notices: t?.wakes ?? []) {
                if let t { ForEach(unfoldWork(wakeLines(t, live: t.id == liveTurnID && !t.tail.isEmpty))) { lineView($0) } }
            }
            .environment(\.chatPin, nil) // the drawer has a list of its own; the chat's must not scroll for it
            .environment(menu)
            .environment(\.openFold, actions.openFold)
            .environment(\.taskIndex, store.tasks)
            .environment(\.jumpToCall, actions.jumpToCall)
        }
        // New content while following: back to offset 0 (exact; nothing to estimate).
        .onChange(of: store.resets) { _, _ in frozen = nil } // the snapshot's items are gone
        .onChange(of: followsEnd) { _, follows in
            if follows {
                frozen = nil
            } else if let last = store.items.last {
                frozen = Frozen(seq: last.seq, part: last.part, live: liveTurnID)
            }
        }
        .onChange(of: rows.last?.id) { _, _ in contentArrived(foldChrome: false) }
        .onChange(of: lines.count, initial: true) { _, _ in landEntry(lines) }
        .onChange(of: store.loadedOnce) { _, _ in landEntry(lines) }
        .onChange(of: liveTurnID) { _, _ in contentArrived(foldChrome: false) }
        // Following the newest as it streams in folds the chrome too (once the user has taken the list).
        // Not while a tapped header is held in place (`ChatPin`): opening a work line loads its steps.
        .onChange(of: store.items.count) { _, _ in contentArrived(foldChrome: true) }
        .modifier(ChatStreamFollow(store: store) { contentArrived(foldChrome: true) })
        // Your own message brings you to the newest — only yours (one typed on the computer, or sent by another session,
        // used to pull you out of the history you were reading too), and only if you have not scrolled since sending it:
        // Claude can take it in seconds later, while you went back up to read (2026-10-30).
        .onChange(of: lastPromptID) { old, new in
            guard old != nil, let new, let text = store.items.last(where: { $0.id == new })?.text,
                  let i = scratch.sentHere.firstIndex(where: { PendingMessage.same(text, $0) }) else { return }
            scratch.sentHere.remove(at: i)
            guard scratch.lastUserScroll < scratch.sentAt else { return }
            followsEnd = true
            scrollToEnd()
        }
        .onChange(of: store.pending.count) { old, new in
            guard new > old else { return }
            scratch.sentHere = Array((scratch.sentHere + store.pending.map(\.text)).suffix(20))
            scratch.sentAt = .now
            followsEnd = true
            scrollToEnd(animated: true)
        }
        // Parse what rows will show (Markdown, paths) in the background as messages load: scrolling then only lays out.
        .task(id: prewarmKey) { await prewarmRows() }
    }

    // Out of the modifier chain: inline, Swift 6.3 timed out type-checking the Release build.
    private var prewarmSource: String { "\(shownKey.machineID)/\(shownKey.sessionID)|\(store.resets)" }
    private var prewarmKey: String { "\(prewarmSource)|\(store.version)" }

    private func prewarmRows() async {
        let source = prewarmSource
        if scratch.prewarmSource != source {
            scratch.prewarmSource = source
            scratch.prewarmedIDs = []
        }
        let items = store.items.filter { !scratch.prewarmedIDs.contains($0.id) }
        guard !items.isEmpty else { return }
        let warm = Task.detached(priority: .utility) { RowMemo.prewarm(items) }
        await withTaskCancellationHandler {
            await warm.value
        } onCancel: {
            warm.cancel()
        }
        if !Task.isCancelled { scratch.prewarmedIDs.formUnion(items.map(\.id)) }
    }

    /// The flipped list's offset grows toward older messages. Like Safari's toolbar, the chrome follows the finger:
    /// reading on (toward the newest) folds it as far as the finger goes, going back into the history unfolds it.
    /// Idle time before the open chrome folds itself.
    static var chromeIdleSeconds: TimeInterval {
        #if DEBUG
        // `-holdChromeOpen YES`: the header never folds by itself (UI tests that tap in it).
        if UserDefaults.standard.bool(forKey: "holdChromeOpen") { return .infinity }
        #endif
        return 3
    }

    private func scrolled(by delta: CGFloat) {
        guard style == .dev, pane == .chat, scratch.userDragging, delta != 0 else { return }
        scratch.lastActivity = .now
        let folded = min(max(chrome.collapse - delta, 0), chrome.maxCollapse)
        guard folded != chrome.collapse else { return }
        chrome.collapse = folded
        if folded > 0, showInfo { showInfo = false }
    }

    /// The finger lifted: never stay half folded. At the newest the chrome folds (the page is all chat); elsewhere
    /// it goes to whichever end is nearer.
    private func settleChrome() {
        guard style == .dev, pane == .chat else { return }
        let full = chrome.maxCollapse
        if chrome.offset < 40 {
            hideChrome()
        } else if chrome.collapse > 0, chrome.collapse < full {
            withAnimation(Theme.Motion.layout) { chrome.collapse = chrome.collapse > full / 2 ? full : 0 }
        }
    }

    private func hideChrome() {
        guard style == .dev, pane == .chat, chrome.collapse < chrome.maxCollapse else { return }
        withAnimation(.smooth(duration: 0.35)) { chrome.collapse = chrome.maxCollapse }
        showInfo = false
    }

    private func showChrome() {
        scratch.lastActivity = .now // shown on purpose: give it the full idle time before folding again
        guard chrome.collapse > 0 else { return }
        withAnimation(.smooth(duration: 0.35)) { chrome.collapse = 0 }
    }

    /// The chrome folded away: back, and where you are ("app › leader · 对话") — tap it for everything again.
    private var compactChrome: some View {
        HStack(spacing: 0) {
            Button { back() } label: {
                Image(systemName: Theme.Icon.back).font(Theme.Font.secondaryStrong).dynamicTypeSize(...DynamicTypeSize.xxLarge).scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.wellM)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("返回")
            .noBackWhen(hidesBack)
            Spacer(minLength: 0)
            Button { Haptics.tap(); showChrome() } label: {
                HStack(spacing: Theme.Space.sm) {
                    // Out of reach: the slim bar says so too (the open header's subtitle explains on a tap).
                    if model.connection.shown != .online {
                        ProgressView().controlSize(.mini)
                        Text(model.connection.shown.title).lineLimit(1)
                    } else {
                        if let s = session { StatusDot(state: s.state, size: 7) }
                        Text(compactPlace).lineLimit(1).truncationMode(.middle)
                    }
                }
                .animation(Theme.Motion.standard, value: model.connection.shown)
                .font(Theme.Font.captionStrong)
                .padding(.horizontal, Theme.Space.m)
                .scaledFrame(height: Theme.Size.wellXS)
                .background(Theme.Fill.chip, in: .capsule)
                .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(model.connection.shown != .online ? "\(model.connection.shown.title)，显示标题和标签"
                                : "\(compactPlace)，显示标题和标签")
            Spacer(minLength: 0)
            // A loop or background work: a small mark. As wide as back either way, so the place stays centered.
            // The terminal is in the ··· menu (no button of its own in the header).
            HStack(spacing: 0) {
                if session?.loop != nil || !(session?.background ?? []).isEmpty {
                    Button { Haptics.tap(); showActivity = true } label: { activityMark }
                        .buttonStyle(.plain)
                        .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.wellM)
                        .accessibilityLabel("循环与后台任务，点按查看")
                }
            }
            .scaledFrame(minWidth: Theme.Size.tapTarget)
        }
        .padding(.horizontal, Theme.Space.xs)
        .frame(height: ChatChrome.compactHeight)
    }

    /// The folded header's sign of a loop / background tasks: a gear with the count, or the loop arrows (still: only
    /// 需要你 and the work spinner move).
    private var activityMark: some View {
        let bg = session?.background?.count ?? 0
        return HStack(spacing: Theme.Space.xxs) {
            Image(systemName: bg > 0 ? Theme.Icon.backgroundTaskFilled : Theme.Icon.inProgress)
                .font(Theme.Font.labelHeavy)
            if bg > 0 { Text("\(bg)").font(Theme.Font.labelHeavy.monospacedDigit()) }
        }
        .foregroundStyle(bg > 0 ? Theme.busy : Theme.loop)
        .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.wellM)
        .contentShape(.rect)
    }

    /// The "…" menu, in the custom header (Agents) or the system bar (assistant).
    @ViewBuilder private var moreMenuItems: some View {
            if style == .dev {
                Button { showTerminal = true } label: { Label("终端", systemImage: Theme.Icon.terminal) }
                if pane != .chat {
                    Button { withAnimation(Theme.Motion.standard) { pane = .chat } } label: { Label("回到对话", systemImage: SessionPane.chat.symbol) }
                }
                ForEach(panes.filter { $0 == .files || $0 == .git }) { p in
                    Button { withAnimation(Theme.Motion.standard) { pane = p } } label: {
                        Label(paneBadges[p].map { "\(p.title)（\($0)）" } ?? p.title, systemImage: p.symbol)
                    }
                }
            }
            if status.isActive {
                Button { Task { await model.interrupt(key) } } label: { Label("中断", systemImage: Theme.Icon.interrupt) }
            }
            if style == .dev, let s = session {
                Button { renaming = s } label: { Label("重命名…", systemImage: Theme.Icon.edit) }
            }
            if style == .dev {
                Button { model.toggleMute(key) } label: {
                    Label(model.isMuted(key) ? "开启通知" : "屏蔽通知", systemImage: model.isMuted(key) ? Theme.Icon.unmute : Theme.Icon.mute)
                }
            }
            Divider()
            if let s = session {
                Button { UIPasteboard.general.string = "claude --resume \(s.sessionID)" } label: {
                    Label("拷贝恢复命令", systemImage: Theme.Icon.copy)
                }
                Button { UIPasteboard.general.string = s.cwd } label: { Label("拷贝目录", systemImage: Theme.Icon.folder) }
                if let space = s.spaceKey, let openSpace {
                    Button { openSpace(space) } label: { Label("空间里的所有 Agent", systemImage: Theme.Icon.space) }
                }
                if s.status.isLive {
                    Button(role: .destructive) { closing = s } label: {
                        Label("关闭 Agent…", systemImage: Theme.Icon.end)
                    }
                }
            }
    }

    /// A custom title: tapping it toggles the developer info card (instead of the system scroll-to-top).
    /// While the hub is out of reach the subtitle turns into a spinner and 连接中… (Telegram keeps the name and
    /// swaps the "online" line the same way); a tap then explains the link instead of opening the session info.
    private var titleButton: some View {
        let link = model.connection.shown
        return Button {
            Haptics.tap()
            if link != .online { showLink = true } else { showInfo = true }
        } label: {
            VStack(spacing: Theme.Space.xxs) {
                Text(title).font(Theme.Font.heading).lineLimit(1)
                ZStack {
                    if link != .online {
                        ConnectionLabel(phase: link, compact: true).transition(.opacity)
                    } else {
                        HStack(spacing: Theme.Space.xs) {
                            if style == .dev, let s = session { devSubtitle(s) } else { Text(subtitle).lineLimit(1) }
                        }
                        .font(Theme.Font.label)
                        .foregroundStyle(.secondary)
                        .transition(.opacity)
                    }
                }
                .animation(Theme.Motion.standard, value: link)
            }
            .scaledFrame(maxWidth: Theme.Size.box260)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(link != .online ? "\(title)，\(link.title)，查看连接详情" : "\(title)，查看会话信息")
        .connectionDetails($showLink)
    }

    /// Agents: the title's second line — the state (and for how long it has worked this turn), then where it runs.
    /// The state's words take its color; the place stays secondary. The tab's name is left out when the strip below
    /// shows the space's tabs (the selected one is this agent's; 2026-11-01 user request: it said it twice). Short of
    /// room the machine goes; the place always stays (cut short if it must), so the line keeps one shape whatever the state.
    private func devSubtitle(_ s: Session) -> some View {
        TimelineView(.periodic(from: .now, by: 30)) { ctx in
            let words = stateWords(s, now: ctx.date)
            let tabShown = model.siblings(of: key).count > 1
            let place = [s.spaceName, tabShown ? nil : s.slotLabel].compactMap { $0 }.joined(separator: " › ")
            let machine = model.machine(s.machineID)?.name ?? s.machineID
            HStack(spacing: Theme.Space.xs) {
                StatusDot(state: s.state, size: 7)
                ViewThatFits(in: .horizontal) {
                    subtitleLine(s, words, [machine, place])
                    subtitleLine(s, words, [place.isEmpty ? machine : place]) // truncates when it must
                }
            }
        }
    }

    /// Quiet states (已看, 未运行) say nothing in words (2026-10-31 user request): the dot carries them, the line is where
    /// it runs. 需要你 / 工作中 N 分 / 完成未看 still lead the line.
    private func subtitleLine(_ s: Session, _ words: String, _ rest: [String]) -> some View {
        let parts = rest.filter { !$0.isEmpty }
        if Self.quiet(s.state) {
            return Text(parts.isEmpty ? words : parts.joined(separator: " · ")).foregroundStyle(.secondary).lineLimit(1)
        }
        let tail = parts.map { " · " + $0 }.joined()
        return (Text(words).fontWeight(.semibold)
            .foregroundStyle(s.state == .idle || s.state == .offline || s.state == .unknown ? Color.secondary : s.state.tone.text)
            + Text(tail).foregroundStyle(.secondary))
            .lineLimit(1) // ViewThatFits picks by the ideal (one-line) width; the last one truncates
    }

    static func quiet(_ state: AgentState) -> Bool { state == .idle || state == .offline || state == .unknown }

    /// "工作中 22 分" while a turn runs (minutes: the work line in the chat has the seconds), else the state's name.
    private func stateWords(_ s: Session, now: Date) -> String {
        guard s.state == .working, status.isActive, case .turn(let t)? = store.blocks.last, let start = t.startedAt else { return s.state.label }
        let m = Int(now.timeIntervalSince(start)) / 60
        if m < 1 { return s.state.label }
        return s.state.label + " " + (m < 60 ? "\(m) 分" : "\(m / 60) 小时 \(m % 60) 分")
    }

    /// The Agents header: back, title (with the state), the terminal and ⋯; under them, when there are any, the other
    /// agents of the space (a menu) and a /loop or background work. It folds into a slim bar as the list scrolls.
    private var devChrome: some View {
        ChromeHeader(chrome: chrome, touched: { scratch.lastActivity = .now }) {
            VStack(spacing: 0) {
                // One container for the row's glass (back, the terminal, ⋯): drawn together instead of as separate glasses.
                GlassEffectContainer(spacing: 8) {
                    HStack(spacing: Theme.Space.s) {
                        Button { back() } label: {
                            Image(systemName: Theme.Icon.back).font(Theme.Font.heading).dynamicTypeSize(...DynamicTypeSize.xxLarge).scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                                .contentShape(.circle)
                        }
                        .buttonStyle(.plain)
                        .surface(interactive: true, in: .circle)
                        .accessibilityLabel("返回")
                        .noBackWhen(hidesBack)
                        Spacer(minLength: 0)
                        titleButton
                        Spacer(minLength: 0)
                        // The terminal (it comes up from the bottom over the page, TerminalCover), files and Git are in
                        // the ··· menu; files and Git also a swipe to the left away — no tab row (docs/ios.md).
                        Menu { moreMenuItems } label: {
                            Image(systemName: Theme.Icon.more).font(Theme.Font.heading).dynamicTypeSize(...DynamicTypeSize.xxLarge).scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                                .contentShape(.circle)
                        }
                        .surface(interactive: true, in: .circle)
                        .accessibilityLabel("更多")
                    }
                }
                .padding(.horizontal, Theme.Space.l)
                .padding(.top, Theme.Space.xxs)
                .padding(.bottom, Theme.Space.sm)
                // Row two, only when there is something for it: the space's other agents and a /loop or background
                // work (small chips; a tap opens them). Files and Git are a swipe or the ··· menu away.
                let hasActivity = session?.loop != nil || !(session?.background ?? []).isEmpty
                if model.siblings(of: key).count > 1 || hasActivity {
                    HStack(spacing: Theme.Space.s) {
                        // The tabs take the width there is; without them the activity chips sit on the left.
                        SiblingTabs(current: key) { next in switchTo(next) }
                        if hasActivity {
                            ActivityChips(loop: session?.loop, background: session?.background ?? [], compact: true) { showActivity = true }
                                .fixedSize()
                        }
                        if model.siblings(of: key).count < 2 { Spacer(minLength: 0) }
                    }
                    .padding(.horizontal, Theme.Space.l)
                    .padding(.bottom, Theme.Space.s)
                }
                // Only while subagents run (plus the one being read): finished ones stay reachable from their
                // Agent call ("查看子 Agent") without taking a row here.
                let agents = (session?.subagents ?? []).filter { $0.running || $0.id == viewing?.id }
                if pane == .chat, !agents.isEmpty {
                    AgentStrip(agents: agents, viewing: viewing?.id) { a in view(a) }
                }
            }
        } compact: {
            compactChrome
        }
        .sheet(isPresented: $showActivity) {
            ActivitySheet(loop: session?.loop, background: session?.background ?? [], stopLoop: { model.stopLoop(key) }) { t in
                showActivity = false
                if let out = t.outputFile { file = FileRef(machineID: key.machineID, path: out, cwd: session?.cwd) }
            }
        }
    }

    /// Back: out of files / Git to the chat first, then off the page.
    /// The widest the conversation gets: lines longer than this are hard to read.
    static let readableWidth: CGFloat = 760

    /// An Agents chat whose actions are in the system's side column (no header of its own).
    private var sideBars: Bool { style == .dev && barsVertical }

    /// The header's actions as system toolbar items, for the side column: back on top (a custom one, so it leaves files /
    /// Git for the chat first), then the panes, the terminal and ⋯. Each has a title as well as a symbol: the column
    /// shows the symbol, the overflow menu both.
    @ToolbarContentBuilder private var sideBarItems: some ToolbarContent {
        if !hidesBack {
            ToolbarItem(placement: .cancellationAction) {
                Button { back() } label: { Label("返回", systemImage: Theme.Icon.back) }
            }
        }
        ToolbarItemGroup(placement: .topBarTrailing) {
            if pane != .chat {
                Button { withAnimation(Theme.Motion.standard) { pane = .chat } } label: { Label("对话", systemImage: SessionPane.chat.symbol) }
            }
            Button { Haptics.tap(); showTerminal = true } label: { Label("终端", systemImage: SessionPane.terminal.symbol) }
            ForEach(panes.filter { ($0 == .git || $0 == .files) && $0 != pane }) { p in
                Button { Haptics.tap(); withAnimation(Theme.Motion.standard) { pane = p } } label: {
                    Label(paneBadges[p].map { "\(p.title)（\($0)）" } ?? p.title, systemImage: p.symbol)
                }
            }
            Menu { moreMenuItems } label: { Label("更多", systemImage: Theme.Icon.more) }
        }
    }

    /// The header's other half while its buttons are in the side column: the title (a tap shows the session's info),
    /// and when there are any the space's other agents, a /loop or background work and running subagents. It stays put.
    private var sideHeader: some View {
        VStack(spacing: Theme.Space.sm) {
            titleButton
            let hasActivity = session?.loop != nil || !(session?.background ?? []).isEmpty
            if model.siblings(of: key).count > 1 || hasActivity {
                HStack(spacing: Theme.Space.s) {
                    SiblingTabs(current: key) { next in switchTo(next) }
                    if hasActivity {
                        ActivityChips(loop: session?.loop, background: session?.background ?? [], compact: true) { showActivity = true }
                            .fixedSize()
                    }
                }
            }
            let agents = (session?.subagents ?? []).filter { $0.running || $0.id == viewing?.id }
            if pane == .chat, !agents.isEmpty {
                AgentStrip(agents: agents, viewing: viewing?.id) { a in view(a) }
            }
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.top, Theme.Space.xs)
        .padding(.bottom, Theme.Space.s)
        .frame(maxWidth: .infinity)
        .background(alignment: .top) {
            // The page's fog from the very top, so messages scrolling up do not show through the title.
            GeometryReader { g in
                TopFog(solidHeight: g.safeAreaInsets.top + g.size.height, fadeHeight: 16)
                    .frame(height: g.safeAreaInsets.top + g.size.height + 16)
                    .offset(y: -g.safeAreaInsets.top)
            }
        }
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { sideHeaderHeight = $0 }
        .sheet(isPresented: $showActivity) {
            ActivitySheet(loop: session?.loop, background: session?.background ?? [], stopLoop: { model.stopLoop(key) }) { t in
                showActivity = false
                if let out = t.outputFile { file = FileRef(machineID: key.machineID, path: out, cwd: session?.cwd) }
            }
        }
    }

    /// The first page of a split's detail column, on the chat: nothing to go back to (the list is beside it).
    private var hidesBack: Bool { split?.isFirstPage == true && pane == .chat }

    private func back() {
        if pane != .chat { withAnimation(Theme.Motion.standard) { pane = .chat } } else { dismissChat() }
    }

    /// A swipe to the left goes from the chat to its files (then Git); to the right, back toward the chat.
    private func swipePane(forward: Bool) {
        guard style == .dev else { return }
        let order = panes.filter { $0 != .terminal }
        guard let i = order.firstIndex(of: pane) else { return }
        let j = forward ? i + 1 : i - 1
        guard order.indices.contains(j) else { return }
        Haptics.tap()
        withAnimation(Theme.Motion.standard) { pane = order[j] }
    }

    private var compactPlace: String {
        let place = [session?.spaceName, session?.slotLabel].compactMap { $0 }.joined(separator: " › ")
        return (place.isEmpty ? title : place) + " · " + pane.title
    }

    /// A "完成" notification for this chat asked for its latest answer (AppModel.latestAnswerFor); taken once.
    private func takeLatestAnswer() -> Bool {
        guard model.latestAnswerFor == key else { return false }
        model.latestAnswerFor = nil
        return true
    }

    private func appeared() {
            bindMenu()
            if !entered { // only on entry: coming back from a full-screen cover must not move what you were reading
                entered = true
                if style == .dev, session?.state == .unread || takeLatestAnswer() || Self.debugAsUnread {
                    entry = .latestAnswer // finished, not seen (or opened from its notification): the latest answer from its start
                } else if style == .dev, let saved = UserDefaults.standard.string(forKey: readKey) {
                    entry = .line(saved) // where you left off
                }
                settleAtEnd()
            }
            model.subscribe(key, by: owner)
            model.show(key, by: owner)
            model.markSeen(key)
            if style == .dev { AppModel.rememberChat(key) }
            #if DEBUG
            if UserDefaults.standard.bool(forKey: "openTerminal") { showTerminal = true }
            if UserDefaults.standard.bool(forKey: "openSpaceFromChat") { // chat → its space page
                UserDefaults.standard.set(false, forKey: "openSpaceFromChat")
                Task {
                    try? await Task.sleep(for: .seconds(5)) // the session list may not be in yet
                    if let space = session?.spaceKey { openSpace?(space) }
                }
            }
            if UserDefaults.standard.bool(forKey: "showScrollToOldest") { // testing: flick toward the oldest, log progress
                Task {
                    try? await Task.sleep(for: .seconds(8))
                    userScrolled = true // as if dragged: older pages only load after the user took the list
                    followsEnd = false
                    var log: [String] = []
                    var y: CGFloat = chrome.offset
                    for step in 0..<20 {
                        y += 4000
                        withAnimation(.easeOut(duration: 1.5)) { scrollList(to: y) }
                        try? await Task.sleep(for: .seconds(2.5))
                        let s = model.chat(shownKey)
                        log.append("step \(step) offset=\(Int(chrome.offset)) target=\(Int(y)) items=\(s.items.count) rows=\(s.rows.count) first=\(s.items.first?.seq ?? 0) more=\(s.hasMore) loading=\(s.isLoadingOlder) follows=\(followsEnd) atBottom=\(atBottom) ends=\(scratch.endJumps)")
                        y = max(y, chrome.offset)
                    }
                    if let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first {
                        try? log.joined(separator: "\n").write(to: dir.appending(path: "oldest.txt"), atomically: true, encoding: .utf8)
                    }
                }
            }
            if UserDefaults.standard.bool(forKey: "showFoldProbe") { // folding keeps what you read in place: Documents/fold.txt
                Task { await foldProbe() }
            }
            if UserDefaults.standard.bool(forKey: "showFreezeProbe") { // reading history holds still: Documents/freeze.txt
                Task { await freezeProbe() }
            }
            if UserDefaults.standard.bool(forKey: "showChatDump") { // what the chat holds, every 2 s: Documents/chat-dump.json
                Task {
                    let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
                    while !Task.isCancelled {
                        let s = model.chat(shownKey)
                        let dump: [String: Any] = ["session": shownKey.sessionID, "covered": s.covered, "history": s.history ?? "",
                                                   "has_more": s.hasMore, "lines": currentLines().count, "visible": scratch.visibleLines.count,
                                                   "items": s.items.map { [$0.id, $0.seq, $0.kind.rawValue, $0.fullSize ?? 0] as [Any] }]
                        if let data = try? JSONSerialization.data(withJSONObject: dump) {
                            try? data.write(to: dir.appending(path: "chat-dump.json"), options: .atomic)
                        }
                        try? await Task.sleep(for: .seconds(2))
                    }
                }
            }
            if UserDefaults.standard.bool(forKey: "showAutoScroll") { // profiling: flicks through the history, frames counted
                Task {
                    try? await Task.sleep(for: .seconds(12)) // the history loads (-loadAll) and the page settles
                    FrameStats.shared.start()
                    // Like a reader flicking: four fast flicks back into the history, then four back to the newest.
                    var y: CGFloat = 0
                    for step in 0..<8 {
                        y = max(0, y + (step < 4 ? 3000 : -3000))
                        withAnimation(.easeOut(duration: 2.5)) { scrollList(to: y) }
                        try? await Task.sleep(for: .seconds(2.8))
                    }
                    FrameStats.shared.stop(label: "sweep")
                }
            }
            if UserDefaults.standard.bool(forKey: "showCompactChrome") { // folded, once the header has its height
                Task { try? await Task.sleep(for: .seconds(3)); withAnimation(.smooth) { chrome.collapse = chrome.maxCollapse } }
            }
            if UserDefaults.standard.bool(forKey: "showWakeDrawer") { // the newest "后台任务完成" drawer
                Task {
                    try? await Task.sleep(for: .seconds(UserDefaults.standard.bool(forKey: "loadAll") ? 30 : 6))
                    wakeDrawer = Self.debugTrim(model.chat(key).blocks).reversed().lazy.compactMap { b -> String? in
                        if case .turn(let t) = b, !t.wakes.isEmpty { t.id } else { nil }
                    }.first
                }
            }
            if UserDefaults.standard.bool(forKey: "showShareCard") { // the share card of the latest answer
                Task {
                    try? await Task.sleep(for: .seconds(5))
                    let answer = model.chat(key).blocks.reversed().lazy.compactMap { b -> Item? in
                        if case .turn(let t) = b, case .assistant(let i)? = t.answer { i } else { nil }
                    }.first
                    if let a = answer { menu.sharing = ShareMessage(text: a.text ?? "", isUser: false, source: menu.source(), date: a.ts, agent: menu.agent()) }
                }
            }
            if UserDefaults.standard.bool(forKey: "showCloseAgent") { // the close confirmation, for screenshots
                Task { try? await Task.sleep(for: .seconds(3)); closing = session }
            }
            if let u = UserDefaults.standard.string(forKey: "openLinkURL").flatMap(URL.init(string:)) { // `-openLinkURL <url>`
                Task { try? await Task.sleep(for: .seconds(3)); link = LinkTarget(url: u) }
            }
            if UserDefaults.standard.bool(forKey: "openFirstImage") {
                Task {
                    for _ in 0..<30 { // the cached copy drops images; wait for the hub's
                        try? await Task.sleep(for: .seconds(1))
                        let imgs = model.chat(key).items.compactMap(\.image)
                        if !imgs.isEmpty, debugImages == nil { debugImages = imgs; break }
                    }
                }
            }
            if UserDefaults.standard.bool(forKey: "showInfo") { showInfo = true }
            if let t = UserDefaults.standard.string(forKey: "openSendMessage") { // `-openSendMessage <text>`: refusals (SendSafety)
                // `-openSendDelay <s>`: later (ConnectionTests sends while the hub is down)
                let delay = UserDefaults.standard.object(forKey: "openSendDelay") != nil ? UserDefaults.standard.double(forKey: "openSendDelay") : 3
                Task { try? await Task.sleep(for: .seconds(delay)); model.sendNow(key, text: t) }
            }
            if let p = UserDefaults.standard.string(forKey: "openPane").flatMap(SessionPane.init(rawValue:)) {
                if p == .terminal { showTerminal = true } else { pane = p } // the terminal comes up over the page
            }
            if let f = UserDefaults.standard.string(forKey: "openFile") {
                // Once the session is known (its cwd resolves a relative path) and the push is over: a sheet asked for
                // during the push is dropped (a fresh install, FileViewerUITests).
                Task {
                    for _ in 0..<40 where session == nil { try? await Task.sleep(for: .milliseconds(250)) }
                    try? await Task.sleep(for: .seconds(1))
                    file = FileRef(machineID: key.machineID, path: f, cwd: session?.cwd)
                }
            }
            if let id = UserDefaults.standard.string(forKey: "openAgent") { // `-openAgent <agent id>`: that subagent
                Task {
                    try? await Task.sleep(for: .seconds(4))
                    if let a = session?.subagents?.first(where: { $0.id == id }) { view(a) }
                }
            }
            if UserDefaults.standard.bool(forKey: "showOldest") { // the start of what is loaded (with -fromSeq: that turn's top)
                Task {
                    try? await Task.sleep(for: .seconds(5))
                    userScrolled = true
                    followsEnd = false
                    position.scrollTo(edge: .bottom)
                }
            }
            if UserDefaults.standard.bool(forKey: "jumpEnd") { // what tapping the ↓ arrow does, from the top
                Task {
                    try? await Task.sleep(for: .seconds(6))
                    userScrolled = true
                    position.scrollTo(edge: .bottom) // the oldest end of the flipped list
                    try? await Task.sleep(for: .seconds(4))
                    jumpToEnd()
                }
            }
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
                    if UserDefaults.standard.integer(forKey: "fromSeq") == 0 { position.scrollTo(edge: .bottom) } // oldest end
                }
            }
            #endif
    }

    /// Simulator automation: `-openWork YES` shows every work line unfolded (screenshots of the expanded state).
    #if DEBUG
    static let foldProbe = UserDefaults.standard.bool(forKey: "showFoldProbe") || UserDefaults.standard.bool(forKey: "showFreezeProbe")
    /// `-showAsUnread YES`: open the chat as if its answer had not been seen (where it lands).
    static let debugAsUnread = UserDefaults.standard.bool(forKey: "showAsUnread")
    #else
    static let debugAsUnread = false
    #endif

    private static var debugOpenAll: Bool {
        #if DEBUG
        UserDefaults.standard.bool(forKey: "openWork")
        #else
        false
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
            var end = i + 1 // plus work background tasks woke the agent for right after it
            while end < blocks.count {
                if case .turn(let t) = blocks[end], !t.users.isEmpty { break } // up to the next message: recaps too
                end += 1
            }
            var out = Array(blocks[i..<end]) // near the newest end: no long scroll to reach it
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
        menu.agent = { [model, key] in model.session(key)?.agentLabel ?? "Claude" }
        menu.source = { [model, key] in
            let s = model.session(key)
            return [s?.spaceName, s?.slotLabel].compactMap { $0 }.joined(separator: " › ")
        }
    }

    private func switchTo(_ next: SessionKey) {
        guard next != key else { return }
        Haptics.tap()
        rememberReadPosition()
        if viewing != nil { model.unsubscribe(shownKey, by: owner); viewing = nil }
        model.unsubscribe(key, by: owner)
        key = next
        userScrolled = false
        settled = false
        followsEnd = true
        atBottom = false
        settleAtEnd()
        bindMenu()
        menu.selected = nil
        if pane == .git || pane == .files { pane = .chat } // the next agent may live in another directory
        git = nil
        position = ScrollPosition(edge: .top)
        model.subscribe(next, by: owner)
        model.show(next, by: owner)
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
        return [machine, place.isEmpty ? nil : place, s.agentLabel, s.state.label].compactMap { $0 }.joined(separator: " · ")
    }

    @ViewBuilder private func rowView(_ row: ChatRow, isLatest: Bool, live: Bool) -> some View {
        switch row {
        case .user(let i):
            if (i.text ?? "").hasPrefix("/loop") {
                LoopTickRow(item: i)
            } else if let from = i.from {
                PeerMessage(item: i, from: from)
            } else {
                UserBubble(item: i)
            }
        case .assistant(let i): AssistantText(item: i)
        case .thinking(let i):
            if style == .dev && live { ThinkingRow(item: i) }
        case .tools(let steps):
            if style == .dev && live { ToolGroupView(steps: steps, isLatest: isLatest) } else { AssistantToolsRow(steps: steps) }
        case .notice(let i): NoticeRow(item: i)
        case .recap(let i): RecapCard(item: i)
        case .image(let i):
            if let p = i.image {
                HStack { if i.role == "user" { Spacer(minLength: 60) }; Base64Image(payload: p).scaledFrame(maxWidth: Theme.Size.box240) }
            }
        }
    }
}

struct ChatSkeleton: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var on = false
    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.l) {
            ForEach(0..<5, id: \.self) { i in
                RoundedRectangle(cornerRadius: Theme.Radius.control)
                    .fill(on ? Theme.Fill.chip : Theme.Fill.well)
                    .scaledFrame(width: [220, 300, 180, 260, 140][i], height: Theme.Size.markS)
                    .frame(maxWidth: .infinity, alignment: i == 0 ? .trailing : .leading)
            }
        }
        .onAppear { if !reduceMotion { withAnimation(.easeInOut(duration: 0.9).repeatForever()) { on = true } } }
    }
}

/// Message input: text, photos, quick slash commands; turns into a stop button while Claude works.
/// Claude's suggested next message over the composer: a tap puts it in the box to edit, 直接发送 (long press) sends it.
/// A session Jarvis is still starting (node/starting.go): what is sent now waits and goes in once it is ready. Or why
/// it did not start.
struct StartingNote: View {
    var error: String?
    var agent: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
            if let error {
                Image(systemName: Theme.Icon.errorFilled).font(Theme.Font.labelStrong).foregroundStyle(Theme.danger)
                Text("没有启动起来：\(error)").font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(3)
            } else {
                ProgressView().controlSize(.mini)
                Text("正在启动 \(agent)，消息会在它就绪后发出").font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(2)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        .surface(tint: error == nil ? nil : Theme.danger.opacity(0.06), in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("chat.starting")
    }
}

struct SuggestionPill: View {
    var text: String
    var fill: () -> Void
    var send: () -> Void

    var body: some View {
        Button {
            Haptics.tap()
            fill()
        } label: {
            HStack(spacing: Theme.Space.s) {
                Image(systemName: Theme.Icon.ai).font(Theme.Font.labelStrong).foregroundStyle(Theme.ai)
                Text(text).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
                Spacer(minLength: 4)
                Text("采用").font(Theme.Font.labelStrong).foregroundStyle(Theme.accent).lineLimit(1).fixedSize()
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.s)
            .surface(tint: Theme.ai.opacity(0.06), interactive: true, in: .capsule)
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .contextMenu {
            Button { Haptics.success(); send() } label: { Label("直接发送", systemImage: "arrow.up.circle") }
            Button(action: fill) { Label("填入输入框", systemImage: Theme.Icon.insert) }
        } preview: {
            Text(text).font(Theme.Font.body).padding(Theme.Space.l).frame(maxWidth: 320, alignment: .leading)
        }
        .accessibilityLabel("Claude 建议：\(text)")
        .accessibilityHint("填入输入框；长按可直接发送")
        .accessibilityAction(named: "直接发送", send)
    }
}

struct Composer: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var status: SessionStatus
    var placeholder: String?
    /// After /clear the pane runs a new session: the page follows it.
    var onCleared: ((SessionKey) -> Void)? = nil
    private var text: String {
        get { model.drafts[key] ?? "" }
        nonmutating set { model.drafts[key] = newValue }
    }
    private var textBinding: Binding<String> { Binding(get: { text }, set: { text = $0 }) }
    @State private var picks: [PhotosPickerItem] = []
    @State private var showPhotos = false
    @State private var showCamera = false
    @State private var images: [(UIImage, ImagePayload)] = []
    @State private var editingIndex: Int?
    @State private var showFiles = false
    @State private var uploads = FileUploads()
    @State private var selection: TextSelection?
    @State private var confirmClear = false
    @State private var clearing = false
    @FocusState private var focused: Bool

    /// /clear only for Claude in a herdr pane (the headless assistant and Codex / TRAEX have no such command here).
    private var canClear: Bool {
        guard let s = model.session(key) else { return false }
        return s.paneID != nil && !s.isCodex && !s.isTraex
    }

    private var canSend: Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.isEmpty || !(model.composerContexts[key] ?? []).isEmpty
    }

    /// Claude in a herdr pane takes `/model`; Codex and TRAEX pick theirs in a full-screen menu the phone cannot drive.
    private var canPickModel: Bool {
        guard let s = model.session(key) else { return false }
        return s.paneID != nil && !s.isCodex && !s.isTraex && !s.assistant
    }

    /// The @ menu: files this chat's tools touched, then other agents' sessions.
    private func mentions() -> [PromptSource] {
        PromptItems.files(model.chat(key).items, cwd: model.session(key)?.cwd ?? "")
            + PromptItems.sessions(model.allSessions, except: key)
    }

    /// Claude's suggested next message, while the box is empty and the agent waits.
    private var suggestion: String? {
        guard !status.isActive, text.isEmpty, images.isEmpty, let s = model.session(key)?.suggestion, !s.isEmpty else { return nil }
        return s
    }

    private var contexts: Binding<[ContextRef]> {
        Binding(get: { model.composerContexts[key] ?? [] }, set: { model.composerContexts[key] = $0.isEmpty ? nil : $0 })
    }

    var body: some View {
        VStack(spacing: Theme.Space.s) {
            if let s = model.session(key), s.starting == true || s.startError != nil {
                StartingNote(error: s.startError, agent: s.agentLabel)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            if let suggestion {
                SuggestionPill(text: suggestion) {
                    text = suggestion
                    selection = TextSelection(insertionPoint: suggestion.endIndex) // keep typing after it
                    focused = true
                } send: {
                    Task { await sendText(suggestion) }
                }
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            if !(model.composerContexts[key] ?? []).isEmpty { ContextChips(refs: contexts) }
            if !uploads.items.isEmpty { UploadsBar(uploads: uploads) }
            if !images.isEmpty {
                ComposerImages(images: $images) { editingIndex = $0 } // a tap marks it up before sending
            }
            // The BeautifulUI style's prompt bar; every other style keeps the original input line.
            if AppStyle.current.usesBeautifulUI {
                PromptComposer(placeholder: status.isActive ? "补充说明（会排队发送）" : (suggestion ?? placeholder ?? "给 Claude 发消息"),
                               text: textBinding, selection: $selection, focused: $focused,
                               hasAttachments: !images.isEmpty || !(model.composerContexts[key] ?? []).isEmpty,
                               stop: status.isActive ? { Task { await model.interrupt(key) } } : nil,
                               attach: PromptItems.attachments(camera: UIImagePickerController.isSourceTypeAvailable(.camera)),
                               mentions: mentions,
                               commands: PromptItems.commands(clear: canClear && !status.isActive && !clearing),
                               models: canPickModel ? PromptItems.claudeModels : [],
                               modelID: PromptItems.claudeModelID(model.session(key)?.model),
                               pickModel: { id in model.sendNow(key, text: "/model \(id)", images: []) }, // photos and quotes stay for the message
                               onAttach: { source in
                                   // A PhotosPicker cannot present from inside the menu: these sheets hang off the composer.
                                   switch source.id {
                                   case "photo": showPhotos = true
                                   case "camera": showCamera = true
                                   default: showFiles = true
                                   }
                               },
                               onMention: { source in
                                   guard let ref = PromptItems.quote(source, in: model.allSessions) else { return false }
                                   contexts.wrappedValue.append(ref)
                                   return true
                               },
                               onCommand: { command in
                                   guard command.name == "/clear" else { return false }
                                   confirmClear = true
                                   return true
                               },
                               send: { let t = text; Task { await sendText(t) } }) // the bar clears the draft right after
            } else {
                ComposerRow(placeholder: status.isActive ? "补充说明（会排队发送）" : (suggestion ?? placeholder ?? "给 Claude 发消息"),
                            text: textBinding, selection: $selection, focused: $focused, canSend: canSend,
                            stop: status.isActive ? { Task { await model.interrupt(key) } } : nil,
                            send: { Task { await sendText(text) } }) {
                    // A PhotosPicker inside a Menu never presents (the menu dismisses first): present it from here.
                    Button { showPhotos = true } label: { Label("照片", systemImage: Theme.Icon.photos) }
                    if UIImagePickerController.isSourceTypeAvailable(.camera) {
                        Button { showCamera = true } label: { Label("拍照", systemImage: "camera") }
                    }
                    Button { showFiles = true } label: { Label("文件", systemImage: Theme.Icon.file) }
                    Section("快捷指令") {
                        ForEach(SlashCommand.all) { cmd in
                            Button { Task { await sendText(cmd.text) } } label: { Label(cmd.title, systemImage: cmd.symbol) }
                        }
                        if canClear {
                            Button(role: .destructive) { confirmClear = true } label: {
                                Label("清空上下文 /clear", systemImage: "eraser")
                            }
                            .disabled(status.isActive || clearing)
                        }
                    }
                }
            }
        }
        #if DEBUG
        .task { // `-focusComposer YES`: the keyboard up, for screenshots of the chat while typing
            if UserDefaults.standard.bool(forKey: "focusComposer") {
                try? await Task.sleep(for: .seconds(6))
                focused = true
            }
        }
        #endif
        .animation(Theme.Motion.standard, value: suggestion)
        #if DEBUG
        .task { // `-showClearConfirm YES`: the /clear confirmation, for screenshots
            guard UserDefaults.standard.bool(forKey: "showClearConfirm") else { return }
            try? await Task.sleep(for: .seconds(4))
            for _ in 0..<20 where !canClear { try? await Task.sleep(for: .seconds(1)) } // the session list loads
            confirmClear = canClear
        }
        #endif
        .confirmationDialog("清空上下文？", isPresented: $confirmClear, titleVisibility: .visible) {
            Button("清空", role: .destructive) {
                clearing = true
                Task {
                    let next = await model.clearContext(key)
                    clearing = false
                    if let next { onCleared?(next) }
                }
            }
        } message: {
            Text("\(model.session(key)?.agentLabel ?? "Claude") 会开一个新会话，这段对话留在历史里，页面会跳到新会话。")
        }
        .photosPicker(isPresented: $showPhotos, selection: $picks, maxSelectionCount: 4, matching: .images)
        .fileUploadPicker(isPresented: $showFiles) { urls in
            uploads.start(urls, machine: key.machineID, model: model) { insertPath($0) }
        }
        #if os(macOS)
        // Finder drags: pictures are attached like 照片, other files uploaded to the machine like 文件 (docs/mac.md).
        // ⌘V of a screenshot or of files copied in Finder does the same; text is pasted as usual.
        .macComposerFiles(images: images.count, focused: focused, attach: { add($0) }, upload: { upload($0) })
        #if DEBUG
        .task { // `-openDropDemo a.png,b.md`: those files dropped on the composer 2 s after opening, for screenshots
            if let list = UserDefaults.standard.string(forKey: "openDropDemo") {
                try? await Task.sleep(for: .seconds(2))
                take(list.split(separator: ",").map { URL(fileURLWithPath: String($0)) })
            }
        }
        #endif
        #endif
        #if DEBUG
        .task { // `-openUploadDemo <name>`: a made-up 24 MB file uploading, for screenshots of the progress bar
            if let name = UserDefaults.standard.string(forKey: "openUploadDemo") {
                try? await Task.sleep(for: .seconds(3))
                let url = FileManager.default.temporaryDirectory.appending(path: name)
                try? Data(count: 24 << 20).write(to: url)
                uploads.start([url], machine: key.machineID, model: model) { insertPath($0) }
            }
        }
        #endif
        .fullScreenCover(isPresented: $showCamera) {
            CameraPicker { img in add(img) }.ignoresSafeArea()
        }
        .onChange(of: picks) { _, items in
            guard !items.isEmpty else { return }
            Task { await load(items) }
        }
        .onChange(of: model.composerInbox[key]?.count ?? 0, initial: true) { _, n in
            guard n > 0, let incoming = model.composerInbox[key] else { return }
            model.composerInbox[key] = nil
            for img in incoming { add(img) }
            focused = true
        }
        .fullScreenCover(item: Binding(get: { editingIndex.map(EditIndex.init) }, set: { if $0 == nil { editingIndex = nil } })) { e in
            if images.indices.contains(e.id) {
                ImageEditor(image: images[e.id].0) { edited in
                    let scaled = edited.scaledDown(maxSide: 1600)
                    if let jpeg = scaled.jpegData(compressionQuality: 0.75), images.indices.contains(e.id) {
                        images[e.id] = (scaled, ImagePayload(mime: "image/jpeg", data: jpeg.base64EncodedString()))
                    }
                }
            }
        }
    }

    private func sendText(_ raw: String) async {
        let t = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        let ctx = model.composerContexts[key] ?? []
        guard !t.isEmpty || !images.isEmpty || !ctx.isEmpty else { return }
        // Picked page excerpts ride along as <jarvis-context> tags before what was typed.
        let body = t.isEmpty ? (images.isEmpty ? "参考上面的内容。" : "请看图片") : t
        let message = ctx.isEmpty ? body : ctx.map(\.xml).joined(separator: "\n") + "\n\n" + body
        // On screen at once; the request runs in the background (a failure turns the bubble into "重发").
        model.sendNow(key, text: message, images: images.map(\.1))
        if raw == text { text = "" }
        images = []
        model.composerContexts[key] = nil
    }

    #if os(macOS)
    /// Files dropped or pasted: pictures attached, other files uploaded to the machine, folders refused.
    private func take(_ urls: [URL]) {
        MacDrop.take(urls, images: images.count, attach: { add($0) }, upload: { upload($0) })
    }

    private func upload(_ urls: [URL]) {
        uploads.start(urls, machine: key.machineID, model: model) { insertPath($0) }
        focused = true
    }
    #endif

    /// A file's path on the machine, at the cursor; not sent.
    private func insertPath(_ path: String) {
        text = PathInsert.edit(path, text: text, selection: $selection, focused: $focused)
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

/// The composer's "/" menu: Claude Code's own commands, and Jarvis' quick messages under a short word. Picking one
/// types `text` into the draft; send sends it.
struct SlashCommand: Identifiable {
    /// The + menu's entry (the original input line).
    var title: String
    var symbol: String
    /// The "/" word and its description (the BeautifulUI style's prompt bar).
    var word: String
    var detail: String
    var text: String
    var id: String { word }

    static let all: [SlashCommand] = [
        SlashCommand(title: "继续", symbol: "forward", word: "/继续", detail: "让它接着做", text: "继续"),
        SlashCommand(title: "压缩上下文 /compact", symbol: Theme.Icon.collapseAll, word: "/compact", detail: "压缩上下文", text: "/compact"),
        SlashCommand(title: "查看上下文 /context", symbol: "chart.pie", word: "/context", detail: "查看上下文", text: "/context"),
        SlashCommand(title: "提交并推送", symbol: "arrow.up.doc", word: "/提交", detail: "按规范提交并推送", text: "把当前改动按规范提交并推送"),
        SlashCommand(title: "汇报进展", symbol: Theme.Icon.list, word: "/汇报", detail: "汇报进展、还剩什么", text: "简要汇报一下目前的进展、还剩什么"),
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

/// The session's live terminal, come up from the bottom over the page (终端, or the terminal button of a reply): all
/// of the screen goes to it — no title, no tabs — under a slim bar that drags it back down (a tap on its handle too).
/// Its size does not change with anything around it but the keyboard (a new size is a resize of the pane it drives).
struct TerminalCover: View {
    @Environment(\.dismiss) private var dismiss
    let key: SessionKey
    var switchTo: ((SessionKey) -> Void)?
    @State private var offset: CGFloat = 0

    var body: some View {
        TerminalPanel(key: key, switchTo: switchTo, grab: TerminalGrab(
            dragged: { offset = max(0, $0) },
            released: { distance, predicted in
                if distance > 140 || predicted > 420 { close() } else { withAnimation(.spring(duration: 0.3)) { offset = 0 } }
            },
            close: close))
            .padding(.horizontal, Theme.Space.xs)
            .padding(.bottom, Theme.Space.xxs)
            .offset(y: offset)
            .background {
                // The page shows through, dimmed less as the terminal goes down.
                Color.black.opacity(Double(0.4 * (1 - min(offset / 500, 1)))).ignoresSafeArea()
            }
    }

    private func close() { dismiss() }
}

/// Input typed while Claude works and not yet taken, shown above the composer instead of in the chat.
/// Editing pulls it back (like ↑ in the terminal); the other queued messages are re-queued in order.
struct QueueStrip: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var queued: [String]
    @State private var busy = false

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            HStack(spacing: Theme.Space.sm) {
                Image(systemName: "tray.and.arrow.down")
                Text("排队中 \(queued.count) 条 · Claude 忙完会依次处理").lineLimit(1)
                Spacer(minLength: 0)
                if busy { ProgressView().controlSize(.mini) }
            }
            .font(Theme.Font.labelStrong)
            .foregroundStyle(.secondary)
            ForEach(Array(queued.enumerated()), id: \.offset) { i, q in
                Menu {
                    Button { act(i, delete: false) } label: { Label("撤回编辑", systemImage: Theme.Icon.edit) }
                    Button { UIPasteboard.general.string = q } label: { Label("拷贝", systemImage: Theme.Icon.copy) }
                    Button(role: .destructive) { act(i, delete: true) } label: { Label("删除", systemImage: Theme.Icon.delete) }
                } label: {
                    HStack(alignment: .top, spacing: Theme.Space.s) {
                        Text("\(i + 1)")
                            .font(Theme.Font.labelHeavy.monospacedDigit())
                            .foregroundStyle(Theme.accent)
                            .scaledFrame(width: Theme.Size.badge, height: Theme.Size.badge)
                            .background(Theme.accent.opacity(0.14), in: .circle)
                        Text(q).font(Theme.Font.secondary).foregroundStyle(Color.primary).lineLimit(2).multilineTextAlignment(.leading)
                        Spacer(minLength: 0)
                        Image(systemName: Theme.Icon.more).font(Theme.Font.label).foregroundStyle(.secondary).padding(.top, Theme.Space.xs)
                    }
                    .padding(.horizontal, Theme.Space.m)
                    .padding(.vertical, Theme.Space.s)
                    .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
                }
                .disabled(busy)
            }
        }
        .padding(Theme.Space.m)
        .surface(in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous))
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

#if os(iOS)
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
#endif

/// Developer facts about a session, shown under the title: model, context in use, git branch, Claude Code version…
/// 会话信息, from the bottom: who and where at the top, then what it runs, how far it has got, its settings and where
/// its files are; copying the id or the resume command last. Settings-app grouping, values on the right.
struct SessionInfoSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let key: SessionKey
    var session: Session?
    @State private var info: APIClient.SessionInfo?
    @State private var error: String?

    var body: some View {
        InfoContainer {
            List {
                Section { header }
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 0, leading: 4, bottom: 4, trailing: 4))
                if let info {
                    Section("运行") {
                        row(Theme.Icon.model, "模型", Fmt.model(info.model) ?? info.model)
                        if let l = info.launcher ?? session?.launcher, !l.isEmpty { row("bolt", "启动器", l) }
                        row("shippingbox", agent, info.version)
                        row("lock.open", "权限", SessionModes.modeLabel(info.permissionMode))
                        row("gauge.with.dots.needle.50percent", "思考强度", SessionModes.effortLabel(info.effort))
                    }
                    Section("用量") {
                        row(Theme.Icon.context, "上下文", Self.tokens(info.contextTokens) + " tokens")
                        if info.compactions > 0 { row("rectangle.compress.vertical", "压缩", "\(info.compactions) 次") }
                        row(Theme.Icon.conversation, "对话", "\(info.turns) 轮")
                        row("text.word.spacing", "共生成", Self.tokens(info.outputTokens) + " tokens")
                        if let started = info.startedAt, started.isMeaningful {
                            row(Theme.Icon.time, "开始", started.formatted(date: .abbreviated, time: .shortened))
                        }
                    }
                    Section("位置") {
                        row(Theme.Icon.folder, "目录", Fmt.path(info.cwd ?? session?.cwd ?? "", home: model.machine(key.machineID)?.home), mono: true)
                        if let b = info.gitBranch, !b.isEmpty { row("arrow.triangle.branch", "分支", b, mono: true) }
                    }
                    Section {
                        copyButton("拷贝 session id", symbol: Theme.Icon.id, text: info.sessionID, toast: "已拷贝 session id")
                        copyButton("拷贝恢复命令", symbol: Theme.Icon.terminal, text: "cd \(info.cwd ?? "") && " + resume(info.sessionID), toast: "已拷贝恢复命令")
                    } footer: {
                        Text(info.sessionID).font(Theme.Font.label.monospaced()).textSelection(.enabled)
                    }
                } else if let error {
                    Section { Label(error, systemImage: Theme.Icon.error).font(Theme.Font.caption).foregroundStyle(Theme.danger) }
                } else {
                    Section { ProgressView().frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xl) }.listRowBackground(Color.clear)
                }
            }
            .listStyle(.insetGrouped)
            .listSectionSpacing(.compact)
            #if os(iOS)
            .navigationTitle("会话信息")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() }.lineLimit(1) }
            }
            #endif
        }
        .task(id: key) {
            do { info = try await model.api?.sessionInfo(key) } catch { self.error = error.shown }
        }
    }

    /// A sheet of its own on iOS; on the Mac an inspector column inside the chat page, where a second
    /// NavigationStack would take over the page's (the chat is a pushed page of the Agents split view) and the title
    /// would replace the window's.
    private struct InfoContainer<Content: View>: View {
        @ViewBuilder var content: Content
        var body: some View {
            #if os(iOS)
            NavigationStack { content }
            #else
            content
            #endif
        }
    }

    /// The title, its state in words (the one place they still show) and where it runs.
    private var header: some View {
        let s = session
        let place = [model.machine(key.machineID)?.name ?? key.machineID, s?.spaceName, s?.slotLabel]
            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " › ")
        return VStack(alignment: .leading, spacing: Theme.Space.sm) {
            Text(s?.displayTitle ?? "会话").font(Theme.Font.title).lineLimit(2)
            HStack(spacing: Theme.Space.sm) {
                if let st = s?.state {
                    HStack(spacing: Theme.Space.xs) {
                        StatusDot(state: st, size: 7)
                        Text(st.label).font(Theme.Font.labelStrong)
                            .foregroundStyle(ChatView.quiet(st) ? Color.secondary : st.tone.text)
                    }
                    .padding(.horizontal, Theme.Space.s).padding(.vertical, Theme.Space.xs)
                    .background(Color.primary.opacity(0.06), in: .capsule)
                }
                Text(place).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func row(_ symbol: String, _ label: String, _ value: String?, mono: Bool = false) -> some View {
        LabeledContent {
            Text(value.flatMap { $0.isEmpty ? nil : $0 } ?? "—")
                .font(mono ? .subheadline.monospaced() : .subheadline)
                .lineLimit(2)
                .truncationMode(.middle)
                .multilineTextAlignment(.trailing)
                .textSelection(.enabled)
        } label: {
            Label(label, systemImage: symbol).lineLimit(1)
        }
    }

    private func copyButton(_ title: String, symbol: String, text: String, toast: String) -> some View {
        Button {
            UIPasteboard.general.string = text
            Haptics.success()
            model.toast = Toast(toast, style: .success)
        } label: {
            Label(title, systemImage: symbol).lineLimit(1)
        }
    }

    /// The coding agent in the pane: Claude Code unless the session says Codex / TRAEX.
    private var agent: String { session?.isCodex == true || session?.isTraex == true ? session!.agentLabel : "Claude Code" }

    private func resume(_ id: String) -> String {
        if session?.isCodex == true { return "codex resume \(id)" }
        if session?.isTraex == true { return "traex resume \(id)" }
        return "claude --resume \(id)"
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
/// One line of the chat. A turn reads as exchanges: each message, one quiet work line for what the agent did after
/// it ("调用了 3 个工具，思考 1 次 · 1 分 20 秒 ›", tap to see every step), then the agent's last words before the
/// next message. Running and finished turns look the same; the running exchange's work line ticks. Lines are
/// separate lazy rows on purpose (a running turn can have hundreds of steps).
struct ChatLine: Identifiable {
    let id: String
    let content: Content

    indirect enum Content {
        case row(ChatRow)
        case work(id: String, items: [WorkItem], live: Bool, start: Date?, end: Date?)
        case userImages(id: String, items: [Item])
        case mergedUsers(id: String, rows: [ChatRow])
        case splitHeader(id: String, count: Int)
        case wakes(Turn, live: Bool)
        /// Two or more "while you were away" recaps in a row, as one line (they open in one drawer, newest first).
        case recaps(id: String, items: [Item])
        case workStep(group: String, item: WorkItem, first: Bool)
        /// Two or more tool calls in a row of an open work line, as one line of chips.
        case toolRun(id: String, calls: [ToolCall], open: Bool, first: Bool)
        case workEnd(group: String, count: Int)
        /// A model API error (`APIErrorInfo`), repeated `count` times in a row.
        case apiError(item: Item, info: APIErrorInfo, count: Int)

        var id: String {
            switch self {
            case .row(let r): r.id
            case .work(let id, _, _, _, _): id
            case .workStep(let group, let item, _): group + "/" + item.id
            case .workEnd(let group, _): group + "-end"
            case .toolRun(let id, _, _, _): id
            case .userImages(let id, _): id
            case .mergedUsers(let id, _): id
            case .splitHeader(let id, _): id + "-h"
            case .wakes(let t, _): "wk-" + t.id
            case .recaps(let id, _): id
            case .apiError(let item, _, _): item.id
            }
        }
    }

    private init(_ content: Content) {
        id = content.id
        self.content = content
    }

    static func row(_ row: ChatRow) -> Self { Self(.row(row)) }
    static func work(id: String, items: [WorkItem], live: Bool, start: Date?, end: Date?) -> Self {
        Self(.work(id: id, items: items, live: live, start: start, end: end))
    }
    static func userImages(id: String, items: [Item]) -> Self { Self(.userImages(id: id, items: items)) }
    static func mergedUsers(id: String, rows: [ChatRow]) -> Self { Self(.mergedUsers(id: id, rows: rows)) }
    static func splitHeader(id: String, count: Int) -> Self { Self(.splitHeader(id: id, count: count)) }
    static func wakes(_ turn: Turn, live: Bool) -> Self { Self(.wakes(turn, live: live)) }
    static func recaps(id: String, items: [Item]) -> Self { Self(.recaps(id: id, items: items)) }
    static func workStep(group: String, item: WorkItem, first: Bool) -> Self {
        Self(.workStep(group: group, item: item, first: first))
    }
    static func workEnd(group: String, count: Int) -> Self { Self(.workEnd(group: group, count: count)) }
    static func apiError(item: Item, info: APIErrorInfo, count: Int) -> Self { Self(.apiError(item: item, info: info, count: count)) }
    static func toolRun(id: String, calls: [ToolCall], open: Bool, first: Bool) -> Self {
        Self(.toolRun(id: id, calls: calls, open: open, first: first))
    }

    func holds(_ id: String?) -> Bool {
        guard let id else { return false }
        switch content {
        case .row(let r): return r.id == id
        case .mergedUsers(_, let rows): return rows.first?.id == id
        case .recaps(_, let items): return items.contains { $0.id == id }
        default: return false
        }
    }

    var isInput: Bool {
        switch content {
        case .row(.user), .mergedUsers: true
        default: false
        }
    }
}

/// What a work line folds: tool calls, thoughts, system notices and the agent's words before its last ones.
nonisolated indirect enum WorkItem: Identifiable, Equatable, Sendable {
    case step(ToolStep)
    case text(Item)

    var id: String {
        switch self {
        case .step(let s): s.id
        case .text(let i): "t-" + i.id
        }
    }
}

/// One step of an open work line, a line of the chat list itself. The steps read as one card under their header:
/// each draws its slice of it (`WorkCardSlice`), with a hairline between steps, instead of a tile per step.
struct WorkStepView: View, Equatable {
    @Environment(\.openSubagent) private var openSubagent
    nonisolated let item: WorkItem
    nonisolated let first: Bool

    nonisolated static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.item == rhs.item && lhs.first == rhs.first
    }

    @MainActor
    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("WorkStepView")
        #endif
        Group {
            switch item {
            case .step(.call(let c)):
                VStack(alignment: .leading, spacing: 0) {
                    ToolCallRow(call: c, autoExpand: false, grouped: true)
                    if c.name == "Agent" || c.name == "Task", let open = openSubagent, let id = c.use.toolUseID {
                        Button { open(id) } label: {
                            Label("查看子 Agent", systemImage: Theme.Icon.open).font(Theme.Font.labelStrong).lineLimit(1)
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(Theme.accent)
                        .padding(.leading, WorkCardSlice.textColumn)
                        .padding(.bottom, Theme.Space.m)
                    }
                }
            case .step(.thought(let t)) where t.task != nil && t.wake != true:
                TaskNoteRow(item: t, grouped: true) // a Monitor event between two commands: one line like theirs
            case .step(.thought(let t)):
                Group { if t.kind == .notice { NoticeRow(item: t) } else { ThinkingRow(item: t, compact: !WorkCardSlice.timeline) } }
                    .padding(.leading, WorkCardSlice.contentLeading)
                    .padding(.trailing, WorkCardSlice.inset)
                    .padding(.vertical, WorkCardSlice.timeline ? Theme.Space.s : Theme.Space.m)
            case .step(.folded(let f)):
                FoldedWorkRow(fold: f)
                    .padding(.leading, WorkCardSlice.contentLeading)
                    .padding(.trailing, WorkCardSlice.inset)
                    .padding(.vertical, WorkCardSlice.timeline ? Theme.Space.s : Theme.Space.m)
            case .text(let i):
                // The agent's words between its steps: on the timeline in the quieter type, so they read as part of
                // the process and not as its answer.
                AssistantText(item: i, quiet: WorkCardSlice.timeline)
                    .padding(.leading, WorkCardSlice.contentLeading)
                    .padding(.trailing, WorkCardSlice.inset)
                    .padding(.vertical, WorkCardSlice.timeline ? Theme.Space.s : Theme.Space.m)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background { WorkCardSlice(top: first, bottom: false) }
        .overlay(alignment: .top) { if !first { WorkCardSlice.separator } }
    }
}

/// A slice of an open work line's card. The steps are lines of their own in the lazy chat list (a turn can hold
/// hundreds; one view holding them all stalled the page), so each draws its part: rounded on top for the first, at
/// the bottom for the 收起 foot, reaching halfway into the list's gaps to meet its neighbours.
/// The chat's actions handed down through the environment (open a file, a link, an image, a subagent…), as closures
/// made once per page that call whatever the page's last render set. A closure written in `body` is a new value on
/// every render and SwiftUI cannot tell it is the same one: every row that reads one (each visible work step reads
/// several) was evaluated again on every push from the hub (2026-10-31, `LiveUpdateTests`).
@MainActor final class ChatActions {
    struct Handlers {
        var openFile: (String) -> Void = { _ in }
        var openLink: (URL) -> Void = { _ in }
        var viewImages: (ImageViewing) -> Void = { _ in }
        var attachImage: (UIImage) -> Void = { _ in }
        var loadImage: (String) async -> ImagePayload? = { _ in nil }
        var loadFullText: (String) async throws -> String = { _ in "" }
        var openSubagent: (String) -> Void = { _ in }
        var openFold: (Item) async throws -> Void = { _ in }
        var jumpToCall: (String) -> Void = { _ in }
    }

    private var handlers = Handlers()

    func set(_ h: Handlers) { handlers = h }

    private(set) lazy var openFile: (String) -> Void = { [weak self] in self?.handlers.openFile($0) }
    private(set) lazy var openLink: (URL) -> Void = { [weak self] in self?.handlers.openLink($0) }
    private(set) lazy var viewImages: (ImageViewing) -> Void = { [weak self] in self?.handlers.viewImages($0) }
    private(set) lazy var attachImage: (UIImage) -> Void = { [weak self] in self?.handlers.attachImage($0) }
    // The async ones run on the main actor, where the handlers are.
    private(set) lazy var loadImage: (String) async -> ImagePayload? = { @MainActor [weak self] path in
        guard let load = self?.handlers.loadImage else { return nil }
        return await load(path)
    }
    private(set) lazy var loadFullText: (String) async throws -> String = { @MainActor [weak self] id in
        guard let load = self?.handlers.loadFullText else { throw CancellationError() }
        return try await load(id)
    }
    private(set) lazy var openSubagent: (String) -> Void = { [weak self] in self?.handlers.openSubagent($0) }
    private(set) lazy var openFold: (Item) async throws -> Void = { @MainActor [weak self] item in
        guard let open = self?.handlers.openFold else { return }
        try await open(item)
    }
    private(set) lazy var jumpToCall: (String) -> Void = { [weak self] in self?.handlers.jumpToCall($0) }
    /// Web links open in the in-app sheet (Feishu docs in the native reader); anything else as before.
    private(set) lazy var openURL = OpenURLAction { [weak self] url in
        guard ["http", "https"].contains(url.scheme?.lowercased() ?? ""), let self else {
            // Mac: NSWorkspace would run any scheme it names (LinkPolicy).
            #if os(macOS)
            return LinkPolicy.system(url)
            #else
            return .systemAction
            #endif
        }
        handlers.openLink(url)
        return .handled
    }
}

struct WorkCardSlice: View {
    var top: Bool
    var bottom: Bool
    /// Half the list's spacing (`LazyVStack(spacing: Theme.Space.l)`): the slices meet in the middle of each gap. It was
    /// 7 from when the spacing was 14; at 16 every seam showed a 2 pt gap (white lines across the card, the rail broken).
    static let bridge: CGFloat = Theme.Space.l / 2
    static let radius = Theme.Radius.card
    /// The rows' side padding inside the card.
    static let inset: CGFloat = 12
    /// Where a tool row's words start (inset + 24 pt icon + 10): separators start there.
    static let textColumn: CGFloat = 46
    /// The timeline's rail runs through the middle of the tools' icons.
    static let railX: CGFloat = inset + Theme.Size.icon / 2

    static var timeline: Bool { StyleSpec.current.workList == .timeline }
    /// Where a step's own content starts: the card's inset, or (timeline) right of the rail, where the tools' words are.
    static var contentLeading: CGFloat { timeline ? textColumn : inset }

    var body: some View {
        if Self.timeline { rail } else { card }
    }

    /// 液态玻璃: no card, a thin line down the left through the steps, reaching into the gaps to meet its neighbours;
    /// it starts at the first step and stops in the middle of the 收起 foot.
    private var rail: some View {
        GeometryReader { g in
            Rectangle()
                .fill(Color(uiColor: .separator))
                .frame(width: Theme.Size.lineMedium, height: bottom ? g.size.height / 2 + (top ? 0 : Self.bridge) : g.size.height + (top ? 0 : Self.bridge) + Self.bridge)
                .offset(x: Self.railX - Theme.Size.lineMedium / 2, y: top ? Theme.Space.m : -Self.bridge)
        }
        .allowsHitTesting(false)
    }

    private var card: some View {
        let r = Self.radius
        let fill = UnevenRoundedRectangle(topLeadingRadius: top ? r : 0, bottomLeadingRadius: bottom ? r : 0,
                                          bottomTrailingRadius: bottom ? r : 0, topTrailingRadius: top ? r : 0,
                                          style: .continuous).fill(Theme.Fill.well)
        return StrokeShapeView(shape: WorkCardEdges(top: top, bottom: bottom, radius: r),
                        style: Theme.cardStroke, strokeStyle: StrokeStyle(lineWidth: 0.5),
                        isAntialiased: true, background: fill)
            .padding(.top, top ? 0 : -Self.bridge)
            .padding(.bottom, bottom ? 0 : -Self.bridge)
    }

    /// The hairline between two steps, in the gap above a row, from the words' column (none on the timeline: the rail
    /// joins the steps).
    @ViewBuilder static var separator: some View {
        if !timeline {
            Rectangle().fill(Color(uiColor: .separator)).frame(height: Theme.Size.hairline)
                .padding(.leading, textColumn)
                .offset(y: -bridge)
                .allowsHitTesting(false)
        }
    }
}

/// The card's outline for one slice: the sides, and the rounded top or bottom where the card starts or ends.
private nonisolated struct WorkCardEdges: Shape {
    var top: Bool
    var bottom: Bool
    var radius: CGFloat

    func path(in rect: CGRect) -> Path {
        var p = Path()
        let r = min(radius, rect.height / 2, rect.width / 2)
        // Left side, bottom to top, then across the top and down the right side.
        p.move(to: CGPoint(x: rect.minX, y: bottom ? rect.maxY - r : rect.maxY))
        if top {
            p.addLine(to: CGPoint(x: rect.minX, y: rect.minY + r))
            p.addArc(tangent1End: CGPoint(x: rect.minX, y: rect.minY), tangent2End: CGPoint(x: rect.minX + r, y: rect.minY), radius: r)
            p.addLine(to: CGPoint(x: rect.maxX - r, y: rect.minY))
            p.addArc(tangent1End: CGPoint(x: rect.maxX, y: rect.minY), tangent2End: CGPoint(x: rect.maxX, y: rect.minY + r), radius: r)
        } else {
            p.addLine(to: CGPoint(x: rect.minX, y: rect.minY))
            p.move(to: CGPoint(x: rect.maxX, y: rect.minY))
        }
        p.addLine(to: CGPoint(x: rect.maxX, y: bottom ? rect.maxY - r : rect.maxY))
        if bottom {
            p.addArc(tangent1End: CGPoint(x: rect.maxX, y: rect.maxY), tangent2End: CGPoint(x: rect.maxX - r, y: rect.maxY), radius: r)
            p.addLine(to: CGPoint(x: rect.minX + r, y: rect.maxY))
            p.addArc(tangent1End: CGPoint(x: rect.minX, y: rect.maxY), tangent2End: CGPoint(x: rect.minX, y: rect.maxY - r), radius: r)
        }
        return p
    }
}

/// The quiet line under a message: what the agent did for it. Finished: "工具 3 · 思考 1 · 1 分 20 秒 ›";
/// running: a spinner and "正在工作 39 秒 · 工具 2" (ticking) with the terminal at the end. Tap to list every
/// step right here (tools, thoughts, notices, earlier replies); tap the same line again to fold.
struct WorkSummary {
    let full: [String]
    let short: [String]

    init(_ items: [WorkItem]) {
        var calls = 0, thoughts = 0, notices = 0, texts = 0
        for item in items {
            switch item {
            case .step(.call): calls += 1
            case .step(.folded(let f)): // work the page left out counts as what it stands for
                calls += f.fold?.tools ?? 0
                thoughts += f.fold?.thoughts ?? 0
            case .step(.thought(let thought)):
                if thought.kind == .notice {
                    if thought.wake == true { calls += 1 } else { notices += 1 }
                } else { thoughts += 1 }
            case .text: texts += 1
            }
        }
        var full: [String] = []
        var short: [String] = []
        if calls > 0 { full.append("工具 \(calls)"); short.append("工具 \(calls)") }
        if thoughts > 0 { full.append("思考 \(thoughts)") }
        if texts > 0 { full.append("回复 \(texts)") }
        if full.isEmpty && notices > 0 { full.append("后台通知 \(notices)") }
        if short.isEmpty && notices > 0 { short.append("后台通知 \(notices)") }
        self.full = full
        self.short = short
    }
}

struct WorkLine: View {
    private let isEmpty: Bool
    private let counts: WorkSummary
    let live: Bool
    let start: Date?
    let end: Date?
    let open: Bool
    let openTerminal: (() -> Void)?
    let toggle: () -> Void

    init(items: [WorkItem], live: Bool, start: Date?, end: Date?, open: Bool,
         openTerminal: (() -> Void)?, toggle: @escaping () -> Void) {
        isEmpty = items.isEmpty
        counts = WorkSummary(items)
        self.live = live
        self.start = start
        self.end = end
        self.open = open
        self.openTerminal = openTerminal
        self.toggle = toggle
    }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("WorkLine")
        #endif
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            HStack(spacing: Theme.Space.m) {
                Button(action: toggle) {
                    HStack(spacing: Theme.Space.sm) {
                        // Working: the state's amber spinner, the words stay secondary (the dot says the state).
                        if live { ProgressView().controlSize(.mini).tint(Theme.busy) }
                        // Whole words, never cut: the full summary, or a shorter one when the row is narrow.
                        if live {
                            TimelineView(.periodic(from: .now, by: 1)) { ctx in summary(now: ctx.date) }
                        } else {
                            summary(now: end ?? .now)
                        }
                        if !isEmpty {
                            Image(systemName: Theme.Icon.disclosure)
                                .font(Theme.Font.labelStrong)
                                .rotationEffect(.degrees(open ? 90 : 0))
                        }
                    }
                    .font(Theme.Font.caption.monospacedDigit())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .disabled(isEmpty)
                .accessibilityLabel(open ? "收起过程" : "展开过程")
                .accessibilityValue(label(now: end ?? .now, short: false))
                .accessibilityIdentifier("work-head")
                Spacer(minLength: 0)
                if let openTerminal {
                    Button(action: openTerminal) { Image(systemName: Theme.Icon.terminal).font(Theme.Font.caption).foregroundStyle(.secondary) }
                        .buttonStyle(.plain)
                        .accessibilityLabel("打开终端")
                }
            }
        }
    }

    private func summary(now: Date) -> some View {
        ViewThatFits(in: .horizontal) {
            Text(label(now: now, short: false)).fixedSize()
            Text(label(now: now, short: true)).fixedSize()
        }
    }

    private func label(now: Date, short: Bool) -> String {
        var parts = short ? counts.short : counts.full
        let took = Self.duration(from: start, to: now)
        if live { parts.insert("正在工作 \(took)", at: 0) } else if !took.isEmpty { parts.append(took) }
        return parts.isEmpty ? "已完成" : parts.joined(separator: " · ")
    }

    static func duration(from start: Date?, to end: Date) -> String {
        guard let start else { return "" }
        let s = max(0, Int(end.timeIntervalSince(start)))
        if s < 60 { return "\(s) 秒" }
        if s < 3600 { return s % 60 == 0 ? "\(s / 60) 分" : "\(s / 60) 分 \(s % 60) 秒" }
        return "\(s / 3600) 小时 \(s % 3600 / 60) 分"
    }
}

private struct EditIndex: Identifiable { var id: Int }


/// The chat list's offset and visible height, watched together (see `scrolled`).
private struct ListScroll: Equatable {
    var offset: CGFloat
    var height: CGFloat
    var x: CGFloat
}

private struct ChatRegion<Content: View>: View {
    let name: StaticString
    @ViewBuilder var content: () -> Content

    var body: some View {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event(name)
        #endif
        content()
    }
}

private struct ChatLiveText: View {
    let store: ChatStore

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("ChatLiveText")
        #endif
        let live = store.liveText
        let lastText: String? = {
            if case .assistant(let item) = store.rows.last { return item.text }
            return nil
        }()
        if !live.isEmpty && !(lastText.map { $0.hasPrefix(live) || live == $0 } ?? false) {
            StreamingText(text: live).id("live").flippedBack()
        }
    }
}

private struct ChatStreamFollow: ViewModifier {
    let store: ChatStore
    let follow: () -> Void

    func body(content: Content) -> some View {
        content.onChange(of: store.liveText.count / 40) { _, _ in follow() }
    }
}

/// How far the Agents header is folded (0 = open, maxCollapse = just the slim bar). Written on every scroll frame,
/// read only by `ChromeHeader`, so following the finger redraws the header and nothing else.
@Observable
final class ChatChrome {
    static let compactHeight: CGFloat = 40
    var collapse: CGFloat = 0
    /// The open header's height, measured.
    var height: CGFloat = 0
    /// The list's offset (0 = newest), kept for deciding where to settle.
    @ObservationIgnored var offset: CGFloat = 0
    var maxCollapse: CGFloat { max(0, height - Self.compactHeight) }
}

/// The open header slides up under a shrinking clip while the slim bar fades in over it: one continuous motion,
/// driven by `chrome.collapse` (the finger, or a short animation when it settles).
private struct ChromeHeader<Full: View, Compact: View>: View {
    var chrome: ChatChrome
    /// Any touch on the header (switching agents, scrolling the strip): it stays open while in use.
    var touched: () -> Void
    @ViewBuilder var full: () -> Full
    @ViewBuilder var compact: () -> Compact

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("ChromeHeader")
        #endif
        let maxC = chrome.maxCollapse
        let c = min(max(chrome.collapse, 0), maxC)
        let p = maxC > 0 ? c / maxC : 0 // 0 open … 1 folded
        ZStack(alignment: .top) {
            ChatRegion(name: "ChromeFull.body", content: full)
                .fixedSize(horizontal: false, vertical: true)
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { chrome.height = $0 }
                .offset(y: -c)
                .opacity(Double(1 - min(1, p * 1.5)))
                .allowsHitTesting(p < 0.5)
            ChatRegion(name: "ChromeCompact.body", content: compact)
                .opacity(Double(max(0, (p - 0.5) * 2)))
                .allowsHitTesting(p >= 0.5)
        }
        .frame(height: chrome.height > 0 ? max(chrome.height - c, ChatChrome.compactHeight) : nil, alignment: .top)
        .clipped()
        .background(.bar, ignoresSafeAreaEdges: .top)
        .simultaneousGesture(DragGesture(minimumDistance: 0).onChanged { _ in touched() })
    }
}

/// Per-chat bookkeeping that must not redraw the page when it changes. A plain class held in @State: SwiftUI keeps
/// the same instance across renders but does not observe it.
final class ChatScratch {
    var prewarmSource = ""
    var prewarmedIDs = Set<String>()
    struct LinesKey: Equatable {
        var store: String
        var version: Int
        var first: String?
        var live: String?
        var open: Set<String>
        var runs: Set<String>
        var frozen: ChatView.Frozen?
    }
    /// What was sent from this chat lately: its transcript copy arriving brings you to the newest, others do not.
    var sentHere: [String] = []
    /// When the last message went out from here, and when the user last took the list (a drag): a message's
    /// transcript copy only brings you to the newest if you have not scrolled since sending it.
    var sentAt = Date.distantPast
    var lastUserScroll = Date.distantPast
    /// The frozen chat's rows up to its last exchange (`ChatStore.rebuild`).
    var frozenSettled: ChatStore.SettledRows?
    /// Rows on screen (for remembering where you left off), written by the list as it scrolls.
    var visibleLines: [String] = []
    /// The finger is on the list (or it is gliding after a flick): only then does the chrome follow it.
    var userDragging = false
    /// Between a landing's first jump and its end (`ChatView.landed`): the list is not where it is going yet.
    var landingInFlight = false
    /// Last time the user scrolled or touched the header: the chrome folds itself after a few idle seconds.
    var lastActivity = Date.now
    var linesKey: LinesKey?
    var lines: [ChatLine] = []
    /// Where each open work line's header is on screen (global midY), and the list's frame: folding or opening from
    /// the header keeps the header under the finger.
    var workHeadY: [String: CGFloat] = [:]
    var listFrame: CGRect = .zero
    /// The bottom on screen of the line being landed on (`ChatView.landing`).
    var landingMaxY: CGFloat?
    /// The list's horizontal offset at rest (-16: the side margins are content margins). Vertical jumps keep it:
    /// `scrollTo(y:)` set it to 0 and the page lost its margins.
    var offsetX: CGFloat = 0
    #if DEBUG
    var endJumps = 0 // jumps to the newest (scrollToEnd), for the scroll probe
    var lineY: [String: CGFloat] = [:] // each line's top on screen, for the fold probe (-showFoldProbe)
    #endif
}

extension View {
    /// DEBUG, with -showFoldProbe: records the line's top on screen (fold probe). Otherwise nothing.
    @ViewBuilder func debugLineY(_ id: String, _ scratch: ChatScratch) -> some View {
        #if DEBUG
        if ChatView.foldProbe {
            onGeometryChange(for: CGFloat.self, of: { $0.frame(in: .global).minY }) { scratch.lineY[id] = $0 }
        } else {
            self
        }
        #else
        self
        #endif
    }
}

extension View {
    /// Reports this line's bottom on screen while the chat lands on it (`ChatView.landUnderInput`).
    @ViewBuilder func landingFrame(_ on: Bool, _ scratch: ChatScratch) -> some View {
        if on {
            onGeometryChange(for: CGFloat.self, of: { $0.frame(in: .global).maxY }) { scratch.landingMaxY = $0 }
        } else {
            self
        }
    }
}

private extension View {
    /// Hidden but keeping its room, so the title stays centered.
    func noBackWhen(_ hidden: Bool) -> some View {
        opacity(hidden ? 0 : 1).disabled(hidden).accessibilityHidden(hidden)
    }
}

private extension View {
    /// Whether the system presents this page's bars down the side of the screen (iPhone Duo); always false before iOS 27.1.
    func readsVerticalBars(_ vertical: Binding<Bool>) -> some View { modifier(VerticalBarsReader(vertical: vertical)) }
}

private struct VerticalBarsReader: ViewModifier {
    @Binding var vertical: Bool

    func body(content: Content) -> some View {
        #if compiler(>=6.3) && os(iOS) // `toolbarVerticalEdge` is in the iOS 27.1 SDK; Xcode 26 (Swift 6.2) has no such key path
        if #available(iOS 27.1, *) {
            content.modifier(VerticalBarsEdgeReader(vertical: $vertical))
        } else {
            content
        }
        #else
        content
        #endif
    }
}

#if compiler(>=6.3) && os(iOS)
@available(iOS 27.1, *)
private struct VerticalBarsEdgeReader: ViewModifier {
    @Environment(\.toolbarVerticalEdge) private var edge
    @Binding var vertical: Bool

    func body(content: Content) -> some View {
        content.onChange(of: edge != nil, initial: true) { _, v in vertical = v }
    }
}
#endif
