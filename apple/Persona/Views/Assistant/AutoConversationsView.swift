import PersonaKit
import SwiftUI

/// Which 全部对话 the sidebar opens: filtered to one source of 自动对话 (`AutoConversationSource.id`), or all (nil).
struct AutoConversationsRoute: Identifiable, Hashable {
    var source: String?
    var id: String { source ?? "" }
}

/// 全部对话 (#3264, docs/protocol.md「侧边栏」): the assistant's conversations, newest first, filtered along the top by
/// who opened them: 全部, 我的 (the user's own), then each source of 自动对话 that has any. The sidebar's 自动对话 opens
/// it on a source. A tap opens one; a swipe or a long press archives it, as in the sidebar.
struct AutoConversationsView: View {
    /// What the list shows: every conversation, the user's own, or one source's.
    enum Filter: Hashable {
        case all, mine
        case source(String)

        var id: String {
            switch self {
            case .all: "all"
            case .mine: "mine"
            case .source(let id): id
            }
        }
    }

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var filter: Filter
    private let taskChats = AssistantTaskChatsStore.shared
    var open: (SessionKey) -> Void

    init(source: String?, open: @escaping (SessionKey) -> Void) {
        _filter = State(initialValue: source.map(Filter.source) ?? .all)
        self.open = open
    }

    var body: some View {
        let everyone = model.assistantConversations.filter { $0.key != model.assistantMain }
        let split = AutoConversations(everyone, tasks: taskChats.known, current: nil,
                                      hubSays: model.hubFeatures.contains(AutoConversationSource.feature))
        let groups = split.bySource(tasks: taskChats.known)
        let shown = AssistantSidebarGroups.newestFirst(Self.list(filter, all: everyone, mine: split.mine, groups: groups))
        NavigationStack {
            List {
                filters(groups, all: everyone.count, mine: split.mine.count)
                    .themedRow()
                    .listRowSeparator(.hidden)
                ForEach(shown) { s in row(s).themedRow() }
            }
            .listStyle(.plain)
            .themedPage()
            .overlay {
                if shown.isEmpty {
                    EmptyStateView(symbol: Theme.Icon.autoConversations, title: "这里没有对话",
                                   message: "收件箱的 AI 分身、定时任务、飞书 /agent 开的对话会按来源收在这里。")
                }
            }
            .navigationTitle("全部对话")
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

    private static func list(_ filter: Filter, all: [Session], mine: [Session], groups: [AutoConversations.Group]) -> [Session] {
        switch filter {
        case .all: all
        case .mine: mine
        case .source(let id): groups.first { $0.source.id == id }?.sessions ?? []
        }
    }

    /// 全部, 我的 and each source, the chosen one tinted.
    private func filters(_ groups: [AutoConversations.Group], all: Int, mine: Int) -> some View {
        FlowLayoutRow {
            chip("全部", symbol: Theme.Icon.list, count: all, filter: .all)
            chip("我的", symbol: Theme.Icon.user, count: mine, filter: .mine)
            ForEach(groups, id: \.source) { g in chip(g.source.name, symbol: g.source.symbol, count: g.sessions.count, filter: .source(g.source.id)) }
        }
        .padding(.vertical, Theme.Space.xs)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func chip(_ name: String, symbol: String, count: Int, filter f: Filter) -> some View {
        let chosen = filter == f
        return Button {
            withAnimation(Theme.Motion.state) { filter = f }
        } label: {
            Chip(text: "\(name) \(count)", symbol: symbol, look: chosen ? .tint(Theme.accent) : .neutral)
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(name)，\(count) 个")
        .accessibilityAddTraits(chosen ? .isSelected : [])
        .accessibilityIdentifier("auto-filter-" + f.id)
    }

    private func row(_ s: Session) -> some View {
        let title = s.title.isEmpty || s.title == "Claude Code" ? "新对话" : s.title
        let from = AutoConversations.source(of: s, tasks: taskChats.known)
        let detail = [filter == .all ? from?.name : nil, [.working, .needsYou, .unread].contains(s.state) ? s.state.label : s.updatedAt.map { Fmt.relative($0) }]
            .compactMap { $0 }.joined(separator: " · ")
        return Button { open(s.key) } label: {
            HStack(spacing: Theme.Space.m) {
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    if let t = ConversationTitle(title) {
                        Text("\(Text(t.head).foregroundStyle(Color.secondary))\(t.topic)")
                            .font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                    } else {
                        Text(title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                    }
                    if !detail.isEmpty {
                        Text(detail).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                Spacer(minLength: Theme.Space.xs)
                mark(s)
            }
            .padding(.vertical, Theme.Space.xs)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel([title, detail.isEmpty ? nil : detail].compactMap { $0 }.joined(separator: "，"))
        .accessibilityIdentifier("auto-conversation")
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button { Task { await model.archiveConversation(s.key, true) } } label: {
                Label("归档", systemImage: Theme.Icon.archive)
            }
            .tint(Theme.accent)
        }
        .contextMenu {
            Button { Task { await model.archiveConversation(s.key, true) } } label: {
                Label("归档", systemImage: Theme.Icon.archive)
            }
        }
    }

    /// Waiting for you, working or unread, as the sidebar's rows mark it.
    @ViewBuilder private func mark(_ s: Session) -> some View {
        switch s.state {
        case .working: ProgressView().controlSize(.mini)
        case .needsYou: Circle().fill(Theme.needsYou).scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot)
        case .unread: Circle().fill(Theme.accent).scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot)
        default: EmptyView()
        }
    }
}
