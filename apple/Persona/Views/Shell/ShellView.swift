import BeautifulUI
import SwiftUI

#if os(iOS)
/// The iPhone's ChatGPT-style shell (docs/ios.md「新外壳」): no tab bar. 助手 (the Persona assistant) and Agents (the Agents
/// list) are two pages under the top switch, both kept alive like tabs (switching keeps each one's place); the drawer
/// slides both aside; 收件箱 / 定时任务 / 知识库 / 机器 push onto 助手; 设置 is a sheet.
///
/// Every route in the app still goes through `AppModel.selectedTab` (notifications, Siri, 交给 Persona, the inbox's
/// rows…): `.assistant` / `.chats` pick the page, `.mail` opens the inbox on 助手, `.settings` the sheet.
struct ShellView: View {
    @Environment(AppModel.self) private var model
    /// Opens on the app's tab: the page behind is built when it is first selected or dragged in.
    @State private var shell = ShellState(page: ShellState.index(for: AppModel.shared.selectedTab),
                                          pageCount: LarkHomePreference.shared.enabled ? 3 : 2)

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("ShellView")
        #endif
        @Bindable var model = model
        AssistantDrawerLayout(drawer: shell.drawer, enabled: shell.atRoot,
                              backdrop: Theme.Shell.drawer, pager: shell.pager, sidebar: { sidebar }, page: pages)
            .environment(\.shell, shell)
            .sheet(isPresented: $model.shellSettings) {
                // The sheet outlives RootView's rebuild for a new look: it takes the look as its identity too, or 外观's
                // sample and everything else drawn with the static tokens keeps the old colors.
                SettingsView()
                    .environment(\.shell, shell)
                    .id(model.look)
            }
            .onChange(of: LarkHomePreference.shared.enabled, initial: true) { _, enabled in
                shell.setPageCount(enabled ? 3 : 2)
            }
            // The shell takes the app's tab as it is set (`ShellState.route`): not an onChange here, which was skipped now
            // and then (docs/shell.md).
            .onAppear {
                shell.attach(to: model)
                #if DEBUG
                // `-showToasts "<at>:<info|success|error>:<words>;…"`: toasts at those seconds after launch (UI tests of the
                // status row: one after another, over the link's state).
                for part in (UserDefaults.standard.string(forKey: "showToasts") ?? "").split(separator: ";") {
                    let f = part.split(separator: ":", maxSplits: 2).map(String.init)
                    guard f.count == 3, let at = Double(f[0]) else { continue }
                    let style: Toast.Style = f[1] == "error" ? .error : f[1] == "success" ? .success : .info
                    DispatchQueue.main.asyncAfter(deadline: .now() + at) { model.toast = Toast(f[2], style: style) }
                }
                let at = UserDefaults.standard.double(forKey: "showShellPagerAt")
                if at > 0 {
                    shell.select(1, animated: false)
                    DispatchQueue.main.asyncAfter(deadline: .now() + 1) { shell.pager.hold(at: at) }
                }
                #endif
            }
            .onDisappear { shell.detach(from: model) }
            // 等待网络 / 连接中… / 更新中… and the toasts: one row under the status bar that takes its room, the pages and
            // their bars making way (docs/shell.md「状态行」); it opens and folds once no finger moves the screen.
            .overlay { ShellStatusRow(notices: shell.notices) }
            .background { ShellWindowHooks(notices: shell.notices, pager: shell.pager) }
    }

    /// The two pages on one strip, 助手 on the left (`ShellPager`): at rest only the selected one is on screen.
    private var pages: some View {
        GeometryReader { g in
            ZStack {
                if shell.assistantBuilt {
                    AssistantView()
                        .modifier(ShellPageSlide(shell: shell, index: 0))
                }
                if shell.agentsBuilt {
                    ChatsView()
                        .modifier(ShellPageSlide(shell: shell, index: 1))
                }
                if LarkHomePreference.shared.enabled, shell.feishuBuilt {
                    LarkConversationsView()
                        .modifier(ShellPageSlide(shell: shell, index: 2))
                }
            }
            .frame(width: g.size.width, height: g.size.height)
            .overlay(alignment: .top) { ShellMovingBar(pager: shell.pager) }
            #if DEBUG
            .overlay(alignment: .bottomTrailing) { ShellPagerProbe(shell: shell) }
            #endif
            .onAppear { shell.pager.width = g.size.width }
            .onChange(of: g.size.width) { _, w in shell.pager.width = w }
        }
    }

    private var sidebar: some View {
        AssistantSidebar(current: model.assistantCurrent,
                         open: { key in
                             model.assistantCurrent = key
                             model.selectedTab = .assistant
                             model.assistantReveal += 1
                             shell.drawer.close()
                         },
                         newConversation: { go(.newConversation) },
                         openProfile: { go(.profile) },
                         openRoutines: { go(.routines) },
                         openKnowledge: { go(.knowledge) },
                         openSettings: {
                             shell.drawer.close()
                             model.shellSettings = true
                         },
                         drawer: shell.drawer,
                         openInbox: { go(.inbox) },
                         openTodos: { go(.todos) },
                         onMachines: { go(.machines) })
    }

    private func go(_ page: ShellPage) {
        model.selectedTab = .assistant
        shell.show(page)
        shell.drawer.close()
    }
}

