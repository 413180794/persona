import SwiftUI

#if os(iOS) || os(visionOS)
/// 「Mac 上的 Persona 已登录」 over the Agents list, like WeChat's line over its chats (docs/ios.md「Mac 已登录」): one
/// quiet row while a desktop app is connected, Phone Muted beside it when on. A tap opens `DesktopPresenceSheet`.
struct DesktopPresenceRow: View {
    @Environment(AppModel.self) private var model
    @State private var showSheet = false

    var body: some View {
        Button { showSheet = true } label: {
            HStack(spacing: Theme.Space.s) {
                Image(systemName: Theme.Icon.laptop)
                    .foregroundStyle(.secondary)
                    .scaledFrame(width: Theme.Size.iconS)
                Text(DesktopDevice.bannerTitle(model.desktops))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                Spacer(minLength: Theme.Space.s)
                if model.muteWhileDesktop {
                    Label("手机静音", systemImage: Theme.Icon.muted)
                        .labelStyle(.titleAndIcon)
                        .font(Theme.Font.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            .font(Theme.Font.secondary)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("agents.desktops")
        .sheet(isPresented: $showSheet) { DesktopPresenceSheet() }
        #if DEBUG
        .task { // `-openDesktopSheet YES`: the sheet open, for screenshots
            if UserDefaults.standard.bool(forKey: "openDesktopSheet") { try? await Task.sleep(for: .seconds(1)); showSheet = true }
        }
        #endif
    }
}

/// What the banner opens, after WeChat's: which Macs are signed in and since when, Phone Muted, and Sign Out for each (it
/// revokes that Mac's device token; the hub closes its connection and it has to pair again).
struct DesktopPresenceSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var signingOut: DesktopDevice?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Toggle(isOn: Binding(get: { model.muteWhileDesktop }, set: { on in Task { await model.setMuteWhileDesktop(on) } })) {
                        Label("手机静音", systemImage: Theme.Icon.muted)
                    }
                    .accessibilityIdentifier("desktops.mute")
                } footer: {
                    Text("Mac 上的 Persona 在线时，这台 iPhone 不再推送「需要你处理」和「Agent 做完」的通知，由 Mac 提醒你；你让 Persona 定的提醒照常推送。")
                }

                ForEach(model.desktops) { d in
                    Section {
                        LabeledContent {
                            if let since = d.since {
                                Text(String(localized: "登录于 \(since.formatted(.relative(presentation: .named)))")).lineLimit(1)
                            }
                        } label: {
                            Label(d.name, systemImage: Theme.Icon.laptop).lineLimit(1)
                        }
                        if !d.device.isEmpty {
                            Button(role: .destructive) { signingOut = d } label: {
                                Text("退出这台 Mac 上的 Persona").lineLimit(1)
                            }
                        }
                    } footer: {
                        if d.device.isEmpty {
                            Text("这台 Mac 直接用配对凭据连着 hub，不能单独退出。")
                        }
                    }
                }
            }
            .navigationTitle("已登录的 Mac")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() }.lineLimit(1) }
            }
            .confirmationDialog(signingOut.map { String(localized: "退出「\($0.name)」上的 Persona？") } ?? "",
                                isPresented: Binding(get: { signingOut != nil }, set: { if !$0 { signingOut = nil } }),
                                titleVisibility: .visible) {
                Button("退出", role: .destructive) {
                    if let d = signingOut { Task { await model.signOutDesktop(d) } }
                }
            } message: {
                Text("退出后，那台 Mac 要重新配对才能再用 Persona。")
            }
            .onChange(of: model.desktops.isEmpty) { _, none in if none { dismiss() } }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }
}
#endif
