import PersonaKit
import SwiftUI

/// 已归档: the conversations swiped out of the sidebar (or archived by Persona), newest first. A tap opens one, a swipe
/// or a long press brings it back into the list. Nothing here deletes: archiving only hides (docs/assistant.md).
struct ArchivedConversationsView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var open: (SessionKey) -> Void

    var body: some View {
        let list = model.archivedConversations
        NavigationStack {
            List {
                ForEach(list) { s in row(s).themedRow() }
            }
            .listStyle(.plain)
            .themedPage()
            .overlay {
                if list.isEmpty {
                    EmptyStateView(symbol: Theme.Icon.archive, title: "没有归档的对话", message: "在对话列表里左滑一个对话就能归档，它会收在这里。")
                }
            }
            .navigationTitle("已归档")
            #if os(iOS) || os(visionOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            #if os(macOS)
            .macSheetDone { dismiss() }
            #else
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") { dismiss() }.lineLimit(1)
                }
            }
            #endif
        }
        #if os(macOS)
        .macSheet()
        #endif
    }

    private func row(_ s: Session) -> some View {
        let title = s.title.isEmpty || s.title == "Claude Code" ? "新对话" : s.title
        return Button { open(s.key) } label: {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                if let t = ConversationTitle(title) {
                    Text("\(Text(t.head).foregroundStyle(Color.secondary))\(t.topic)")
                        .font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                } else {
                    Text(title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                }
                if let at = s.updatedAt {
                    Text(Fmt.relative(at)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            .padding(.vertical, Theme.Space.xs)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("archived-conversation")
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button { Task { await model.archiveConversation(s.key, false) } } label: {
                Label("取消归档", systemImage: Theme.Icon.unarchive)
            }
            .tint(Theme.accent)
        }
        .contextMenu {
            Button { Task { await model.archiveConversation(s.key, false) } } label: {
                Label("取消归档", systemImage: Theme.Icon.unarchive)
            }
        }
    }
}
