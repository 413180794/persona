import BeautifulUI
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
    /// The strip under the switch: its pill follows the pages while they slide (`ShellPager`).
    @Environment(\.shell) private var shell
    @Environment(\.buiTheme) private var theme

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("ShellSwitch")
        #endif
        if AppStyle.current.usesBeautifulUI {
            // BeautifulUI's pill segmented control: a field-colored track, the chosen segment a raised capsule.
            row
                .padding(Theme.Space.xs)
                .background(theme.colors.field, in: .capsule)
                .overlay(Capsule().strokeBorder(theme.colors.line, lineWidth: Theme.Size.hairline))
                .animation(Theme.Motion.state, value: tab)
        } else {
            row
                .padding(Theme.Space.xs)
                .surface(interactive: true, in: .capsule)
                .animation(Theme.Motion.state, value: tab)
        }
    }

    private var row: some View {
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
        .background(alignment: .leading) {
            if let pager = shell?.pager { PagerPill(pager: pager) }
        }
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

    @ViewBuilder private func segment(_ title: String, selected: Bool) -> some View {
        if AppStyle.current.usesBeautifulUI { beautifulSegment(title, selected: selected) } else { plainSegment(title, selected: selected) }
    }

    private func beautifulSegment(_ title: String, selected: Bool) -> some View {
        Text(title)
            .buiFont(BUITextStyle(size: 13, weight: selected ? .semibold : .medium, lineHeight: 1.19))
            .foregroundStyle(selected ? theme.colors.ink : theme.colors.ink3)
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .scaledFrame(width: Theme.Shell.segmentWidth, height: Theme.Shell.switchHeight - Theme.Space.s)
            .background {
                if selected, shell == nil {
                    Color.clear.buiSurface(Capsule(), fill: theme.colors.surface, elevation: .button)
                        .matchedGeometryEffect(id: "pill", in: pill)
                }
            }
            .contentShape(.capsule)
    }

    private func plainSegment(_ title: String, selected: Bool) -> some View {
        Text(title)
            .font(selected ? Theme.Font.bodyStrong : Theme.Font.body)
            .foregroundStyle(selected ? Color.primary : Color.secondary)
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .scaledFrame(width: Theme.Shell.segmentWidth, height: Theme.Shell.switchHeight - Theme.Space.s)
            .background {
                if selected, shell == nil {
                    Capsule().fill(Theme.Shell.segment).matchedGeometryEffect(id: "pill", in: pill)
                }
            }
            .contentShape(.capsule)
    }
}

/// The switch's pill in the shell: under 助手 or Agents as the strip's position says, gliding with the finger. Its own
/// view: only it reads the position, the switch (and its menus) are not drawn again on every frame of a drag.
private struct PagerPill: View {
    let pager: ShellPager
    @Environment(\.buiTheme) private var theme

    var body: some View {
        GeometryReader { g in
            let half = g.size.width / 2
            if AppStyle.current.usesBeautifulUI {
                // BeautifulUI's segmented control: the chosen segment is a raised capsule.
                Color.clear.buiSurface(Capsule(), fill: theme.colors.surface, elevation: .button)
                    .frame(width: half, height: g.size.height)
                    .offset(x: min(1, max(0, pager.position)) * half)
            } else {
                Capsule().fill(Theme.Shell.segment)
                    .frame(width: half, height: g.size.height)
                    .offset(x: min(1, max(0, pager.position)) * half)
            }
        }
    }
}

/// How many agents wait on the user, over the switch's Agents. Its own view: the count changes with nearly every push
/// (an agent asks, another is answered), and only this badge is drawn again.
private struct WaitingBadge: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        let waiting = model.agentsWaiting
        if waiting > 0, AppStyle.current.usesBeautifulUI {
            BUIPill(waiting > 99 ? "99+" : "\(waiting)", tone: .custom(ink: .white, fill: Theme.needsYou))
                .offset(x: Theme.Space.s, y: -Theme.Space.sm)
                .allowsHitTesting(false)
                .accessibilityLabel("\(waiting) 个在等你")
        } else if waiting > 0 {
            CountBadge(count: waiting, look: .solid(Theme.needsYou))
                .offset(x: Theme.Space.s, y: -Theme.Space.sm)
                .allowsHitTesting(false)
                .accessibilityLabel("\(waiting) 个在等你")
        }
    }
}

