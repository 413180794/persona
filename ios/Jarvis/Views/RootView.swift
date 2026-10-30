import BeautifulUI
import SwiftUI

struct RootView: View {
    @Environment(AppModel.self) private var model

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
        // A new style rebuilds the whole tree (tokens are static); the open tab and chat are remembered.
        .id(model.style)
        .tint(Theme.accent)
        .fontDesign(model.style.fontDesign)
        .buiTheme(.jarvis(model.style))
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
        Tab("Jarvis", systemImage: "sparkles", value: AppTab.assistant) {
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
        Tab("Agents", systemImage: "chevron.left.forwardslash.chevron.right", value: AppTab.chats) {
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
        Tab("收件箱", systemImage: "tray.full", value: AppTab.mail) {
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
        Tab("设置", systemImage: "gearshape", value: AppTab.settings) {
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
                HStack(spacing: 10) {
                    Image(systemName: toast.style == .error ? "exclamationmark.triangle.fill" : toast.style == .success ? "checkmark.circle.fill" : "info.circle.fill")
                        .foregroundStyle(toast.style == .error ? Theme.danger : toast.style == .success ? Theme.positive : Theme.accent)
                    Text(toast.message)
                        .font(.subheadline.weight(.medium))
                        .lineLimit(3)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
                .surface(in: .capsule)
                .padding(.horizontal, 24)
                .transition(.move(edge: .top).combined(with: .opacity))
                .onTapGesture { model.toast = nil }
                .task(id: toast.id) {
                    try? await Task.sleep(for: .seconds(toast.style == .error ? 4 : 2.5))
                    withAnimation(.snappy) { model.toast = nil }
                }
            }
        }
        .animation(.snappy, value: model.toast)
    }
}
