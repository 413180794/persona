import SwiftUI

/// The built-in persona's face (docs/persona.md「默认形象」): drawn from shapes in the current style's accent, so it
/// follows 设置 › 界面风格 and stays sharp at any size; no bitmap assets. As the top bar's dot it moves for the state:
/// a blink now and then (空闲), eyes looking up under a thought bubble (思考中), eyes scanning (工作中), a pulsing ring
/// in 需要你's red (等你处理), dimmed and gray (离线).
struct JarvisAvatar: View {
    /// The three designs that were tried (docs/assistant.md「头像」); `.visor` is the one in use.
    enum Design: String, CaseIterable, Identifiable {
        case visor, spark, halo
        var id: String { rawValue }
        var title: String {
            switch self {
            case .visor: "面罩"
            case .spark: "火花"
            case .halo: "光环"
            }
        }
    }

    var size: CGFloat
    var design: Design = .visor
    var state: PersonaState = .idle
    /// Hold still (Reduce Motion, Low Power Mode): the state's look without its motion.
    var still = false
    /// Defaults to the current style's accent.
    var tint: Color?

    init(size: CGFloat, design: Design = .visor, state: PersonaState = .idle, still: Bool = false, tint: Color? = nil) {
        self.size = size
        self.design = design
        self.state = state
        self.still = still
        self.tint = tint
    }

    private var accent: Color { state == .offline ? Color(white: 0.62) : tint ?? Theme.accent }
    /// Moving every frame: only these states, and never when still.
    private var moving: Bool { !still && [.thinking, .working, .needsYou].contains(state) }

