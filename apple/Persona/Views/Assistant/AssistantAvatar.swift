import SwiftUI

/// Mochi's animated face, shared with the still drawing in the Live Activity.
struct AssistantAvatar: View {
    enum Design: String, CaseIterable, Identifiable {
        case mochi
        var id: String { rawValue }
        var title: String { String(localized: "Mochi") }
    }

    @Environment(\.tabIsActive) private var inFront
    var size: CGFloat
    var design: Design = .mochi
    var state: PersonaState = .idle
    /// Hold still in Reduce Motion and Low Power Mode.
    var still = false

    private var moving: Bool { !still && inFront && [.thinking, .working, .needsYou].contains(state) }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("PersonaAvatar")
        #endif
        TimelineView(.animation(minimumInterval: 1 / 15, paused: !moving)) { tl in
            PersonaFace(size: size, design: PersonaFace.Design(rawValue: design.rawValue) ?? .mochi, state: state,
                        time: tl.date.timeIntervalSinceReferenceDate, moving: moving,
                        blink: still || !inFront ? nil : Theme.Motion.blink)
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

#if DEBUG
/// The remaining built-in face at its display sizes, in both appearances.
enum AssistantAvatarGallery {
    struct Panels: View {
        var body: some View {
            VStack(spacing: Theme.Space.l) {
                panel(.light)
                panel(.dark)
            }
        }

        private func panel(_ scheme: ColorScheme) -> some View {
            HStack(spacing: Theme.Space.l) {
                Text(AssistantAvatar.Design.mochi.title).font(Theme.Font.secondaryStrong)
                AssistantAvatar(size: Theme.Size.avatar)
                AssistantAvatar(size: PersonaStore.thumbnailPoints)
                Spacer(minLength: 0)
            }
            .padding(Theme.Space.l)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(uiColor: .systemBackground), in: .rect(cornerRadius: Theme.Radius.bubble))
            .environment(\.colorScheme, scheme)
        }
    }
}
#endif
