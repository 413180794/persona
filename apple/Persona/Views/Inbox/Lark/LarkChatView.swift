import SwiftUI

/// Message positions are decimal strings that can outgrow Int64 in theory; compare them as numbers.
nonisolated enum LarkPosition {
    static func less(_ a: String, _ b: String) -> Bool {
        let x = a.trimmingCharacters(in: .whitespaces), y = b.trimmingCharacters(in: .whitespaces)
        let nx = x.hasPrefix("-"), ny = y.hasPrefix("-")
        if nx != ny { return nx }
        let ax = nx ? String(x.dropFirst()) : x, ay = ny ? String(y.dropFirst()) : y
        let r = ax.count != ay.count ? ax.count < ay.count : ax < ay
        return nx ? !r && ax != ay : r
    }

    static func next(_ p: String) -> String {
        if let n = Int64(p) { return String(n + 1) }
        return p
    }

    static func prev(_ p: String) -> String {
        if let n = Int64(p) { return String(n - 1) }
        return p
    }
}

/// One Feishu chat, opened like Feishu: at the oldest unread message under a 以下为新消息 line (or at the latest
/// when all is read), newer pages as you scroll down, read state reported only for what was on screen,
/// 回到最新 / 有人@你 jumps.
struct LarkChatView: View {
    @Environment(AppModel.self) private var model
    /// Polled and reported only while its page is in front: a hidden chat reports nothing to Feishu (TabActivity.swift).
    @Environment(\.tabIsActive) private var tabActive
    var conversation: LarkConversation
    @State private var pager = LarkPager()
    /// The hub answered with a topic group's topics for a chat opened without `isTopic` (from a profile, an old inbox item).
    @State private var foundTopic = false
    @State private var loadingOlder = false
    @State private var loadingNewer = false
    /// The first load is on its way (the page shows a spinner when nothing was cached).
    @State private var loading = true
    /// Why the last load failed: the page's placeholder when there is nothing to show, a line on top otherwise.
    @State private var problem: LarkLoadProblem?
    /// What is typed in the box; with `replyTo`, kept as the chat's draft (LarkDrafts) until it is sent.
    @State private var text: String
    /// The message 回复 picked: quoted over the box and by what is sent next.
    @State private var replyTo: LarkReplyTo?
    /// Picked with the @ picker since the last send.
    @State private var mentions: [LarkMention]
    @FocusState private var composerFocused: Bool
    /// What I wrote that is on its way (on screen at once, sent one after another in the background).
    @State private var outbox = LarkChatOutbox()
    @State private var attachments: [LarkAttachment] = []
    @State private var showPhotos = false
    @State private var showFiles = false
    @State private var preparingAttachment = false
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var fileRequest: (url: String, name: String)?
    /// Read position when the chat was opened: the divider sits above the first message after it. Frozen for the visit.
    @State private var anchor: String?
    @State private var atBottom = true
    /// The list stays at its newest end as it changes (larkFollowsEnd): on opening at the latest, after sending,
    /// after 回到最新; off once the user scrolls up or the page opens at a message.
    @State private var following: Bool
    /// The first load has put the page where it opens; until then nothing moves it to the end.
    @State private var landed = false
    @State private var mention: String?
    @State private var person: LarkPersonRef?
    @State private var debugPick: LarkMessage?
    @State private var debugPost: String?
    /// Read receipts of my messages, as shown; the asking is kept in `book`.
    @State private var receiptStates: [String: LarkReadState] = [:]
    /// What rows coming on and off screen keep track of (LarkChatBook).
    @State private var book = LarkChatBook()

    init(conversation: LarkConversation) {
        self.conversation = conversation
        // The draft left here last time, quote and all.
        let draft = LarkDrafts.shared[.chat(conversation.id)]
        _text = State(initialValue: draft.text)
        _replyTo = State(initialValue: draft.replyTo)
        _following = State(initialValue: !Self.opensAtMessage(conversation) && !Self.debugTop)
        _mentions = State(initialValue: draft.mentions ?? [])
    }

    private var isTopic: Bool { conversation.isTopic == true || foundTopic }
    /// Opens at a message (first unread / a flagged message) rather than at the bottom.
    private var opensAtMessage: Bool { Self.opensAtMessage(conversation) }
    private var visibleMessages: [LarkMessage] { pager.messages.filter { !outbox.hidesEcho($0) } }
    private var openingAnchor: UnitPoint { Self.debugTop || opensAtMessage ? .top : .bottom }
    private static func opensAtMessage(_ c: LarkConversation) -> Bool { c.focus != nil || (c.isTopic != true && c.unread > 0) }

