import SwiftUI

// Components of the ChatGPT-style shell (docs/ios.md「新外壳」). Neutral in every style (Theme.Shell): the shell is laid
// out like ChatGPT's, the styles still color the rest of the app.

/// The top switch: 助手 | Agents in a glass capsule, the selected one on a soft gray pill that glides across. Agents carries
/// the count of agents waiting on the user. Tapping 助手 while it is selected opens `chatMenu` (the conversation's
/// menu: 知识库, 详细信息, the launcher…), like tapping the title in ChatGPT; tapping Agents while it is selected opens
/// the list's options (分组, 显示, 机器…).
///
/// The switch reads nothing that the hub's pushes change: an open menu is laid out again whenever its button is, and
/// went back to its top under the finger choosing a machine (TestFlight 70, docs/lessons/ios.md). The count is
/// `WaitingBadge`, beside the menu rather than in its label, and reads the agents itself.
struct ShellSwitch<ChatMenu: View, WorkMenu: View>: View {
    @Binding var tab: AppTab
    @ViewBuilder var chatMenu: () -> ChatMenu
    @ViewBuilder var workMenu: () -> WorkMenu
    @Namespace private var pill

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("ShellSwitch")
        #endif
        HStack(spacing: .zero) {
            if tab == .assistant {
                Menu { chatMenu() } label: { segment("助手", selected: true) }
                    .menuStyle(.button)
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("shell-assistant")
                    .accessibilityHint("对话的菜单")
            } else {
                button("助手", .assistant, id: "shell-assistant")
            }
            ZStack(alignment: .topTrailing) {
                if tab == .chats {
                    Menu { workMenu() } label: { segment("Agents", selected: true) }
                        .menuStyle(.button)
                        .buttonStyle(.plain)
                        .accessibilityIdentifier("shell-agents")
                        .accessibilityHint("列表的选项")
                } else {
                    button("Agents", .chats, id: "shell-agents")
                }
                WaitingBadge()
            }
        }
        .padding(Theme.Space.xs)
        .surface(interactive: true, in: .capsule)
        .animation(Theme.Motion.state, value: tab)
    }

    private func button(_ title: String, _ value: AppTab, id: String) -> some View {
        Button {
            guard tab != value else { return }
            Haptics.tap()
            tab = value
        } label: {
            segment(title, selected: tab == value)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier(id)
        .accessibilityAddTraits(tab == value ? .isSelected : [])
    }

    private func segment(_ title: String, selected: Bool) -> some View {
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
            .contentShape(.capsule)
    }
}

/// How many agents wait on the user, over the switch's Agents. Its own view: the count changes with nearly every push
/// (an agent asks, another is answered), and only this badge is drawn again.
private struct WaitingBadge: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        let waiting = model.agentsWaiting
        if waiting > 0 {
            CountBadge(count: waiting, look: .solid(Theme.needsYou))
                .offset(x: Theme.Space.s, y: -Theme.Space.sm)
                .allowsHitTesting(false)
                .accessibilityLabel("\(waiting) 个在等你")
        }
    }
}

/// A round glass button of the shell's top bar (☰, 新对话): the same size as the system toolbar's, so the 助手 page's
/// floating bar and the Agents page's navigation bar line up. `inboxDot` marks the inbox having something for the user
/// (☰); the button reads it itself, so the bar around it is not drawn again with each inbox update.
struct ShellCircleButton: View {
    var symbol: String
    var label: String
    var id: String
    var inboxDot = false
    var action: () -> Void

    var body: some View {
        let dot = inboxDot && InboxStore.shared.actionCount > 0
        Button {
            Haptics.tap()
            action()
        } label: {
            Image(systemName: symbol)
                .font(Theme.Font.heading)
                .fontWeight(.medium) // a touch heavier and rounder, like ChatGPT's line icons
                .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
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

extension View {
    /// The shell's chrome a size up (docs/ios.md「新外壳」): one Dynamic Type step, about 12 %, so its text, icons, buttons
    /// and the composer grow together and keep their proportions, ChatGPT's roomier scale. The conversation's messages
    /// keep the reading size. 小 / 标准 / 大 and the system's size still apply under it.
    func shellScaled() -> some View { modifier(ShellScaled()) }
}

private struct ShellScaled: ViewModifier {
    @Environment(\.dynamicTypeSize) private var size

    func body(content: Content) -> some View {
        let all = DynamicTypeSize.allCases
        let i = all.firstIndex(of: size) ?? 0
        content.dynamicTypeSize(all[min(i + 1, all.count - 1)])
    }
}

extension ShellSwitch where WorkMenu == EmptyView {
    init(tab: Binding<AppTab>, @ViewBuilder chatMenu: @escaping () -> ChatMenu) {
        self.init(tab: tab, chatMenu: chatMenu, workMenu: { EmptyView() })
    }
}
