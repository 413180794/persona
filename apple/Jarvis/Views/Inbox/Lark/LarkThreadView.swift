import SwiftUI

/// A thread: its first message, then the replies. Opens on the latest page; older pages come with 加载更早的回复
/// (like the chat's 加载更早的消息), and a change in the chat while it is open brings the newest replies in.
/// The box at the bottom replies in the thread: the reply shows at once, goes out in the background, can be sent
/// again when it fails, and the newest page comes in once it is sent.
struct LarkThreadView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var root: LarkMessage
    var isGroup = true
    var feedID: String? = nil
    /// Opened by 回复 on a topic group's thread: the box is focused.
    var focusBox = false
    @State private var person: LarkPersonRef?
    /// The replies loaded so far (never the root) and where paging stands (LarkPager).
    @State private var pager = LarkPager()
    @State private var loading = true
    @State private var loadingOlder = false
    @State private var error: String?
    @State private var fileRequest: (url: String, name: String)?
    @State private var text = ""
    @FocusState private var boxFocused: Bool
    /// Replies on their way (LarkOutgoing, `thread` true), under the loaded ones until their copies arrive.
    @State private var outgoing: [LarkOutgoing] = []
    @State private var sendChain: Task<Void, Never>?
    @State private var position = ScrollPosition()

    private var replies: [LarkMessage] { pager.messages }

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    LarkTimeSeparator(date: root.createdAt)
                    LarkMessageRow(message: root, grouped: false, isGroup: isGroup, feedID: feedID, inThread: true)
                    HStack {
                        Text(loading ? "加载回复…" : "\(replies.count) 条回复").font(Theme.Font.labelMedium).foregroundStyle(.secondary)
                        Rectangle().fill(Color.primary.opacity(0.08)).frame(height: Theme.Size.line)
                    }
                    .padding(.vertical, Theme.Space.m)
                    if let error { Label(error, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger) }
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
                    }
                    outgoingRows
                }
                .padding(Theme.Space.m)
            }
            .scrollPosition($position)
            .safeAreaInset(edge: .bottom) {
                LarkComposer(placeholder: "回复话题", text: $text, focused: $boxFocused, sendID: "lark.thread.send", send: send)
            }
            .onChange(of: pager.messages) { _, now in settleOutgoing(now) }
            .environment(\.larkReply, LarkReplyHandler(inThread: true) { _ in boxFocused = true })
            .environment(\.larkOpenFile, { url, name in fileRequest = (url, name) })
            .environment(\.larkOpenProfile, { person = $0 })
            .environment(\.larkReact, { m, key in
                guard let i = pager.messages.firstIndex(where: { $0.id == m.id }) else { return }
                let (next, remove) = m.togglingReaction(key, me: "我")
                pager.messages[i] = next
                Task { do { try await model.api?.larkReact(m.id, key: key, remove: remove) } catch { model.show(error) } }
            })
            .modifier(LarkFileOpener(request: $fileRequest))
            .sheet(item: $person) { LarkProfileView(person: $0) }
            .navigationTitle("话题")
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
                defer { loading = false }
                guard let api = model.api else { return }
                do { pager.replace(with: try await api.larkThread(root.id).replies(of: root.id)) } catch { self.error = error.shown }
            }
            // A reply in the thread is a change in its chat: bring the newest replies in while the sheet is open.
            .task(id: feedID) {
                guard let feedID else { return }
                for await _ in LarkSync.shared.changes(of: feedID) { await refreshLatest() }
            }
        }
    }

    private func loadOlder() async {
        guard let api = model.api, let cursor = pager.olderRequest, pager.hasOlder, !loadingOlder else { return }
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
                            edit: { discard(o.id); text = text.isEmpty ? o.text : text + "\n" + o.text },
                            discard: { discard(o.id) })
                .padding(.top, LarkChatLayout.gap(grouped: grouped))
                .id(o.id)
        }
    }

    private func send() {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return }
        text = ""
        let o = LarkOutgoing(text: t, replyTo: LarkReply.quote(root), thread: true)
        withAnimation(Theme.Motion.standard) { outgoing.append(o) }
        Haptics.tap()
        deliver(o.id)
        Task {
            // After the row is laid out: scrolling in the same update lands above it.
            try? await Task.sleep(for: .milliseconds(60))
            withAnimation(Theme.Motion.standard) { position.scrollTo(id: o.id, anchor: .bottom) }
        }
    }

    private func resend(_ id: UUID) {
        guard let i = outgoing.firstIndex(where: { $0.id == id }) else { return }
        outgoing[i].retry()
        deliver(id)
    }

    private func discard(_ id: UUID) {
        withAnimation(Theme.Motion.standard) { outgoing.removeAll { $0.id == id } }
    }

    private func deliver(_ id: UUID) {
        let before = sendChain
        sendChain = Task {
            await before?.value // in the order written
            guard let api = model.api, let o = outgoing.first(where: { $0.id == id }), o.state == .sending else { return }
            do {
                try await LarkOutbox.deliver(o, feed: feedID ?? "", api: api)
                setState(id, .sent)
                // Its copy replaces it (settleOutgoing) when the newest page brings it.
                await refreshLatest()
                try? await Task.sleep(for: .seconds(2))
                await refreshLatest()
                try? await Task.sleep(for: .seconds(LarkOutbox.copyWait))
                settleOutgoing(pager.messages)
            } catch {
                setState(id, .failed(error.shown ?? error.localizedDescription))
                Haptics.error()
            }
        }
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
        guard !loading, let api = model.api, let r = try? await api.larkThread(root.id) else { return }
        let fresh = pager.refresh(with: r.replies(of: root.id))
        guard !fresh.isEmpty else { return }
        withAnimation(Theme.Motion.standard) { pager.add(fresh) }
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