    var body: some View {
        let messages = visibleMessages
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                if pager.hasOlder {
                    LarkLoadOlderButton(title: "加载更早的消息", loading: loadingOlder) { Task { await loadOlder() } }
                }
                if hasContent, let line = problem?.inline { Label(line, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger).padding(.bottom, Theme.Space.s) }
                ForEach(Array(messages.enumerated()), id: \.element.id) { i, m in
                    let prev = i > 0 ? messages[i - 1] : nil
                    VStack(alignment: .leading, spacing: 0) {
                        if isFirstUnread(m, after: prev) { LarkNewMessagesDivider() }
                        if LarkChatLayout.showsTimeSeparator(m, after: prev) { LarkTimeSeparator(date: m.createdAt) }
                        let grouped = LarkChatLayout.continues(m, after: prev)
                        LarkMessageRow(message: m, grouped: grouped, isGroup: conversation.isGroup, feedID: conversation.id,
                                       readState: receiptStates[m.id])
                            .padding(.top, LarkChatLayout.gap(grouped: grouped))
                    }
                    .onAppear {
                        markSeen(m)
                        receiptAppeared(m)
                    }
                    .onDisappear { book.receipts.disappeared(m.id) }
                }
                if pager.hasNewer {
                    ProgressView().controlSize(.small).scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.tapTarget)
                        .onAppear { Task { await loadNewer() } }
                } else {
                    outgoingRows
                }
            }
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.m)
        }
        .scrollPosition($position)
        // Where it opens and how it aligns; size changes (the keyboard, the box, new rows) are larkFollowsEnd's.
        .larkFollowsEnd($position, following: $following, resting: openingAnchor, active: landed)
        .defaultScrollAnchor(openingAnchor, for: .initialOffset)
        .defaultScrollAnchor(openingAnchor, for: .alignment)
        // The visible part ends above the composer (bottom inset); containerSize leaves out both insets, so it can't be added to
        // contentOffset: under a tall top bar (opened from the inbox) that never reached the end and the arrow never went away.
        .onScrollGeometryChange(for: Bool.self) { g in
            g.visibleRect.maxY - g.contentInsets.bottom >= g.contentSize.height - 80
        } action: { _, v in atBottom = v }
        .overlay { placeholder }
        .overlay(alignment: .bottomTrailing) { jumpToLatestButton }
        .overlay(alignment: .topTrailing) { mentionButton }
        .environment(\.larkOpenFile, { url, name in fileRequest = (url, name) })
        .environment(\.larkRefresh, { Task { await refreshLatest() } })
        .environment(\.larkOpenProfile, { person = $0 })
        .environment(\.larkReact, { m, key in react(m, key) })
        .environment(\.larkReply, LarkReplyHandler(inTopicGroup: isTopic) { m in
            replyTo = LarkReply.quote(m)
            composerFocused = true
        })
        .modifier(LarkFileOpener(request: $fileRequest))
        .sheet(item: $person) { LarkProfileView(person: $0) }
        .sheet(item: $debugPick) { m in LarkReactionPicker(message: m) { react(m, $0) } }
        .sheet(isPresented: Binding(get: { debugPost != nil }, set: { if !$0 { debugPost = nil } })) {
            if let id = debugPost { LarkMomentsPostView(postID: id) }
        }
        .safeAreaInset(edge: .bottom) { composer }
        .onChange(of: LarkDraft(text: text, replyTo: replyTo, mentions: mentions.isEmpty ? nil : mentions)) { _, draft in
            LarkDrafts.shared.set(draft, for: .chat(conversation.id))
        }
        .onDisappear { LarkDrafts.shared.flush() }
        .modifier(LarkAttachmentPicker(attachments: $attachments, photos: $showPhotos, files: $showFiles, preparing: $preparingAttachment))
        .onChange(of: pager.messages) { _, now in settleOutgoing(now) }
        .themedPage()
        .navigationTitle(conversation.name)
        .navigationSubtitle(isTopic ? "飞书话题群" : "飞书")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar(.hidden, for: .tabBar)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { Task { await askJarvis() } } label: { Label("让 Persona 总结", systemImage: Theme.Icon.ai) }
            }
        }
        .task {
            if let config = model.api?.config { outbox = .shared(config: config, feed: conversation.id) }
            #if DEBUG
            if attachments.isEmpty { attachments = await LarkAttachmentFixture.load(config: model.api?.config) }
            #endif
            LarkMedia.shared.loadEmojiCatalog()
            // The last messages seen here paint at once (offline too); the network only brings them up to date.
            // Background sync (LarkSync) keeps this cache current, so it is usually already the latest. An unread chat
            // opens at its first unread message: the cache serves when it reaches back that far.
            if pager.messages.isEmpty, conversation.focus == nil,
               let cached = StateCache.load("lark-chat-\(conversation.id)", as: [LarkMessage].self), !cached.isEmpty,
               !opensAtMessage || cached.first.map({ !LarkPosition.less(LarkPosition.next(conversation.readPosition ?? "0"), $0.position) }) == true {
                pager.messages = cached
                pager.hasOlder = true
                pager.olderCursor = cached.first?.position
                pager.newerCursor = cached.last?.position
                let read = conversation.readPosition ?? "0"
                if opensAtMessage, let first = cached.first(where: { LarkPosition.less(read, $0.position) }) { await scroll(to: first.id) }
            }
            await load()
            if !outbox.items.isEmpty {
                try? await Task.sleep(for: .milliseconds(60))
                position.scrollTo(edge: .bottom)
            }
        }
        // A fallback: changes arrive as pushes (below). It polls only while the chat's page is in front.
        .tabActiveTask {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(60))
                await refreshLatest()
                askReceiptsAgain()
            }
        }
        .task {
            for await _ in LarkSync.shared.changes(of: conversation.id) {
                await refreshLatest()
                askReceiptsAgain()
            }
        }
        // Back in front: what came on screen while it was in the back is reported now.
        .onTabVisibility(appear: reportSoon, disappear: flushRead)
        // What the jump buttons count is set once the page is in: rows already on screen (the cached page) count.
        .onChange(of: anchor) { pager.seen.formUnion(book.seen) }
        .onChange(of: mention) { pager.seen.formUnion(book.seen) }
    }

    private var composer: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if !attachments.isEmpty {
                ScrollView(.horizontal) {
                    HStack(spacing: Theme.Space.s) {
                        ForEach(attachments) { attachment in
                            HStack(spacing: Theme.Space.xs) {
                                LarkAttachmentView(attachment: attachment, compact: true)
                                Button { attachments.removeAll { $0.id == attachment.id } } label: {
                                    Image(systemName: Theme.Icon.close).font(Theme.Font.secondary)
                                        .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                                }
                                .buttonStyle(.plain).accessibilityLabel("移除 \(attachment.name)")
                            }
                        }
                    }
                }
                .padding(.horizontal, Theme.Space.m)
                .accessibilityIdentifier("lark.draft.attachments")
                if replyTo != nil || !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    Text(replyTo == nil ? "文字和附件将分别发送" : "添加回复文字，或取消引用后发送附件").font(Theme.Font.label).foregroundStyle(.secondary)
                        .padding(.horizontal, Theme.Space.m)
                }
            }
            if preparingAttachment {
                HStack(spacing: Theme.Space.s) {
                    ProgressView().controlSize(.small)
                    Text("正在准备附件…").font(Theme.Font.label).foregroundStyle(.secondary)
                }
            }
            LarkComposer(placeholder: "回复 \(conversation.name)", text: $text, focused: $composerFocused,
                         replyTo: replyTo, cancelReply: { replyTo = nil },
                         attachmentPicker: { pickAttachment(photo: $0) },
                         canAddAttachments: attachments.count < LarkAttachment.selectionLimit,
                         hasAttachments: !attachments.isEmpty, preparingAttachment: preparingAttachment,
                          mentionFeed: conversation.isGroup ? conversation.id : nil,
                          me: pager.messages.last(where: \.isMine)?.senderID, mentions: $mentions, send: send)
        }
        .padding(.top, !attachments.isEmpty || preparingAttachment ? Theme.Space.s : 0)
        .background {
            if !attachments.isEmpty || preparingAttachment {
                Theme.pageBackground.ignoresSafeArea(edges: .bottom)
            }
        }
    }

    // MARK: Jumps

    /// Unread messages from others that have not been on screen yet.
    private var unseenNew: Int {
        guard let anchor else { return 0 }
        let seenUnread = pager.messages.filter { pager.seen.contains($0.position) && LarkPager.countsAsNew($0, past: anchor) }.count
        return max(0, conversation.unread - seenUnread)
    }

    @ViewBuilder private var jumpToLatestButton: some View {
        if pager.hasNewer || !atBottom {
            let n = unseenNew
            Button { Task { await jumpToLatest() } } label: {
                HStack(spacing: Theme.Space.sm) {
                    Image(systemName: Theme.Icon.expand).font(.system(size: 13, weight: .bold))
                    if n > 0 { Text("\(n > 99 ? "99+" : "\(n)") 条新消息").font(Theme.Font.captionMedium).lineLimit(1) }
                }
                .foregroundStyle(LarkColor.blue)
                .padding(.horizontal, n > 0 ? 12 : 0)
                .scaledFrame(minWidth: Theme.Size.wellXL, minHeight: Theme.Size.wellXL)
                .surface(interactive: true, in: .capsule)
                .contentShape(.capsule) // the glass is drawn but not hit-tested: without this only the chevron takes a touch
            }
            .buttonStyle(.plain)
            .padding(.trailing, Theme.Space.l)
            .padding(.bottom, Theme.Space.s)
            .accessibilityLabel(n > 0 ? "\(n) 条新消息，回到最新" : "回到最新")
            .transition(.scale.combined(with: .opacity))
        }
    }

    @ViewBuilder private var mentionButton: some View {
        if let mention, !pager.seen.contains(mention) {
            Button { Task { await jump(to: mention) } } label: {
                Label("有人@你", systemImage: Theme.Icon.mention).font(Theme.Font.captionMedium).lineLimit(1)
                    .foregroundStyle(LarkColor.hue("red"))
                    .padding(.horizontal, Theme.Space.m).scaledFrame(minHeight: Theme.Size.wellM)
                    .surface(interactive: true, in: .capsule)
                    .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .padding(.trailing, Theme.Space.l)
            .padding(.top, Theme.Space.s)
        }
    }

    private func isFirstUnread(_ m: LarkMessage, after prev: LarkMessage?) -> Bool {
        guard let anchor, LarkPosition.less(anchor, m.position) else { return false }
        guard let prev else { return true }
        return !LarkPosition.less(anchor, prev.position)
    }

    #if DEBUG
    /// Simulator automation: `-larkTop YES` starts at the oldest loaded message (screenshots of long chats).
    static var debugTop: Bool { UserDefaults.standard.bool(forKey: "larkTop") }
    #else
    static let debugTop = false
    #endif

    // MARK: Loading

    private func apply(_ r: APIClient.LarkMessages) {
        if r.topic == true { foundTopic = true }
        pager.replace(with: r)
        if !r.hasNewer.map({ $0 }).isTrue { StateCache.save("lark-chat-\(conversation.id)", Array(r.messages.suffix(80))) }
    }

    /// Messages, or mine on their way: a problem is then one line on top rather than the page's placeholder.
    private var hasContent: Bool { !visibleMessages.isEmpty || !outbox.items.isEmpty }

    /// Loading, empty, or what went wrong, while there are no messages to show (BeautifulUI draws its own).
    @ViewBuilder private var placeholder: some View {
        switch LarkChatPlaceholder.of(hasContent: hasContent, loading: loading, problem: problem) {
        case .loading:
            LoadableContent(state: Loadable<Void>.loading, retry: nil) { _ in EmptyView() }
        case .empty:
            EmptyStateView(symbol: Theme.Icon.feishuChats, title: "还没有消息", message: "发出的第一条消息会出现在这里。")
        case .failed(let p):
            EmptyStateView(symbol: p.symbol, title: p.title, message: p.message, action: (title: "重试", run: retry))
        case nil:
            EmptyView()
        }
    }

    private func retry() {
        problem = nil
        loading = true
        Task { await load() }
    }

    private func load() async {
        defer {
            loading = false
            landed = true
        }
        guard let api = model.api else { return }
        do {
            if let focus = conversation.focus {
                apply(try await api.larkMessages(conversation.id, around: focus))
                pager.openRead(at: focus, readPosition: conversation.readPosition)
                if let m = pager.messages.first(where: { $0.position == focus }) { await scroll(to: m.id) }
            } else if opensAtMessage {
                // Feishu opens an unread chat at its read position: the page around the first unread message.
                let read = conversation.readPosition ?? "0"
                anchor = read
                pager.readCursor = read
                if let p = conversation.mentionPosition, LarkPosition.less(read, p) { mention = p }
                apply(try await api.larkMessages(conversation.id, around: LarkPosition.next(read)))
                if let first = pager.messages.first(where: { LarkPosition.less(read, $0.position) }) {
                    await scroll(to: first.id)
                } else {
                    following = true
                    position.scrollTo(edge: .bottom)
                }
            } else {
                apply(try await api.larkMessages(conversation.id, topic: isTopic))
                pager.readCursor = pager.messages.last?.position
            }
            problem = nil
            #if DEBUG
            // Simulator automation: `-larkFocus <messageId>` scrolls to that message (screenshots of one kind of card).
            if let focus = UserDefaults.standard.string(forKey: "larkFocus") { await scroll(to: focus) }
            // `-larkProfile <userId>` opens that person's card, `-larkPicker YES` the reaction picker on the last message.
            if let uid = UserDefaults.standard.string(forKey: "larkProfile") { person = LarkPersonRef(id: uid, name: "", feedID: conversation.id) }
            if UserDefaults.standard.bool(forKey: "larkPicker"), let last = pager.messages.last(where: { !$0.isSystem }) { debugPick = last }
            // `-larkMoments <postId>` opens that removed post.
            if let post = UserDefaults.standard.string(forKey: "larkMoments") { debugPost = post }
            #endif
        } catch {
            problem = LarkLoadProblem.of(error)
        }
    }

    /// Scrolls once the rows exist (the lazy stack lays them out after the data lands).
    private func scroll(to id: String) async {
        following = false
        try? await Task.sleep(for: .milliseconds(80))
        position.scrollTo(id: id, anchor: .top)
        try? await Task.sleep(for: .milliseconds(250))
        position.scrollTo(id: id, anchor: .top)
    }

    /// Polls the tail while it is loaded: new messages append, cards that changed (alert status…) update in place.
    private func refreshLatest() async {
        guard !pager.hasNewer, let api = model.api, let r = try? await api.larkMessages(conversation.id, topic: isTopic) else { return }
        problem = nil // it answers again (lark-connector signed in again, the network is back)
        let fresh = pager.refresh(with: r)
        guard !fresh.isEmpty else { return }
        if atBottom { following = true } // near the end: the new ones come into view (larkFollowsEnd)
        withAnimation(Theme.Motion.standard) { pager.add(fresh) }
    }

    private func loadOlder() async {
        guard let api = model.api, let cursor = pager.olderRequest, !loadingOlder else { return }
        loadingOlder = true
        defer { loadingOlder = false }
        guard let r = try? await api.larkMessages(conversation.id, before: cursor, topic: isTopic) else { return }
        pager.prepend(r)
    }

    private func loadNewer() async {
        guard let api = model.api, let cursor = pager.newerRequest, !loadingNewer, pager.hasNewer else { return }
        loadingNewer = true
        defer { loadingNewer = false }
        guard let r = try? await api.larkMessages(conversation.id, after: cursor) else { return }
        pager.append(r, from: cursor)
    }

    /// 回到最新: the latest page; the new-messages line goes away.
    private func jumpToLatest() async {
        if pager.hasNewer, let api = model.api, let r = try? await api.larkMessages(conversation.id, topic: isTopic) {
            apply(r)
            anchor = nil
        }
        following = true
        withAnimation(Theme.Motion.standard) { position.scrollTo(edge: .bottom) }
    }

    /// 有人@你: to the mention, loading the page around it if needed.
    private func jump(to target: String) async {
        following = false
        if let m = pager.messages.first(where: { $0.position == target }) {
            withAnimation(Theme.Motion.standard) { position.scrollTo(id: m.id, anchor: .top) }
            return
        }
        guard let api = model.api, let r = try? await api.larkMessages(conversation.id, around: target) else { return }
        apply(r)
        if let m = pager.messages.first(where: { $0.position == target }) { await scroll(to: m.id) }
    }

    // MARK: Reactions

    /// Toggles my reaction at once, then tells Feishu; the next poll brings the server's version (names…).
    private func react(_ m: LarkMessage, _ key: String) {
        guard let i = pager.messages.firstIndex(where: { $0.id == m.id }) else { return }
        let me = pager.messages.last(where: \.isMine)?.senderName ?? "我"
        let (next, remove) = pager.messages[i].togglingReaction(key, me: me)
        withAnimation(Theme.Motion.standard) { pager.messages[i] = next }
        Haptics.tap()
        Task {
            do {
                try await model.api?.larkReact(m.id, key: key, remove: remove)
            } catch {
                model.show(error)
                if let j = pager.messages.firstIndex(where: { $0.id == m.id }) { pager.messages[j] = m }
            }
        }
    }

    // MARK: Read reporting

    /// A message reached the screen: once rows stop arriving, report everything up to the newest one seen.
    /// Only a message the jump buttons count goes into `pager` at once; the rest wait in `book` for the report.
    private func markSeen(_ m: LarkMessage) {
        guard !pager.seen.contains(m.position), book.seen.insert(m.position).inserted else { return }
        if anchor.map({ LarkPager.countsAsNew(m, past: $0) }) == true || m.position == mention { _ = pager.see(m) }
        reportSoon()
    }

    /// Read state goes to Feishu 400 ms after the last row came on screen, and only while the chat's tab is in front.
    private func reportSoon() {
        guard tabActive, !isTopic else { return }
        book.reportTask?.cancel()
        book.reportTask = Task {
            try? await Task.sleep(for: .milliseconds(400))
            guard !Task.isCancelled else { return }
            flushRead()
        }
    }

    private func flushRead() {
        if !book.seen.isSubset(of: pager.seen) { pager.seen.formUnion(book.seen) }
        guard !isTopic, let api = model.api, let report = pager.readReport() else { return }
        let feed = conversation.id
        Task { try? await api.larkRead(feed, report) }
    }

    // MARK: Read receipts

    private func receiptAppeared(_ m: LarkMessage) {
        guard LarkReadReceipts.tracks(m) else { return }
        book.receipts.appeared(m)
        askReceipts()
    }

    /// The chat changed (a push, the fallback poll): receipts of mine on screen that not everyone has read may have too.
    private func askReceiptsAgain() {
        book.receipts.chatChanged()
        askReceipts()
    }

    /// One request at a time, no sooner than LarkReadReceipts.minGap after the last; rows that come on screen
    /// together go in one request.
    private func askReceipts() {
        guard book.receiptTask == nil, let wait = book.receipts.wait(now: .now), let api = model.api else { return }
        let feed = conversation.id
        let book = book
        book.receiptTask = Task {
            try? await Task.sleep(for: .seconds(max(wait, 0.4)))
            let ids = book.receipts.take(now: .now)
            if !ids.isEmpty {
                do {
                    book.receipts.record(try await api.larkReadStates(feed, ids: ids))
                } catch {
                    book.receipts.failed(status: (error as? APIError)?.status)
                }
            }
            if receiptStates != book.receipts.states { receiptStates = book.receipts.states }
            book.receiptTask = nil
            askReceipts() // what came due meanwhile
        }
    }

    // MARK: Sending

    private func pickAttachment(photo: Bool) {
        guard model.hubFeatures.isEmpty || model.hubFeatures.contains("lark_attachments") else {
            model.toast = Toast("请先更新 Hub，再发送附件", style: .error)
            return
        }
        if photo { showPhotos = true } else { showFiles = true }
    }

    @ViewBuilder private var outgoingRows: some View {
        let mine = pager.messages.last(where: \.isMine)
        ForEach(Array(outbox.items.enumerated()), id: \.element.id) { i, o in
            let m = o.message(like: mine)
            let prev = i > 0 ? outbox.items[i - 1].message(like: mine) : pager.messages.last
            let grouped = LarkChatLayout.continues(m, after: prev)
            if LarkChatLayout.showsTimeSeparator(m, after: prev) { LarkTimeSeparator(date: m.createdAt) }
            LarkOutgoingRow(outgoing: o, message: m, grouped: grouped, isGroup: conversation.isGroup,
                            retry: { resend(o.id) }, edit: { restore(o) },
                            discard: { discard(o.id) })
                .padding(.top, LarkChatLayout.gap(grouped: grouped))
                .id(o.id)
        }
    }

    /// On screen at once; Feishu gets it in the background, after what was written before it.
    private func send() {
        guard model.api != nil else { model.toast = Toast("请先连接 Hub", style: .error); return }
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !preparingAttachment, !t.isEmpty || (!attachments.isEmpty && replyTo == nil) else { return }
        var batch: [LarkOutgoing] = []
        if !t.isEmpty { batch.append(LarkOutgoing(text: t, replyTo: replyTo, mentions: LarkMentions.mentions(in: t, picked: mentions))) }
        batch += attachments.map { LarkOutgoing(text: "", attachment: $0) }
        text = ""
        replyTo = nil
        mentions = []
        attachments = []
        withAnimation(Theme.Motion.standard) { outbox.items += batch }
        Haptics.tap()
        for item in batch { deliver(item.id) }
        // The row shows right above the box (and the quote bar going away): larkFollowsEnd.
        if pager.hasNewer { Task { await jumpToLatest() } } else { following = true }
    }

    private func resend(_ id: UUID) {
        guard let i = outbox.items.firstIndex(where: { $0.id == id }) else { return }
        outbox.items[i].retry()
        deliver(id)
    }

    private func discard(_ id: UUID) {
        withAnimation(Theme.Motion.standard) { outbox.items.removeAll { $0.id == id } }
    }

    private func restore(_ outgoing: LarkOutgoing) {
        if let attachment = outgoing.attachment {
            guard attachments.count < LarkAttachment.selectionLimit else {
                model.toast = Toast("请先移除一个附件，再放回输入框", style: .error)
                return
            }
            attachments.append(attachment)
        } else {
            text = text.isEmpty ? outgoing.text : text + "\n" + outgoing.text
            if replyTo == nil { replyTo = outgoing.replyTo }
            mentions += outgoing.mentions
        }
        discard(outgoing.id)
    }

    private func deliver(_ id: UUID) {
        guard let api = model.api else { return }
        outbox.deliver(id, using: api, feed: conversation.id) { await refreshLatest(); settleOutgoing(pager.messages) }
    }

    private func settleOutgoing(_ messages: [LarkMessage]) {
        outbox.settle(messages)
    }

    private func askJarvis() async {
        guard let api = model.api else { return }
        let prompt = "读取我飞书会话「\(conversation.name)」（lark-connector feedId \(conversation.id)）最近的消息，总结要点，列出需要我回复或处理的事，并给出建议的回复。"
        do {
            let s = try await api.startConversation(prompt: prompt)
            model.assistantCurrent = s.key
            model.selectedTab = .assistant
        } catch {
            model.show(error)
        }
    }
}