/// A round glass button of the shell's top bar (新对话, 新建会话): the same size as the system toolbar's, so the 助手
/// page's floating bar and the Agents page's navigation bar line up.
struct ShellCircleButton: View {
    var symbol: String
    var label: String
    var id: String
    var action: () -> Void
    @Environment(\.buiTheme) private var theme

    var body: some View {
        Button {
            Haptics.tap()
            action()
        } label: {
            Image(systemName: symbol)
                .font(Theme.Font.heading)
                .fontWeight(.medium) // a touch heavier and rounder, like ChatGPT's line icons
                .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
                .foregroundStyle(AppStyle.current.usesBeautifulUI ? theme.colors.ink : Color.primary)
                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .modifier(CircleSurface())
        .accessibilityLabel(label)
        .accessibilityIdentifier(id)
    }
}

/// The top bar's left button on both pages (2026-11-01 user request, in place of ☰): the persona's face in Persona's
/// state, as the old layout's bar shows it in the middle (`PersonaDot`, docs/persona.md「状态」), opening the drawer
/// (历史对话, 收件箱…). The state is that of Persona's conversation on 助手 (`assistantCurrent`), on Agents too, so the
/// face stays the same while the strip slides. A tray mark sits on the face's lower corner while the inbox has
/// something for the user: a tray, not a dot, so it does not read as the face's state, and just outside the frame's
/// corner, where it clears the round face and its rings (needs-you, working) with a gap, and the corners the packages
/// draw in (the thought bubble and the z are top right). The button reads all of it itself: the bars around it are not drawn again with each push (#178).
struct ShellPersonaButton: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dynamicTypeSize) private var typeSize
    var id: String
    var action: () -> Void

    var body: some View {
        let key = model.assistantCurrent
        let state = model.personaState(key, machineID: model.assistantMachineID(key))
        let inbox = InboxStore.shared.actionCount > 0
        // As big as the bar's round buttons (ShellCircleButton's frame): the tap target, growing with the text size.
        let k = Theme.sizeScale(typeSize)
        let mark = Theme.Size.badge * k
        Button {
            Haptics.tap()
            action()
        } label: {
            PersonaDot(size: Theme.Size.tapTarget * k, state: state)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .overlay(alignment: .bottomTrailing) {
            if inbox {
                InboxMark(size: mark)
                    .offset(x: mark / 2 + Theme.Space.xxs, y: mark / 2 + Theme.Space.xxs)
            }
        }
        .accessibilityLabel("历史对话")
        .accessibilityValue([model.persona.name + "，" + state.title, inbox ? "收件箱有事等你" : nil]
            .compactMap { $0 }.joined(separator: "，"))
        .accessibilityHint("打开侧边栏")
        .accessibilityIdentifier(id)
    }
}

/// The inbox's mark on the persona's face: a tray on the 需要你 red, ringed in the page's color to stand apart from the face.
private struct InboxMark: View {
    var size: CGFloat

    var body: some View {
        Image(systemName: Theme.Icon.inbox)
            .font(Theme.Font.labelHeavy)
            .imageScale(.small)
            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
            .foregroundStyle(.white)
            .frame(width: size, height: size)
            .background(Theme.needsYou, in: .circle)
            .overlay(Circle().stroke(Theme.pageBackground, lineWidth: Theme.Size.lineMedium))
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }
}

/// A round button's surface: glass, or in the BeautifulUI style its raised white circle.
private struct CircleSurface: ViewModifier {
    func body(content: Content) -> some View {
        if AppStyle.current.usesBeautifulUI {
            content.buiSurface(Circle(), elevation: .button)
        } else {
            content.surface(interactive: true, in: .circle)
        }
    }
}

extension View {
    /// The shell's chrome a size up (docs/ios.md「新外壳」): one Dynamic Type step, about 12 %, so its text, icons, buttons
    /// and the composer grow together and keep their proportions, ChatGPT's roomier scale. The conversation's messages
    /// keep the reading size. 小 / 标准 / 大 and the system's size still apply under it.
    func shellScaled() -> some View { modifier(ShellScaled()) }

    /// A top bar of the shell: a size up like the rest of its chrome, but no larger than
    /// `Theme.Shell.topBarLargestTypeSize`, so it stays clear of Agents' search field at the accessibility sizes.
    func shellTopBarScaled() -> some View { dynamicTypeSize(...Theme.Shell.topBarLargestTypeSize).shellScaled() }
}