    var body: some View {
        Group {
            switch design {
            case .visor: visor
            case .spark: spark
            case .halo: halo
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }

    // MARK: 面罩: an accent disc with a dark visor and two bright eyes, a glint on top.

    private var visor: some View {
        TimelineView(.animation(minimumInterval: 1 / 15, paused: !moving)) { tl in
            let t = tl.date.timeIntervalSinceReferenceDate
            ZStack {
                Circle().fill(LinearGradient(colors: [accent.mix(with: .white, by: 0.28), accent, accent.mix(with: .black, by: 0.18)],
                                             startPoint: .topLeading, endPoint: .bottomTrailing))
                Capsule()
                    .fill(LinearGradient(colors: [Color(white: 0.16), Color(white: 0.05)], startPoint: .top, endPoint: .bottom))
                    .frame(width: size * 0.66, height: size * 0.30)
                    .overlay(Capsule().strokeBorder(.white.opacity(0.14), lineWidth: max(0.5, size * 0.012)))
                    .overlay { eyes(t) }
                    .offset(y: size * 0.02)
                if state == .thinking {
                    bubble(t).offset(x: size * 0.25, y: -size * 0.3)
                } else {
                    Image(systemName: Theme.Icon.jarvis)
                        .font(.system(size: size * 0.2, weight: .bold))
                        .foregroundStyle(.white.opacity(state == .offline ? 0.5 : 0.92))
                        .offset(x: size * 0.25, y: -size * 0.27)
                }
            }
            .overlay(Circle().strokeBorder(.white.opacity(0.18), lineWidth: max(0.5, size * 0.015)))
            .overlay {
                if state == .needsYou {
                    let pulse = moving ? 0.55 + 0.45 * cos(t * 3) : 1
                    Circle().strokeBorder(Theme.needsYou.opacity(pulse), lineWidth: max(1.5, size * 0.07))
                }
            }
        }
    }

    /// Where the eyes look, and how open they are, for the state at time t.
    private func eyes(_ t: TimeInterval) -> some View {
        let look: CGSize = switch state {
        case .working: CGSize(width: moving ? sin(t * 1.6) * size * 0.05 : 0, height: 0)
        case .thinking: CGSize(width: size * 0.04 + (moving ? sin(t * 0.9) * size * 0.012 : 0), height: -size * 0.035)
        default: .zero
        }
        return HStack(spacing: size * 0.15) {
            eye
            eye
        }
        .offset(look)
        .modifier(Blink(on: state == .idle && !still))
    }

    private var eye: some View {
        Capsule()
            .fill(state == .offline ? Color(white: 0.5) : accent.mix(with: .white, by: 0.55))
            .frame(width: size * 0.085, height: state == .offline ? size * 0.03 : size * 0.13)
            .shadow(color: state == .offline ? .clear : accent.opacity(0.9), radius: size * 0.05)
    }

    /// A small thought bubble whose three dots light up in turn.
    private func bubble(_ t: TimeInterval) -> some View {
        let lit = moving ? Int(t * 2.5) % 4 : 3
        return HStack(spacing: size * 0.025) {
            ForEach(0..<3) { i in
                Circle()
                    .fill(i == lit || lit == 3 ? accent : Color(white: 0.7))
                    .frame(width: size * 0.05, height: size * 0.05)
            }
        }
        .padding(.horizontal, size * 0.04)
        .frame(height: size * 0.13)
        .background(.white, in: .capsule)
        .shadow(color: .black.opacity(0.15), radius: size * 0.02, y: size * 0.01)
    }

    /// A blink every few seconds: a periodic timeline (one wake-up per blink, nothing between) driving a short
    /// phase animation.
    private struct Blink: ViewModifier {
        var on: Bool

        func body(content: Content) -> some View {
            if on {
                TimelineView(.periodic(from: .now, by: 3.6)) { tl in
                    content.phaseAnimator([1.0, 0.12, 1.0], trigger: tl.date) { v, open in
                        v.scaleEffect(x: 1, y: open, anchor: .center)
                    } animation: { _ in Theme.Motion.blink }
                }
            } else {
                content
            }
        }
    }

    // MARK: 火花: the app icon's sparkle on a dark disc tinted with the accent.

    private var spark: some View {
        ZStack {
            Circle().fill(LinearGradient(colors: [accent.mix(with: .black, by: 0.72), accent.mix(with: .black, by: 0.88)],
                                         startPoint: .top, endPoint: .bottom))
            Image(systemName: Theme.Icon.jarvis)
                .font(.system(size: size * 0.5, weight: .semibold))
                .foregroundStyle(LinearGradient(colors: [accent.mix(with: .white, by: 0.45), accent], startPoint: .top, endPoint: .bottom))
        }
        .overlay(Circle().strokeBorder(accent.opacity(0.35), lineWidth: max(0.5, size * 0.02)))
    }

    // MARK: 光环: a broken ring around a glowing core, like a heads-up display.

    private var halo: some View {
        ZStack {
            Circle().fill(LinearGradient(colors: [Color(white: 0.14), Color(white: 0.04)], startPoint: .top, endPoint: .bottom))
            TimelineView(.animation(minimumInterval: 1 / 30, paused: !moving)) { tl in
                let angle = moving ? tl.date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 3) * 120 : 0
                ZStack {
                    ForEach(0..<3) { i in
                        Circle()
                            .trim(from: Double(i) / 3 + 0.03, to: Double(i + 1) / 3 - 0.03)
                            .stroke(accent, style: StrokeStyle(lineWidth: size * 0.07, lineCap: .round))
                    }
                }
                .frame(width: size * 0.66, height: size * 0.66)
                .rotationEffect(.degrees(angle - 90))
            }
            Circle()
                .fill(RadialGradient(colors: [accent.mix(with: .white, by: 0.6), accent], center: .center, startRadius: 0, endRadius: size * 0.12))
                .frame(width: size * 0.24, height: size * 0.24)
                .shadow(color: accent, radius: size * 0.08)
        }
    }
}

#if DEBUG
/// The three designs that were tried, at the sizes they are used, light and dark, and the chosen one in every
/// style's accent (under PersonaGallery, `-openAssistantPage avatars`).
enum JarvisAvatarGallery {
    struct Panels: View {
        var body: some View {
            VStack(spacing: Theme.Space.l) {
                panel(.light)
                panel(.dark)
            }
        }

        private func panel(_ scheme: ColorScheme) -> some View {
            VStack(alignment: .leading, spacing: Theme.Space.l) {
                ForEach(JarvisAvatar.Design.allCases) { d in
                    HStack(spacing: Theme.Space.l) {
                        Text(d.title).font(Theme.Font.secondaryStrong).scaledFrame(width: Theme.Size.avatar, alignment: .leading)
                        JarvisAvatar(size: 28, design: d)
                        JarvisAvatar(size: 44, design: d)
                        JarvisAvatar(size: 84, design: d)
                        Spacer(minLength: 0)
                    }
                }
                HStack(spacing: Theme.Space.m) {
                    ForEach(AppStyle.allCases) { s in
                        VStack(spacing: Theme.Space.xs) {
                            JarvisAvatar(size: 40, tint: Theme.accent(s))
                            Text(s.title).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                }
            }
            .padding(Theme.Space.l)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(uiColor: .systemBackground), in: .rect(cornerRadius: Theme.Radius.bubble))
            .environment(\.colorScheme, scheme)
        }
    }
}
#endif