/// 加载更早的…: the older page of a chat or thread, a spinner while it comes.
struct LarkLoadOlderButton: View {
    var title: String
    var loading: Bool
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            Group { if loading { ProgressView().controlSize(.small) } else { Text(title).lineLimit(1) } }
                .font(Theme.Font.labelMedium)
                .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.well)
        }
        .buttonStyle(.plain)
        .foregroundStyle(LarkColor.blue)
    }
}

/// Feishu's 以下为新消息 line above the first unread message.
struct LarkNewMessagesDivider: View {
    var body: some View {
        HStack(spacing: Theme.Space.m) {
            Rectangle().fill(LarkColor.blue.opacity(0.35)).frame(height: Theme.Size.line)
            Text("以下为新消息").font(Theme.Font.labelMedium).foregroundStyle(LarkColor.blue).lineLimit(1).fixedSize()
            Rectangle().fill(LarkColor.blue.opacity(0.35)).frame(height: Theme.Size.line)
        }
        .padding(.vertical, Theme.Space.m)
        .accessibilityAddTraits(.isHeader)
    }
}

/// A centred time between messages (LarkChatLayout.showsTimeSeparator): a new day, or a pause in the chat.
struct LarkTimeSeparator: View {
    var date: Date?

    var body: some View {
        Text(LarkChatLayout.dayLabel(date)).font(Theme.Font.labelMedium).foregroundStyle(.secondary).lineLimit(1)
            .frame(maxWidth: .infinity).padding(.vertical, Theme.Space.m)
            .accessibilityAddTraits(.isHeader)
    }
}

