#if os(iOS)
import SwiftUI

/// 眼镜拍照 (docs/meta-glasses.md): connect the Meta glasses, preview what they see, take a photo and attach it.
/// Works in both builds: without the SDK, `MetaGlasses` only reports that the glasses are not built in.
struct GlassesCaptureSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    @State private var glasses = MetaGlasses.shared
    @State private var allowed = false
    @State private var photo: UIImage?
    @State private var busy = false
    var pick: (UIImage) -> Void

    var body: some View {
        NavigationStack {
            Group {
                if !glasses.registered {
                    connectView
                } else if !allowed {
                    permissionView
                } else {
                    captureView
                }
            }
            .navigationTitle("眼镜拍照")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("关闭") { dismiss() }.lineLimit(1)
                }
            }
            .task(id: glasses.registered) {
                guard glasses.registered else {
                    allowed = false
                    return
                }
                allowed = await glasses.cameraAllowed()
            }
            .onChange(of: scenePhase) { _, phase in
                if phase == .background {
                    glasses.stop()
                } else if phase == .active, glasses.registered {
                    Task {
                        allowed = await glasses.cameraAllowed()
                        if allowed, photo == nil {
                            try? await glasses.startPreview()
                        }
                    }
                }
            }
            .onDisappear { glasses.stop() }
        }
    }

    private var connectView: some View {
        VStack(spacing: Theme.Space.l) {
            Spacer(minLength: 0)
            Text("需要先在 Meta AI 里连上眼镜。点下面的按钮会跳到 Meta AI，连好后再回到 Persona。")
                .font(Theme.Font.body)
                .multilineTextAlignment(.center)
            Button { Task { await act { try await glasses.connect() } } } label: {
                Label("连接 Meta 眼镜", systemImage: Theme.Icon.glasses).lineLimit(1)
            }
            .themedButton(prominent: true)
            .disabled(busy)
            errorText
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, Theme.Space.page)
        .themedPage()
    }

    private var permissionView: some View {
        VStack(spacing: Theme.Space.l) {
            Spacer(minLength: 0)
            Text("要在 Meta AI 里允许 Persona 使用眼镜摄像头，会跳到 Meta AI。")
                .font(Theme.Font.body)
                .multilineTextAlignment(.center)
            Button {
                Task {
                    await act {
                        allowed = try await glasses.requestCamera()
                    }
                }
            } label: {
                Label("去允许", systemImage: Theme.Icon.glasses).lineLimit(1)
            }
            .themedButton(prominent: true)
            .disabled(busy)
            errorText
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, Theme.Space.page)
        .themedPage()
    }

    private var captureView: some View {
        VStack(spacing: Theme.Space.l) {
            ZStack {
                if let photo {
                    Image(uiImage: photo).resizable().scaledToFit()
                } else if let frame = glasses.preview {
                    Image(uiImage: frame).resizable().scaledToFit()
                } else if glasses.error == nil {
                    ProgressView("正在连接眼镜").font(Theme.Font.secondary)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.card))
            errorText
            if let photo {
                HStack(spacing: Theme.Space.m) {
                    Button {
                        self.photo = nil
                        Task { try? await glasses.startPreview() }
                    } label: {
                        Label("重拍", systemImage: Theme.Icon.retry).lineLimit(1)
                    }
                    .themedButton()
                    Button {
                        pick(photo)
                        dismiss()
                    } label: {
                        Label("用这张", systemImage: Theme.Icon.done).lineLimit(1)
                    }
                    .themedButton(prominent: true)
                }
            } else if glasses.preview == nil, glasses.error != nil {
                Button { Task { await act { try await glasses.startPreview() } } } label: {
                    Label("重试", systemImage: Theme.Icon.retry).lineLimit(1)
                }
                .themedButton(prominent: true)
                .disabled(busy)
            } else {
                Button { Task { await shoot() } } label: {
                    Label("拍一张", systemImage: Theme.Icon.camera).lineLimit(1)
                }
                .themedButton(prominent: true)
                .disabled(glasses.preview == nil || busy)
            }
        }
        .padding(.horizontal, Theme.Space.page)
        .padding(.vertical, Theme.Space.l)
        .themedPage()
        .task { try? await glasses.startPreview() }
    }

    @ViewBuilder private var errorText: some View {
        if let message = glasses.error {
            Text(message).font(Theme.Font.secondary).foregroundStyle(Theme.danger)
                .multilineTextAlignment(.center)
        }
    }

    /// Runs one step that may throw; the sheet shows `MetaGlasses.error`, so a failure needs no alert here.
    private func act(_ step: () async throws -> Void) async {
        busy = true
        defer { busy = false }
        try? await step()
    }

    private func shoot() async {
        busy = true
        defer { busy = false }
        if let image = try? await glasses.capture() {
            photo = image
            glasses.stop()
        }
    }
}
#endif
