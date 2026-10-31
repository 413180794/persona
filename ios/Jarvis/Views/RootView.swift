import BeautifulUI
import SwiftUI

struct RootView: View {
    @Environment(AppModel.self) private var model
    /// The system's text size, before 小 / 标准 / 大 moves it.
    @Environment(\.dynamicTypeSize) private var systemTypeSize

    var body: some View {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("RootView.body")
        #endif
        Group {
            if model.isPaired {
                RootTabs()
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
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("RootTabs.body")
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

@MainActor private struct AssistantRootTab: TabContent {
    @Environment(AppModel.self) private var model

    var body: some TabContent<AppTab> {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("AssistantRootTab.body")
        #endif
        Tab("Jarvis", systemImage: Theme.Icon.ai, value: AppTab.assistant) {
            AssistantView()
            #if PERF_SIGNPOST
                .modifier(PerfTabMount("assistant"))
            #endif
        }
        .badge(model.pendingInteractions.filter { model.session($0.sessionKey)?.assistant == true }.count)
    }
}

@MainActor private struct AgentsRootTab: TabContent {
    @Environment(AppModel.self) private var model

    var body: some TabContent<AppTab> {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("AgentsRootTab.body")
        #endif
        Tab("Agents", systemImage: Theme.Icon.agents, value: AppTab.chats) {
            ChatsView()
            #if PERF_SIGNPOST
                .modifier(PerfTabMount("agents"))
            #endif
        }
        .badge(model.pendingInteractions.filter {
            model.session($0.sessionKey)?.assistant != true && !model.isMuted($0.sessionKey)
        }.count)
    }
}

@MainActor private struct InboxRootTab: TabContent {
    var body: some TabContent<AppTab> {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("InboxRootTab.body")
        #endif
        Tab("收件箱", systemImage: Theme.Icon.inbox, value: AppTab.mail) {
            InboxView()
            #if PERF_SIGNPOST
                .modifier(PerfTabMount("inbox"))
            #endif
        }
        .badge(InboxStore.shared.actionCount)
    }
}

@MainActor private struct SettingsRootTab: TabContent {
    @Environment(AppModel.self) private var model

    var body: some TabContent<AppTab> {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("SettingsRootTab.body")
        #endif
        Tab("设置", systemImage: Theme.Icon.settings, value: AppTab.settings) {
            SettingsView()
            #if PERF_SIGNPOST
                .modifier(PerfTabMount("settings"))
            #endif
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