/// 设置 › 翻译 for a chat picked there: a message from someone else in another language gets its translation under
/// the bubble, asked for when the row shows (the requests of one screen go together).
struct LarkTranslationLine: View {
    var message: LarkMessage
    var feedID: String?
    @AppStorage(TranslationPrefs.enabledKey) private var enabled = false
    @AppStorage(TranslationPrefs.targetKey) private var target = TranslationPrefs.defaultTarget
    @AppStorage(TranslationPrefs.chatsKey) private var chatsRaw = ""
    private var translator: Translator { .shared }

    private var applies: Bool {
        guard enabled, let feedID, !message.isMine, !message.recalled, !message.text.isEmpty,
              chatsRaw.contains(feedID), TranslationPrefs.chats(chatsRaw).contains(where: { $0.id == feedID }) else { return false }
        return translator.isForeign(message.text, target: target)
    }

    var body: some View {
        if applies {
            ZStack(alignment: .leading) { // a container even while empty, so the task runs
                if let t = translator.translation(of: message.text, to: target) {
                    Label {
                        Text(t).textSelection(.enabled)
                    } icon: {
                        Image(systemName: Theme.Icon.translate).foregroundStyle(.tertiary)
                    }
                    .font(Theme.Font.secondary)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, Theme.Space.xs)
                    .accessibilityLabel("译文：" + t)
                }
            }
            .task(id: message.id + target) { translator.request(message.text, to: target) }
        }
    }
}