/// Room for the shell's floating top bar (`AgentsTopBar`) on a page that keeps the system navigation bar for its search
/// field (Agents): a blank prompt line on the page's own navigation item, which makes that page's navigation bar taller
/// by the line, so the search field the bar holds and the list under it start below the top bar. While searching the
/// line goes, and the field rises into the top row.
///
/// The room is the page's, not the navigation container's: it used to be padding around the container, taken away for
/// a chat pushed on the list and given back on the way back, and UIKit does not lay out its navigation bar again in the
/// middle of a push or pop. Coming back, the bar sat where it had been for the chat (52 pt low), the list's inset with
/// it, which unfolded the search field, and everything sprang up once the pop was over (#340). A page's own navigation
/// item is carried through the transition by UIKit itself; the container keeps its frame and its safe area (which the
/// shell's status row changes, ShellStatus).
///
/// The line's height is UIKit's, the same at every text size; the top bar stops growing at
/// `Theme.Shell.topBarLargestTypeSize` to stay within it (`needed(at:)`, ShellNavigationClearanceTests).
struct ShellNavigationClearance: View {
    var active: Bool

    /// How far below the status bar the search field must start at this text size: the top bar's height (its buttons
    /// a size up, no larger than the top bars' largest; the inbox mark hanging off the face; the bar's top padding) and
    /// a step of space. The navigation bar with the prompt line must be at least this tall.
    static func needed(at size: DynamicTypeSize) -> CGFloat {
        let all = DynamicTypeSize.allCases
        let up = all[min((all.firstIndex(of: size) ?? 0) + 1, all.count - 1)]
        let k = Theme.sizeScale(min(up, Theme.Shell.topBarLargestTypeSize))
        return Theme.Size.tapTarget * k + Theme.Size.badge * k / 2 + Theme.Space.xxs * 2 + Theme.Space.s
    }

    /// The blank prompt line.
    static let line = " "

    var body: some View {
        #if os(iOS)
        PromptLine(active: active)
        #endif
    }
}

#if os(iOS)
private struct PromptLine: UIViewControllerRepresentable {
    var active: Bool

    func makeUIViewController(context: Context) -> Line { Line() }
    func updateUIViewController(_ line: Line, context: Context) { line.active = active }

    /// A child of the page's controller (the representable's), which owns the navigation item.
    final class Line: UIViewController {
        var active = false { didSet { apply() } }

        override func loadView() {
            view = UIView()
            view.isUserInteractionEnabled = false
        }

        override func didMove(toParent parent: UIViewController?) {
            super.didMove(toParent: parent)
            apply()
        }

        private func apply() {
            guard let item = parent?.navigationItem else { return }
            let prompt = active ? ShellNavigationClearance.line : nil
            if item.prompt != prompt { item.prompt = prompt }
        }
    }
}
#endif

/// The text size the app settled on (the system's, moved by 小 / 标准 / 大), set once at the root. `shellScaled` goes one
/// step up from this, not from the environment's own size: that one is not always what the page has. The drawer's
/// sidebar lives in a hosting controller whose environment follows its UIKit trait collection, which takes up the
/// step set inside it, so reading the environment there added a step on every refresh (收件箱's row 59 → 78 → 80 pt).
private struct ShellBaseTypeSizeKey: EnvironmentKey {
    static let defaultValue: DynamicTypeSize? = nil
}

extension EnvironmentValues {
    var shellBaseTypeSize: DynamicTypeSize? {
        get { self[ShellBaseTypeSizeKey.self] }
        set { self[ShellBaseTypeSizeKey.self] = newValue }
    }
}

private struct ShellScaled: ViewModifier {
    @Environment(\.dynamicTypeSize) private var size
    @Environment(\.shellBaseTypeSize) private var base

    func body(content: Content) -> some View {
        let all = DynamicTypeSize.allCases
        let i = all.firstIndex(of: base ?? size) ?? 0
        content.dynamicTypeSize(all[min(i + 1, all.count - 1)])
    }
}

extension ShellSwitch where WorkMenu == EmptyView {
    init(tab: Binding<AppTab>, @ViewBuilder chatMenu: @escaping () -> ChatMenu) {
        self.init(tab: tab, chatMenu: chatMenu, workMenu: { EmptyView() })
    }
}
