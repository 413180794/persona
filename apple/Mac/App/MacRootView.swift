import BeautifulUI
import SwiftUI

/// The window's sidebar sections: the iPhone's four tabs, plus 知识库 and 机器 (on the phone a page inside
/// Jarvis, and 机器监控 inside Agents / 设置), which a Mac with room for a sidebar shows as sections of their own
/// (docs/mac.md「主窗口」). ⌘1…⌘6 in this order.
enum MacSection: String, CaseIterable, Identifiable {
    case assistant, agents, inbox, knowledge, machines, settings

    var id: String { rawValue }

    /// The iPhone tab it is; nil for the Mac's own sections.
    var tab: AppTab? {
        switch self {
        case .assistant: .assistant
        case .agents: .chats
        case .inbox: .mail
        case .settings: .settings
        case .knowledge, .machines: nil
        }
    }

    /// The section on screen: one of the Mac's own while picked, else the tab's (a notification, ⌘K or a link that
    /// moves the tab moves the section with it, MacShell clears the pick).
    static func shown(override: MacSection?, tab: AppTab) -> MacSection { override ?? MacSection(tab) }

    /// What picking a section changes: the tab, or the Mac-side pick.
    static func pick(_ s: MacSection) -> (tab: AppTab?, override: MacSection?) {
        if let tab = s.tab { return (tab, nil) }
        return (nil, s)
    }

    init(_ tab: AppTab) {
        switch tab {
        case .assistant: self = .assistant
        case .chats: self = .agents
        case .mail: self = .inbox
        case .settings: self = .settings
        }
    }

    var title: String {
        switch self {
        case .assistant: "Jarvis"
        case .agents: "Agents"
        case .inbox: "收件箱"
        case .knowledge: "知识库"
        case .machines: "机器"
        case .settings: "设置"
        }
    }

    var icon: String {
        switch self {
        case .assistant: Theme.Icon.ai
        case .agents: Theme.Icon.agents
        case .inbox: Theme.Icon.inbox
        case .knowledge: Theme.Icon.knowledge
        case .machines: Theme.Icon.machines
        case .settings: Theme.Icon.settings
        }
    }
}

/// The window: unpaired, the welcome page (pairing, 体验模式); paired, a sidebar with the four sections beside the
/// section's page (the iOS pages, `horizontalSizeClass` regular).
struct MacRootView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        Group {
            if model.isPaired {
                MacShell()
            } else {
                WelcomeView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .frame(minWidth: Theme.Size.macWindowMinWidth, minHeight: Theme.Size.macWindowMinHeight)
        .macLook(model)
        .overlay(alignment: .top) { ToastOverlay() }
        .alert("Claude 想在这台 Mac 上执行", isPresented: Binding(
            get: { model.pendingConfirmation != nil },
            set: { if !$0 { model.pendingConfirmation?.respond(false); model.pendingConfirmation = nil } }
        ), presenting: model.pendingConfirmation) { c in
            Button("允许") { c.respond(true); model.pendingConfirmation = nil }
            Button("拒绝", role: .cancel) { c.respond(false); model.pendingConfirmation = nil }
        } message: { c in
            Text(c.summary)
        }
        .sendRefusalAlert(model)
        .overlay { if model.isPaired { MacQuickOpenLayer() } }
        .modifier(MacWindowOpener())
        .sheet(isPresented: Binding(get: { MacShellState.shared.newAgent }, set: { MacShellState.shared.newAgent = $0 })) {
            // 新建 Agent (⌘N, the menu bar, ⌘K): the iOS form in a window sheet; the new agent opens in Agents.
            NavigationStack {
                NewChatView { session in
                    MacShellState.shared.newAgent = false
                    MacShellState.shared.open(session.key, model)
                }
            }
            .macSheet()
            .environment(model)
        }
        #if DEBUG
        .preferredColorScheme(MacSnapshot.colorScheme)
        #endif
    }
}

extension View {
    /// What RootView sets on the whole tree on iOS: the style's tint, font design and theme, the text size, and a
    /// rebuild when the style or persona changes (tokens are static).
    func macLook(_ model: AppModel) -> some View {
        id(model.look)
            .tint(Theme.accent)
            .fontDesign(model.style.fontDesign)
            .dynamicTypeSize(model.textSize.applied(to: .large))
            .buiTheme(.jarvis(model.style))
            .scrollIndicators(.automatic)
    }
}

/// The sections column and the section's page side by side. Not a `NavigationSplitView`: the pages are split views
/// themselves (Agents, Jarvis, 收件箱 in regular width), and a split nested in another one's detail column laid its
/// own detail out as if the outer sidebar were still in front of it (the chat sat a sidebar's width to the right,
/// its composer cut off). The page's split is the window's; this column sits at its leading edge.
private struct MacShell: View {
    @Environment(AppModel.self) private var model
    private var shell: MacShellState { .shared }

    var body: some View {
        HStack(spacing: 0) {
            List(selection: Binding(get: { MacSection.shown(override: shell.section, tab: model.selectedTab) },
                                    set: { if let s = $0 { shell.go(s, model) } })) {
                ForEach(MacSection.allCases) { section in
                    Label(section.title, systemImage: section.icon)
                        .lineLimit(1)
                        .badge(badge(section))
                        .tag(section)
                        .accessibilityIdentifier("mac.section.\(section.rawValue)")
                }
            }
            .listStyle(.sidebar)
            .frame(width: Theme.Size.macSidebarIdeal)
            .background(Theme.systemSidebar)
            Divider()
            page
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        // Something moved the tab (a notification, ⌘K, a link): its section comes to the front.
        .onChange(of: model.selectedTab) { shell.section = nil }
        .onChange(of: model.openChat) { if model.openChat != nil { shell.section = nil } }
        .onChange(of: model.assistantCurrent) { shell.section = nil }
    }

    /// The iOS tab bar's badges (`RootView`).
    private func badge(_ section: MacSection) -> Text? {
        let n: Int
        switch section {
        case .knowledge, .machines: return nil
        case .assistant: n = model.pendingInteractions.filter { model.session($0.sessionKey)?.assistant == true }.count
        case .agents: n = model.pendingInteractions.filter { model.session($0.sessionKey)?.assistant != true && !model.isMuted($0.sessionKey) }.count
        case .inbox: n = InboxStore.shared.actionCount
        case .settings: return model.isDemo ? Text("体验") : nil
        }
        return n > 0 ? Text("\(n)") : nil
    }

    @ViewBuilder private var page: some View {
        switch MacSection.shown(override: shell.section, tab: model.selectedTab) {
        case .assistant: AssistantView()
        case .agents: ChatsView()
        case .inbox: MacInboxPage()
        case .knowledge: NavigationStack { KnowledgeView() }
        case .machines:
            NavigationStack {
                MachineDashboardView()
                    .navigationDestination(for: MachineMonitorRoute.self) { r in MachineMonitorView(machineID: r.id) }
                    .navigationDestination(for: MachineDetailRoute.self) { r in MachineDetailView(machineID: r.id) }
            }
        case .settings: SettingsView()
        }
    }
}
