import SwiftUI

/// What the persona's dot shows (docs/persona.md「状态」). Raw values are persona.json's.
nonisolated enum PersonaState: String, CaseIterable, Identifiable, Sendable {
    case idle, thinking, working
    case needsYou = "needs_you"
    case offline

    var id: String { rawValue }
    var title: String {
        switch self {
        case .idle: "空闲"
        case .thinking: "思考中"
        case .working: "工作中"
        case .needsYou: "等你处理"
        case .offline: "离线"
        }
    }
}

/// The built-in personas' faces (面罩, 汇流) at one moment: everything JarvisAvatar draws for them, without the clock.
/// Shared with the widget extension, which draws the Live Activity (docs/live-activity.md「形象」) and cannot run
/// timelines: it passes `moving: false` and gets each state's still frame (the same one Reduce Motion shows). The app
/// wraps it in a TimelineView and passes the time (JarvisAvatar). Reads no app-only token: the tint, the brand mark and
/// the blink come in from the caller.
struct PersonaFace: View {
    enum Design: String, Sendable {
        case visor, circuit
    }

    var size: CGFloat
    var design: Design
    var state: PersonaState
    /// The accent it is drawn in (offline turns it gray).
    var tint: Color
    /// The SF Symbol of the brand's star on 面罩 (`Theme.Icon.jarvis`).
    var mark: String
    /// Seconds (any epoch) for the moving states; ignored when not `moving`.
    var time: TimeInterval = 0
    /// The thinking / working / needs-you motion runs; false: each state's still frame.
    var moving = false
    /// The idle blink every few seconds (`Theme.Motion.blink`); nil: eyes stay open.
    var blink: Animation?

    private var accent: Color { state == .offline ? Color(white: 0.62) : tint }
    private var needsYou: Color { StatusPalette.needsYou.dot }

    var body: some View {
        switch design {
        case .circuit: circuit
        case .visor: visor
        }
    }

    // MARK: 汇流: three pads on the left whose traces run into a core with two eyes (your computers' agents coming
    // together in Jarvis), in the accent on a dark disc of it.

    private var circuit: some View {
        let ground = accent.mix(with: .black, by: 0.82)
        let mark = accent.mix(with: .white, by: 0.15)
        let t = time
        return ZStack {
            Circle().fill(LinearGradient(colors: [accent.mix(with: .black, by: 0.76), accent.mix(with: .black, by: 0.88)],
                                         startPoint: .top, endPoint: .bottom))
            Canvas { ctx, box in
                let w = box.width
                let traces = Circuit.rows.map { Circuit.points($0) }
                var lines = Path()
                for pts in traces { lines.addLines(pts.map { CGPoint(x: $0.x * w, y: $0.y * w) }) }
                ctx.stroke(lines, with: .color(mark), style: StrokeStyle(lineWidth: w * Circuit.trace, lineCap: .round, lineJoin: .round))
                if state == .working {
                    // One signal per trace, a third of a lap apart, sinking into the core.
                    for (i, pts) in traces.enumerated() {
                        let f = (t / Circuit.lap + Double(i) / 3).truncatingRemainder(dividingBy: 1)
                        let p = Circuit.point(on: pts, at: f)
                        let r = w * Circuit.trace * 0.6
                        ctx.fill(Path(ellipseIn: CGRect(x: p.x * w - r, y: p.y * w - r, width: r * 2, height: r * 2)),
                                 with: .color(accent.mix(with: .white, by: 0.75)))
                    }
                }
                var solid = Path()
                for row in Circuit.rows {
                    solid.addEllipse(in: Circuit.circle(CGPoint(x: Circuit.padX, y: row), Circuit.padRadius, w))
                }
                solid.addEllipse(in: Circuit.circle(Circuit.core, Circuit.coreRadius, w))
                ctx.fill(solid, with: .color(mark))
            }
            circuitEyes(t, color: ground)
                .offset(x: size * (Circuit.core.x - 0.5), y: size * (Circuit.core.y - 0.5))
            if state == .thinking {
                bubble(t).offset(x: size * 0.25, y: -size * 0.3)
            }
        }
        .overlay(Circle().strokeBorder(accent.opacity(0.35), lineWidth: max(0.5, size * 0.02)))
        .overlay { needsYouRing(t) }
    }

    /// The core's two eyes, cut out in the disc's color: they blink when idle, look up while thinking, watch the
    /// traces while working, and close when offline.
    private func circuitEyes(_ t: TimeInterval, color: Color) -> some View {
        HStack(spacing: size * 0.064) {
            ForEach(0..<2, id: \.self) { _ in
                Capsule()
                    .fill(color)
                    .frame(width: size * 0.044, height: state == .offline ? size * 0.025 : size * 0.125)
            }
        }
        .offset(circuitLook(t))
        .modifier(Blink(animation: state == .idle ? blink : nil))
    }

