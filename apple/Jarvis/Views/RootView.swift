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
            if model.isPaired {
                #if os(iOS)
                if usesShell { ShellView() } else { RootTabs() }
                #else
                RootTabs()
                #endif
            } else {
                WelcomeView()
            }
        }
        // A new style or a persona's colors rebuild the whole tree (tokens are static); the open tab and chat are remembered.
        .id(model.look)
        .tint(Theme.accent)
        .fontDesign(model.style.fontDesign)
        .dynamicTypeSize(model.textSize.applied(to: systemTypeSize))
        .buiTheme(.jarvis(model.style))
        // No scroll bars anywhere (2026-10-31, the user: they look bad); the environment reaches every ScrollView,
        // List and sheet below. UIKit scroll views (MonoTextView, the terminal, web views) turn theirs off themselves.
        .scrollIndicators(.hidden)
        .overlay(alignment: .top) { ToastOverlay() }
        #if DEBUG
        .overlay(alignment: .bottomTrailing) { PerfCountProbe() }
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
        Tab("Jarvis", systemImage: Theme.Icon.ai, value: AppTab.assistant) {
            TabPage {
                AssistantView()
                #if PERF_SIGNPOST
                    .modifier(PerfTabMount("assistant"))
                #endif
            }
            .equatable()
        }
        .badge(model.pendingInteractions.filter { model.session($0.sessionKey)?.assistant == true }.count)
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
                #if PERF_SIGNPOST
                    .modifier(PerfTabMount("agents"))
                #endif
            }
            .equatable()
        }
        .badge(model.pendingInteractions.filter {
            model.session($0.sessionKey)?.assistant != true && !model.isMuted($0.sessionKey)
        }.count)
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
                #if PERF_SIGNPOST
                    .modifier(PerfTabMount("settings"))
                #endif
            }
            .equatable()
        }
        .badge(model.isDemo ? Text("体验") : nil) // 体验模式 is left from 设置
    }
}

struct ToastOverlay: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        ZStack {
            if let toast = model.toast {
                HStack(spacing: Theme.Space.m) {
                    Image(systemName: toast.style == .error ? Theme.Icon.errorFilled : toast.style == .success ? Theme.Icon.successFilled : "info.circle.fill")
                        .foregroundStyle(toast.style == .error ? Theme.danger : toast.style == .success ? Theme.positive : Theme.accent)
                    Text(toast.message)
                        .font(Theme.Font.secondaryMedium)
                        .lineLimit(3)
                }
                .padding(.horizontal, Theme.Space.l)
                .padding(.vertical, Theme.Space.m)
                .surface(in: .capsule)
                .padding(.horizontal, Theme.Space.xl)
                .transition(.move(edge: .top).combined(with: .opacity))
                .onTapGesture { model.toast = nil }
                .task(id: toast.id) {
                    try? await Task.sleep(for: .seconds(toast.style == .error ? 4 : 2.5))
                    withAnimation(Theme.Motion.standard) { model.toast = nil }
                }
            }
        }
        .animation(Theme.Motion.standard, value: model.toast)
    }
}
