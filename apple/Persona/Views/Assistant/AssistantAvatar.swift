import SwiftUI

/// The built-in personas' faces (docs/persona.md「内置形象」): drawn from shapes in the current style's accent, so they
/// follow 设置 › 界面风格 and stay sharp at any size; no bitmap assets. As the top bar's dot each moves for the state:
/// a blink now and then (空闲), eyes looking up under a thought bubble (思考中), eyes scanning (面罩) or signals
/// running down the traces into the core (汇流) (工作中), a pulsing ring in 需要你's red (等你处理), dimmed and gray (离线).
struct AssistantAvatar: View {
    /// The designs that were tried (docs/assistant.md「头像」): `.visor` is the default persona's, `.circuit` the
    /// built-in 汇流's (`Persona.circuit`).
    enum Design: String, CaseIterable, Identifiable {
        case visor, circuit, spark, halo
        var id: String { rawValue }
        var title: String {
            switch self {
            case .circuit: "汇流"
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
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("JarvisAvatar")
        #endif
        Group {
            switch design {
            case .circuit: face(.circuit)
            case .visor: face(.visor)
            case .spark: spark
            case .halo: halo
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }

    // MARK: 汇流 and 面罩: PersonaFace (shared with the Live Activity), on a clock while moving.

    private func face(_ d: PersonaFace.Design) -> some View {
        TimelineView(.animation(minimumInterval: 1 / 15, paused: !moving)) { tl in
            PersonaFace(size: size, design: d, state: state, tint: tint ?? Theme.accent, mark: Theme.Icon.jarvis,
                        time: tl.date.timeIntervalSinceReferenceDate, moving: moving, blink: still ? nil : Theme.Motion.blink)
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
/// The designs that were tried, at the sizes they are used, light and dark, and the chosen one in every
/// style's accent (under PersonaGallery, `-openAssistantPage avatars`).
enum AssistantAvatarGallery {
    struct Panels: View {
        var body: some View {
            VStack(spacing: Theme.Space.l) {
                panel(.light)
                panel(.dark)
            }
        }

        private func panel(_ scheme: ColorScheme) -> some View {
            VStack(alignment: .leading, spacing: Theme.Space.l) {
                ForEach(AssistantAvatar.Design.allCases) { d in
                    HStack(spacing: Theme.Space.l) {
                        Text(d.title).font(Theme.Font.secondaryStrong).scaledFrame(width: Theme.Size.avatar, alignment: .leading)
                        AssistantAvatar(size: 28, design: d)
                        AssistantAvatar(size: 44, design: d)
                        AssistantAvatar(size: 84, design: d)
                        Spacer(minLength: 0)
                    }
                }
                HStack(spacing: Theme.Space.m) {
                    ForEach(ThemeColor.presets) { c in
                        VStack(spacing: Theme.Space.xs) {
                            AssistantAvatar(size: 40, tint: c.preset?.accent ?? Theme.accent)
                            Text(c.title).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
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
