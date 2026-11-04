import SwiftUI
import OSLog

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

extension EnvironmentValues {
    /// The Feishu account of the chat a view is in (`LarkConversation.account`): the requests it makes name it.
    @Entry var larkAccount: Int? = nil
}

/// One Feishu chat, opened like Feishu: at the oldest unread message under a 以下为新消息 line (or at the latest
/// when all is read), newer pages as you scroll down, read state reported only for what was on screen,
/// 回到最新 / 有人@你 jumps.
struct LarkChatView: View {
    @Environment(AppModel.self) private var model
    /// Polled and reported only while its page is in front: a hidden chat reports nothing to Feishu (TabActivity.swift).
    @Environment(\.tabIsActive) private var tabActive
    @Environment(\.scenePhase) private var scenePhase
    var conversation: LarkConversation
    var messageComposer = false
    @State private var encryptionDraft: LarkEncryptedDraft?
    @State private var encryptionStatus: LarkNativeEncryptionStatus?
    @State private var pinsModel: LarkChatPinsModel?
    @State private var showPins = false
    @State private var pinTarget: LarkChatPinTarget?
    @State private var pinThread: LarkChatPinTarget?
    @State private var addingPin: LarkMessage?
    @State private var changingPin = false
    @State private var pinNeedsRecheck = false
    @State private var showEncryptionCovers = false
    @State private var showEncryptionUnlock = false
    @State private var checkingEncryption = false
    @State private var encryptionProblem: String?
    @State private var encryptionRetryOpensCovers = false
    @State private var pager = LarkPager()
    /// The hub answered with a topic group's topics for a chat opened without `isTopic` (from a profile, an old inbox item).
    @State private var foundTopic = false
    @State private var history = LarkHistoryPaging()
    @State private var historyTask: Task<Void, Never>?
    @State private var nearHistory = false
    @State private var historyVisible = false
    /// Keeps the message being read in place while an older page goes in above it (not observed).
    @State private var historyHold = LarkHistoryHold()
    /// The one message that reports where it is while a page goes in above it.
    @State private var historyAnchor: String?
    @State private var loadingNewer = false
    /// 回到最新 is waiting for the hub (none of the newest messages on this device).
    @State private var jumping = false
    /// The first load is on its way (the page shows a spinner when nothing was cached).
    @State private var loading = true
    /// Why the last load failed: the page's placeholder when there is nothing to show, a line on top otherwise.
    @State private var problem: LarkLoadProblem?
    /// What is typed in the box; with `replyTo`, kept as the chat's draft (LarkDrafts) until it is sent.
    @State private var text: String
    /// The message 回复 picked: quoted over the box and by what is sent next.
    @State private var replyTo: LarkReplyTo?
    /// The message a reply's quote just led back to (`QuoteFlash`).
    @State private var flash: QuoteFlashMark?
    /// Picked with the @ picker since the last send.
    @State private var mentions: [LarkMention]
    @FocusState private var composerFocused: Bool
    /// What I wrote that is on its way (on screen at once, sent one after another in the background).
    @State private var outbox = LarkChatOutbox()
    @State private var attachments: [LarkAttachment] = []
    @State private var showPhotos = false
    @State private var showFiles = false
    @State private var preparingAttachment = false
    @State private var savingOutgoing = false
    @State private var checkingOutgoing: Set<UUID> = []
    @State private var draftLoaded = false
    @State private var confirmDraftRecovery = false
    @State private var chatLink: HubLink?
    @State private var accountProblem: String?
    @State private var legacyDraft: LarkOutboxSnapshot?
    @State private var legacyStore: LarkOutboxStore?
    @State private var confirmLegacyDraft = false
    @State private var position = ScrollPosition(idType: String.self)
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
    /// A topic group's newest replies, for its cards.
    @State private var topicReplies = LarkTopicReplies()

    init(conversation: LarkConversation, messageComposer: Bool = false) {
        self.conversation = conversation
        self.messageComposer = messageComposer
        // Restore only after binding the composer to its Hub credential.
        _text = State(initialValue: "")
        _replyTo = State(initialValue: nil)
        _mentions = State(initialValue: [])
        _following = State(initialValue: !Self.opensAtMessage(conversation) && !Self.debugTop)
    }

    private var chatAPI: APIClient? {
        guard let chatLink, model.link(chatLink.group) === chatLink else { return nil }
        return model.api(chatLink.group)?.lark(conversation.account)
    }

    private var isTopic: Bool { conversation.isTopic == true || foundTopic }
    /// How the topic cards read a topic's newest replies (`LarkTopicReplies`), on the chat's account. `chatAPI` spelled
    /// out: through it the closure would hold this page, whose state holds the closure. A function of its own, not in
    /// `.task`: a closure in `body`'s modifier chain is type-checked with the whole chain (Xcode 26.0, docs/lessons/ios.md).
    private func bindTopicReplies() {
        let link = chatLink ?? model.hub, app = model, account = conversation.account
        topicReplies.fetch = { id, limit in
            guard let link, app.link(link.group) === link, let api = app.api(link.group)?.lark(account) else { throw CancellationError() }
            return try await api.larkThread(id, limit: limit).messages
        }
    }
    /// Under the bars above the box: the page's own background (a topic group's is the grouped one).
    private var barBackground: Color { isTopic ? Theme.groupedPage : Theme.pageBackground }
    /// Under the list: a topic group's cards stand on the grouped page, as Feishu's do. A property, not a ternary in
    /// `body`'s long modifier chain (Xcode 26.0 type-checks such a chain as one expression, docs/lessons/ios.md).
    private var pageBackground: Color { isTopic ? Theme.groupedPage : .clear }
    /// Opens at a message (first unread / a flagged message) rather than at the bottom.
    private var opensAtMessage: Bool { Self.opensAtMessage(conversation) }
    private var visibleMessages: [LarkMessage] { pager.messages.filter { !outbox.hidesEcho($0) } }
    private var openingAnchor: UnitPoint { Self.debugTop || opensAtMessage ? .top : .bottom }
    private static func opensAtMessage(_ c: LarkConversation) -> Bool { c.focus != nil || (c.isTopic != true && c.unread > 0) }

