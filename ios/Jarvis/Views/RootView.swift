import SwiftUI

struct RootView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        @Bindable var model = model
        Group {
            if model.isPaired {
                TabView(selection: $model.selectedTab) {
                    Tab("对话", systemImage: "sparkles", value: AppTab.assistant) {
                        AssistantView()
                    }
                    .badge(model.pendingInteractions.filter { model.session($0.sessionKey)?.assistant == true }.count)
                    Tab("Agents", systemImage: "chevron.left.forwardslash.chevron.right", value: AppTab.chats) {
                        ChatsView()
                    }
                    .badge(model.pendingInteractions.filter { model.session($0.sessionKey)?.assistant != true && !model.isMuted($0.sessionKey) }.count)
                    Tab("信息", systemImage: "tray.full", value: AppTab.mail) {
                        InboxView()
                    }
                    Tab("工作台", systemImage: "square.grid.2x2", value: AppTab.workbench) {
                        WorkbenchView()
                    }
                    Tab("设置", systemImage: "gearshape", value: AppTab.settings) {
                        SettingsView()
                    }
                }
            } else {
                WelcomeView()
            }
        }
        // A new style rebuilds the whole tree (tokens are static); the open tab and chat are remembered.
        .id(model.style)
        .tint(Theme.accent)
        .fontDesign(model.style.fontDesign)
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
    }
}

struct ToastOverlay: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        ZStack {
            if let toast = model.toast {
                HStack(spacing: 10) {
                    Image(systemName: toast.style == .error ? "exclamationmark.triangle.fill" : toast.style == .success ? "checkmark.circle.fill" : "info.circle.fill")
                        .foregroundStyle(toast.style == .error ? Theme.danger : toast.style == .success ? Theme.idle : Theme.accent)
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
