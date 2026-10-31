import SwiftUI

// Components of the ChatGPT-style shell (docs/ios.md「新外壳」). Neutral in every style (Theme.Shell): the shell is laid
// out like ChatGPT's, the styles still color the rest of the app.

/// The top switch: 聊天 | 工作 in a glass capsule, the selected one on a soft gray pill that glides across. 工作 carries
/// the count of agents waiting on the user. Tapping 聊天 while it is selected opens `chatMenu` (the conversation's
/// menu: 知识库, 详细信息, the launcher…), like tapping the title in ChatGPT.
struct ShellSwitch<ChatMenu: View>: View {
    @Binding var tab: AppTab
    var waiting: Int
    @ViewBuilder var chatMenu: () -> ChatMenu
    @Namespace private var pill

    var body: some View {
        HStack(spacing: .zero) {
            if tab == .assistant {
                Menu { chatMenu() } label: { segment("聊天", selected: true) }
                    .menuStyle(.button)
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("shell-chat")
                    .accessibilityHint("对话的菜单")
            } else {
                button("聊天", .assistant, id: "shell-chat")
            }
            button("工作", .chats, id: "shell-work", badge: waiting)
        }
        .padding(Theme.Space.xs)
        .surface(interactive: true, in: .capsule)
        .animation(Theme.Motion.state, value: tab)
    }

    private func button(_ title: String, _ value: AppTab, id: String, badge: Int = 0) -> some View {
        Button {
            guard tab != value else { return }
            Haptics.tap()
            tab = value
        } label: {
            segment(title, selected: tab == value, badge: badge)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier(id)
        .accessibilityAddTraits(tab == value ? .isSelected : [])
        .accessibilityValue(badge > 0 ? "\(badge) 个在等你" : "")
    }

    private func segment(_ title: String, selected: Bool, badge: Int = 0) -> some View {
        Text(title)
            .font(selected ? Theme.Font.bodyStrong : Theme.Font.body)
            .foregroundStyle(selected ? Color.primary : Color.secondary)
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .scaledFrame(width: Theme.Shell.segmentWidth, height: Theme.Shell.switchHeight - Theme.Space.s)
            .background {
                if selected {
                    Capsule().fill(Theme.Shell.segment).matchedGeometryEffect(id: "pill", in: pill)
                }
            }
            .overlay(alignment: .topTrailing) {
                if badge > 0 {
                    CountBadge(count: badge, look: .solid(Theme.needsYou))
                        .offset(x: Theme.Space.s, y: -Theme.Space.sm)
                        .allowsHitTesting(false)
                }
            }
            .contentShape(.capsule)
    }
}

/// A round glass button of the shell's top bar (☰, 新对话): the same size as the system toolbar's, so the 聊天 page's
/// floating bar and the 工作 page's navigation bar line up. `dot` marks something waiting behind it (☰: the inbox).
struct ShellCircleButton: View {
    var symbol: String
    var label: String
    var id: String
    var dot = false
    var action: () -> Void

    var body: some View {
        Button {
            Haptics.tap()
            action()
        } label: {
            Image(systemName: symbol)
                .font(Theme.Font.heading)
                .dynamicTypeSize(...DynamicTypeSize.xxLarge)
                .foregroundStyle(Color.primary)
                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .surface(interactive: true, in: .circle)
        .overlay(alignment: .topTrailing) {
            if dot {
                Circle().fill(Theme.needsYou)
                    .scaledFrame(width: Theme.Size.dotXL, height: Theme.Size.dotXL)
                    .overlay(Circle().stroke(Theme.pageBackground, lineWidth: Theme.Size.lineMedium))
                    .offset(x: -Theme.Space.xxs, y: Theme.Space.xxs)
                    .allowsHitTesting(false)
            }
        }
        .accessibilityLabel(label)
        .accessibilityValue(dot ? "有事等你" : "")
        .accessibilityIdentifier(id)
    }
}
