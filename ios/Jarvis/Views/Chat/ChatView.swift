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
    @State private var position = ScrollPosition(edge: .top) // .top is the newest end of the flipped list
    /// The bottom sentinel is on screen. Starts false: if the first jump lands in unrendered space the arrow is the
    /// way out, and `settleAtEnd` retries until the sentinel has been seen.
    @State private var atBottom = false
    /// Hide the arrow for the first moment on entry (it would flash while the list settles at the end).
    @State private var settled = false
    @State private var showTerminal = false
    @State private var file: FileRef?
    @State private var debugImages: [ImagePayload]?
    @State private var showInfo = false
    @State private var pane: SessionPane = .chat
    @State private var git: GitStatus?
    @State private var menu = MessageMenu()
    /// Work lines the user tapped open (by line id; kept here so lazy rows coming back keep their state).
    @State private var openWork: Set<String> = []
    /// While you read history, lines newer than this one wait (inserting at the newest end of the flipped list would
    /// push what you read); history loaded at the far end still shows. Cleared when you are back at the newest.
    @State private var frozenAt: String?
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
    @State private var visibleLines: [String] = []

    private enum Entry: Equatable { case latestAnswer, line(String) }

    private var readKey: String { "chat.read.\(shownKey.machineID)/\(shownKey.sessionID)" }
    @State private var renaming: Session?
    @State private var closing: Session?
    /// The turn whose "后台任务完成" drawer is open.
    @State private var wakeDrawer: String?
    @Environment(\.dismiss) private var dismissChat
    /// Safari-style chrome (title row, agent strip, tabs): it follows the finger as the list scrolls and settles
    /// open or folded when the finger lifts. Its own object so the per-frame updates only redraw the header.
    @State private var chrome = ChatChrome()
    /// The finger is on the list (or the list is gliding after a flick): only then does the chrome follow it.
    @State private var userDragging = false
    @Environment(\.openSpace) private var openSpace
    @State private var link: LinkTarget?

    init(key: SessionKey, style: ChatStyle = .dev) {
        _key = State(initialValue: key)
        self.style = style
    }

    /// A subagent of this session being looked at instead of the main agent (read-only, same message UI).
    @State private var viewing: Subagent?

    /// What the chat shows: the main agent, or the subagent picked in the agent strip.
    private var shownKey: SessionKey { viewing.map { SessionKey(machineID: key.machineID, sessionID: $0.sessionID) } ?? key }
    private var store: ChatStore { model.chat(shownKey) }
    private var session: Session? { model.session(key) }
    /// The latest state of the subagent being viewed (the list refreshes with the session).
    private var viewedAgent: Subagent? { viewing.flatMap { v in session?.subagents?.first { $0.id == v.id } ?? v } }
    private var status: SessionStatus {
        if let a = viewedAgent { return a.running ? .working : .idle }
        return session?.status ?? .unknown
    }

    var body: some View {
        paneContent
        .background(Theme.pageBackground)
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        // Agents chats draw their own header (devChrome) so it can follow the scroll; the assistant keeps the system bar.
        .toolbarVisibility(style == .dev ? .hidden : .visible, for: .navigationBar)
        .overlay(alignment: .top) {
            if style == .dev { devChrome }
        }
        .overlay(alignment: .top) {
            if showInfo {
                DevInfoCard(key: key, session: session)
                    .padding(.horizontal, 12)
                    .padding(.top, (style == .dev ? chrome.height : 0) + 6)
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .toolbar {
            if style == .assistant {
                ToolbarItem(placement: .principal) { titleButton }
                ToolbarItem(placement: .topBarTrailing) { AssistantModelMenu(key: key) }
                ToolbarItem(placement: .topBarTrailing) {
                    Menu { moreMenuItems } label: { Image(systemName: "ellipsis") }
                }
            }
        }
        .task(id: "\(key.machineID)/\(key.sessionID)|\(session?.cwd ?? "")") { await loadGit() }
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
        .toolbar(style == .dev ? .hidden : .automatic, for: .tabBar)
        .fullScreenCover(isPresented: $showTerminal) { TerminalSheet(key: key) }
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
        .environment(\.openFile) { path in
            file = FileRef(machineID: key.machineID, path: path, cwd: session?.cwd)
        }
        .environment(\.chatImages, store.items.compactMap(\.image))
        .environment(\.openLink) { url in link = LinkTarget(url: url) }
        // Web links open in the in-app sheet (Feishu docs in the native reader); anything else as before.
        .environment(\.openURL, OpenURLAction { url in
            guard ["http", "https"].contains(url.scheme?.lowercased() ?? "") else { return .systemAction }
            link = LinkTarget(url: url)
            return .handled
        })
        .sheet(item: $link) { t in LinkSheet(target: t, key: key) }
        .environment(\.attachImage) { [model, key] img in model.composerInbox[key, default: []].append(img) }
        .environment(\.loadImage) { [model, key] path in
            guard let f = try? await model.api?.file(machine: key.machineID, path: path, cwd: nil), let data = f.data else { return nil }
            return ImagePayload(mime: f.mime ?? "image/jpeg", data: data)
        }
        .onAppear(perform: appeared)
        .onDisappear {
            rememberReadPosition()
            if viewing != nil { model.unsubscribe(shownKey) }
            model.unsubscribe(key)
            if model.visibleSession == key { model.visibleSession = nil }
        }
        .onChange(of: session?.state) { _, st in
            if st == .unread && model.visibleSession == key { model.markSeen(key) }
        }
    }

    /// The chat as a flat list of lines (see `ChatLine`): every message of a turn opens its own exchange — the
    /// message, one work line for what the agent did after it, and the agent's last words before the next message.
    private func chatLines(_ blocks: [ChatBlock]) -> [ChatLine] {
        let liveID = liveTurnID
        var out: [ChatLine] = []
        for block in blocks {
            switch block {
            case .row(let r):
                out.append(.row(r))
            case .turn(var t):
                // Work a background task woke the agent for opens with that notice, where a message would be.
                var wokeAt: Date?
                if let first = t.ordered.first, case .notice(let n) = first, n.wake == true {
                    out.append(.row(first))
                    t.ordered.removeFirst()
                    wokeAt = n.ts
                }
                let segments = Self.segments(t.ordered)
                let groupID = "wt-" + t.id
                let messages = segments.compactMap(\.user)
                // A finished turn that took several messages reads as one exchange: the messages in one bubble,
                // one work line for everything, the final answer. Opening the work line splits it back up.
                if t.id != liveID && messages.count > 1 {
                    if !openWork.contains(groupID) {
                        out.append(.mergedUsers(id: "mu-" + t.id, rows: messages))
                        let photos = segments.flatMap { Self.photos($0.rows) }
                        if !photos.isEmpty { out.append(.userImages(id: "img-" + photos[0].id, items: photos)) }
                        let answer = t.ordered.lastIndex { if case .assistant = $0 { true } else { false } }
                        let rest = t.ordered.enumerated().filter { n, r in
                            if case .user = r { return false }
                            return n != answer
                        }.map(\.element)
                        out.append(.work(id: groupID, items: Self.workItems(rest), live: false, start: t.startedAt, end: t.endedAt))
                        if let answer { out.append(.row(t.ordered[answer])) }
                        if !t.wakes.isEmpty { out.append(.wakes(t, live: false)) }
                        out += t.trailing.map { .row($0) }
                        continue
                    }
                    out.append(.splitHeader(id: groupID, count: messages.count))
                }
                for (k, seg) in segments.enumerated() {
                    // Once the answer is in and a background task woke the agent, the new work runs behind the chip.
                    let live = t.id == liveID && k == segments.count - 1 && t.tail.isEmpty
                    if let u = seg.user { out.append(.row(u)) }
                    // Photos sent with the message (Claude inlines a pasted image path as an image block) sit under
                    // it, not in the agent's work.
                    let photos = Self.photos(seg.rows)
                    if !photos.isEmpty { out.append(.userImages(id: "img-" + photos[0].id, items: photos)) }
                    let last = seg.rows.lastIndex { if case .assistant = $0 { true } else { false } }
                    let items = Self.workItems(seg.rows.enumerated().filter { $0.offset != last }.map(\.element))
                    if !items.isEmpty || live {
                        let id = "w-" + (seg.user?.id ?? seg.rows.first?.id ?? t.id)
                        out.append(.work(id: id, items: items, live: live,
                                         start: seg.user?.firstTS ?? (k == 0 ? wokeAt : nil) ?? seg.rows.first?.firstTS ?? t.startedAt,
                                         end: seg.rows.last?.lastTS))
                    }
                    if let last { out.append(.row(seg.rows[last])) }
                }
                // After the answer: one small line for the background tasks; what they led to opens in a drawer.
                if !t.wakes.isEmpty { out.append(.wakes(t, live: t.id == liveID && !t.tail.isEmpty)) }
                out += t.trailing.map { .row($0) }
            }
        }
        return out
    }

    private static func photos(_ rows: [ChatRow]) -> [Item] {
        rows.compactMap { r -> Item? in
            if case .image(let i) = r, i.role == "user", i.toolUseID == nil, i.image != nil { return i }
            return nil
        }
    }

    /// What a work line folds from these rows: tools, thoughts, notices and the agent's words.
    private static func workItems(_ rows: [ChatRow]) -> [WorkItem] {
        var items: [WorkItem] = []
        for r in rows {
            switch r {
            case .thinking(let i):
                if !(i.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { items.append(.step(.thought(i))) }
            case .tools(let steps): items += steps.map { .step($0) }
            case .notice(let i): items.append(.step(.thought(i)))
            case .assistant(let i): items.append(.text(i))
            default: break
            }
        }
        return items
    }

    /// A turn cut at each message: (message, what came after it until the next one).
    private static func segments(_ ordered: [ChatRow]) -> [(user: ChatRow?, rows: [ChatRow])] {
        var out: [(user: ChatRow?, rows: [ChatRow])] = []
        var user: ChatRow?
        var rows: [ChatRow] = []
        var started = false
        for r in ordered {
            if case .user = r {
                if started { out.append((user, rows)) }
                user = r
                rows = []
            } else {
                rows.append(r)
            }
            started = true
        }
        if started { out.append((user, rows)) }
        return out
    }

    @ViewBuilder private func lineView(_ line: ChatLine) -> some View {
        switch line {
        case .row(let r): rowView(r, isLatest: false, live: true)
        case .userImages(_, let items): SentPhotos(images: items.compactMap(\.image))
        case .mergedUsers(_, let rows):
            MergedUserBubble(items: rows.compactMap { if case .user(let i) = $0 { i } else { nil } })
        case .splitHeader(let id, let count):
            Button {
                Haptics.tap()
                withAnimation(.snappy) { _ = openWork.remove(id) }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "rectangle.split.1x2").font(.caption)
                    Text("已按 \(count) 条消息拆开 · 收起").lineLimit(1).fixedSize()
                    Image(systemName: "chevron.up").font(.caption2.weight(.semibold))
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .trailing)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
        case .work(let id, let items, let live, let start, let end):
            WorkLine(items: items, live: live, start: start, end: end, open: openWork.contains(id) != Self.debugOpenAll,
                     openTerminal: live && style == .dev ? { showTerminal = true } : nil) {
                Haptics.tap()
                withAnimation(.snappy) { if openWork.contains(id) { openWork.remove(id) } else { openWork.insert(id) } }
            }
        case .wakes(let t, let live):
            WakeChip(notices: t.wakes, live: live) {
                Haptics.tap()
                wakeDrawer = t.id
            }
        }
    }

    /// The drawer's content: what happened from the first wake on, as messages and work lines.
    private func wakeLines(_ t: Turn, live: Bool) -> [ChatLine] {
        var out: [ChatLine] = []
        let segments = Self.segments(t.afterWake)
        for (k, seg) in segments.enumerated() {
            if let u = seg.user { out.append(.row(u)) }
            let last = seg.rows.lastIndex { if case .assistant = $0 { true } else { false } }
            let isLive = live && k == segments.count - 1
            let items = Self.workItems(seg.rows.enumerated().filter { $0.offset != last }.map(\.element))
            if !items.isEmpty || isLive {
                out.append(.work(id: "dw-" + (seg.rows.first?.id ?? t.id), items: items, live: isLive,
                                 start: seg.rows.first?.firstTS, end: seg.rows.last?.lastTS))
            }
            if let last { out.append(.row(seg.rows[last])) }
        }
        return out
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
            if lines.contains(where: { $0.id == saved }) { id = saved }
        }
        guard let id else { return }
        entry = nil
        followsEnd = false
        Task { @MainActor in
            await Task.yield()
            // Flipped: anchoring at the scroll view's bottom is the top of the screen — the line starts the page.
            position.scrollTo(id: id, anchor: .bottomLeading)
            try? await Task.sleep(for: .milliseconds(300))
            position.scrollTo(id: id, anchor: .bottomLeading)
        }
    }

    /// Remembers the top line on screen when leaving (nothing when at the newest: open at the newest again).
    private func rememberReadPosition() {
        guard viewing == nil else { return }
        if atBottom || visibleLines.isEmpty {
            UserDefaults.standard.removeObject(forKey: readKey)
            return
        }
        let order = chatLines(store.blocks).map(\.id)
        let top = visibleLines.min { (order.firstIndex(of: $0) ?? .max) < (order.firstIndex(of: $1) ?? .max) }
        UserDefaults.standard.set(top, forKey: readKey)
    }

    /// The newest message is at offset 0 of the flipped list: an exact jump, no estimate involved.
    private func scrollToEnd(animated: Bool = false) {
        if animated { withAnimation(.snappy) { position.scrollTo(edge: .top) } } else { position.scrollTo(edge: .top) }
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

    @ViewBuilder private var paneContent: some View {
        switch pane {
        case .chat: chatPane
        case .terminal: TerminalPanel(key: key).padding(.horizontal, 12).padding(.bottom, 8).padding(.top, headerRoom)
        case .git: GitPane(machineID: key.machineID, cwd: session?.cwd ?? "", status: $git).padding(.top, headerRoom)
        case .files: FilesPane(machineID: key.machineID, cwd: session?.cwd ?? "", git: git).padding(.top, headerRoom)
        }
    }

    /// The custom header's full height (Agents): the other panes sit below it, the chat list scrolls under it.
    private var headerRoom: CGFloat { style == .dev ? chrome.height : 0 }

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
            Color.clear.frame(height: 1)
            chatScroll()
                .overlay(alignment: .bottomTrailing) { jumpArrow }
            footer
        }
        .environment(\.openSubagent) { toolUseID in
            if let a = session?.subagents?.first(where: { $0.toolUseID == toolUseID }) { view(a) }
        }
    }

    @ViewBuilder private var footer: some View {
        if let a = viewedAgent {
            SubagentFooter(agent: a) { view(nil) }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
        } else {
            mainFooter.padding(.top, 8)
        }
    }

    @ViewBuilder private var jumpArrow: some View {
            if !atBottom && settled {
                Button(action: jumpToEnd) {
                    Image(systemName: "arrow.down").font(.callout.weight(.semibold)).frame(width: 40, height: 40)
                }
                .themedButton()
                .clipShape(.circle)
                .padding(16)
                .transition(.scale.combined(with: .opacity))
            }
    }

    private var mainFooter: some View {
            VStack(spacing: 10) {
                ForEach(model.interactions(for: key)) { ix in InteractionPanel(interaction: ix) }
                #if DEBUG
                if UserDefaults.standard.bool(forKey: "showSampleQuestion"), let ix = Interaction.sampleQuestion(key) {
                    InteractionPanel(interaction: ix) // `-showSampleQuestion YES`: the question UI, for screenshots
                }
                #endif
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
                Composer(key: key, status: status, placeholder: style == .assistant ? "问 Jarvis…" : "给 \(session?.agentLabel ?? "Claude") 发消息")
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .animation(.snappy, value: status)
            .animation(.snappy, value: model.interactions(for: key).map(\.id))
    }

    /// Switches the chat to a subagent (nil: back to the main agent); its transcript streams like a session's.
    private func view(_ agent: Subagent?) {
        guard agent?.id != viewing?.id else { return }
        Haptics.tap()
        if viewing != nil { model.unsubscribe(shownKey) }
        withAnimation(.snappy) { viewing = agent }
        if agent != nil { model.subscribe(shownKey) }
        followsEnd = true
        frozenAt = nil
        scrollToEnd()
    }

    private func chatScroll() -> some View {
        let store = store
        let rows = store.rows
        var lines = chatLines(Self.debugTrim(store.blocks))
        if let frozenAt, let i = lines.firstIndex(where: { $0.id == frozenAt }) { lines = Array(lines[...i]) }
        let queued = session?.queued ?? []
        let pending = store.pending.filter { p in !queued.contains { PendingMessage.same($0, p.text) } }
        return ScrollView {
            LazyVStack(alignment: .leading, spacing: 14) {
                // Newest first (the list is drawn upside down): the end sentinel, live text, unsent messages, lines.
                Color.clear
                    .frame(height: 1)
                    .id(Self.bottomID)
                    .onScrollVisibilityChange(threshold: 0.01) { visible in
                        atBottom = visible
                        if visible { followsEnd = true } // back at the newest: follow new messages again
                    }
                if let err = store.error {
                    Label(err, systemImage: "exclamationmark.triangle").font(.caption).foregroundStyle(Theme.danger).flippedBack()
                }
                if style == .assistant {
                    let live = store.liveText
                    let lastText: String? = { if case .assistant(let i) = rows.last { return i.text } else { return nil } }()
                    if !live.isEmpty && !(lastText.map { $0.hasPrefix(live) || live == $0 } ?? false) {
                        StreamingText(text: live).id("live").flippedBack()
                    } else if status == .working && live.isEmpty {
                        TypingIndicator(text: "Jarvis 正在思考…").id("typing").flippedBack()
                    }
                }
                // Sent from here, not in the transcript yet (ones Claude queued show in the queue strip instead).
                ForEach(pending.reversed()) { msg in
                    PendingBubble(msg: msg, retry: { model.retry(key, msg) }, discard: { model.discard(key, msg) })
                        .flippedBack()
                        .id(msg.id.uuidString)
                }
                // Every message, timer line, tool count and answer is its own lazy row (a running turn can hold
                // hundreds of steps; one stack of them froze the page).
                ForEach(lines.reversed()) { line in
                    lineView(line)
                        .flippedBack()
                        .id(line.id)
                        .zIndex(menu.selected != nil && line.holds(menu.selected) ? 1 : 0)
                }
                if !store.loadedOnce && rows.isEmpty {
                    ChatSkeleton().flippedBack()
                } else if rows.isEmpty {
                    EmptyStateView(symbol: "text.bubble", title: "还没有消息", message: "发一句话开始吧。").flippedBack()
                }
                if store.hasMore {
                    // Reaching the far (oldest) end loads the page before it — only once the user has dragged the list.
                    ProgressView()
                        .controlSize(.small)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 6)
                        .task(id: "\(rows.first?.id ?? "")|\(userScrolled)") { if userScrolled { await model.loadOlder(shownKey) } }
                }
            }
            .scrollTargetLayout()
        }
        // Side margins as scroll content margins, not padding: jumping to a row aligns its leading edge with the
        // margin instead of the screen edge (padding shifted the page 16pt sideways after landing on a row).
        .contentMargins(.horizontal, 16, for: .scrollContent)
        .onScrollTargetVisibilityChange(idType: String.self) { ids in visibleLines = ids }
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
        .scrollDismissesKeyboard(.interactively)
        .simultaneousGesture(TapGesture().onEnded { menu.dismiss() })
        // Only the user's own scrolling closes the bar; content streaming in also moves the offset (.animating).
        .onScrollGeometryChange(for: ListScroll.self, of: { ListScroll(offset: $0.contentOffset.y, height: $0.containerSize.height) }) { old, new in
            chrome.offset = new.offset
            // A height change (keyboard) moves the offset too: that is not the user scrolling.
            if old.height != new.height { return }
            scrolled(by: new.offset - old.offset)
        }
        .onScrollPhaseChange { old, phase in
            // The user's own scrolling: a drag, and the glide after a flick. Deceleration alone is not proof — landing
            // on the entry line produced it with no finger down and folded the chrome on open.
            let flick = phase == .decelerating && (old == .tracking || old == .interacting || userDragging)
            userDragging = phase == .tracking || phase == .interacting || flick
            if phase == .idle, old != .idle, old != .animating { settleChrome() } // the finger lifted, the glide ended
            if phase == .tracking || phase == .interacting {
                menu.dismiss()
                userScrolled = true
                followsEnd = false // the user has the list; nothing moves it until they return to the newest
            } else if phase == .idle, userScrolled, atBottom {
                followsEnd = true
            }
        }
        .environment(menu)
        .sheet(item: $menu.selecting) { t in SelectableTextSheet(text: t.text) }
        .sheet(item: $menu.sharing) { m in MessageShareSheet(message: m) }
        .sheet(isPresented: Binding(get: { wakeDrawer != nil }, set: { if !$0 { wakeDrawer = nil } })) {
            // Looked up live: the drawer follows the work while the agent is still on it.
            let t = store.blocks.lazy.compactMap { b -> Turn? in if case .turn(let t) = b, t.id == wakeDrawer { t } else { nil } }.first
            WakeDrawer(notices: t?.wakes ?? []) {
                if let t { ForEach(wakeLines(t, live: t.id == liveTurnID && !t.tail.isEmpty)) { lineView($0) } }
            }
            .environment(menu)
        }
        // New content while following: back to offset 0 (exact; nothing to estimate).
        .onChange(of: followsEnd) { _, follows in
            frozenAt = follows ? nil : chatLines(Self.debugTrim(store.blocks)).last?.id
        }
        .onChange(of: rows.last?.id) { _, _ in if followsEnd { scrollToEnd() } }
        .onChange(of: lines.count, initial: true) { _, _ in landEntry(lines) }
        .onChange(of: liveTurnID) { _, _ in if followsEnd { scrollToEnd() } }
        // Following the newest as it streams in folds the chrome too (once the user has taken the list).
        .onChange(of: store.items.count) { _, _ in if followsEnd { scrollToEnd(); if userScrolled { hideChrome() } } }
        .onChange(of: store.liveText.count / 40) { _, _ in if followsEnd { scrollToEnd(); if userScrolled { hideChrome() } } }
        // Your own message always brings you to the newest.
        .onChange(of: lastPromptID) { old, _ in if old != nil { followsEnd = true; scrollToEnd() } }
        .onChange(of: store.pending.count) { old, new in if new > old { followsEnd = true; scrollToEnd(animated: true) } }
    }

    /// The flipped list's offset grows toward older messages. Like Safari's toolbar, the chrome follows the finger:
    /// reading on (toward the newest) folds it as far as the finger goes, going back into the history unfolds it.
    private func scrolled(by delta: CGFloat) {
        guard style == .dev, pane == .chat, userDragging, delta != 0 else { return }
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
            withAnimation(.smooth(duration: 0.3)) { chrome.collapse = chrome.collapse > full / 2 ? full : 0 }
        }
    }

    private func hideChrome() {
        guard style == .dev, pane == .chat, chrome.collapse < chrome.maxCollapse else { return }
        withAnimation(.smooth(duration: 0.35)) { chrome.collapse = chrome.maxCollapse }
        showInfo = false
    }

    private func showChrome() {
        guard chrome.collapse > 0 else { return }
        withAnimation(.smooth(duration: 0.35)) { chrome.collapse = 0 }
    }

    /// The chrome folded away: back, and where you are ("app › leader · 对话") — tap it for everything again.
    private var compactChrome: some View {
        HStack(spacing: 0) {
            Button { dismissChat() } label: {
                Image(systemName: "chevron.left").font(.system(size: 15, weight: .semibold)).frame(width: 44, height: 34)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("返回")
            Spacer(minLength: 0)
            Button { Haptics.tap(); showChrome() } label: {
                HStack(spacing: 6) {
                    if let s = session { StatusDot(state: s.state, size: 7) }
                    Text(compactPlace).lineLimit(1).truncationMode(.middle)
                }
                .font(.footnote.weight(.semibold))
                .padding(.horizontal, 12)
                .frame(height: 28)
                .background(Color.primary.opacity(0.06), in: .capsule)
                .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(compactPlace)，显示标题和标签")
            Spacer(minLength: 0)
            Color.clear.frame(width: 44, height: 34) // balances the back button: the place stays centered
        }
        .padding(.horizontal, 4)
        .frame(height: ChatChrome.compactHeight)
    }

    /// The "…" menu, in the custom header (Agents) or the system bar (assistant).
    @ViewBuilder private var moreMenuItems: some View {
            if style == .dev {
                Button { withAnimation(.snappy) { pane = .terminal } } label: { Label("终端", systemImage: "terminal") }
            }
            if status.isActive {
                Button { Task { await model.interrupt(key) } } label: { Label("中断", systemImage: "stop.circle") }
            }
            if style == .dev, let s = session {
                Button { renaming = s } label: { Label("重命名…", systemImage: "pencil") }
            }
            if style == .dev {
                Button { model.toggleMute(key) } label: {
                    Label(model.isMuted(key) ? "开启通知" : "屏蔽通知", systemImage: model.isMuted(key) ? "bell" : "bell.slash")
                }
            }
            Divider()
            if let s = session {
                Button { UIPasteboard.general.string = "claude --resume \(s.sessionID)" } label: {
                    Label("拷贝恢复命令", systemImage: "doc.on.doc")
                }
                Button { UIPasteboard.general.string = s.cwd } label: { Label("拷贝目录", systemImage: "folder") }
                if let space = s.spaceKey, let openSpace {
                    Button { openSpace(space) } label: { Label("空间里的所有 Agent", systemImage: "square.stack.3d.up") }
                }
                if s.status.isLive {
                    Button(role: .destructive) { closing = s } label: {
                        Label("关闭 Agent…", systemImage: "xmark.circle")
                    }
                }
            }
    }

    /// A custom title: tapping it toggles the developer info card (instead of the system scroll-to-top).
    private var titleButton: some View {
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
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(title)，\(showInfo ? "收起" : "查看")会话信息")
    }

    /// The Agents header: title row, agent strip and tabs, folding into a slim bar as the list scrolls.
    private var devChrome: some View {
        ChromeHeader(chrome: chrome) {
            VStack(spacing: 0) {
                HStack(spacing: 8) {
                    Button { dismissChat() } label: {
                        Image(systemName: "chevron.left").font(.system(size: 17, weight: .semibold)).frame(width: 44, height: 44)
                            .contentShape(.circle)
                    }
                    .buttonStyle(.plain)
                    .surface(interactive: true, in: .circle)
                    .accessibilityLabel("返回")
                    Spacer(minLength: 0)
                    titleButton
                    Spacer(minLength: 0)
                    Menu { moreMenuItems } label: {
                        Image(systemName: "ellipsis").font(.system(size: 17, weight: .semibold)).frame(width: 44, height: 44)
                            .contentShape(.circle)
                    }
                    .surface(interactive: true, in: .circle)
                }
                .padding(.horizontal, 16)
                .padding(.top, 2)
                .padding(.bottom, 6)
                SiblingStrip(current: key) { next in switchTo(next) }
                SessionPaneBar(pane: $pane, panes: panes, badges: paneBadges)
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
    }

    private var compactPlace: String {
        let place = [session?.spaceName, session?.slotLabel].compactMap { $0 }.joined(separator: " › ")
        return (place.isEmpty ? title : place) + " · " + pane.title
    }

    private func appeared() {
            bindMenu()
            if !entered { // only on entry: coming back from a full-screen cover must not move what you were reading
                entered = true
                if style == .dev, session?.state == .unread {
                    entry = .latestAnswer // finished, not seen: read the latest answer from its start
                } else if style == .dev, let saved = UserDefaults.standard.string(forKey: readKey) {
                    entry = .line(saved) // where you left off
                }
                settleAtEnd()
            }
            model.subscribe(key)
            model.visibleSession = key
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
            if let p = UserDefaults.standard.string(forKey: "openPane").flatMap(SessionPane.init(rawValue:)) { pane = p }
            if let f = UserDefaults.standard.string(forKey: "openFile") { file = FileRef(machineID: key.machineID, path: f, cwd: session?.cwd) }
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
        if viewing != nil { model.unsubscribe(shownKey); viewing = nil }
        model.unsubscribe(key)
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
    @State private var editingIndex: Int?
    @FocusState private var focused: Bool

    private var canSend: Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.isEmpty || !(model.composerContexts[key] ?? []).isEmpty
    }

    private var contexts: Binding<[ContextRef]> {
        Binding(get: { model.composerContexts[key] ?? [] }, set: { model.composerContexts[key] = $0.isEmpty ? nil : $0 })
    }

    var body: some View {
        VStack(spacing: 8) {
            if !(model.composerContexts[key] ?? []).isEmpty { ContextChips(refs: contexts) }
            if !images.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack {
                        ForEach(Array(images.enumerated()), id: \.offset) { idx, pair in
                            Image(uiImage: pair.0).resizable().scaledToFill().frame(width: 56, height: 56)
                                .clipShape(.rect(cornerRadius: 12, style: .continuous))
                                .overlay(alignment: .bottomLeading) {
                                    Image(systemName: "pencil.tip.crop.circle.fill").font(.caption).foregroundStyle(.white)
                                        .shadow(radius: 2).padding(3)
                                }
                                .onTapGesture { editingIndex = idx } // mark it up before sending
                                .accessibilityLabel("图片 \(idx + 1)，点按编辑")
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
                    #if DEBUG
                    .task { // `-focusComposer YES`: the keyboard up, for screenshots of the chat while typing
                        if UserDefaults.standard.bool(forKey: "focusComposer") {
                            try? await Task.sleep(for: .seconds(6))
                            focused = true
                        }
                    }
                    #endif
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
/// One line of the chat. A turn reads as exchanges: each message, one quiet work line for what the agent did after
/// it ("调用了 3 个工具，思考 1 次 · 1 分 20 秒 ›", tap to see every step), then the agent's last words before the
/// next message. Running and finished turns look the same; the running exchange's work line ticks. Lines are
/// separate lazy rows on purpose (a running turn can have hundreds of steps).
enum ChatLine: Identifiable {
    case row(ChatRow)
    case work(id: String, items: [WorkItem], live: Bool, start: Date?, end: Date?)
    /// The photos a message was sent with.
    case userImages(id: String, items: [Item])
    /// A finished turn's messages in one bubble (folded).
    case mergedUsers(id: String, rows: [ChatRow])
    /// A finished multi-message turn unfolded: "N 条消息 · 收起".
    case splitHeader(id: String, count: Int)
    /// "后台任务完成" under an answer: tap for what the agent did once the tasks reported back.
    case wakes(Turn, live: Bool)

    var id: String {
        switch self {
        case .row(let r): r.id
        case .work(let id, _, _, _, _): id
        case .userImages(let id, _): id
        case .mergedUsers(let id, _): id
        case .splitHeader(let id, _): id + "-h"
        case .wakes(let t, _): "wk-" + t.id
        }
    }

    /// Whether the message whose action bar is open is drawn by this line (it is lifted above its neighbours).
    func holds(_ id: String?) -> Bool {
        guard let id else { return false }
        switch self {
        case .row(let r): return r.id == id
        case .mergedUsers(_, let rows): return rows.first?.id == id
        default: return false
        }
    }

    /// One of your messages (where an exchange starts).
    var isInput: Bool {
        switch self {
        case .row(.user), .mergedUsers: true
        default: false
        }
    }
}

/// What a work line folds: tool calls, thoughts, system notices and the agent's words before its last ones.
enum WorkItem: Identifiable {
    case step(ToolStep)
    case text(Item)

    var id: String {
        switch self {
        case .step(let s): s.id
        case .text(let i): "t-" + i.id
        }
    }
}

/// The quiet line under a message: what the agent did for it. Finished: "工具 3 · 思考 1 · 1 分 20 秒 ›";
/// running: a spinner and "正在工作 39 秒 · 工具 2" (ticking) with the terminal at the end. Tap to list every
/// step right here (tools, thoughts, notices, earlier replies); tap the same line again to fold.
struct WorkLine: View {
    @Environment(\.openSubagent) private var openSubagent
    var items: [WorkItem]
    var live: Bool
    var start: Date?
    var end: Date?
    var open: Bool
    var openTerminal: (() -> Void)?
    var toggle: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 10) {
                Button(action: toggle) {
                    HStack(spacing: 6) {
                        if live { ProgressView().controlSize(.mini) }
                        // Whole words, never cut: the full summary, or a shorter one when the row is narrow.
                        if live {
                            TimelineView(.periodic(from: .now, by: 1)) { ctx in summary(now: ctx.date) }
                                .foregroundStyle(Theme.working)
                        } else {
                            summary(now: end ?? .now)
                        }
                        if !items.isEmpty {
                            Image(systemName: "chevron.right")
                                .font(.caption2.weight(.semibold))
                                .rotationEffect(.degrees(open ? 90 : 0))
                        }
                    }
                    .font(.footnote.monospacedDigit())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .disabled(items.isEmpty)
                .accessibilityLabel(open ? "收起过程" : "展开过程")
                Spacer(minLength: 0)
                if let openTerminal {
                    Button(action: openTerminal) { Image(systemName: "terminal").font(.footnote).foregroundStyle(.secondary) }
                        .buttonStyle(.plain)
                        .accessibilityLabel("打开终端")
                }
            }
            if open && !items.isEmpty {
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(items) { item in
                        switch item {
                        case .step(.call(let c)):
                            VStack(alignment: .leading, spacing: 4) {
                                ToolCallRow(call: c, autoExpand: false)
                                if c.name == "Agent" || c.name == "Task", let open = openSubagent, let id = c.use.toolUseID {
                                    Button { open(id) } label: {
                                        Label("查看子 Agent", systemImage: "arrow.up.right").font(.caption.weight(.semibold)).lineLimit(1)
                                    }
                                    .buttonStyle(.plain)
                                    .foregroundStyle(Theme.accent)
                                    .padding(.leading, 12)
                                }
                            }
                        case .step(.thought(let t)):
                            if t.kind == .notice { NoticeRow(item: t) } else { ThinkingRow(item: t, compact: true) }
                        case .text(let i): AssistantText(item: i)
                        }
                    }
                }
                .padding(.leading, 12)
                .overlay(alignment: .leading) { Capsule().fill(Color.secondary.opacity(0.2)).frame(width: 2) }
                .transition(.opacity)
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
        var calls = 0, thoughts = 0, notices = 0, texts = 0
        for item in items {
            switch item {
            case .step(.call): calls += 1
            case .step(.thought(let t)):
                // A background task reporting back counts as one of the agent's tools.
                if t.kind == .notice { if t.wake == true { calls += 1 } else { notices += 1 } } else { thoughts += 1 }
            case .text: texts += 1
            }
        }
        var parts: [String] = []
        // One shape for every count: name, then number ("工具 3 · 思考 2 · 回复 1").
        if calls > 0 { parts.append("工具 \(calls)") }
        if !short {
            if thoughts > 0 { parts.append("思考 \(thoughts)") }
            if texts > 0 { parts.append("回复 \(texts)") }
        }
        if parts.isEmpty && notices > 0 { parts.append("后台通知 \(notices)") }
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
    @ViewBuilder var full: () -> Full
    @ViewBuilder var compact: () -> Compact

    var body: some View {
        let maxC = chrome.maxCollapse
        let c = min(max(chrome.collapse, 0), maxC)
        let p = maxC > 0 ? c / maxC : 0 // 0 open … 1 folded
        ZStack(alignment: .top) {
            full()
                .fixedSize(horizontal: false, vertical: true)
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { chrome.height = $0 }
                .offset(y: -c)
                .opacity(1 - min(1, p * 1.5))
                .allowsHitTesting(p < 0.5)
            compact()
                .opacity(max(0, (p - 0.5) * 2))
                .allowsHitTesting(p >= 0.5)
        }
        .frame(height: chrome.height > 0 ? max(chrome.height - c, ChatChrome.compactHeight) : nil, alignment: .top)
        .clipped()
        .background(.bar, ignoresSafeAreaEdges: .top)
    }
}
