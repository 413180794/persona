import SwiftUI

#if os(iOS)
/// The iPhone's ChatGPT-style shell (docs/ios.md「新外壳」): no tab bar. 聊天 (the Jarvis assistant) and 工作 (the Agents
/// list) are two pages under the top switch, both kept alive like tabs (switching keeps each one's place); the drawer
/// slides both aside; 收件箱 / 定时任务 / 知识库 / 机器 push onto 聊天; 设置 is a sheet.
///
/// Every route in the app still goes through `AppModel.selectedTab` (notifications, Siri, 交给 Jarvis, the inbox's
/// rows…): `.assistant` / `.chats` pick the page, `.mail` opens the inbox on 聊天, `.settings` the sheet.
struct ShellView: View {
    @Environment(AppModel.self) private var model
    @State private var shell = ShellState()
    /// 工作 is built the first time it is shown (TabView does the same), then kept.
    @State private var workBuilt = false
    /// The page under 设置 and the inbox: they come back to it.
    @State private var lastPage: AppTab = .assistant

    private var tab: AppTab { model.selectedTab == .chats ? .chats : .assistant }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("ShellView")
        #endif
        @Bindable var model = model
        AssistantDrawerLayout(drawer: shell.drawer, enabled: tab == .chats ? shell.workAtRoot : shell.chatAtRoot,
                              backdrop: Theme.Shell.drawer, edgeOnly: tab == .chats, sidebar: { sidebar }, page: pages)
            .environment(\.shell, shell)
            .sheet(isPresented: $model.shellSettings) {
                SettingsView()
                    .environment(\.shell, shell)
            }
            .onChange(of: model.selectedTab, initial: true) { _, t in route(t) }
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

    private var pages: some View {
        ZStack {
            AssistantView()
                .modifier(ShellPageVisibility(shown: tab == .assistant))
            if workBuilt || tab == .chats {
                ChatsView()
                    .modifier(ShellPageVisibility(shown: tab == .chats))
            }
        }
    }

    /// Routes that are not one of the two pages land on 聊天 (the inbox) or in the sheet (设置).
    private func route(_ t: AppTab) {
        switch t {
        case .assistant: lastPage = .assistant
        case .chats:
            workBuilt = true
            lastPage = .chats
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
    }

    private func go(_ page: ShellPage) {
        model.selectedTab = .assistant
        shell.show(page)
        shell.drawer.close()
    }
}

/// One of the shell's two pages: the other one stays built underneath, out of sight and out of reach.
private struct ShellPageVisibility: ViewModifier {
    var shown: Bool

    func body(content: Content) -> some View {
        content
            .opacity(shown ? 1 : 0)
            .allowsHitTesting(shown)
            .accessibilityHidden(!shown)
            .zIndex(shown ? 1 : 0)
    }
}
#endif
