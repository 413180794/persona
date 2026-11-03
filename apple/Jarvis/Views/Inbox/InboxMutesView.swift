import SwiftUI

/// 已静音 (the inbox's 更多 menu): the repositories, chats and senders muted from an item's menu, in every source,
/// newest first. A swipe or a long press takes one back; nothing it covered comes back, only what comes in later
/// (docs/inbox.md「静音」).
struct InboxMutesView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var state: Loadable<[InboxMute]> = .loading

    var body: some View {
        NavigationStack {
            LoadableContent(state: state, retry: { Task { await load() } }) { mutes in
                List {
                    ForEach(mutes) { m in row(m).themedRow() }
                }
                .listStyle(.plain)
                .overlay {
                    if mutes.isEmpty {
                        EmptyStateView(symbol: Theme.Icon.mute, title: "没有静音的", message: "在收件箱里长按一条，可以静音它的仓库、会话或发件人。")
                    }
                }
            }
            .themedPage()
            .navigationTitle("已静音")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") { dismiss() }.lineLimit(1)
                }
            }
            .task { await load() }
        }
    }

    private func row(_ m: InboxMute) -> some View {
        HStack(spacing: Theme.Space.m) {
            Image(systemName: Theme.Icon.mute).foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(m.name).font(Theme.Font.body).lineLimit(1)
                Text([m.sourceName ?? m.source, m.tagName].joined(separator: " · "))
                    .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, Theme.Space.xs)
        .accessibilityIdentifier("inbox-mute")
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button { unmute(m) } label: { Label("取消静音", systemImage: Theme.Icon.unmute) }.tint(Theme.accent)
        }
        .contextMenu {
            Button { unmute(m) } label: { Label("取消静音", systemImage: Theme.Icon.unmute) }
        }
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            state = .loaded(try await api.inboxMutes().sorted { ($0.createdAt ?? .distantPast) > ($1.createdAt ?? .distantPast) })
        } catch {
            if case .loaded = state { model.show(error) } else { state = .failed(error.localizedDescription) }
        }
    }

    private func unmute(_ m: InboxMute) {
        guard case .loaded(let list) = state else { return }
        withAnimation(Theme.Motion.standard) { state = .loaded(list.filter { $0.id != m.id }) }
        Task {
            do {
                try await model.api?.unmuteInbox(m.id)
            } catch {
                state = .loaded(list)
                model.show(error)
            }
        }
    }
}