    var body: some View {
        chatContent
        .background(pageBackground)
        .themedPage()
        .navigationTitle(conversation.name)
        .navigationSubtitle([isTopic ? "飞书话题群" : "飞书", conversation.accountName].compactMap { $0 }.joined(separator: " · "))
        .navigationBarTitleDisplayMode(.inline)
        // Keep the bar's material during push/pop instead of resolving its scroll-edge state after arrival.
        .toolbarBackground(.bar, for: .navigationBar)
        .toolbarBackground(.visible, for: .navigationBar)
        .hidesTabBarWhenShown()
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button(action: openPins) { Label("Pin 消息", systemImage: Theme.Icon.pin) }
                    .accessibilityIdentifier("lark.pins.open")
            }
            ToolbarItem(placement: .topBarTrailing) {
                Button { Task { await askJarvis() } } label: { Label("让 Persona 总结", systemImage: Theme.Icon.ai) }
            }
        }
        .task {
            if chatLink == nil { chatLink = model.hub }
            bindTopicReplies()
            async let display: Void = refreshDisplay()
            async let restored: Void = restoreDraft()
            LarkMedia.shared.loadEmojiCatalog()
            // The last messages seen here paint at once (offline too); the network only brings them up to date.
            // Background sync (LarkSync) keeps this cache current, so it is usually already the latest. An unread chat
            // opens at its first unread message: the cache serves when it reaches back that far.
            if pager.messages.isEmpty, conversation.focus == nil,
               let cached = LarkMessage.loadCached(LarkMessage.cacheName(conversation)), !cached.isEmpty,
               !opensAtMessage || cached.first.map({ !LarkPosition.less(LarkPosition.next(conversation.readPosition ?? "0"), $0.position) }) == true {
                pager.messages = cached
                pager.hasOlder = true
                pager.olderCursor = cached.first?.position
                pager.newerCursor = cached.last?.position
                let read = conversation.readPosition ?? "0"
                if opensAtMessage, let first = cached.first(where: { LarkPosition.less(read, $0.position) }) { await scroll(to: first.id) }
            }
            await load()
            await restored
            await display
            #if DEBUG
            if attachments.isEmpty { attachments = await LarkAttachmentFixture.load(config: chatAPI?.config) }
            if let quote = LarkAttachmentFixture.quote(config: chatAPI?.config, in: pager.messages) { replyTo = quote }
            #endif
            if let api = chatAPI, (try? await outbox.checkAccount(using: api)) != nil {
                try? await outbox.settle(pager.messages)
                await outbox.verifyAcknowledged(using: api, feed: conversation.id)
            }
            if !outbox.items.isEmpty {
                try? await Task.sleep(for: .milliseconds(60))
                position.scrollTo(edge: .bottom)
            }
        }
        // A fallback: changes arrive as pushes (below). It polls only while the chat's page is in front.
        .tabActiveTask {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(60))
                await refreshDisplay()
                await refreshLatest()
                askReceiptsAgain()
            }
        }
        .task {
            for await _ in LarkSync.shared.changes(of: conversation.key) {
                await refreshLatest()
                askReceiptsAgain()
            }
        }
        // Back in front: what came on screen while it was in the back is reported now.
        .onTabVisibility(appear: reportSoon, disappear: flushRead)
        .onTabVisibility(appear: {
            historyVisible = true
            Task { await requestHistoryWhenSettled("visible") }
        }, disappear: {
            historyVisible = false
            cancelHistory()
        })
        .onChange(of: landed) { Task { await requestHistoryWhenSettled("landed") } }
        .onChange(of: outbox.identity) { _, _ in cancelHistory(); Task { await requestHistoryWhenSettled("account") } }
        .onChange(of: outbox.invalidated) { _, invalid in if invalid { cancelHistory() } }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await requestHistoryWhenSettled("active") } } else { cancelHistory() }
        }
        .onChange(of: anchor) { pager.seen.formUnion(book.seen) }
        .onChange(of: mention) { pager.seen.formUnion(book.seen) }
        // Outermost, so the sheets above (a thread, a profile, a reaction picker) are about the same account.
        .environment(\.larkAccount, conversation.account)
    }

    @ViewBuilder private var chatContent: some View {
        let messages = visibleMessages
        ScrollView {
            LarkChatStack(lazy: !isTopic) {
                if pager.hasOlder {
                    historyHeader
                }
                if hasContent, let line = problem?.inline { Label(line, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger).padding(.bottom, Theme.Space.s) }
                ForEach(Array(messages.enumerated()), id: \.element.id) { i, m in
                    let prev = i > 0 ? messages[i - 1] : nil
                    VStack(alignment: .leading, spacing: 0) {
                        if isTopic {
                            // A topic group lists topics: each its own card with its time, never run together like a
                            // chat's messages (lark-connector's topic list); a system line stays a line.
                            Group {
                                if m.isSystem {
                                    LarkMessageRow(message: m, grouped: false, isGroup: true, feedID: conversation.id)
                                } else {
                                    LarkTopicCard(topic: m, chatName: conversation.name, feedID: conversation.id, replies: topicReplies)
                                }
                            }
                            .padding(.top, Theme.Space.m)
                        } else {
                            if isFirstUnread(m, after: prev) { LarkNewMessagesDivider() }
                            if LarkChatLayout.showsTimeSeparator(m, after: prev) { LarkTimeSeparator(date: m.createdAt) }
                            let grouped = LarkChatLayout.continues(m, after: prev)
                            LarkMessageRow(message: m, grouped: grouped, isGroup: conversation.isGroup, feedID: conversation.id,
                                           readState: receiptStates[m.id])
                                .background { if historyAnchor == m.id { historyAnchorReport(m.id) } }
                                .padding(.top, LarkChatLayout.gap(grouped: grouped))
                        }
                    }
                    .background { if flash?.line == m.id { QuoteFlash().id(flash?.id).padding(.horizontal, -Theme.Space.m) } }
                    .onAppear {
                        markSeen(m)
                        if !isTopic { receiptAppeared(m) } // a topic's card shows no read receipt
                    }
                    .onDisappear { book.receipts.disappeared(m.id) }
                    .id(m.id)
                }
                if pager.hasNewer {
                    // Loads while it is on screen; still on screen after a page (at the newest end, catching up), it asks
                    // for the next one (onAppear did not; a task keyed on the cursor ran off screen too, to the end). A
                    // topic group's stack lays out every row at once, so on screen is the only signal that holds there too.
                    ProgressView().controlSize(.small).scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.tapTarget)
                        .onScrollVisibilityChange(threshold: 0.01) { visible in
                            book.newerVisible = visible
                            if visible { Task { await loadNewer() } }
                        }
                } else {
                    outgoingRows
                }
            }
            .coordinateSpace(.named(LarkHistoryHold.space))
            .background(LarkHistoryHold.Finder(hold: historyHold))
            .onAppear { historyHold.letGo = { [position = $position] in position.wrappedValue = ScrollPosition(idType: String.self) } }
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.m)
        }
        .scrollPosition($position)
        // Where it opens and how it aligns; size changes (the keyboard, the box, new rows) are larkFollowsEnd's.
        .larkFollowsEnd($position, following: $following, resting: isTopic ? openingAnchor : nil, active: landed)
        .defaultScrollAnchor(openingAnchor, for: .initialOffset)
        .defaultScrollAnchor(openingAnchor, for: .alignment)
        // The visible part ends above the composer (bottom inset); containerSize leaves out both insets, so it can't be added to
        // contentOffset: under a tall top bar (opened from the inbox) that never reached the end and the arrow never went away.
        .onScrollGeometryChange(for: Bool.self) { g in
            g.visibleRect.maxY - g.contentInsets.bottom >= g.contentSize.height - 80
        } action: { _, v in atBottom = v }
        .onScrollTargetVisibilityChange(idType: String.self) { historyHold.visible = $0 }
        .onScrollPhaseChange { old, phase in
            // A drag starts (some go straight to interacting): counted once.
            let finger: [ScrollPhase] = [.tracking, .interacting]
            if finger.contains(phase), !finger.contains(old) { book.drags += 1 }
        }
        // With the content's height in it, the first layout reports too: a list too short to scroll is near its top
        // from the start, and the change-only callback never said so (measured: no request at all).
        .onScrollGeometryChange(for: LarkHistoryReach.self) { g in
            LarkHistoryReach(near: g.contentOffset.y + g.contentInsets.top <= g.containerSize.height / 2, content: g.contentSize.height)
        } action: { _, reach in
            nearHistory = reach.near
            if reach.near { requestHistory(why: "near") }
        }
        .overlay { placeholder }
        .overlay(alignment: .bottomTrailing) { jumpToLatestButton }
        .overlay(alignment: .topTrailing) { mentionButton }
        .environment(\.larkDisplay, chatLink?.larkDisplay.policy ?? .hidden)
        .environment(\.larkOpenFile, { url, name in fileRequest = (url, name) })
        .environment(\.larkRefresh, { Task { await refreshLatest() } })
        .environment(\.larkOpenProfile, { person = $0 })
        .environment(\.larkReact, { m, key in react(m, key) })
        .environment(\.larkJump, { id in jump(to: id) })
        .environment(\.larkReply, LarkReplyHandler(inTopicGroup: isTopic) { m in
            replyTo = LarkReply.quote(m)
            composerFocused = true
        })
        .environment(\.larkAddPin, outbox.identity != nil && !changingPin && !pinNeedsRecheck ? { addingPin = $0 } : nil)
        .modifier(LarkPinPresentation(pins: pinsModel, show: $showPins, target: $pinTarget, thread: $pinThread,
            adding: $addingPin, conversation: conversation, display: chatLink?.larkDisplay.policy ?? .hidden, api: chatAPI,
            finish: finishPinNavigation, add: addPin))
        .modifier(LarkFileOpener(request: $fileRequest))
        .sheet(item: $person) { LarkProfileView(person: $0) }
        .sheet(isPresented: $showEncryptionCovers) {
            if let api = chatAPI, let account = outbox.identity?.account {
                LarkEncryptionCoverPicker(api: api, account: account, feed: conversation.id) { cover in
                    guard encryptionDraft != nil else { return }
                    encryptionDraft?.cover = cover
                }
            }
        }
        .sheet(isPresented: $showEncryptionUnlock) {
            if let api = chatAPI, let account = outbox.identity?.account {
                LarkEncryptionUnlockSheet(api: api, account: account, unified: encryptionStatus?.unifiedPassword == true) { send() }
            }
        }
        .alert("暂时无法使用加密消息", isPresented: Binding(get: { encryptionProblem != nil }, set: { if !$0 { encryptionProblem = nil } })) {
            Button("重新检查") { checkEncryption(openCovers: encryptionRetryOpensCovers) }
            Button("稍后", role: .cancel) { }
        } message: { Text(encryptionProblem ?? "") }
        .sheet(item: $debugPick) { m in LarkReactionPicker(message: m) { react(m, $0) } }
        .sheet(isPresented: Binding(get: { debugPost != nil }, set: { if !$0 { debugPost = nil } })) {
            if let id = debugPost { LarkMomentsPostView(postID: id) }
        }
        .safeAreaInset(edge: .bottom) {
            VStack(spacing: Theme.Space.s) {
                if legacyDraft != nil {
                    HStack(spacing: Theme.Space.s) {
                        Text("发现旧版草稿").font(Theme.Font.label).foregroundStyle(.secondary)
                        Button("恢复") { confirmLegacyDraft = true }.lineLimit(1)
                    }
                    .padding(.horizontal, Theme.Space.m).padding(.vertical, Theme.Space.s)
                    .frame(maxWidth: .infinity).background(barBackground)
                }
                if let problem = accountProblem ?? outbox.preparationProblem {
                    HStack(spacing: Theme.Space.s) {
                        Text(problem).font(Theme.Font.label).foregroundStyle(.secondary)
                        if outbox.canRecover {
                            Button("恢复输入") { confirmDraftRecovery = true }.lineLimit(1)
                        } else {
                            Button("重试") { Task { await restoreDraft() } }.lineLimit(1)
                        }
                    }
                    .padding(.horizontal, Theme.Space.m)
                    .padding(.vertical, Theme.Space.s)
                    .frame(maxWidth: .infinity)
                    .background(barBackground)
                }
                composer
            }
        }
        .confirmationDialog("重新开始编辑？", isPresented: $confirmDraftRecovery, titleVisibility: .visible) {
            Button("清除草稿并重新开始", role: .destructive) {
                Task {
                    do {
                        try await outbox.recover()
                        applyDraft()
                    } catch { model.toast = Toast("未能恢复，请检查本机存储空间后重试", style: .error) }
                }
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text("将清除此会话的本机草稿、待发送记录及附件。已发出的飞书消息不受影响。")
        }
        .confirmationDialog("恢复到当前飞书账号？", isPresented: $confirmLegacyDraft, titleVisibility: .visible) {
            Button("确认属于当前账号，恢复") { restoreLegacyDraft() }.lineLimit(1)
            Button("取消", role: .cancel) {}
        } message: {
            Text("旧版草稿没有记录所属账号。确认后恢复到当前会话，待发送内容不会自动发送。")
        }
        .onChange(of: LarkDraft(text: text, encryption: encryptionDraft, replyTo: replyTo, mentions: mentions.isEmpty ? nil : mentions)) { _, _ in saveDraft() }
        .onChange(of: attachments) { _, _ in saveDraft(immediately: true) }
        .onChange(of: scenePhase) { _, phase in
            guard tabActive else { return }
            if phase != .active { saveDraft(immediately: true) }
            else { Task { await restoreDraft(); await refreshDisplay() } }
        }
        .onChange(of: outbox.storageProblem) { _, problem in
            if let problem { model.toast = Toast(problem, style: .error) }
        }
        .onTabVisibility(appear: {
            if draftLoaded { applyDraft() }
        }, disappear: {
            saveDraft(immediately: true, leavingPage: true)
        })
        .modifier(LarkAttachmentPicker(attachments: $attachments, photos: $showPhotos, files: $showFiles, preparing: $preparingAttachment))
        .onChange(of: pager.messages) { _, now in settleOutgoing(now) }
    }

    private var composer: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if messageComposer {
                LarkEncryptionModeBar(draft: $encryptionDraft, canEncrypt: attachments.isEmpty && !isTopic && !checkingEncryption,
                    checking: checkingEncryption, selectEncrypted: {
                        if encryptionDraft == nil { encryptionDraft = LarkEncryptedDraft() }
                        checkEncryption(openCovers: false)
                    }, pickCover: { checkEncryption(openCovers: true) })
            }
            if !messageComposer, encryptionDraft != nil {
                #if os(iOS)
                Button("前往消息，继续编辑加密草稿") { model.selectedTab = .feishu }
                    .font(Theme.Font.label).lineLimit(1)
                    .padding(.horizontal, Theme.Space.page)
                #else
                Text("请从消息列表继续编辑加密草稿")
                    .font(Theme.Font.label).padding(.horizontal, Theme.Space.page)
                #endif
            }
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
                    Text(replyTo == nil ? "文字和附件将分别发送"
                         : model.larkFeatures.attachmentReplies ? "文字和附件将分别引用发送" : "更新 Hub 上的飞书扩展后才能引用发送附件")
                        .font(Theme.Font.label).foregroundStyle(.secondary)
                        .padding(.horizontal, Theme.Space.m)
                }
            }
            if preparingAttachment {
                HStack(spacing: Theme.Space.s) {
                    ProgressView().controlSize(.small)
                    Text("正在准备附件…").font(Theme.Font.label).foregroundStyle(.secondary)
                }
            }
            // What is sent to a topic group is a new topic (a reply goes into its thread from the topic's card).
            LarkComposer(placeholder: isTopic ? "发布话题" : "回复 \(conversation.name)", text: $text, focused: $composerFocused,
                         replyTo: replyTo, cancelReply: { replyTo = nil },
                         attachmentPicker: { pickAttachment(photo: $0) },
                         canAddAttachments: encryptionDraft == nil && attachments.count < LarkAttachment.selectionLimit,
                         hasAttachments: !attachments.isEmpty, preparingAttachment: preparingAttachment,
                          mentionFeed: encryptionDraft == nil && conversation.isGroup ? conversation.id : nil,
                          me: pager.messages.last(where: \.isMine)?.senderID, mentions: $mentions, send: send)
                .disabled(!messageComposer && encryptionDraft != nil)
        }
        .disabled(savingOutgoing || !draftLoaded || outbox.invalidated)
        .padding(.top, !attachments.isEmpty || preparingAttachment ? Theme.Space.s : 0)
        .background {
            if messageComposer || encryptionDraft != nil || !attachments.isEmpty || preparingAttachment {
                barBackground.ignoresSafeArea(edges: .bottom)
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
        // Not while the list follows its end: it is on its way there (a row sent or arriving makes the content taller
        // a moment before the list catches up).
        if pager.hasNewer || !atBottom && !following {
            let n = unseenNew
            Button { Task { await jumpToLatest() } } label: {
                HStack(spacing: Theme.Space.sm) {
                    if jumping { ProgressView().controlSize(.small) }
                    else { Image(systemName: Theme.Icon.expand).font(.system(size: 13, weight: .bold)) }
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
            .accessibilityValue(jumping ? "正在加载" : "")
            .accessibilityIdentifier("lark.jumpLatest")
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
        resetHistory()
        if r.topic == true { foundTopic = true }
        pager.replace(with: r)
        if !r.hasNewer.map({ $0 }).isTrue { StateCache.save(LarkMessage.cacheName(conversation), Array(r.messages.suffix(80))) }
    }

    /// Messages, or mine on their way: a problem is then one line on top rather than the page's placeholder.
    private var hasContent: Bool { !visibleMessages.isEmpty || !outbox.items.isEmpty }

    /// Loading, empty, or what went wrong, while there are no messages to show (BeautifulUI draws its own).
    @ViewBuilder private var placeholder: some View {
        switch LarkChatPlaceholder.of(hasContent: hasContent, loading: loading, problem: problem) {
        case .loading:
            LoadableContent(state: Loadable<Void>.loading, retry: nil) { _ in EmptyView() }
        case .empty:
            if isTopic {
                EmptyStateView(symbol: Theme.Icon.feishuChats, title: "暂时没有话题", message: "在下面发布第一个话题。")
            } else {
                EmptyStateView(symbol: Theme.Icon.feishuChats, title: "还没有消息", message: "发出的第一条消息会出现在这里。")
            }
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
        guard let api = chatAPI else { return }
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
            // `-larkMoments <postId>` opens that 公司圈 post.
            if let post = UserDefaults.standard.string(forKey: "larkMoments") { debugPost = post }
            #endif
        } catch {
            problem = LarkLoadProblem.of(error)
        }
    }

    /// A reply's quote was tapped: to the message it answers, lit up for a moment. False when it is not loaded here.
    private func jump(to id: String) -> Bool {
        guard visibleMessages.contains(where: { $0.id == id }) else { return false }
        following = false
        withAnimation(Theme.Motion.standard) { position.scrollTo(id: id, anchor: .center) }
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(250))
            flash = QuoteFlashMark(line: id)
        }
        return true
    }

    /// Scrolls once the rows exist (the lazy stack lays them out after the data lands).
    private func scroll(to id: String) async {
        following = false
        try? await Task.sleep(for: .milliseconds(80))
        position.scrollTo(id: id, anchor: .top)
        try? await Task.sleep(for: .milliseconds(250))
        position.scrollTo(id: id, anchor: .top)
    }

    private func refreshDisplay() async {
        guard let link = chatLink, let api = chatAPI else { return }
        await link.larkDisplay.refresh(using: api)
    }

    /// Polls the tail while it is loaded: new messages append, cards that changed (alert status…) update in place.
    /// `untouchedSince`: 回到最新 took the list to its end with the drag count at this; the finger has not taken it since,
    /// so it is still at the end even when the geometry just after the window changed says otherwise.
    private func refreshLatest(untouchedSince: Int? = nil) async {
        guard !pager.hasNewer, let api = chatAPI, let r = try? await api.larkMessages(conversation.id, topic: isTopic),
              !pager.hasNewer else { return } // (the window may have moved away meanwhile)
        problem = nil // it answers again (lark-connector signed in again, the network is back)
        switch pager.latest(r) {
        case .fresh(let fresh):
            guard !fresh.isEmpty else { return }
            if atBottom { following = true } // near the end: the new ones come into view (larkFollowsEnd)
            withAnimation(Theme.Motion.standard) { pager.add(fresh) }
        case .gap:
            // Messages are missing between the last loaded and the latest page (many came while away). At the newest
            // end the latest page takes the window (the list stays at its end); reading further up nothing moves, and
            // the newer pages load in order when the bottom is reached.
            if following || atBottom || untouchedSince == book.drags {
                following = true
                apply(r)
            } else {
                pager.markGap()
            }
        }
    }

    private static let historyLog = Logger(subsystem: "jarvis", category: "lark-history")

    /// Picks a message on screen and waits (a few frames at most) until it has said where it is.
    private func holdHistoryAnchor() async {
        // Following the newest end, the list stays there by itself (larkFollowsEnd).
        guard !following, let id = historyHold.pick(in: pager.messages.map(\.id)) else { return }
        historyHold.begin(id)
        historyAnchor = id
        for _ in 0..<10 where !historyHold.ready {
            try? await Task.sleep(for: .milliseconds(16))
        }
    }

    private func releaseHistoryAnchor() {
        historyHold.end()
        if historyAnchor != nil { historyAnchor = nil }
    }

    private func historyAnchorReport(_ id: String) -> some View {
        Color.clear.onGeometryChange(for: CGFloat.self) { $0.frame(in: .named(LarkHistoryHold.space)).minY } action: {
            historyHold.moved(id, to: $0)
        }
    }

    @ViewBuilder private var historyHeader: some View {
        if let problem = history.problem {
            Button { requestHistory(retry: true) } label: {
                ViewThatFits(in: .horizontal) {
                    Text(problem.text)
                    Text(problem.short)
                }
                .font(Theme.Font.label).lineLimit(1).minimumScaleFactor(0.7)
                .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.well)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain).foregroundStyle(LarkColor.blue)
            .accessibilityLabel(problem.text)
            .accessibilityIdentifier("lark.history.retry")
        } else if isTopic {
            LarkLoadOlderButton(title: "加载更早的话题", loading: history.pending != nil) { requestHistory(retry: true) }
        } else {
            Group {
                if history.pending != nil { ProgressView().controlSize(.small).accessibilityLabel("正在加载更早的消息") }
                else { Color.clear }
            }
            .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.well, maxHeight: Theme.Size.well)
            .accessibilityIdentifier("lark.history.loading")
        }
    }

    private func cancelHistory() {
        historyTask?.cancel()
        historyTask = nil
        history.cancel()
        releaseHistoryAnchor()
    }

    private func resetHistory() { cancelHistory(); history = LarkHistoryPaging() }

    /// The first page lands, the account is confirmed, the page comes back to the front: each before the list has
    /// gone to where it opens. For a moment even a long chat opened at its newest stood at its top (offset at the inset,
    /// logged on reopening with messages still in the outbox) and asked for history at once. Asks once it has settled.
    private func requestHistoryWhenSettled(_ why: String) async {
        historyHold.settling = true
        try? await Task.sleep(for: .milliseconds(400))
        historyHold.settling = false
        requestHistory(why: why)
    }

    private func requestHistory(retry: Bool = false, why: String = "retry") {
        guard historyVisible, scenePhase == .active, landed, pager.hasOlder,
              retry || ((historyHold.nearTop ?? nearHistory) && !isTopic && !historyHold.settling), !outbox.invalidated,
              let account = outbox.identity?.account, let link = chatLink, let api = chatAPI,
              let request = history.begin(cursor: pager.olderRequest, retry: retry) else { return }
        Self.historyLog.notice("older page before \(request.cursor, privacy: .public) on \(why, privacy: .public): \(historyHold.describe, privacy: .public)")
        historyTask = Task {
            do {
                let page = try await api.larkOlderMessages(conversation.id, before: request.cursor, account: account, topic: isTopic)
                guard !Task.isCancelled, historyVisible, model.link(link.group) === link,
                      outbox.identity?.account == account, !outbox.invalidated else { return }
                // Rows added above keep the visible ones where they are (LarkHistoryHold); a topic group's
                // list keeps its own anchor, as before.
                if !isTopic { await holdHistoryAnchor() }
                guard !Task.isCancelled, history.finish(request, page: page, pager: &pager) else { releaseHistoryAnchor(); return }
                // Let the new rows lay out before deciding whether a short page
                // still needs filling. The near-top geometry reported meanwhile is
                // stale (the content grew before the hold moved the list).
                historyHold.settling = true
                defer { historyHold.settling = false }
                try await Task.sleep(for: .milliseconds(300))
                guard !Task.isCancelled else { return }
                historyHold.settling = false
                requestHistory(why: "page")
            } catch {
                guard !Task.isCancelled else { return }
                history.fail(request, problem: LarkHistoryPaging.problem(for: error))
            }
        }
    }

    private func loadNewer() async {
        guard let api = chatAPI, let cursor = pager.newerRequest, !loadingNewer, pager.hasNewer else { return }
        loadingNewer = true
        defer { loadingNewer = false }
        guard let r = try? await api.larkMessages(conversation.id, after: cursor) else { return }
        pager.append(r, from: cursor)
        // Let the page lay out: the spinner still on screen means the next one.
        try? await Task.sleep(for: .milliseconds(150))
        if pager.hasNewer, book.newerVisible { Task { await loadNewer() } }
    }

    /// 回到最新: at once to the newest messages this device has (kept from before the window moved away, or the chat's
    /// cache), then quietly up to date (#1034); the new-messages line goes away. Only with none of them here does it
    /// wait for the hub, and the button says so.
    private func jumpToLatest() async {
        let drags = book.drags
        if pager.hasNewer, !pager.catchingUp {
            if pager.toTail(cached: LarkMessage.loadCached(LarkMessage.cacheName(conversation)) ?? []) {
                resetHistory()
                anchor = nil
                following = true
                position.scrollTo(edge: .bottom) // not animated: it is another window
                await refreshLatest(untouchedSince: drags)
                return
            }
            jumping = true
            defer { jumping = false }
            if let api = chatAPI, let r = try? await api.larkMessages(conversation.id, topic: isTopic) {
                apply(r)
                anchor = nil
            }
        }
        following = true
        withAnimation(Theme.Motion.standard) { position.scrollTo(edge: .bottom) }
        await refreshLatest(untouchedSince: drags)
    }

    /// 有人@你: to the mention, loading the page around it if needed.
    private func jump(to target: String) async {
        following = false
        if let m = pager.messages.first(where: { $0.position == target }) {
            withAnimation(Theme.Motion.standard) { position.scrollTo(id: m.id, anchor: .top) }
            return
        }
        guard let api = chatAPI, let r = try? await api.larkMessages(conversation.id, around: target) else { return }
        apply(r)
        if let m = pager.messages.first(where: { $0.position == target }) { await scroll(to: m.id) }
    }

    private func openPins() {
        guard let api = chatAPI else { return }
        pinsModel = LarkChatPinsModel(account: outbox.identity?.account,
            deps: .live(api: api, feed: conversation.id, topic: isTopic))
        showPins = true
    }

    private func finishPinNavigation() {
        if pinsModel?.loaded == true && pinsModel?.problem == nil { pinNeedsRecheck = false }
        guard let target = pinTarget else { return }
        pinTarget = nil
        if target.root != nil { pinThread = target; return }
        following = false
        resetHistory()
        pager.replace(with: target.page)
        Task { await scroll(to: target.message.id); flash = QuoteFlashMark(line: target.message.id) }
    }

    private func addPin(_ message: LarkMessage) {
        guard let api = chatAPI, let account = outbox.identity?.account, !changingPin, !pinNeedsRecheck else { return }
        changingPin = true
        Task {
            defer { changingPin = false }
            do {
                try await api.larkChangePin(conversation.id, message: message.id, pin: nil, account: account)
                model.toast = Toast("已添加 Pin", style: .success)
            } catch {
                pinNeedsRecheck = (error as? APIError)?.code == "lark_pin_result_unknown" || error.isTimeout || error.isOffline
                model.toast = Toast(LarkChatPinsModel.problemText(error), style: .error)
                if pinNeedsRecheck { openPins() }
            }
        }
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
                try await chatAPI?.larkReact(m.id, key: key, remove: remove)
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
        guard !isTopic, let api = chatAPI, let report = pager.readReport() else { return }
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
        guard book.receiptTask == nil, let wait = book.receipts.wait(now: .now), let api = chatAPI else { return }
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

    private func restoreDraft() async {
        guard let link = chatLink, let api = chatAPI else { return }
        await LarkChatOutbox.waitForCleanup()
        guard model.link(link.group) === link else { return }
        let revision = await LarkOutboxAccounts.shared.revision
        if !draftLoaded, let cached = await LarkOutboxAccounts.shared.cached(group: link.group, base: .applicationSupportDirectory) {
            await bindDraft(account: cached, link: link)
        }
        do {
            let account = try await api.larkAccountID()
            guard model.link(link.group) === link else { return }
            try await LarkOutboxAccounts.shared.remember(account, group: link.group, base: .applicationSupportDirectory, expectedRevision: revision)
            guard model.link(link.group) === link else { return }
            if outbox.identity?.account != account || !draftLoaded {
                if draftLoaded {
                    saveDraft(immediately: true)
                    await outbox.flushDraft()
                    model.toast = Toast("飞书账号已切换，草稿按账号分别保留")
                }
                await bindDraft(account: account, link: link)
            }
            accountProblem = nil
            await findLegacyDraft(link: link)
        } catch {
            if !draftLoaded { accountProblem = "暂时无法确认飞书账号，请检查连接后重试" }
        }
    }

    private func findLegacyDraft(link: HubLink) async {
        guard let identity = outbox.identity, model.link(link.group) === link else { return }
        legacyDraft = nil; legacyStore = nil
        for config in Set([link.config, link.fallback]) {
            let old = LarkOutboxStore(identity: identity, feed: conversation.id, legacyConfig: config)
            if let saved = try? await old.load(), !saved.isEmpty {
                guard model.link(link.group) === link else { return }
                legacyDraft = saved; legacyStore = old
                return
            }
        }
        let draft = LarkDrafts.shared[.chat(conversation.id)]
        if !draft.isEmpty { legacyDraft = LarkOutboxSnapshot(draft: draft) }
    }

    private func restoreLegacyDraft() {
        guard let legacyDraft, let api = chatAPI, !savingOutgoing else { return }
        let queue = outbox, oldStore = legacyStore
        Task {
            do {
                try await queue.checkAccount(using: api)
                guard outbox === queue, text.isEmpty, replyTo == nil, attachments.isEmpty, queue.items.isEmpty else {
                    model.toast = Toast("请先发送或清空当前草稿，再恢复旧版草稿"); return
                }
                savingOutgoing = true
                defer { savingOutgoing = false }
                try await queue.importLegacy(legacyDraft)
                applyDraft()
                try await oldStore?.removeImportedLegacy()
                LarkDrafts.shared.set(LarkDraft(), for: .chat(conversation.id))
                LarkDrafts.shared.flush()
                self.legacyDraft = nil; legacyStore = nil
            } catch { model.toast = Toast("旧版草稿尚未恢复，请检查账号和本机存储空间后重试", style: .error) }
        }
    }

    private func bindDraft(account: String, link: HubLink) async {
        guard model.link(link.group) === link else { return }
        let identity = LarkOutboxIdentity(group: link.group, account: account)
        let queue = LarkChatOutbox.shared(identity: identity, feed: conversation.id)
        outbox = queue
        draftLoaded = false
        #if DEBUG
        await LarkAttachmentFixture.breakDraft(config: link.config, identity: identity, feed: conversation.id)
        #endif
        do {
            try await queue.prepare()
            guard model.link(link.group) === link, outbox === queue else { return }
            applyDraft()
        } catch { /* The recovery row offers a retry or an explicit discard. */ }
    }

    private func applyDraft() {
        let composer = outbox.composer
        text = composer.draft.text
        replyTo = composer.draft.replyTo
        mentions = composer.draft.mentions ?? []
        encryptionDraft = composer.draft.encryption
        attachments = composer.attachments
        if !messageComposer, text.isEmpty, replyTo == nil, attachments.isEmpty { encryptionDraft = nil }
        draftLoaded = true
    }

    private func saveDraft(immediately: Bool = false, leavingPage: Bool = false) {
        guard draftLoaded, !savingOutgoing, tabActive || leavingPage else { return }
        let draft = LarkDraft(text: text, encryption: encryptionDraft, replyTo: replyTo, mentions: mentions.isEmpty ? nil : mentions)
        outbox.scheduleDraft(draft, attachments: attachments, immediately: immediately)
    }

    private func checkEncryption(openCovers: Bool) {
        guard !checkingEncryption, let api = chatAPI, let identity = outbox.identity else { return }
        let queue = outbox
        checkingEncryption = true
        encryptionProblem = nil
        encryptionRetryOpensCovers = openCovers
        Task {
            defer { checkingEncryption = false }
            do {
                let status = try await api.larkEncryptionStatus()
                guard outbox === queue, !queue.invalidated, status.accountId == identity.account else { throw LarkOutboxIdentity.Failure.changed }
                guard encryptionDraft != nil else { return }
                encryptionStatus = status
                guard status.configured else {
                    encryptionProblem = "请先在 lark-connector 网页创建密钥库，再回来重新检查。已输入的内容会保留。"
                    return
                }
                if openCovers { showEncryptionCovers = true }
            } catch {
                if !error.isCancellation, outbox === queue, encryptionDraft != nil {
                    encryptionProblem = error.shown ?? "暂时无法连接加密服务，请稍后重新检查。已输入的内容会保留。"
                }
            }
        }
    }

    /// The durable composer-to-outbox transaction finishes before the input clears and sending starts.
    private func send() {
        guard let api = chatAPI, let identity = outbox.identity else {
            model.toast = Toast("请重新打开此会话后发送", style: .error); return
        }
        let queue = outbox
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard draftLoaded, !savingOutgoing, !preparingAttachment, !t.isEmpty || !attachments.isEmpty else { return }
        // A picture or file quoting a message needs the hub to carry the quote (#1035); an older one would send it on
        // its own, so nothing goes and the draft stays.
        if replyTo != nil, !attachments.isEmpty, !model.larkFeatures.attachmentReplies {
            model.toast = Toast("需要更新 Hub 上的飞书扩展，才能引用消息发送图片和文件", style: .error); return
        }
        let encrypted = encryptionDraft
        if let encrypted {
            guard messageComposer, attachments.isEmpty, !isTopic else {
                model.toast = Toast("请从消息页发送加密文字", style: .error); return
            }
            guard !encrypted.cover.isEmpty else { checkEncryption(openCovers: true); return }
        }
        var batch: [LarkOutgoing] = []
        if !t.isEmpty { batch.append(LarkOutgoing(text: t, replyTo: replyTo, mentions: LarkMentions.mentions(in: t, picked: mentions))) }
        batch += attachments.map { LarkOutgoing(text: "", replyTo: replyTo, attachment: $0) }
        savingOutgoing = true
        Task {
            defer { savingOutgoing = false }
            if let encrypted {
                do {
                    let status = try await api.larkEncryptionStatus()
                    guard status.accountId == identity.account, outbox === queue, !queue.invalidated else { throw LarkOutboxIdentity.Failure.changed }
                    encryptionStatus = status
                    guard status.configured else {
                        model.toast = Toast("请先在 lark-connector 网页创建密钥库", style: .error); return
                    }
                    if status.locked { showEncryptionUnlock = true; return }
                    let payload = try await api.larkPrepareEncryption(account: identity.account, feed: conversation.id, text: t, cover: encrypted.cover)
                    guard outbox === queue, !queue.invalidated else { throw LarkOutboxIdentity.Failure.changed }
                    batch[0].encrypted = payload
                    batch[0].mentions = []
                } catch {
                    if (error as? APIError)?.code == "OLK_VAULT_LOCKED" { showEncryptionUnlock = true }
                    else { model.toast = Toast(error.shown ?? "未能加密，消息保留在输入框", style: .error) }
                    return
                }
            }
            do { try await queue.enqueue(batch) }
            catch {
                model.toast = Toast("消息未发送，无法保存到本机，请检查存储空间", style: .error); return
            }
            if outbox === queue {
                // Animated like the rows the outbox just added: the box shrinking back to a line (and the quote bar
                // going) moves the list's bottom inset with them, so the list does not drop first and then rise.
                withAnimation(Theme.Motion.standard) {
                    text = ""
                    replyTo = nil
                    mentions = []
                    attachments = []
                }
            }
            savingOutgoing = false
            Haptics.tap()
            for item in batch { deliver(item.id, using: api, queue: queue) }
            if pager.hasNewer { Task { await jumpToLatest() } } else { following = true }
        }
    }

    private func resend(_ id: UUID) {
        guard !checkingOutgoing.contains(id), let api = chatAPI, outbox.identity != nil else { return }
        let queue = outbox
        checkingOutgoing.insert(id)
        Task {
            defer { checkingOutgoing.remove(id) }
            do {
                try await queue.checkAccount(using: api)
                // Check with the original account even if the user switches Hub while this request runs.
                let latest = try await api.larkMessages(conversation.id, topic: isTopic)
                try await queue.settle(latest.messages)
                guard let item = queue.items.first(where: { $0.id == id }) else { return }
                guard item.canRetry else {
                    model.toast = Toast("尚未确认文件是否发出，请稍后再检查", style: .error)
                    return
                }
                try await queue.retry(id)
                deliver(id, using: api, queue: queue)
            } catch { model.toast = Toast("暂时无法重试，请检查连接和本机存储空间", style: .error) }
        }
    }

    private func discard(_ id: UUID) {
        Task {
            do { try await outbox.discard(id) }
            catch { model.toast = Toast("未能删除，请检查本机存储空间", style: .error) }
        }
    }

    private func restore(_ outgoing: LarkOutgoing) {
        guard !savingOutgoing else { return }
        let queue = outbox
        if outgoing.encrypted != nil && (!text.isEmpty || !attachments.isEmpty) || outgoing.encrypted == nil && encryptionDraft != nil {
            model.toast = Toast("请先处理输入框中的草稿，再恢复这条消息", style: .error)
            return
        }
        var draft = LarkDraft(text: text, encryption: encryptionDraft, replyTo: replyTo, mentions: mentions.isEmpty ? nil : mentions), files = attachments
        if let encrypted = outgoing.encrypted { draft.encryption = LarkEncryptedDraft(cover: encrypted.cover ?? "") }
        if let attachment = outgoing.attachment {
            guard files.count < LarkAttachment.selectionLimit else {
                model.toast = Toast("请先移除一个附件，再放回输入框", style: .error)
                return
            }
            files.append(attachment)
        } else {
            draft.text = draft.text.isEmpty ? outgoing.text : draft.text + "\n" + outgoing.text
            if draft.replyTo == nil { draft.replyTo = outgoing.replyTo }
            draft.mentions = (draft.mentions ?? []) + outgoing.mentions
        }
        savingOutgoing = true
        Task {
            defer { savingOutgoing = false }
            do {
                try await queue.restore(outgoing.id, draft: draft, attachments: files)
                if outbox === queue { applyDraft() }
            } catch { model.toast = Toast("未能放回输入框，请检查本机存储空间", style: .error) }
        }
    }

    private func deliver(_ id: UUID, using api: APIClient, queue: LarkChatOutbox) {
        let link = chatLink
        queue.deliver(id, client: {
            guard let link, model.link(link.group) === link else { return nil }
            return model.api(link.group)?.lark(conversation.account)
        }, feed: conversation.id) {
            guard let link, model.link(link.group) === link, outbox === queue else { return }
            await refreshLatest()
            settleOutgoing(pager.messages)
        }
    }

    private func settleOutgoing(_ messages: [LarkMessage]) {
        guard let api = chatAPI, !outbox.items.isEmpty else { return }
        let queue = outbox
        Task {
            guard (try? await queue.checkAccount(using: api)) != nil else { return }
            do {
                try await queue.settle(messages)
            }
            catch { model.toast = Toast("发送结果尚未保存，请检查本机存储空间", style: .error) }
        }
    }

    private func askJarvis() async {
        guard let api = chatAPI else { return }
        let prompt = "读取我飞书会话「\(conversation.name)」（lark-connector feedId \(conversation.id)）最近的消息，总结要点，列出需要我回复或处理的事，并给出建议的回复。"
        do {
            let s = try await api.startConversation(prompt: prompt)
            model.began(s, text: prompt)
            model.assistantCurrent = s.key
            model.selectedTab = .assistant
        } catch {
            model.show(error)
        }
    }
}