/// One message, laid out like Feishu on the phone: others' on the left with avatar and, in group chats, the sender's
/// name on the first of a run; mine on the right in blue, without my avatar or name (LarkChatLayout.side).
/// Avatar and name open the profile card.
struct LarkMessageRow: View {
    var message: LarkMessage
    var grouped: Bool
    var isGroup = true
    var feedID: String? = nil
    /// Inside a thread sheet the reply count is not a link to itself.
    var inThread = false
    /// A message of mine on its way: its spinner or failed mark beside the bubble (LarkOutgoingRow).
    var sending: LarkSendMark? = nil
    var localAttachment: LarkAttachment?
    /// The read receipt of a message of mine (LarkReadReceipts), shown beside it once there is one.
    var readState: LarkReadState? = nil
    @Environment(\.larkOpenProfile) private var openProfile
    @Environment(\.larkReact) private var react
    @Environment(\.larkReply) private var reply
    @State private var showThread = false
    /// The thread sheet opens with its box focused (回复 on a topic group's thread).
    @State private var threadFocus = false
    @State private var picking = false

    var body: some View {
        let m = message
        if m.isSystem {
            Group {
                if let p = m.parts?.first, p.metaBool("system") { LarkSystemText(part: p) } else { Text(m.text).font(Theme.Font.label).foregroundStyle(.secondary) }
            }
            .multilineTextAlignment(.center)
            .frame(maxWidth: .infinity)
            .padding(.horizontal, Theme.Space.xl)
            .padding(.vertical, Theme.Space.xs)
        } else {
            let side = LarkChatLayout.side(of: m)
            let avatar = LarkChatLayout.showsAvatar(m, grouped: grouped)
            HStack(alignment: .top, spacing: Theme.Space.s) {
                // Mine have no avatar; the empty slot keeps them as far from the left edge as others' are from the right.
                Group {
                    if avatar {
                        LarkAvatarImage(name: m.senderName, url: m.senderAvatar, size: Theme.Size.wellM)
                            .onTapGesture { showProfile() }
                            .accessibilityAddTraits(.isButton)
                            .accessibilityLabel("\(m.senderName) 的资料")
                    } else { Color.clear }
                }
                .scaledFrame(width: Theme.Size.wellM, height: avatar ? Theme.Size.wellM : Theme.Size.line)
                VStack(alignment: side.horizontal, spacing: Theme.Space.xs) {
                    if LarkChatLayout.showsName(m, grouped: grouped, isGroup: isGroup) {
                        HStack(spacing: Theme.Space.xs) {
                            Text(m.senderName).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                                .onTapGesture { showProfile() }
                            if m.senderType == "bot" {
                                Text("机器人").font(Theme.Font.labelStrong).foregroundStyle(LarkColor.blue)
                                    .padding(.horizontal, Theme.Space.xs).padding(.vertical, 1).background(LarkColor.blue.opacity(0.12), in: .rect(cornerRadius: 3))
                            }
                        }
                    }
                    if let sending {
                        HStack(alignment: .center, spacing: Theme.Space.s) {
                            LarkSendMarkView(mark: sending)
                            content
                        }
                    } else if side == .right, let read = LarkReadMark(state: readState, isGroup: isGroup) {
                        HStack(alignment: .bottom, spacing: Theme.Space.s) {
                            LarkReadMarkView(mark: read)
                            content
                        }
                    } else {
                        content
                    }
                    LarkTranslationLine(message: m, feedID: feedID)
                    footer
                }
                .frame(maxWidth: .infinity, alignment: side.alignment)
            }
            .sheet(isPresented: $picking) {
                LarkReactionPicker(message: m) { react?(m, $0) }
            }
            .sheet(isPresented: $showThread, onDismiss: { threadFocus = false }) {
                LarkThreadView(root: m, isGroup: isGroup, feedID: feedID, focusBox: threadFocus)
            }
        }
    }

