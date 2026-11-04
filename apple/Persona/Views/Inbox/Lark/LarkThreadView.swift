import SwiftUI

/// A thread: its first message, then the replies. Opens on the latest page; older pages come with 加载更早的回复
/// (like the chat's 加载更早的消息), and a change in the chat while it is open brings the newest replies in.
/// The box at the bottom replies in the thread: the reply shows at once, goes out in the background, can be sent
/// again when it fails, and the newest page comes in once it is sent.
struct LarkThreadView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.larkAccount) private var account
    var root: LarkMessage
    var isGroup = true
    var feedID: String? = nil
    /// Opened by 回复 on a topic group's thread: the box is focused.
    var focusBox = false
    var pinTarget: LarkChatPinTarget? = nil
    var pinAPI: APIClient? = nil
    private var api: APIClient? { pinAPI ?? model.api?.lark(account) }
    /// Opened from a topic group's card: 话题详情, 来自 <the group> (lark-connector's topic page).
    var chatName: String? = nil
    @State private var person: LarkPersonRef?
    /// The replies loaded so far (never the root) and where paging stands (LarkPager).
    @State private var pager = LarkPager()
    @State private var loading = true
    @State private var loadingOlder = false
    /// Why the replies did not load: the replies' placeholder while none are shown, a line over them otherwise
    /// (LarkLoadProblem, as on the chat page).
    @State private var problem: LarkLoadProblem?
    @State private var fileRequest: (url: String, name: String)?
    /// What is typed in the box, kept as the thread's draft (LarkDrafts) until it is sent.
    @State private var text = ""
    /// Picked with the @ picker since the last send.
    @State private var mentions: [LarkMention] = []
    @FocusState private var boxFocused: Bool
    /// Replies on their way (LarkOutgoing, `thread` true), under the loaded ones until their copies arrive.
    @State private var outgoing: [LarkOutgoing] = []
    @State private var sendChain: Task<Void, Never>?
    @State private var position = ScrollPosition()
    /// The list stays at its newest reply as it changes (larkFollowsEnd): after sending, or once scrolled to the end.
    @State private var following = false
    @State private var usedPinTarget = false
    @State private var flash: QuoteFlashMark?

    private var replies: [LarkMessage] { pager.messages }

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    LarkTimeSeparator(date: root.createdAt)
                    LarkMessageRow(message: root, grouped: false, isGroup: isGroup, feedID: feedID, inThread: true)
                        .overlay { if flash?.line == root.id { QuoteFlash().id(flash?.id) } }
                        .id(root.id)
                    HStack {
                        Text(loading ? "加载回复…" : hasReplies || problem == nil ? "\(replies.count) 条回复" : "回复")
                            .font(Theme.Font.labelMedium).foregroundStyle(.secondary)
                        Rectangle().fill(Color.primary.opacity(0.08)).frame(height: Theme.Size.line)
                    }
                    .padding(.vertical, Theme.Space.m)
                    if let state = LarkChatPlaceholder.of(hasContent: hasReplies, loading: loading, problem: problem) {
                        placeholder(state)
                    } else if let line = problem?.inline {
                        Label(line, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger)
                    }
                    if pager.hasOlder {
                        LarkLoadOlderButton(title: "加载更早的回复", loading: loadingOlder) { Task { await loadOlder() } }
                    }
                    ForEach(Array(replies.enumerated()), id: \.element.id) { i, m in
                        // The first loaded reply's time counts from the root's (under the 条回复 line).
                        let prev = i > 0 ? replies[i - 1] : root
                        let grouped = i > 0 && LarkChatLayout.continues(m, after: prev)
                        if LarkChatLayout.showsTimeSeparator(m, after: prev) { LarkTimeSeparator(date: m.createdAt) }
                        LarkMessageRow(message: m, grouped: grouped, isGroup: isGroup, feedID: feedID, inThread: true)
                            .padding(.top, LarkChatLayout.gap(grouped: grouped))
                            .overlay { if flash?.line == m.id { QuoteFlash().id(flash?.id) } }
                            .id(m.id)
                    }
                    outgoingRows
                }
                .padding(Theme.Space.m)
            }
            .scrollPosition($position)
            .larkFollowsEnd($position, following: $following, active: !loading)
            .safeAreaInset(edge: .bottom) {
                LarkComposer(placeholder: "回复话题", text: $text, focused: $boxFocused, boxID: "lark.thread.box", sendID: "lark.thread.send",
                             mentionFeed: isGroup ? feedID : nil, me: (replies + [root]).last(where: \.isMine)?.senderID,
                             mentions: $mentions, send: send)
            }
            .onChange(of: pager.messages) { _, now in settleOutgoing(now) }
            .onAppear {
                guard text.isEmpty else { return }
                let draft = LarkDrafts.shared[.thread(root.id)]
                text = draft.text
                mentions = draft.mentions ?? []
            }
            .onChange(of: LarkDraft(text: text, mentions: mentions.isEmpty ? nil : mentions)) { _, draft in
                LarkDrafts.shared.set(draft, for: .thread(root.id))
            }
            .onDisappear { LarkDrafts.shared.flush() }
            .environment(\.larkReply, LarkReplyHandler(inThread: true) { _ in boxFocused = true })
            .environment(\.larkOpenFile, { url, name in fileRequest = (url, name) })
            .environment(\.larkOpenProfile, { person = $0 })
            .environment(\.larkReact, { m, key in
                guard let i = pager.messages.firstIndex(where: { $0.id == m.id }) else { return }
                let (next, remove) = m.togglingReaction(key, me: "我")
                pager.messages[i] = next
                Task { do { try await api?.larkReact(m.id, key: key, remove: remove) } catch { model.show(error) } }
            })
            .modifier(LarkFileOpener(request: $fileRequest))
            .sheet(item: $person) { LarkProfileView(person: $0) }
            .modifier(LarkThreadTitle(chatName: chatName))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("完成") { dismiss() } } }
            .task {
                if focusBox {
                    // Once the sheet is up: focusing while it slides in is dropped.
                    Task {
                        try? await Task.sleep(for: .milliseconds(350))
                        boxFocused = true
                    }
                }
                await load()
            }
            // A reply in the thread is a change in its chat: bring the newest replies in while the sheet is open.
            .task(id: feedID) {
                guard let feedID else { return }
                for await _ in LarkSync.shared.changes(of: LarkConversation.key(feedID, account: account)) { await refreshLatest() }
            }
        }
    }

    /// Replies, or mine on their way: a problem is then one line over them rather than the placeholder.
    private var hasReplies: Bool { !replies.isEmpty || !outgoing.isEmpty }

    /// Under the 条回复 line while there are no replies to show: loading, none yet, or what went wrong with 重试
    /// (the chat page's states; BeautifulUI draws its own).
    @ViewBuilder private func placeholder(_ state: LarkChatPlaceholder) -> some View {
        switch state {
        case .loading:
            LoadableContent(state: Loadable<Void>.loading, retry: nil) { _ in EmptyView() }
                .padding(.vertical, Theme.Space.xl)
        case .empty:
            EmptyStateView(symbol: Theme.Icon.conversation, title: "还没有回复", message: "在下面回复，开始这个话题。")
        case .failed(let p):
            EmptyStateView(symbol: p.symbol, title: p.title, message: p.message, action: (title: "重试", run: retry))
        }
    }

    private func load() async {
        defer { loading = false }
        if let target = pinTarget, !usedPinTarget {
            usedPinTarget = true
            pager.replace(with: target.page.replies(of: root.id))
            loading = false
            try? await Task.sleep(for: .milliseconds(80))
            guard !Task.isCancelled else { return }
            position.scrollTo(id: target.id, anchor: .center)
            flash = QuoteFlashMark(line: target.id)
            return
        }
        guard let api = api else { return }
        do {
            pager.replace(with: try await api.larkThread(root.id).replies(of: root.id))
            problem = nil
        } catch {
            problem = LarkLoadProblem.of(error, subject: "这个话题")
        }
    }

    private func retry() {
        problem = nil
        loading = true
        Task { await load() }
    }

    private func loadOlder() async {
        guard let api = api, let cursor = pager.olderRequest, pager.hasOlder, !loadingOlder else { return }
        loadingOlder = true
        defer { loadingOlder = false }
        guard let r = try? await api.larkThread(root.id, before: cursor) else { return }
        pager.prepend(r.replies(of: root.id))
    }

    // MARK: Sending

    @ViewBuilder private var outgoingRows: some View {
        let mine = (replies + [root]).last(where: \.isMine)
        ForEach(Array(outgoing.enumerated()), id: \.element.id) { i, o in
            let m = o.message(like: mine)
            let prev = i > 0 ? outgoing[i - 1].message(like: mine) : (replies.last ?? root)
            let grouped = LarkChatLayout.continues(m, after: prev)
            if LarkChatLayout.showsTimeSeparator(m, after: prev) { LarkTimeSeparator(date: m.createdAt) }
            LarkOutgoingRow(outgoing: o, message: m, grouped: grouped, isGroup: isGroup,
                            retry: { resend(o.id) },
                            edit: {
                                discard(o.id)
                                text = text.isEmpty ? o.text : text + "\n" + o.text
                                mentions += o.mentions
                            },
                            discard: { discard(o.id) })
                .padding(.top, LarkChatLayout.gap(grouped: grouped))
                .id(o.id)
        }
    }

    private func send() {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return }
        let o = LarkOutgoing(text: t, replyTo: LarkReply.quote(root), thread: true, mentions: LarkMentions.mentions(in: t, picked: mentions))
        // One animation for the box shrinking back to a line and the row coming in: the list's bottom inset moves with
        // the row, so the list does not drop first and then rise.
        withAnimation(Theme.Motion.standard) {
            text = ""
            mentions = []
            outgoing.append(o)
        }
        Haptics.tap()
        deliver(o.id)
        following = true // the reply shows right above the box (larkFollowsEnd)
    }

    private func resend(_ id: UUID) {
        guard let i = outgoing.firstIndex(where: { $0.id == id }) else { return }
        outgoing[i].retry()
        deliver(id)
    }

    private func discard(_ id: UUID) {
        withAnimation(Theme.Motion.standard) { outgoing.removeAll { $0.id == id } }
    }

    /// Replies go out in the order written: the chain holds only the send itself. Waiting for each one's copy runs on
    /// its own, so the next reply does not wait behind the one before for its refreshes and `copyWait` (#851: the
    /// second of three replies went out 24 s after it was sent), as the chat's outbox does.
    private func deliver(_ id: UUID) {
        let before = sendChain
        sendChain = Task {
            await before?.value // in the order written
            guard let api = api, let o = outgoing.first(where: { $0.id == id }), o.state == .sending else { return }
            do {
                try await LarkOutbox.deliver(o, feed: feedID ?? "", api: api)
                setState(id, .sent)
            } catch {
                setState(id, .failed(error.shown ?? error.localizedDescription))
                Haptics.error()
                return
            }
            Task { await awaitCopy() }
        }
    }

    /// The sent reply's copy replaces it (settleOutgoing) when the newest page brings it.
    private func awaitCopy() async {
        await refreshLatest()
        try? await Task.sleep(for: .seconds(2))
        await refreshLatest()
        try? await Task.sleep(for: .seconds(LarkOutbox.copyWait))
        settleOutgoing(pager.messages)
    }

    private func setState(_ id: UUID, _ state: LarkOutgoing.State) {
        guard let i = outgoing.firstIndex(where: { $0.id == id }) else { return }
        outgoing[i].state = state
        settleOutgoing(pager.messages)
    }

    private func settleOutgoing(_ messages: [LarkMessage]) {
        let left = LarkOutbox.remaining(outgoing, messages: messages)
        if left != outgoing { outgoing = left }
    }

    private func refreshLatest() async {
        guard !loading, let api = api, let r = try? await api.larkThread(root.id) else { return }
        problem = nil // it answers again (lark-connector signed in again, the network is back)
        let fresh = pager.refresh(with: r.replies(of: root.id))
        guard !fresh.isEmpty else { return }
        withAnimation(Theme.Motion.standard) { pager.add(fresh) }
    }
}

/// 话题 for a thread in a chat; a topic group's topic is 话题详情 with the group it is from under it.
private struct LarkThreadTitle: ViewModifier {
    var chatName: String?

    func body(content: Content) -> some View {
        if let chatName {
            content.navigationTitle("话题详情").navigationSubtitle("来自 \(chatName)")
        } else {
            content.navigationTitle("话题")
        }
    }
}

extension APIClient.LarkMessages {
    /// A thread page without its root (the sheet shows the root above the replies).
    func replies(of rootID: String) -> Self {
        var r = self
        r.messages.removeAll { $0.id == rootID }
        return r
    }
}
