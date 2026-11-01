import SwiftUI

#if os(iOS)
/// The iPhone's ChatGPT-style shell (docs/ios.md「新外壳」): no tab bar. 助手 (the Jarvis assistant) and Agents (the Agents
/// list) are two pages under the top switch, both kept alive like tabs (switching keeps each one's place); the drawer
/// slides both aside; 收件箱 / 定时任务 / 知识库 / 机器 push onto 助手; 设置 is a sheet.
///
/// Every route in the app still goes through `AppModel.selectedTab` (notifications, Siri, 交给 Jarvis, the inbox's
/// rows…): `.assistant` / `.chats` pick the page, `.mail` opens the inbox on 助手, `.settings` the sheet.
struct ShellView: View {
    @Environment(AppModel.self) private var model
    @State private var shell = ShellState()
    /// Agents is built the first time it is shown or dragged in (TabView builds a tab when shown), then kept. Not before:
    /// hidden, its controls are still in the accessibility tree (its glass and list are UIKit) and UI tests on 助手 met them.
    @State private var workBuilt = false
    /// The page under 设置 and the inbox: they come back to it.
    @State private var lastPage: AppTab = .assistant
    /// The first route lands on its page without sliding there.
    @State private var routed = false

    private var tab: AppTab { model.selectedTab == .chats ? .chats : .assistant }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("ShellView")
        #endif
        @Bindable var model = model
        AssistantDrawerLayout(drawer: shell.drawer, enabled: tab == .chats ? shell.workAtRoot : shell.chatAtRoot,
                              backdrop: Theme.Shell.drawer, pager: shell.pager, sidebar: { sidebar }, page: pages)
            .environment(\.shell, shell)
            .sheet(isPresented: $model.shellSettings) {
                // The sheet outlives RootView's rebuild for a new look: it takes the look as its identity too, or 外观's
                // sample and everything else drawn with the static tokens keeps the old colors.
                SettingsView()
                    .environment(\.shell, shell)
                    .id(model.look)
            }
            .onChange(of: model.selectedTab, initial: true) { _, t in route(t) }
            .onAppear {
                // A drag that settles on the other page selects it; one that starts builds Agents if it is not yet.
                shell.pager.settled = { page in model.selectedTab = page == 1 ? .chats : .assistant }
                shell.pager.began = { workBuilt = true }
                #if DEBUG
                let at = UserDefaults.standard.double(forKey: "showShellPagerAt")
                if at > 0 {
                    workBuilt = true
                    DispatchQueue.main.asyncAfter(deadline: .now() + 1) { shell.pager.hold(at: at) }
                }
                #endif
            }
            // Weak network: one small line under the top bar, for both pages (the switch keeps the title's place).
            .overlay(alignment: .top) {
                let link = model.connection.shown
                if link != .online {
                    ConnectionLabel(phase: link, compact: true)
                        .padding(.horizontal, Theme.Space.m)
                        .scaledFrame(height: Theme.Size.icon)
                        .background(.regularMaterial, in: .capsule)
                        .padding(.top, AssistantTopBar.height)
                        .transition(.opacity)
                        .allowsHitTesting(false)
                }
            }
            .animation(Theme.Motion.standard, value: model.connection.shown)
    }

    /// The two pages on one strip, 助手 on the left (`ShellPager`): at rest only the selected one is on screen.
    private var pages: some View {
        GeometryReader { g in
            ZStack {
                AssistantView()
                    .modifier(ShellPageSlide(pager: shell.pager, index: 0))
                if workBuilt || tab == .chats {
                    ChatsView()
                        .modifier(ShellPageSlide(pager: shell.pager, index: 1))
                }
            }
            .frame(width: g.size.width, height: g.size.height)
            .overlay(alignment: .top) { ShellMovingBar(pager: shell.pager) }
            .onAppear { shell.pager.width = g.size.width }
            .onChange(of: g.size.width) { _, w in shell.pager.width = w }
        }
    }

    /// Routes that are not one of the two pages land on 助手 (the inbox) or in the sheet (设置).
    private func route(_ t: AppTab) {
        defer { routed = true }
        switch t {
        case .assistant:
            lastPage = .assistant
            shell.pager.slide(to: 0, animated: routed)
        case .chats:
            workBuilt = true
            lastPage = .chats
            shell.pager.slide(to: 1, animated: routed)
        case .mail:
            shell.show(.inbox)
            model.selectedTab = .assistant
        case .settings:
            model.shellSettings = true
            model.selectedTab = lastPage
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
                         openMachines: { go(.machines) })
            .shellScaled()
    }

    private func go(_ page: ShellPage) {
        model.selectedTab = .assistant
        shell.show(page)
        shell.drawer.close()
    }
}

/// The top bar while the strip moves, over both pages and still: the face and the switch where they always are (the switch's
/// pill under the finger), the right-hand button fading from 助手's 新对话 into Agents' 新建会话, 助手's fog fading with
/// its page. Only to look at: the pages' own bars (hidden meanwhile, `ShellBarAtRest`) take the taps at rest. Laid out as
/// `AgentsTopBar`, which lines up with 助手's to the pixel.
private struct ShellMovingBar: View {
    @Environment(AppModel.self) private var model
    let pager: ShellPager

    var body: some View {
        if pager.moving {
            let p = min(1, max(0, pager.position))
            HStack(alignment: .top, spacing: Theme.Space.s) {
                ShellPersonaButton(id: "shell-moving-sidebar") {}
                Spacer(minLength: 0)
                ZStack {
                    // 新对话 is dimmed on an empty chat (AssistantTopBar).
                    ShellCircleButton(symbol: Theme.Icon.compose, label: "新对话", id: "shell-moving-new") {}
                        .opacity((1 - p) * (model.assistantCurrent == nil ? 0.4 : 1))
                    ShellCircleButton(symbol: Theme.Icon.compose, label: "新建会话", id: "shell-moving-agents-new") {}
                        .opacity(p)
                }
            }
            .overlay(alignment: .top) {
                ShellSwitch(tab: .constant(pager.page == 1 ? .chats : .assistant)) { EmptyView() } workMenu: { EmptyView() }
            }
            .padding(.horizontal, Theme.Space.l)
            .padding(.top, Theme.Space.xxs)
            .shellScaled()
            .background(alignment: .top) {
                GeometryReader { g in
                    let solid = g.safeAreaInsets.top + AssistantTopBar.height - Theme.Space.sm
                    TopFog(solidHeight: solid, fadeHeight: AssistantTopBar.fade)
                        .frame(width: g.size.width, height: solid + AssistantTopBar.fade)
                        .offset(y: -g.safeAreaInsets.top)
                }
                .opacity(1 - p)
            }
            .allowsHitTesting(false)
            .accessibilityHidden(true)
        }
    }
}
#endif