    private func showProfile() {
        guard let id = message.senderID, !id.isEmpty else { return }
        openProfile?(LarkPersonRef(id: id, name: message.senderName, avatar: message.senderAvatar, feedID: feedID))
    }

    @ViewBuilder private var content: some View {
        let m = message
        let parts = m.parts ?? []
        if let localAttachment {
            LarkAttachmentView(attachment: localAttachment)
        } else if m.recalled {
            Text(m.isMine ? "你撤回了一条消息" : "\(m.senderName) 撤回了一条消息").font(Theme.Font.caption).foregroundStyle(.tertiary).italic()
                .padding(.horizontal, Theme.Space.m).padding(.vertical, Theme.Space.s)
                .background(Color.primary.opacity(0.04), in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
        } else if parts.isEmpty {
            bubble {
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    if let reply = m.replyTo { LarkReplyQuote(reply: reply) }
                    Text(m.text.isEmpty ? "[\(m.type.lowercased())]" : m.text).font(Theme.Font.body)
                }
            }
        } else if LarkPartsView.isBare(parts) {
            if let reply = m.replyTo { LarkReplyQuote(reply: reply).padding(.horizontal, Theme.Space.xs) }
            LarkPartsView(parts: parts)
                .environment(\.larkContext, LarkRenderContext(messageID: m.id, images: LarkPartsView.imageURLs(parts)))
                .frame(maxWidth: parts.contains { $0.kind == "card" || $0.kind == "link" } ? .infinity : nil, alignment: .leading)
                .contextMenu { menu }
        } else {
            bubble {
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    if let reply = m.replyTo { LarkReplyQuote(reply: reply) }
                    LarkPartsView(parts: parts)
                        .environment(\.larkContext, LarkRenderContext(messageID: m.id, images: LarkPartsView.imageURLs(parts)))
                }
            }
        }
    }

    private func bubble<C: View>(@ViewBuilder _ c: () -> C) -> some View {
        c()
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.m)
            .background(message.isMine ? LarkColor.myBubble : LarkColor.otherBubble, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            .contextMenu { menu }
    }

    /// Long press: a bar of quick reactions on top (one tap reacts), then reply, more emoji, copy.
    /// A context menu cannot put a tappable view above itself (its preview takes no taps), so the bar is the menu's
    /// first row, as a palette: six emoji across (all the palette shows without scrolling) instead of the four a compact
    /// group fits.
    @ViewBuilder private var menu: some View {
        if react != nil && !message.recalled {
            ControlGroup {
                ForEach(LarkQuickReactions.bar(LarkMedia.shared.quickReactions), id: \.self) { key in
                    Button { react?(message, key) } label: {
                        if let img = LarkMedia.shared.emojiImage(key, points: 28) {
                            Image(uiImage: img).renderingMode(.original)
                        } else {
                            Text(LarkMedia.labelFallback(key, text: nil))
                        }
                    }
                    .accessibilityLabel(LarkMedia.shared.emoji(key)?.label ?? LarkMedia.labelFallback(key, text: nil))
                    .accessibilityIdentifier("lark.quickReaction")
                }
            }
            .controlGroupStyle(.palette)
            .menuActionDismissBehavior(.enabled) // a palette keeps the menu up after a tap; Feishu closes it
        }
        if let reply, let route = LarkReply.route(for: message, inTopicGroup: reply.inTopicGroup, inThread: inThread) {
            Button {
                if route == .openThread {
                    threadFocus = true
                    showThread = true
                } else {
                    reply.answer(message)
                }
            } label: { Label("回复", systemImage: Theme.Icon.reply) }
        }
        if react != nil && !message.recalled {
            Button { picking = true } label: { Label("表情回复", systemImage: "face.smiling") }
        }
        Button {
            UIPasteboard.general.string = message.text
            Haptics.success()
        } label: { Label("拷贝", systemImage: Theme.Icon.copy) }
        if let id = message.senderID, !id.isEmpty, openProfile != nil {
            Button { showProfile() } label: { Label("查看 \(message.senderName) 的资料", systemImage: Theme.Icon.person) }
        }
    }

    @ViewBuilder private var footer: some View {
        let m = message
        let reactions = m.reactions ?? []
        if !reactions.isEmpty {
            LarkFlowLayout(spacing: 6, lineSpacing: 6, trailing: LarkChatLayout.side(of: m) == .right) {
                ForEach(reactions, id: \.key) { r in
                    Button { react?(m, r.key) } label: { LarkReactionChip(reaction: r) }
                        .buttonStyle(.plain)
                        .disabled(react == nil)
                }
                if react != nil {
                    Button { picking = true } label: {
                        Image(systemName: "face.smiling").font(.system(size: 14)).foregroundStyle(.secondary)
                            .scaledFrame(width: Theme.Size.well, height: Theme.Size.iconL).background(Color.primary.opacity(0.05), in: .capsule)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("表情回复")
                }
            }
            .scaledFrame(maxWidth: Theme.Size.box300, alignment: LarkChatLayout.side(of: m).alignment)
        }
        let replies = max(m.threadReplyCount ?? 0, m.replyCount)
        // No time here: it is in the separators above (LarkChatLayout).
        if replies > 0 || m.edited == true {
            HStack(spacing: Theme.Space.s) {
                if replies > 0 && !inThread {
                    Button { showThread = true } label: {
                        Label("\(replies) 条回复", systemImage: Theme.Icon.reply).foregroundStyle(LarkColor.blue)
                    }
                    .buttonStyle(.plain)
                }
                if m.edited == true { Text("已编辑") }
            }
            .font(Theme.Font.label)
            .foregroundStyle(.secondary)
            .lineLimit(1)
        }
    }
}