/// A chat's rows, laid out as they come on screen; a topic group's cards all at once. In a lazy stack with a scroll
/// position, a topic card opened under 展开 left the app never idle again on the iPhone (#1087; likely the off-screen
/// rows placed by estimated heights and measured again without end, docs/lessons/ios.md). A page of a topic group is
/// 40 topics; each 加载更早的话题 adds 40 more, all laid out: accepted, a group is rarely read that far back.
private struct LarkChatStack<Content: View>: View {
    var lazy: Bool
    @ViewBuilder var content: Content

    var body: some View {
        // The scroll target layout on the stack itself: on this view it would not reach the rows.
        if lazy {
            LazyVStack(alignment: .leading, spacing: 0) { content }.scrollTargetLayout()
        } else {
            VStack(alignment: .leading, spacing: 0) { content }.scrollTargetLayout()
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
    @Environment(\.larkAddPin) private var addPin
    @Environment(\.larkReact) private var react
    @Environment(\.larkReply) private var reply
    @Environment(\.larkDisplay) private var display
    @State private var showHistory = false
    @State private var showThread = false
    /// The thread sheet opens with its box focused (回复 on a topic group's thread).
    @State private var threadFocus = false
    @State private var picking = false

    var body: some View {
        let m = message
        if m.isSystem {
            Group {
                if m.recalled { Text(recallNotice).font(Theme.Font.label).foregroundStyle(.secondary) }
                else if let p = m.parts?.first, p.metaBool("system") { LarkSystemText(part: p) }
                else { Text(m.text).font(Theme.Font.label).foregroundStyle(.secondary) }
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
                    if m.olk == nil { LarkTranslationLine(message: m, feedID: feedID) }
                    footer
                }
                .frame(maxWidth: .infinity, alignment: side.alignment)
            }
            .sheet(isPresented: $showHistory) { LarkEditHistoryView(message: message) }
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

    private var recallNotice: String { message.recallNotice }

    @ViewBuilder private var content: some View {
        let m = message
        let parts = m.parts ?? []
        if let localAttachment {
            // A picture or file quoting a message shows the quote above it (#1035), as one block: next to the sending
            // mark the row is a horizontal stack, and two loose views went side by side.
            VStack(alignment: LarkChatLayout.side(of: m).horizontal, spacing: Theme.Space.xs) {
                if let reply = m.replyTo { LarkReplyQuote(reply: reply) }
                LarkAttachmentView(attachment: localAttachment)
            }
        } else if !display.showsBody(of: m) {
            Text(recallNotice).font(Theme.Font.caption).foregroundStyle(.tertiary).italic()
                .padding(.horizontal, Theme.Space.m).padding(.vertical, Theme.Space.s)
                .background(Color.primary.opacity(0.04), in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
        } else if let encryption = m.olk {
            bubble {
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    if let reply = m.replyTo { LarkReplyQuote(reply: reply) }
                    LarkEncryptedContent(encryption: encryption)
                }
            }
        } else if parts.isEmpty {
            bubble {
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    if let reply = m.replyTo { LarkReplyQuote(reply: reply) }
                    Text(m.text.isEmpty ? "[\(m.type.lowercased())]" : m.text).font(Theme.Font.body)
                }
            }
        } else if LarkPartsView.isBare(parts) {
            // One block (see the local attachment above): the quote over the picture, not beside it.
            VStack(alignment: LarkChatLayout.side(of: m).horizontal, spacing: Theme.Space.xs) {
                if let reply = m.replyTo { LarkReplyQuote(reply: reply).padding(.horizontal, Theme.Space.xs) }
                LarkPartsView(parts: parts)
                    .environment(\.larkContext, LarkRenderContext(messageID: m.recalled ? nil : m.id, images: LarkPartsView.imageURLs(parts)))
                    .frame(maxWidth: parts.contains { $0.kind == "card" || $0.kind == "link" } ? .infinity : nil, alignment: .leading)
                    .contextMenu { menu }
            }
        } else {
            bubble {
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    if let reply = m.replyTo { LarkReplyQuote(reply: reply) }
                    LarkPartsView(parts: parts)
                        .environment(\.larkContext, LarkRenderContext(messageID: m.recalled ? nil : m.id, images: LarkPartsView.imageURLs(parts)))
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

    private var menu: some View {
        LarkMessageMenu(message: message, react: react, reply: replyAction, pickReaction: { picking = true }, addPin: addPin,
                        showProfile: openProfile == nil ? nil : { showProfile() })
    }

    /// 回复 in the menu: the quote bar over the chat's box, the thread's box, or (a topic group's topic) its thread
    /// with the box focused. Nil when there is nothing to answer.
    private var replyAction: (() -> Void)? {
        guard let reply, let route = LarkReply.route(for: message, inTopicGroup: reply.inTopicGroup, inThread: inThread) else { return nil }
        return {
            if route == .openThread {
                threadFocus = true
                showThread = true
            } else {
                reply.answer(message)
            }
        }
    }

    @ViewBuilder private var footer: some View {
        let m = message
        let reactions = m.reactions ?? []
        if !reactions.isEmpty && !m.recalled {
            LarkFlowLayout(spacing: 6, lineSpacing: 6, trailing: LarkChatLayout.side(of: m) == .right) {
                ForEach(reactions, id: \.key) { r in
                    Button { react?(m, r.key) } label: { LarkReactionChip(reaction: r) }
                        .buttonStyle(.plain)
                        .disabled(react == nil)
                }
                if react != nil {
                    Button { picking = true } label: {
                        Image(systemName: Theme.Icon.react).font(.system(size: 14)).foregroundStyle(.secondary)
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
        if m.recalled && display.showsBody(of: m) {
            Text("已撤回").font(Theme.Font.caption).foregroundStyle(.secondary)
        }
        if (!m.recalled && replies > 0) || (m.edited == true && display.showsBody(of: m)) {
            HStack(spacing: Theme.Space.s) {
                if replies > 0 && !inThread && !m.recalled {
                    Button { showThread = true } label: {
                        Label("\(replies) 条回复", systemImage: Theme.Icon.reply).foregroundStyle(LarkColor.blue)
                    }
                    .buttonStyle(.plain)
                }
                if m.edited == true {
                    if display.showsHistory(of: m) {
                        Button("已编辑") { showHistory = true }.buttonStyle(.plain).lineLimit(1)
                            .accessibilityLabel("查看编辑历史").accessibilityIdentifier("lark.editHistory.\(m.id)")
                    } else { Text("已编辑") }
                }
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

extension LarkMessage {
    /// What a recalled message says where its body is not shown (`LarkMessageDisplay.showsBody`): a chat's row, a topic
    /// group's card.
    var recallNotice: String {
        if isMine { return "你撤回了一条消息" }
        let name = senderName.trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? "消息已撤回" : "\(name) 撤回了一条消息"
    }
}

/// Long press on a Feishu message (a chat's row, a topic group's card): a bar of quick reactions on top (one tap reacts),
/// then reply, more emoji, copy, the sender's card. A context menu cannot put a tappable view above itself (its preview
/// takes no taps), so the bar is the menu's first row, as a palette: six emoji across (all the palette shows without
/// scrolling) instead of the four a compact group fits.
struct LarkMessageMenu: View {
    var message: LarkMessage
    var react: ((LarkMessage, String) -> Void)?
    /// 回复, when there is somewhere to answer.
    var reply: (() -> Void)?
    /// 表情回复: the whole picker.
    var pickReaction: () -> Void
    /// 添加 Pin, where the chat can pin (`larkAddPin`).
    var addPin: ((LarkMessage) -> Void)?
    var showProfile: (() -> Void)?

    var body: some View {
        if let react, !message.recalled {
            ControlGroup {
                ForEach(LarkQuickReactions.bar(LarkMedia.shared.quickReactions), id: \.self) { key in
                    // A title with the picture: the menu takes its items' names from their text (an image alone left
                    // the item nameless, and VoiceOver and the tests found no 「爱心」).
                    let name = LarkMedia.shared.emoji(key)?.label ?? LarkMedia.labelFallback(key, text: nil)
                    Button { react(message, key) } label: {
                        if let img = LarkMedia.shared.emojiImage(key, points: LarkMedia.barPoints) {
                            Label { Text(name) } icon: { Image(uiImage: img).renderingMode(.original) }
                        } else {
                            Text(name)
                        }
                    }
                    .accessibilityLabel(name)
                    .accessibilityIdentifier("lark.quickReaction")
                }
            }
            .controlGroupStyle(.palette)
            .menuActionDismissBehavior(.enabled) // a palette keeps the menu up after a tap; Feishu closes it
        }
        if let reply {
            Button(action: reply) { Label("回复", systemImage: Theme.Icon.reply) }
        }
        if react != nil && !message.recalled {
            Button(action: pickReaction) { Label("表情回复", systemImage: Theme.Icon.react) }
        }
        Button {
            UIPasteboard.general.string = message.displayText
            Haptics.success()
        } label: { Label("拷贝", systemImage: Theme.Icon.copy) }
        if let addPin, message.canPin == true && !message.recalled && message.deleted != true && message.localState != "pending" && message.localState != "failed" {
            Button { addPin(message) } label: { Label("添加 Pin", systemImage: Theme.Icon.pin) }
                .accessibilityIdentifier("lark.pin.add")
        }
        if let showProfile, let id = message.senderID, !id.isEmpty {
            Button(action: showProfile) { Label("查看 \(message.senderName) 的资料", systemImage: Theme.Icon.person) }
        }
    }
}

/// The message a reply quotes, at the top of the bubble: its author and a line or two of it (`QuoteCard`). A tap goes
/// back to it in the chat, lit up for a moment, when it is loaded there.
struct LarkReplyQuote: View {
    var reply: LarkReplyTo
    @Environment(\.larkDisplay) private var display
    @Environment(\.larkJump) private var jump

    var body: some View {
        let summary = reply.recalled == true && !(display.antiRecall && reply.retained == true)
            ? "消息已撤回"
            : "\(Fmt.plain(reply.displayText))\(reply.recalled == true ? " · 已撤回" : "")"
        QuoteCard(heading: reply.senderName, summary: summary, tint: LarkColor.blue, ink: .primary) {
            if jump?(reply.id) == true { Haptics.tap() } else { Haptics.attention() }
        }
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