#if DEBUG
/// What the strip shows, for UI tests (`probe.shell`, docs/ios-state.md「探针」; read by `ShellPageProbe.swift`): the page
/// the strip rests on (`assistant` / `agents` / `feishu`, `moving` while it slides), the page in front (`ShellState.page`, what the
/// switch shows), the app's tab, the drawer and the pages' roots, the status row, and the latest `ShellTrace`. Pixels
/// told which page was drawn before; both pages stay built, so `exists` cannot.
private struct ShellPagerProbe: View {
    @Environment(AppModel.self) private var model
    let shell: ShellState

    var body: some View {
        Color.clear
            .frame(width: Theme.Size.line, height: Theme.Size.line)
            .allowsHitTesting(false)
            .screenProbe("shell", ScreenProbe.enabled ? ShellShown(shell, tab: model.selectedTab) : ShellShown())
    }
}

/// `probe.shell`'s value.
nonisolated struct ShellShown: Encodable, Equatable {
    var strip = "", selected = "", tab = ""
    var position = 0.0
    var moving = false, lingering = false, drawerOpen = false, chatRoot = true, workRoot = true
    /// The status row: `none` or `link` (toasts float, `ToastWindow`), how many times it opened and folded, and its room (points).
    var status = "none"
    var opened = 0, folded = 0, room = 0
    var trace: [String] = []

    init() {}

    @MainActor init(_ shell: ShellState, tab t: AppTab) {
        let pager = shell.pager, n = shell.notices
        strip = pager.moving ? "moving" : Self.name(pager.page)
        selected = Self.name(shell.page)
        tab = t == .chats ? "agents" : t == .assistant ? "assistant" : t.rawValue
        position = (pager.position * 100).rounded() / 100
        moving = pager.moving
        lingering = pager.lingering
        drawerOpen = shell.drawer.isOpen
        chatRoot = shell.chatAtRoot
        workRoot = shell.workAtRoot
        status = n.shown == nil ? "none" : "link"
        opened = n.opened
        folded = n.folded
        room = Int(n.room.rounded())
        trace = ShellTrace.shared.events
    }

    /// A page of the strip by name: 0 = 助手, 1 = Agents, 2 = 飞书.
    private static func name(_ page: Int) -> String {
        page == 2 ? "feishu" : page == 1 ? "agents" : "assistant"
    }
}
#endif

/// The top bar while the strip moves, over both pages and still: the face and the switch where they always are (the switch's
/// pill under the finger), the right-hand button fading from 助手's 新对话 into Agents' 新建会话, 助手's fog fading with
/// its page. Only its switch takes taps (one while the strip slides picks where it ends: a tap is never lost to the
/// slide); the pages' own bars (hidden meanwhile, `ShellBarAtRest`) take the taps at rest. Laid out as
/// `AgentsTopBar`, which lines up with 助手's to the pixel.
private struct ShellMovingBar: View {
    @Environment(AppModel.self) private var model
    let pager: ShellPager

    var body: some View {
        if pager.moving || pager.lingering {
            let p = Double(min(CGFloat(pager.pageCount - 1), max(0, pager.position)))
            HStack(alignment: .top, spacing: Theme.Space.s) {
                ShellPersonaButton(id: "shell-moving-sidebar") {}
                Spacer(minLength: 0)
                ZStack {
                    // 新对话 is dimmed on an empty chat (AssistantTopBar).
                    ShellCircleButton(symbol: Theme.Icon.compose, label: "新对话", id: "shell-moving-new") {}
                        .opacity(max(0, 1 - p) * (model.assistantCurrent == nil ? 0.4 : 1))
                    ShellCircleButton(symbol: Theme.Icon.compose, label: "新建会话", id: "shell-moving-agents-new") {}
                        .opacity(max(0, 1 - abs(p - 1)))
                    ShellCircleButton(symbol: Theme.Icon.settings, label: "飞书设置", id: "shell-moving-feishu-settings") {}
                        .opacity(max(0, p - 1))
                }
            }
            .allowsHitTesting(false)
            .overlay(alignment: .top) {
                ShellSwitch(tab: .constant(ShellState.tab(for: pager.page))) { EmptyView() } workMenu: { EmptyView() }
                    .environment(\.shellPillFollowsStrip, true)
            }
            .padding(.horizontal, Theme.Space.l)
            .padding(.top, Theme.Space.xxs)
            .shellTopBarScaled()
            .background(alignment: .top) {
                BarFog(solid: AssistantTopBar.height - Theme.Space.sm, fade: AssistantTopBar.fade)
                    .opacity(max(0, 1 - p))
                    .allowsHitTesting(false)
            }
            .accessibilityHidden(true)
        }
    }
}
#endif
