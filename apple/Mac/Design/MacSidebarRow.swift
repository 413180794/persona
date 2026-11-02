import SwiftUI

/// A row of a Mac side column (the shell's sections, the inbox's filters): the row `AssistantSidebar` draws for
/// 新对话 / 定时任务 / 知识库 (an icon in a fixed box, body type), selected with the same rounded well its current
/// conversation has, a lighter one under the pointer. Drawn here rather than by a sidebar `List`: the system's rows
/// turned their words light gray whenever the window was not the active one, and selected in the accent beside the
/// shell's gray well (2026-11-02 user: 收件箱中间那栏文字看不太清).
struct MacSidebarRow: View {
    enum Badge: Equatable {
        /// Things waiting for the user (the tab bar's red number).
        case count(Int)
        /// A quiet word or number in place of the red one (设置 in 体验模式, a filter's count).
        case word(String)
    }

    var icon: String
    var title: String
    var selected: Bool
    var badge: Badge?
    var identifier: String
    var action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: Theme.Space.l) {
                // The title says it (VoiceOver read the symbol's name too).
                Image(systemName: icon).resizable().scaledToFit().foregroundStyle(Color.primary)
                    .scaledFrame(width: Theme.Size.iconS, height: Theme.Size.iconS)
                    .scaledFrame(width: Theme.Size.icon)
                    .accessibilityHidden(true)
                // One small gap between the title and its count (the outer spacing on each side of the spacer was
                // 32 pt): a mail account's address in the inbox's filters keeps more of itself.
                HStack(spacing: 0) {
                    Text(title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                    Spacer(minLength: Theme.Space.s)
                    switch badge {
                    case .count(let n): CountBadge(count: n, look: .solid(Theme.needsYou))
                    case .word(let w): Text(w).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                    case nil: EmptyView()
                    }
                }
            }
            .padding(.horizontal, Theme.Space.m)
            .scaledFrame(minHeight: Theme.Size.avatarL)
            .background {
                if selected || hovering {
                    RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
                        .fill(selected ? Theme.Fill.well : Theme.Fill.macHover)
                }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityIdentifier(identifier)
    }
}

/// A group's name over a side column's rows (收件箱 / 类型), as `AssistantSidebar` heads its days.
struct MacSidebarHeading: View {
    var title: String

    var body: some View {
        Text(title).font(Theme.Font.caption).foregroundStyle(Color.secondary).lineLimit(1)
            .accessibilityAddTraits(.isHeader)
            .padding(.leading, Theme.Space.m)
            .padding(.top, Theme.Space.l)
            .padding(.bottom, Theme.Space.xs)
    }
}