extension LarkMessage {
    /// The message with my reaction `key` toggled (lark-connector's optimistic update: remove if mine, else add).
    func togglingReaction(_ key: String, me: String) -> (LarkMessage, remove: Bool) {
        var m = self
        var list = m.reactions ?? []
        if let i = list.firstIndex(where: { $0.key.lowercased() == key.lowercased() }) {
            var r = list[i]
            if r.mine == true {
                r.count -= 1
                r.mine = false
                r.users?.removeAll { $0 == me }
                if r.count <= 0 { list.remove(at: i) } else { list[i] = r }
                m.reactions = list
                return (m, true)
            }
            r.count += 1
            r.mine = true
            r.users = (r.users ?? []) + [me]
            list[i] = r
        } else {
            list.append(LarkReaction(key: key, count: 1, mine: true, users: [me]))
        }
        m.reactions = list
        return (m, false)
    }
}

/// The message a reply quotes: a bar, the author, one or two lines of it.
struct LarkReplyQuote: View {
    var reply: LarkReplyTo

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            Capsule().fill(Color.primary.opacity(0.2)).frame(width: Theme.Size.ruleWide)
            Text(reply.recalled == true ? "\(reply.senderName)：消息已撤回" : "\(reply.senderName)：\(Fmt.plain(reply.text))")
                .font(Theme.Font.caption)
                .foregroundStyle(.secondary)
                .lineLimit(2)
        }
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// A reaction: the emoji picture, then who (a few names) or how many.
struct LarkReactionChip: View {
    var reaction: LarkReaction

    var body: some View {
        let media = LarkMedia.shared
        let users = reaction.users ?? []
        HStack(spacing: Theme.Space.xs) {
            if let url = media.emoji(reaction.key)?.url {
                LarkRemoteImage(url: url, maxPixel: 64, contentMode: .fit).scaledFrame(width: Theme.Size.markL, height: Theme.Size.markL)
            } else {
                Text(LarkMedia.labelFallback(reaction.key, text: nil)).font(Theme.Font.label)
            }
            Text(users.isEmpty || users.count > 3 ? "\(reaction.count)" : users.joined(separator: "、"))
                .font(Theme.Font.label)
                .foregroundStyle(reaction.mine == true ? LarkColor.blue : .secondary)
                .lineLimit(1)
        }
        .padding(.horizontal, Theme.Space.s)
        .padding(.vertical, Theme.Space.xs)
        .background(reaction.mine == true ? LarkColor.blue.opacity(0.12) : Color.primary.opacity(0.05), in: .capsule)
        .accessibilityLabel("\(media.emoji(reaction.key)?.label ?? reaction.key) \(reaction.count)")
    }
}

private extension Optional where Wrapped == Bool {
    var isTrue: Bool { self == true }
}