    /// Where 汇流's eyes look at time t: toward the traces while working, up and right while thinking.
    private func circuitLook(_ t: TimeInterval) -> CGSize {
        let sway: CGFloat = moving ? sin(t * (state == .working ? 1.6 : 0.9)) : 0
        switch state {
        case .working: return CGSize(width: size * (sway * 0.012 - 0.025), height: 0)
        case .thinking: return CGSize(width: size * (sway * 0.008 + 0.02), height: -size * 0.035)
        default: return .zero
        }
    }

    // MARK: 面罩: an accent disc with a dark visor and two bright eyes, a glint on top.

    private var visor: some View {
        let t = time
        return ZStack {
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
                Image(systemName: mark)
                    .font(.system(size: size * 0.2, weight: .bold))
                    .foregroundStyle(.white.opacity(state == .offline ? 0.5 : 0.92))
                    .offset(x: size * 0.25, y: -size * 0.27)
            }
        }
        .overlay(Circle().strokeBorder(.white.opacity(0.18), lineWidth: max(0.5, size * 0.015)))
        .overlay { needsYouRing(t) }
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
        .modifier(Blink(animation: state == .idle ? blink : nil))
    }

    private var eye: some View {
        Capsule()
            .fill(state == .offline ? Color(white: 0.5) : accent.mix(with: .white, by: 0.55))
            .frame(width: size * 0.085, height: state == .offline ? size * 0.03 : size * 0.13)
            .shadow(color: state == .offline ? .clear : accent.opacity(0.9), radius: size * 0.05)
    }

    /// 需要你's red ring, breathing while moving.
    @ViewBuilder private func needsYouRing(_ t: TimeInterval) -> some View {
        if state == .needsYou {
            let pulse = moving ? 0.55 + 0.45 * cos(t * 3) : 1
            Circle().strokeBorder(needsYou.opacity(pulse), lineWidth: max(1.5, size * 0.07))
        }
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
    /// phase animation. No animation: no blink.
    private struct Blink: ViewModifier {
        var animation: Animation?

        func body(content: Content) -> some View {
            if let animation {
                TimelineView(.periodic(from: .now, by: 3.6)) { tl in
                    content.phaseAnimator([1.0, 0.12, 1.0], trigger: tl.date) { v, open in
                        v.scaleEffect(x: 1, y: open, anchor: .center)
                    } animation: { _ in animation }
                }
            } else {
                content
            }
        }
    }
}

/// 汇流's geometry in fractions of the avatar's side, 0…1 from the top left: pads on the left at three rows, the
/// outer traces turning at exactly 45° into the core's center.
nonisolated private enum Circuit {
    static let rows: [CGFloat] = [0.31, 0.5, 0.69]
    static let padX: CGFloat = 0.22
    static let padRadius: CGFloat = 0.065
    static let core = CGPoint(x: 0.64, y: 0.5)
    static let coreRadius: CGFloat = 0.19
    static let trace: CGFloat = 0.056
    /// Seconds for a signal to run from its pad into the core.
    static let lap: Double = 1.4

    /// A trace from its pad into the core: straight, or level and then down (or up) at 45°.
    static func points(_ row: CGFloat) -> [CGPoint] {
        let turn = core.x - abs(core.y - row)
        let pad = CGPoint(x: padX, y: row)
        return turn < core.x ? [pad, CGPoint(x: turn, y: row), core] : [pad, core]
    }

    /// The point a fraction f of the way along a trace, by length.
    static func point(on pts: [CGPoint], at f: Double) -> CGPoint {
        let legs = zip(pts, pts.dropFirst()).map { a, b in (a, b, hypot(b.x - a.x, b.y - a.y)) }
        var left = CGFloat(f) * legs.reduce(0) { $0 + $1.2 }
        for (a, b, len) in legs {
            if left <= len {
                let k = len > 0 ? left / len : 0
                return CGPoint(x: a.x + (b.x - a.x) * k, y: a.y + (b.y - a.y) * k)
            }
            left -= len
        }
        return pts.last ?? core
    }

    /// A circle around a unit point, in a box of side w.
    static func circle(_ c: CGPoint, _ r: CGFloat, _ w: CGFloat) -> CGRect {
        CGRect(x: (c.x - r) * w, y: (c.y - r) * w, width: r * 2 * w, height: r * 2 * w)
    }
}
