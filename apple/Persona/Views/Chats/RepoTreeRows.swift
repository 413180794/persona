import SwiftUI

/// A machine's heading where the Agents list is split by machine (按仓库, 按项目 with agents on more than one machine,
/// #2108): its mark in a tile and its name a level above the headers under it, its agents' state counts on the right.
struct HostHeading: View {
    var symbol: String
    var title: String
    var group: SpaceGroup
    @Environment(\.compactList) private var compact

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Image(systemName: symbol)
                .font(Theme.Font.secondaryStrong)
                .foregroundStyle(Color.primary)
                .scaledFrame(width: Theme.Size.wellXS, height: Theme.Size.wellXS)
                .background(Theme.Fill.chip, in: .rect(cornerRadius: Theme.Radius.badge))
            // 细列表's spaces and repositories are titled at the heading's size: the machine goes a level up.
            Text(title).font(compact ? Theme.Font.title : Theme.Font.heading).foregroundStyle(Color.primary).textCase(nil).lineLimit(1)
            Spacer(minLength: Theme.Space.xs)
            StateCounts(group: group)
        }
    }
}

/// A working tree's row in its repository's card (按仓库): ⌂ for the main checkout, the branch mark for the others, how
/// many agents (空闲 when nothing goes on), and the chevron that turns as it opens.
struct WorktreeRow: View {
    var name: String
    var main: Bool
    var count: Int
    var quiet: Bool
    var open: Bool
    @Environment(\.compactList) private var compact

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Image(systemName: main ? Theme.Icon.home : Theme.Icon.gitBranch)
                .font(compact ? CompactList.meta : Theme.Font.caption)
                .foregroundStyle(.secondary)
                .scaledFrame(width: compact ? CompactList.mark : Theme.Size.iconXS)
            Text(name)
                .font(compact ? CompactList.metaStrong : Theme.Font.secondaryMedium)
                .foregroundStyle(quiet ? Color.secondary : Color.primary)
                .lineLimit(1)
            Spacer(minLength: Theme.Space.xs)
            Text(quiet ? "空闲" : "\(count) 个 Agent")
                .font(compact ? CompactList.meta : Theme.Font.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .fixedSize()
            FoldChevron(folded: !open)
        }
        .padding(.vertical, compact ? 0 : Theme.Space.xxs)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([main ? "主工作树" : "工作树", name, quiet ? "空闲" : "\(count) 个 Agent", open ? "已展开" : "已折叠"]
            .joined(separator: "，"))
        .accessibilityAddTraits(.isButton)
    }
}

/// The「N 个子项」row under a working tree (按仓库): the spaces its host nests under it (Orca's child worktrees, #3075),
/// their agents' state counts, and the chevron that turns as it opens.
struct ChildSpacesRow: View {
    var count: Int
    var open: Bool
    /// Everything nested under it, for the counts.
    var group: SpaceGroup
    @Environment(\.compactList) private var compact

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Image(systemName: Theme.Icon.childSpaces)
                .font(compact ? CompactList.meta : Theme.Font.caption)
                .foregroundStyle(.secondary)
                .scaledFrame(width: compact ? CompactList.mark : Theme.Size.iconXS)
            Text("\(count) 个子项")
                .font(compact ? CompactList.metaStrong : Theme.Font.secondaryMedium)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .fixedSize()
            Spacer(minLength: Theme.Space.xs)
            StateCounts(group: group)
            FoldChevron(folded: !open)
        }
        .padding(.vertical, compact ? 0 : Theme.Space.xxs)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(["\(count) 个子项", StateCounts.spoken(group), open ? "已展开" : "已折叠"].filter { !$0.isEmpty }
            .joined(separator: "，"))
        .accessibilityAddTraits(.isButton)
    }
}

/// The ⋯ of a repository whose agents are in more than one space (按仓库): what applies to all of them. Equal while
/// its unread agents are: the menu held open is not rebuilt by every push.
struct RepoMenuItems: View, Equatable {
    @Environment(AppModel.self) private var model
    nonisolated let unread: [SessionKey]

    nonisolated static func == (a: Self, b: Self) -> Bool { a.unread == b.unread }

    var body: some View {
        if !unread.isEmpty {
            Button { model.markSeen(unread) } label: { Label("全部标为已读（\(unread.count)）", systemImage: Theme.Icon.success) }
        }
    }
}
