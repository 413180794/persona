import SwiftUI

/// A thread: its first message, then the replies. Opens on the latest page; older pages come with 加载更早的回复
/// (like the chat's 加载更早的消息), and a change in the chat while it is open brings the newest replies in.
struct LarkThreadView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var root: LarkMessage
    var isGroup = true
    var feedID: String? = nil
    @State private var person: LarkPersonRef?
    /// The replies loaded so far (never the root) and where paging stands (LarkPager).
    @State private var pager = LarkPager()
    @State private var loading = true
    @State private var loadingOlder = false
    @State private var error: String?
    @State private var fileRequest: (url: String, name: String)?

    private var replies: [LarkMessage] { pager.messages }

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
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
                        let prev = i > 0 ? replies[i - 1] : nil
                        LarkMessageRow(message: m, grouped: LarkChatView.continues(m, after: prev), isGroup: isGroup, feedID: feedID, inThread: true)
                            .padding(.top, LarkChatView.gap(grouped: LarkChatView.continues(m, after: prev)))
                    }
                }
                .padding(Theme.Space.m)
            }
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
