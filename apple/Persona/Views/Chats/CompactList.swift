import SwiftUI

/// 细列表's one scale (docs/ios.md「Agents 列表」): every row in it — an agent, 还有 N 个空闲, 收起, a quiet space —
/// is one line of the same height with its mark (state dot, fold chevron, space icon) in one column, so the titles
/// start at one edge; three levels of type: section titles, row titles, meta. All Dynamic Type levels, so the three
/// text sizes (小 / 标准 / 大) keep the ratios; read each time, since the Mac's move with `Theme.Font.scale`. The cards (卡片) do not use it.
enum CompactList {
    /// A space's name, and the list's other section titles (需要你处理, 空闲的空间, a state, a machine).
    static var sectionTitle: Font { Theme.Font.heading }
    /// What a row is: an agent's title, a quiet space's name, 还有 N 个空闲.
    static var title: Font { Theme.Font.secondary }
    /// A finished-unseen agent's title.
    static var titleUnread: Font { Theme.Font.secondaryStrong }
    /// Where and when, the tab names behind 还有 N 个空闲, a header's counts.
    static var meta: Font { Theme.Font.caption }
    static var metaStrong: Font { Theme.Font.captionMedium }
    /// The column the marks sit in (`scaledFrame`: it grows with the text).
    static let mark = Theme.Size.markM
    /// Between the mark and the title, and between the things on a line.
    static let gap = Theme.Space.s
    /// Every row: the same height whatever it holds.
    static let rowInsets = EdgeInsets(top: Theme.Space.s, leading: Theme.Space.l, bottom: Theme.Space.s, trailing: Theme.Space.m)
    /// A row a level in, under a working tree's row (按仓库).
    static let nestedRowInsets = EdgeInsets(top: Theme.Space.s, leading: Theme.Space.l + Theme.Space.l, bottom: Theme.Space.s, trailing: Theme.Space.m)
}

extension EnvironmentValues {
    /// The Agents list shows 细列表: headers and fold rows take `CompactList`'s scale.
    @Entry var compactList = false
}

/// 细列表: the idle agents of a busy space behind one line, the size of an agent's — their tab names as words, not chips.
struct CompactFoldRow: View {
    var sessions: [Session]

    var body: some View {
        HStack(spacing: CompactList.gap) {
            Image(systemName: Theme.Icon.expand)
                .font(CompactList.metaStrong)
                .foregroundStyle(.secondary)
                .scaledFrame(width: CompactList.mark)
            Text("还有 \(sessions.count) 个空闲")
                .font(CompactList.title)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .fixedSize()
            Text(sessions.prefix(6).map { $0.slotLabel ?? $0.displayTitle }.joined(separator: " · "))
                .font(CompactList.meta)
                .foregroundStyle(.tertiary)
                .lineLimit(1)
            Spacer(minLength: 0)
        }
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("展开 \(sessions.count) 个空闲的 agent")
    }
}

/// 细列表: 收起 under the idle agents it opened, laid out like the line that opened them.
struct CompactCollapseRow: View {
    var title: String

    var body: some View {
        HStack(spacing: CompactList.gap) {
            Image(systemName: Theme.Icon.collapse)
                .font(CompactList.metaStrong)
                .scaledFrame(width: CompactList.mark)
            Text(title).font(CompactList.title).lineLimit(1)
            Spacer(minLength: 0)
        }
        .foregroundStyle(Theme.accent)
        .contentShape(.rect)
    }
}

/// 细列表: a space where every agent is idle, one line like an agent's: its name, how many tabs, when; tap to list them.
struct CompactQuietSpaceRow: View {
    var group: SpaceGroup
    var machine: Machine?
    var showMachine: Bool
    var expanded: Bool
    /// A repository (按仓库's 空闲的仓库), not a space: its folder, and its agents counted rather than its tabs.
    var repo = false
    @Environment(AgentsFeed.self) private var feed: AgentsFeed?

    var body: some View {
        let at = Fmt.relative(group.updatedAt(feed))
        HStack(spacing: CompactList.gap) {
            Image(systemName: repo ? Theme.Icon.folder : Theme.Icon.space)
                .font(CompactList.meta)
                .foregroundStyle(.secondary)
                .scaledFrame(width: CompactList.mark)
            HStack(alignment: .firstTextBaseline, spacing: CompactList.gap) {
                Text(group.name).font(CompactList.title).lineLimit(1).layoutPriority(1)
                Text([showMachine ? machine?.name : nil, count].compactMap { $0 }.joined(separator: " · "))
                    .font(CompactList.meta).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: Theme.Space.xs)
            Text(at).font(CompactList.meta.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
                .fixedSize()
            FoldChevron(folded: !expanded)
        }
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([group.name, expanded ? "已展开" : "已折叠", showMachine ? machine?.name ?? "" : "",
                             count, at].filter { !$0.isEmpty }.joined(separator: "，"))
        .accessibilityAddTraits(.isButton)
    }

    private var count: String { "\(group.sessions.count) 个" + (repo ? " Agent" : "标签") }
}

/// 细列表: something waiting for you (a plan, a question, a permission) as one row like an agent's: its kind's mark in
/// 需要你 red, what it is, which agent and machine, when. Tapping opens the chat, which answers it.
struct CompactInteractionRow: View {
    var interaction: Interaction
    var machine: String?

    var body: some View {
        HStack(spacing: CompactList.gap) {
            Image(systemName: interaction.kind.symbol)
                .font(CompactList.metaStrong)
                .foregroundStyle(StatusPalette.needsYou.dot)
                .scaledFrame(width: CompactList.mark)
            HStack(alignment: .firstTextBaseline, spacing: CompactList.gap) {
                Text(interaction.title).font(CompactList.titleUnread).lineLimit(1).fixedSize()
                Text([interaction.sessionTitle, machine].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(CompactList.meta).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: Theme.Space.xs)
            TimelineView(.periodic(from: .now, by: 30)) { ctx in
                Text(Fmt.relative(interaction.createdAt, now: ctx.date)).font(CompactList.meta.monospacedDigit())
                    .foregroundStyle(.secondary).lineLimit(1)
            }
            .fixedSize()
        }
        .accessibilityElement(children: .combine)
    }
}
