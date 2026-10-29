import SwiftUI

/// Jarvis's face: on the Jarvis tab's top right (it opens the profile), at the top of the profile and on the empty
/// conversation page. Drawn from shapes in the current style's accent, so it follows 设置 › 界面风格 and stays sharp at
/// any size; no bitmap assets.
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
    /// Working: the eyes look around (visor) / the ring turns (halo).
    var working = false
    /// Defaults to the current style's accent.
    var tint: Color?

    private var accent: Color { tint ?? Theme.accent }

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
        ZStack {
            Circle().fill(LinearGradient(colors: [accent.mix(with: .white, by: 0.28), accent, accent.mix(with: .black, by: 0.18)],
                                         startPoint: .topLeading, endPoint: .bottomTrailing))
            Capsule()
                .fill(LinearGradient(colors: [Color(white: 0.16), Color(white: 0.05)], startPoint: .top, endPoint: .bottom))
                .frame(width: size * 0.66, height: size * 0.30)
                .overlay(Capsule().strokeBorder(.white.opacity(0.14), lineWidth: max(0.5, size * 0.012)))
                .overlay { eyes }
                .offset(y: size * 0.02)
            Image(systemName: "sparkle")
                .font(.system(size: size * 0.2, weight: .bold))
                .foregroundStyle(.white.opacity(0.92))
                .offset(x: size * 0.25, y: -size * 0.27)
        }
        .overlay(Circle().strokeBorder(.white.opacity(0.18), lineWidth: max(0.5, size * 0.015)))
    }

    private var eyes: some View {
        TimelineView(.animation(minimumInterval: 1 / 20, paused: !working)) { tl in
            let t = tl.date.timeIntervalSinceReferenceDate
            let look = working ? sin(t * 1.6) * size * 0.05 : 0
            HStack(spacing: size * 0.15) {
                eye
                eye
            }
            .offset(x: look)
        }
    }

    private var eye: some View {
        Capsule()
            .fill(accent.mix(with: .white, by: 0.55))
            .frame(width: size * 0.085, height: size * 0.13)
            .shadow(color: accent.opacity(0.9), radius: size * 0.05)
    }

    // MARK: 火花: the app icon's sparkle on a dark disc tinted with the accent.

    private var spark: some View {
        ZStack {
            Circle().fill(LinearGradient(colors: [accent.mix(with: .black, by: 0.72), accent.mix(with: .black, by: 0.88)],
                                         startPoint: .top, endPoint: .bottom))
            Image(systemName: "sparkle")
                .font(.system(size: size * 0.5, weight: .semibold))
                .foregroundStyle(LinearGradient(colors: [accent.mix(with: .white, by: 0.45), accent], startPoint: .top, endPoint: .bottom))
        }
        .overlay(Circle().strokeBorder(accent.opacity(0.35), lineWidth: max(0.5, size * 0.02)))
    }

    // MARK: 光环: a broken ring around a glowing core, like a heads-up display.

    private var halo: some View {
        ZStack {
            Circle().fill(LinearGradient(colors: [Color(white: 0.14), Color(white: 0.04)], startPoint: .top, endPoint: .bottom))
            TimelineView(.animation(minimumInterval: 1 / 30, paused: !working)) { tl in
                let angle = working ? tl.date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 3) * 120 : 0
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
/// `-openAssistantPage avatars` (with `-tab assistant`): the three designs at the sizes they are used, light and dark,
/// and the chosen one in every style's accent — for comparing them in screenshots.
struct JarvisAvatarGallery: View {
    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                panel(.light)
                panel(.dark)
            }
            .padding()
        }
        .navigationTitle("头像方案")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func panel(_ scheme: ColorScheme) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(JarvisAvatar.Design.allCases) { d in
                HStack(spacing: 18) {
                    Text(d.title).font(.subheadline.weight(.semibold)).frame(width: 40, alignment: .leading)
                    JarvisAvatar(size: 28, design: d)
                    JarvisAvatar(size: 44, design: d)
                    JarvisAvatar(size: 84, design: d)
                    Spacer(minLength: 0)
                }
            }
            HStack(spacing: 12) {
                ForEach(AppStyle.allCases) { s in
                    VStack(spacing: 4) {
                        JarvisAvatar(size: 40, tint: Theme.accent(s))
                        Text(s.title).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(uiColor: .systemBackground), in: .rect(cornerRadius: 20))
        .environment(\.colorScheme, scheme)
    }
}
#endif
