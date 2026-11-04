import MarkdownUI
import BeautifulUI
import SwiftUI

struct RootView: View {
    @Environment(AppModel.self) private var model
    /// The system's text size, before 小 / 标准 / 大 moves it.
    @Environment(\.dynamicTypeSize) private var systemTypeSize
    @Environment(\.horizontalSizeClass) private var widthClass

    /// The ChatGPT-style shell (docs/ios.md「新外壳」): an iPhone-width screen, unless 设置 › 外观 › 旧布局 is on.
    private var usesShell: Bool {
        #if os(iOS)
        return !model.oldLayout && widthClass == .compact
        #else
        return false
        #endif
    }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("RootView")
        #endif
        Group {
            if let ref = model.pairingEngineMachine {
                PairingEngineView(ref: ref)
            } else if model.isPaired {
                #if os(iOS)
                if usesShell { ShellView() } else { RootTabs() }
                #else
                RootTabs()
                #endif
            } else {
                WelcomeView()
            }
        }
        // The plugins' main-navigation entries and cards, for 设置's badge (docs/plugins.md「UI 插槽」).
        .task(id: PluginSlots.source(model)) { await PluginSlots.shared.reload(for: PluginSlots.source(model).hub) }
        // A new style, 主题色 or persona rebuilds the whole tree (tokens are static); the open tab and chat are remembered.
        .id(model.look)
        .tint(Theme.accent)
        .fontDesign(model.style.fontDesign)
        .dynamicTypeSize(model.textSize.applied(to: systemTypeSize))
        .environment(\.shellBaseTypeSize, model.textSize.applied(to: systemTypeSize))
        .buiTheme(.jarvis())
        // No scroll bars anywhere (2026-10-31, the user: they look bad); the environment reaches every ScrollView,
        // List and sheet below. UIKit scroll views (MonoTextView, the terminal, web views) turn theirs off themselves.
        .scrollIndicators(.hidden)
        .markdownInlineImageProvider(MathInlineImages()) // inline formulas in every Markdown (MathMarkdown)
        // The shell shows the toasts in a window of its own, over its sheets too (`ToastWindow`).
        .overlay(alignment: .top) { if !usesShell { ToastOverlay() } }
        #if os(iOS)
        // 设置 › 外观 › 深浅色 on this scene's window (and any other open one) as it first shows.
        .onAppear { model.colorScheme.apply() }
        // The 主题色 on the windows too, for what UIKit draws in the window's tint (设置's row icons, `WindowTint`).
        .onChange(of: model.look, initial: true) { WindowTint.apply() }
        #endif
        #if DEBUG
        .overlay(alignment: .bottomTrailing) { PerfCountProbe() }
        // UI tests see the small window (VNC in picture in picture, #1343), which is the system's.
        .overlay(alignment: .topLeading) {
            if model.vncPiP != nil {
                Color.clear.frame(width: Theme.Size.hairline, height: Theme.Size.hairline)
                    .accessibilityElement().accessibilityLabel("VNC 小窗开着").accessibilityIdentifier("vnc-pip-on")
            }
        }
        .screenProbe("connection", ConnectionShown(shown: "\(model.connection.shown)", network: model.connection.networkUp))
        #endif
        .alert("Claude 想在 iPhone 上执行", isPresented: Binding(
            get: { model.pendingConfirmation != nil },
            set: { if !$0 { model.pendingConfirmation?.respond(false); model.pendingConfirmation = nil } }
        ), presenting: model.pendingConfirmation) { c in
            Button("允许") { c.respond(true); model.pendingConfirmation = nil }
            Button("拒绝", role: .cancel) { c.respond(false); model.pendingConfirmation = nil }
        } message: { c in
            Text(c.summary)
        }
        .sendRefusalAlert(model)
        .loadsChannels() // the names under a message that came in from a channel (「来自 微信」)
        #if os(iOS)
        .onChange(of: LarkHomePreference.shared.shows) { _, enabled in
            if !enabled, model.selectedTab == .feishu { model.selectedTab = .assistant }
        }
        #endif
    }
}

