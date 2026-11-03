import BeautifulUI
import SwiftUI

/// One toast (`AppModel.toast`): the style's icon and the words on a floating capsule; in the BeautifulUI style its
/// raised capsule. A toast tells the result of something just done (已复制, 已归档, 发送失败…) and goes by itself after
/// a moment; what lasts (the hub's link: 等待网络, 连接中…) is the shell's status row's (docs/shell.md「状态行」).
struct ToastCapsule: View {
    let toast: Toast

    var body: some View {
        let row = HStack(spacing: Theme.Space.m) {
            Image(systemName: toast.style == .error ? Theme.Icon.errorFilled : toast.style == .success ? Theme.Icon.successFilled : Theme.Icon.infoFilled)
                .foregroundStyle(toast.style == .error ? Theme.danger : toast.style == .success ? Theme.positive : Theme.accent)
            Text(toast.message)
                .font(Theme.Font.secondaryMedium)
                .lineLimit(3)
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.m)
        if AppStyle.current.usesBeautifulUI {
            row.buiSurface(Capsule(), elevation: .button)
        } else {
            row.surface(in: .capsule)
        }
    }

    /// How long it stays.
    static func shown(_ toast: Toast) -> Duration { .seconds(toast.style == .error ? 4 : 2.5) }
}

/// The app's toast at the top of what it covers, over the content (it moves nothing), gone after `ToastCapsule.shown`.
/// `tapToDismiss`: a tap on it closes it (the tabs, the Mac); the iPhone's shell shows it in a window of its own that
/// takes no touches (`ToastWindow`), so nothing under it ever misses a tap.
struct ToastOverlay: View {
    @Environment(AppModel.self) private var model
    var tapToDismiss = true

    var body: some View {
        ZStack {
            if let toast = model.toast {
                ToastCapsule(toast: toast)
                    .padding(.horizontal, Theme.Space.xl)
                    .transition(.move(edge: .top).combined(with: .opacity))
                    .onTapGesture { if tapToDismiss { model.toast = nil } }
                    .accessibilityElement(children: .combine)
                    .accessibilityIdentifier("toast")
                    .task(id: toast.id) {
                        #if os(iOS)
                        // In a window that takes no touches VoiceOver does not come to it: it is read out as it comes.
                        if !tapToDismiss { AccessibilityNotification.Announcement(toast.message).post() }
                        #endif
                        try? await Task.sleep(for: ToastCapsule.shown(toast))
                        guard model.toast?.id == toast.id else { return }
                        withAnimation(Theme.Motion.standard) { model.toast = nil }
                    }
            }
        }
        .animation(Theme.Motion.standard, value: model.toast)
    }
}