@MainActor private struct RootTabs: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("RootTabs")
        #endif
        @Bindable var model = model
        TabView(selection: $model.selectedTab) {
            AssistantRootTab()
            AgentsRootTab()
            #if os(iOS)
            if LarkHomePreference.shared.shows { FeishuRootTab() }
            #endif
            InboxRootTab()
            SettingsRootTab()
        }
    }
}

/// A tab's page, drawn again only by what it reads itself. Each tab's badge (the agents waiting, the inbox's 需要你) is read
/// by its `Tab`, and the hub changes them with nearly every push: without this the `Tab` built its page again each time,
/// the page ran its body (the Agents list's toolbar handed to UIKit again: the open ⋯ menu went back to its top while
/// choosing a machine; the open chat laid out again under the finger), TestFlight 70, docs/lessons/ios.md. The pages
/// take nothing from here, so nothing is lost.
private struct TabPage<Page: View>: View, Equatable {
    @ViewBuilder var page: () -> Page

    nonisolated static func == (a: Self, b: Self) -> Bool { true }

    var body: some View { page() }
}

@MainActor private struct AssistantRootTab: TabContent {
    @Environment(AppModel.self) private var model

    var body: some TabContent<AppTab> {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AssistantRootTab")
        #endif
        Tab("Persona", systemImage: Theme.Icon.ai, value: AppTab.assistant) {
            TabPage {
                AssistantView()
                    .modifier(TabActivityScope(tab: .assistant))
                #if PERF_SIGNPOST
                    .modifier(PerfTabMount("assistant"))
                #endif
            }
            .equatable()
        }
        .badge(model.badges.assistant)
    }
}

@MainActor private struct AgentsRootTab: TabContent {
    @Environment(AppModel.self) private var model

    var body: some TabContent<AppTab> {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("AgentsRootTab")
        #endif
        Tab("Agents", systemImage: Theme.Icon.agents, value: AppTab.chats) {
            TabPage {
                ChatsView()
                    .modifier(TabActivityScope(tab: .chats))
                #if PERF_SIGNPOST
                    .modifier(PerfTabMount("agents"))
                #endif
            }
            .equatable()
        }
        .badge(model.badges.agents)
    }
}

@MainActor private struct InboxRootTab: TabContent {
    var body: some TabContent<AppTab> {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("InboxRootTab")
        #endif
        Tab("收件箱", systemImage: Theme.Icon.inbox, value: AppTab.mail) {
            TabPage {
                InboxView()
                    .modifier(TabActivityScope(tab: .mail))
                #if PERF_SIGNPOST
                    .modifier(PerfTabMount("inbox"))
                #endif
            }
            .equatable()
        }
        .badge(InboxStore.shared.actionCount)
    }
}

@MainActor private struct SettingsRootTab: TabContent {
    @Environment(AppModel.self) private var model

    var body: some TabContent<AppTab> {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("SettingsRootTab")
        #endif
        Tab("设置", systemImage: Theme.Icon.settings, value: AppTab.settings) {
            TabPage {
                SettingsView()
                    .modifier(TabActivityScope(tab: .settings))
                #if PERF_SIGNPOST
                    .modifier(PerfTabMount("settings"))
                #endif
            }
            .equatable()
        }
        .badge(badge) // 体验模式 is left from 设置; the plugins' entries are on it (docs/plugins.md「UI 插槽」角标)
    }

    /// 体验; else what the plugins' main-navigation entries' badges add up to (only words: a dot).
    private var badge: Text? {
        if model.isDemo { return Text("体验") }
        let total = PluginSlots.shared.total
        if total.count > 0 { return Text(total.count > 99 ? "99+" : "\(total.count)") }
        return total.dot ? Text("•") : nil
    }
}

#if DEBUG
/// What the titles say about the link (`probe.connection`): 等待网络 / 连接中… / 更新中…, or nothing (online).
nonisolated struct ConnectionShown: Encodable, Equatable {
    var shown: String
    var network: Bool
}
#endif

#if os(iOS)
@MainActor private struct FeishuRootTab: TabContent {
    var body: some TabContent<AppTab> {
        Tab("消息", systemImage: Theme.Icon.feishuChats, value: AppTab.feishu) {
            TabPage { LarkConversationsView().modifier(TabActivityScope(tab: .feishu)) }.equatable()
        }
    }
}
#endif
