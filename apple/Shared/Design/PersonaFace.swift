import SwiftUI

/// What the persona's dot shows (docs/persona.md「状态」). Raw values are persona.json's.
nonisolated enum PersonaState: String, CaseIterable, Identifiable, Sendable {
    case idle, thinking, working
    case needsYou = "needs_you"
    case offline

    var id: String { rawValue }
    var title: String {
        switch self {
        case .idle: String(localized: "Idle")
        case .thinking: String(localized: "Thinking [Persona Face 43ce45]")
        case .working: String(localized: "Working [Assistant Profile View f42082]")
        case .needsYou: String(localized: "Waiting for you [Persona Face 66ea80]")
        case .offline: String(localized: "Offline [Machine Entity be1b4f]")
        }
    }
}

/// The built-in personas' faces (Mochi, Visor, Circuit, Pixel, Facet, Jelly) at one moment: everything PersonaAvatar draws for them, without the clock.
/// Shared with the widget extension, which draws the Live Activity (docs/live-activity.md「形象」) and cannot run
/// timelines: it passes `moving: false` and gets each state's still frame (the same one Reduce Motion shows). The app
/// wraps it in a TimelineView and passes the time (PersonaAvatar). Reads no app-only token: the tint, the brand mark and
/// the blink come in from the caller.
struct PersonaFace: View {
    enum Design: String, Sendable {
        /// Mochi, the default: the app icon's character in the icon's own colors (not the tint).
        case mochi
        case visor, circuit
        /// Pixel, Facet, Jelly (#350): an 8-bit face, a low-poly folded head, a jelly; in the accent like Visor and Circuit.
        case pixel, facet, blob
    }

    var size: CGFloat
    var design: Design
    var state: PersonaState
    /// The accent it is drawn in (offline turns it gray).
    var tint: Color
    /// The SF Symbol of the brand's star on Visor (`Theme.Icon.persona`).
    var mark: String
    /// Seconds (any epoch) for the moving states; ignored when not `moving`.
    var time: TimeInterval = 0
    /// The thinking / working / needs-you motion runs; false: each state's still frame.
    var moving = false
    /// The idle blink every few seconds (`Theme.Motion.blink`); nil: eyes stay open.
    var blink: Animation?

    /// Needs You's ring: this share of the face's side, never thinner than `ringMin`. A package's picture with no frame for
    /// the state gets the same ring on the activity (FleetFace), and offline makes it gray at this opacity.
    static let ringShare: CGFloat = 0.07
    static let ringMin: CGFloat = 1.5
    static let offlineOpacity = 0.7

    @Environment(\.colorScheme) private var scheme

    private var accent: Color { state == .offline ? Color(white: 0.62) : tint }
    private var needsYou: Color { StatusPalette.needsYou.dot }

    var body: some View {
        switch design {
        case .mochi: mochi
        case .circuit: circuit
        case .visor: visor
        case .pixel: pixel
        case .facet: facet
        case .blob: blob
        }
    }

    // MARK: Mochi: the app icon's white blob waving on its paper disc (a dark one in the dark), outlined in ink, with
    // its orange cheek. It ignores the tint: the icon's colors are what make it the icon's character.

    private var mochi: some View {
        let t = time
        let ink = MochiInk(dark: scheme == .dark, offline: state == .offline)
        let compact = size < Mochi.compactBelow
        return ZStack {
            Circle().fill(LinearGradient(colors: ink.disc, startPoint: .top, endPoint: .bottom))
            if state == .working { mochiArc(t) }
            ZStack {
                Canvas { ctx, box in Mochi.draw(&ctx, side: box.width, ink: ink, compact: compact, lines: mochiLines(t)) }
                if state != .offline { mochiEyes(t, ink: ink, compact: compact) }
            }
            .rotationEffect(.degrees(mochiRock(t)), anchor: UnitPoint(x: Mochi.pivot.x, y: Mochi.pivot.y))
            .offset(y: mochiBob(t))
            if state == .thinking {
                bubble(t, lit: Mochi.cheekColor).offset(x: size * 0.25, y: -size * 0.29)
            }
        }
        .frame(width: size, height: size)
        .clipShape(.circle)
        .overlay { needsYouRing(t) }
    }

    /// The motion lines beside the hand: steady when idle and needs-you, lit one after another while working, none
    /// while thinking (the bubble is there) or offline.
    private func mochiLines(_ t: TimeInterval) -> [Double] {
        switch state {
        case .idle, .needsYou: return [1, 1, 1]
        case .working:
            guard moving else { return [1, 0.35, 0.35] }
            let lit = Int(t * 4) % 3
            return (0..<3).map { $0 == lit ? 1 : 0.35 }
        case .thinking, .offline: return []
        }
    }

    /// The eyes: they blink when idle, look up at the bubble while thinking, and scan while working.
    private func mochiEyes(_ t: TimeInterval, ink: MochiInk, compact: Bool) -> some View {
        let r = Mochi.eyeRadius * (compact ? Mochi.compactFeatures : 1) * size
        let look: CGSize = switch state {
        case .thinking: CGSize(width: size * 0.022, height: -size * 0.03)
        case .working: CGSize(width: size * (moving ? sin(t * 1.8) * 0.028 : -0.024), height: size * 0.012)
        default: .zero
        }
        return ZStack {
            ForEach(Mochi.eyes.indices, id: \.self) { i in
                Circle().fill(ink.line).frame(width: r * 2, height: r * 2)
                    .position(x: Mochi.eyes[i].x * size, y: Mochi.eyes[i].y * size)
            }
        }
        .frame(width: size, height: size)
        .modifier(Blink(animation: state == .idle ? blink : nil, anchor: UnitPoint(x: 0.5, y: Mochi.eyeLine)))
        .offset(look)
    }

    /// Needs You waves: the body rocks on its bottom like the icon's hello.
    private func mochiRock(_ t: TimeInterval) -> Double {
        state == .needsYou && moving ? sin(t * 7) * 5 : 0
    }

    /// Working bobs a little.
    private func mochiBob(_ t: TimeInterval) -> CGFloat {
        state == .working && moving ? CGFloat(abs(sin(t * 5))) * -size * 0.02 : 0
    }

    /// Working's arc of the cheek's orange running around the rim.
    private func mochiArc(_ t: TimeInterval) -> some View {
        let turn = moving ? (t / 1.6).truncatingRemainder(dividingBy: 1) * 360 : 300
        let w = max(Self.ringMin, size * Self.ringShare)
        return Circle()
            .trim(from: 0, to: 0.28)
            .stroke(Mochi.cheekColor, style: StrokeStyle(lineWidth: w, lineCap: .round))
            .padding(w / 2)
            .rotationEffect(.degrees(turn - 90))
    }

    // MARK: Circuit: three pads on the left whose traces run into a core with two eyes (your computers' agents coming
    // together in Persona), in the accent on a dark disc of it.

    private var circuit: some View {
        let ground = accent.blended(with: .black, by: 0.82)
        let mark = accent.blended(with: .white, by: 0.15)
        let t = time
        return ZStack {
            Circle().fill(LinearGradient(colors: [accent.blended(with: .black, by: 0.76), accent.blended(with: .black, by: 0.88)],
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
                                 with: .color(accent.blended(with: .white, by: 0.75)))
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
                bubble(t, lit: accent).offset(x: size * 0.25, y: -size * 0.3)
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

    /// Where Circuit's eyes look at time t: toward the traces while working, up and right while thinking.
    private func circuitLook(_ t: TimeInterval) -> CGSize {
        let sway: CGFloat = moving ? sin(t * (state == .working ? 1.6 : 0.9)) : 0
        switch state {
        case .working: return CGSize(width: size * (sway * 0.012 - 0.025), height: 0)
        case .thinking: return CGSize(width: size * (sway * 0.008 + 0.02), height: -size * 0.035)
        default: return .zero
        }
    }

    // MARK: Visor: an accent disc with a dark visor and two bright eyes, a glint on top.

    private var visor: some View {
        let t = time
        return ZStack {
            Circle().fill(LinearGradient(colors: [accent.blended(with: .white, by: 0.28), accent, accent.blended(with: .black, by: 0.18)],
                                         startPoint: .topLeading, endPoint: .bottomTrailing))
            Capsule()
                .fill(LinearGradient(colors: [Color(white: 0.16), Color(white: 0.05)], startPoint: .top, endPoint: .bottom))
                .frame(width: size * 0.66, height: size * 0.30)
                .overlay(Capsule().strokeBorder(.white.opacity(0.14), lineWidth: max(0.5, size * 0.012)))
                .overlay { eyes(t) }
                .offset(y: size * 0.02)
            if state == .thinking {
                bubble(t, lit: accent).offset(x: size * 0.25, y: -size * 0.3)
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
            .fill(state == .offline ? Color(white: 0.5) : accent.blended(with: .white, by: 0.55))
            .frame(width: size * 0.085, height: state == .offline ? size * 0.03 : size * 0.13)
            .shadow(color: state == .offline ? .clear : accent.opacity(0.9), radius: size * 0.05)
    }

    // MARK: Pixel: an 8-bit face on a dark disc of the accent. The eyes step a cell at a time; while working the mouth's
    // cells light up in turn like a loading bar.

    private var pixel: some View {
        let t = time
        let lit = accent.blended(with: .white, by: 0.35)
        let dim = accent.blended(with: .black, by: 0.55)
        return ZStack {
            Circle().fill(LinearGradient(colors: [accent.blended(with: .black, by: 0.78), accent.blended(with: .black, by: 0.9)],
                                         startPoint: .top, endPoint: .bottom))
            Canvas { ctx, box in
                let cell = box.width * Pixel.cell
                let origin = (box.width - cell * 8) / 2
                func square(_ col: Int, _ row: Int, _ color: Color) {
                    ctx.fill(Path(CGRect(x: origin + CGFloat(col) * cell, y: origin + CGFloat(row) * cell, width: cell * 0.9, height: cell * 0.9)),
                             with: .color(color))
                }
                for (col, row) in Pixel.frame { square(col, row, dim) }
                let mouth = pixelMouth(t)
                for (i, col) in Pixel.mouth.enumerated() { square(col, 6, mouth[i] ? lit : dim) }
            }
            pixelEyes(t, color: lit)
            if state == .thinking {
                bubble(t, lit: accent).offset(x: size * 0.25, y: -size * 0.3)
            }
        }
        .overlay(Circle().strokeBorder(accent.opacity(0.35), lineWidth: max(0.5, size * 0.02)))
        .overlay { needsYouRing(t) }
    }

    /// Which of the mouth's four cells are lit: the middle two at rest, one more each beat while working, none when
    /// offline.
    private func pixelMouth(_ t: TimeInterval) -> [Bool] {
        switch state {
        case .working:
            let n = moving ? Int(t * 5) % 5 : 4
            return (0..<4).map { $0 < n }
        case .offline: return [false, false, false, false]
        default: return [false, true, true, false]
        }
    }

    /// The eyes, two by two cells (one row when offline): a cell to the side and back while working, up and to the
    /// right while thinking, a blink when idle.
    private func pixelEyes(_ t: TimeInterval, color: Color) -> some View {
        let cell = size * Pixel.cell
        let step: CGFloat = switch state {
        case .working: moving ? CGFloat([0, 1, 0, -1][Int(t * 2.5) % 4]) : 0
        case .thinking: 1
        default: 0
        }
        let up: CGFloat = state == .thinking ? -1 : 0
        let tall = state == .offline ? 0.45 : 1.9
        return HStack(spacing: cell * 2) {
            ForEach(0..<2, id: \.self) { _ in
                Rectangle().fill(color).frame(width: cell * 1.9, height: cell * tall)
            }
        }
        .offset(x: step * cell * 0.5, y: (Pixel.eyeRow - 3.5 + up * 0.5) * cell + (state == .offline ? cell * 0.7 : 0))
        .modifier(Blink(animation: state == .idle ? blink : nil))
    }

    // MARK: Facet: a low-poly head folded from facets of the accent, slanted eyes. While working the facets' shading moves
    // round like light sweeping over it.

    private var facet: some View {
        let t = time
        return ZStack {
            Circle().fill(accent.blended(with: .black, by: 0.35))
            Canvas { ctx, box in
                let w = box.width
                let c = CGPoint(x: Facet.center.x * w, y: Facet.center.y * w)
                let n = Facet.rim.count
                for i in 0..<n {
                    let a = Facet.rim[i], b = Facet.rim[(i + 1) % n]
                    var tri = Path()
                    tri.move(to: c)
                    tri.addLine(to: CGPoint(x: a.x * w, y: a.y * w))
                    tri.addLine(to: CGPoint(x: b.x * w, y: b.y * w))
                    tri.closeSubpath()
                    ctx.fill(tri, with: .color(facetShade(i, of: n, t)))
                    ctx.stroke(tri, with: .color(.white.opacity(0.12)), lineWidth: max(0.5, w * 0.006))
                }
            }
            facetEyes(t)
            if state == .thinking {
                bubble(t, lit: accent).offset(x: size * 0.27, y: -size * 0.31)
            }
        }
        .clipShape(.circle)
        .overlay(Circle().strokeBorder(.white.opacity(0.18), lineWidth: max(0.5, size * 0.015)))
        .overlay { needsYouRing(t) }
    }

    /// A facet's color: lighter toward the top left as if lit from there; while working the light goes round.
    private func facetShade(_ i: Int, of n: Int, _ t: TimeInterval) -> Color {
        let angle = Double(i) / Double(n) * 2 * .pi
        let sweep = state == .working && moving ? t * 2.4 : 0
        let light = cos(angle - 3.9 - sweep) // 3.9 rad: the top left
        let k = 0.18 * light
        return k >= 0 ? accent.blended(with: .white, by: k) : accent.blended(with: .black, by: -k)
    }

    /// Two slanted eyes in the dark: they blink when idle, look up while thinking, glance aside while working, and are
    /// thin lines when offline.
    private func facetEyes(_ t: TimeInterval) -> some View {
        let look: CGSize = switch state {
        case .working: CGSize(width: moving ? sin(t * 1.6) * size * 0.03 : 0, height: 0)
        case .thinking: CGSize(width: size * 0.03, height: -size * 0.04)
        default: .zero
        }
        let tall = state == .offline ? 0.025 : 0.1
        return HStack(spacing: size * 0.14) {
            ForEach(0..<2, id: \.self) { i in
                FacetEye(slant: i == 0 ? 1 : -1)
                    .fill(Color(white: 0.08).opacity(state == .offline ? 0.6 : 0.9))
                    .frame(width: size * 0.13, height: size * tall)
            }
        }
        .offset(x: look.width, y: size * 0.02 + look.height)
        .modifier(Blink(animation: state == .idle ? blink : nil))
    }

    /// An eye as a parallelogram leaning toward the nose.
    nonisolated private struct FacetEye: Shape {
        var slant: CGFloat
        func path(in r: CGRect) -> Path {
            let lean = r.height * 0.6 * slant
            var p = Path()
            p.move(to: CGPoint(x: r.minX + max(0, lean), y: r.minY))
            p.addLine(to: CGPoint(x: r.maxX + min(0, lean), y: r.minY))
            p.addLine(to: CGPoint(x: r.maxX - max(0, lean), y: r.maxY))
            p.addLine(to: CGPoint(x: r.minX - min(0, lean), y: r.maxY))
            p.closeSubpath()
            return p
        }
    }

    // MARK: Jelly: a soft jelly of the accent on a pale disc, big eyes and pink cheeks. While working it squashes and
    // springs back, its edge wobbling.

    private var blob: some View {
        let t = time
        let squash = state == .working && moving ? CGFloat(max(0, sin(t * 5))) * 0.09 : 0
        let wobble: CGFloat = (state == .working || state == .needsYou) && moving ? 0.035 : 0
        return ZStack {
            Circle().fill(LinearGradient(colors: scheme == .dark
                                         ? [accent.blended(with: .black, by: 0.7), accent.blended(with: .black, by: 0.82)]
                                         : [accent.blended(with: .white, by: 0.86), accent.blended(with: .white, by: 0.74)],
                                         startPoint: .top, endPoint: .bottom))
            ZStack {
                BlobBody(phase: t * 4, wobble: wobble)
                    .fill(LinearGradient(colors: [accent.blended(with: .white, by: 0.3), accent, accent.blended(with: .black, by: 0.15)],
                                         startPoint: .topLeading, endPoint: .bottom))
                    .overlay(alignment: .topLeading) {
                        Capsule().fill(.white.opacity(0.45))
                            .frame(width: size * 0.12, height: size * 0.06)
                            .rotationEffect(.degrees(-30))
                            .offset(x: size * 0.18, y: size * 0.24)
                    }
                blobFace(t)
            }
            .scaleEffect(x: 1 + squash, y: 1 - squash, anchor: .bottom)
            if state == .thinking {
                bubble(t, lit: accent).offset(x: size * 0.26, y: -size * 0.32)
            }
        }
        .clipShape(.circle)
        .overlay { needsYouRing(t) }
    }

    /// Big round eyes with pupils (closed curves when offline) and the cheeks.
    private func blobFace(_ t: TimeInterval) -> some View {
        let look: CGSize = switch state {
        case .working: CGSize(width: moving ? sin(t * 1.8) * size * 0.02 : 0, height: size * 0.01)
        case .thinking: CGSize(width: size * 0.018, height: -size * 0.025)
        default: .zero
        }
        let eye = size * 0.15
        return ZStack {
            HStack(spacing: size * 0.09) {
                ForEach(0..<2, id: \.self) { _ in
                    if state == .offline {
                        Capsule().fill(Color(white: 0.2)).frame(width: eye * 0.8, height: max(1, size * 0.02))
                    } else {
                        Circle().fill(.white)
                            .frame(width: eye, height: eye)
                            .overlay {
                                Circle().fill(Color(white: 0.1)).frame(width: eye * 0.55, height: eye * 0.55)
                                    .overlay(alignment: .topTrailing) {
                                        Circle().fill(.white).frame(width: eye * 0.18, height: eye * 0.18).offset(x: -eye * 0.06, y: eye * 0.06)
                                    }
                                    .offset(look)
                            }
                    }
                }
            }
            .modifier(Blink(animation: state == .idle ? blink : nil))
            .offset(y: size * 0.04)
            HStack(spacing: size * 0.3) {
                ForEach(0..<2, id: \.self) { _ in
                    Ellipse().fill(Color(hex: 0xFF8FA3).opacity(state == .offline ? 0.3 : 0.75))
                        .frame(width: size * 0.09, height: size * 0.05)
                }
            }
            .offset(y: size * 0.16)
        }
    }

    /// The jelly's outline: a rounded dome with a flat-ish bottom, its edge rippling by `wobble` of the side.
    nonisolated private struct BlobBody: Shape {
        var phase: Double
        var wobble: CGFloat
        func path(in r: CGRect) -> Path {
            let c = CGPoint(x: r.midX, y: r.midY + r.height * 0.06)
            let rx = r.width * 0.36, ry = r.height * 0.32
            var p = Path()
            let steps = 48
            for i in 0...steps {
                let a = Double(i) / Double(steps) * 2 * .pi
                let ripple = 1 + wobble * CGFloat(sin(a * 3 + phase))
                // flatter below: the bottom half is squeezed toward the base line
                let y = sin(a) > 0 ? CGFloat(sin(a)) * ry * 0.82 : CGFloat(sin(a)) * ry
                let pt = CGPoint(x: c.x + CGFloat(cos(a)) * rx * ripple, y: c.y + y * ripple)
                if i == 0 { p.move(to: pt) } else { p.addLine(to: pt) }
            }
            p.closeSubpath()
            return p
        }
    }

    /// Needs You's red ring, breathing while moving.
    @ViewBuilder private func needsYouRing(_ t: TimeInterval) -> some View {
        if state == .needsYou {
            let pulse = moving ? 0.55 + 0.45 * cos(t * 3) : 1
            Circle().strokeBorder(needsYou.opacity(pulse), lineWidth: max(Self.ringMin, size * Self.ringShare))
        }
    }

    /// A small thought bubble whose three dots light up in turn.
    private func bubble(_ t: TimeInterval, lit color: Color) -> some View {
        let lit = moving ? Int(t * 2.5) % 4 : 3
        return HStack(spacing: size * 0.025) {
            ForEach(0..<3) { i in
                Circle()
                    .fill(i == lit || lit == 3 ? color : Color(white: 0.7))
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
        var anchor: UnitPoint = .center

        func body(content: Content) -> some View {
            if let animation {
                TimelineView(.periodic(from: .now, by: 3.6)) { tl in
                    content.phaseAnimator([1.0, 0.12, 1.0], trigger: tl.date) { v, open in
                        v.scaleEffect(x: 1, y: open, anchor: anchor)
                    } animation: { _ in animation }
                }
            } else {
                content
            }
        }
    }
}

/// Mochi's colors in one appearance: the icon's paper (or its dark) behind, a white body outlined in ink. Offline
/// fades the body to gray.
nonisolated struct MochiInk {
    var disc: [Color]
    var body: [Color]
    var line: Color
    /// The motion lines: ink on the paper, light gray on the dark disc (as in the dark icon).
    var motion: Color
    var outline: Color
    var gloss: Color
    var cheek: Color
    var shadow: Color
    /// Asleep: closed eyes, drawn with the body.
    var offline: Bool

    init(dark: Bool, offline: Bool) {
        self.offline = offline
        disc = dark ? [Color(hex: 0x332A26), Color(hex: 0x1D1816)] : [Color(hex: 0xF4EADF), Color(hex: 0xE7D7C4)]
        body = offline ? [Color(hex: 0xD9D4D0), Color(hex: 0xBDB6B1)] : [.white, Color(hex: 0xE9E3DE)]
        line = offline ? Color(hex: 0x6E6763) : Color(hex: 0x1D1816)
        motion = dark ? Color(hex: 0xD6D1CD) : line
        // The dark icon keeps the ink outline: on the dark disc it reads as the body's shadow edge.
        outline = offline ? Color(hex: 0x6E6763) : Color(hex: dark ? 0x0E0B0A : 0x1D1816)
        gloss = .white
        cheek = offline ? Color(hex: 0xA59D98) : Mochi.cheekColor
        shadow = .black.opacity(dark ? 0.45 : 0.14)
    }
}

/// Mochi's geometry in fractions of the avatar's side, 0…1 from the top left, traced from the 1024 px app icon
/// (`p` maps icon pixels into the disc, a little larger than the icon's square so it fills the circle).
nonisolated enum Mochi {
    static let scale: CGFloat = 1.16
    static let origin = CGPoint(x: 545, y: 548)
    static let cheekColor = Color(hex: 0xF2703C)
    /// Below this many points the motion lines and the gloss go, and the eyes and cheek grow, so the face still reads.
    static let compactBelow: CGFloat = 36
    static let compactFeatures: CGFloat = 1.3

    static func p(_ x: CGFloat, _ y: CGFloat) -> CGPoint {
        CGPoint(x: 0.5 + (x - origin.x) / 1024 * scale, y: 0.5 + (y - origin.y) / 1024 * scale)
    }

    static let eyes = [p(397, 497), p(553, 480)]
    static var eyeLine: CGFloat { (eyes[0].y + eyes[1].y) / 2 }
    static let eyeRadius: CGFloat = 29 / 1024 * scale
    static let cheek = p(312, 556)
    static let cheekRadius: CGFloat = 37 / 1024 * scale
    /// The outline, as a share of the side; never thinner than `strokeMin` points.
    static let stroke: CGFloat = 30 / 1024 * scale
    static let strokeMin: CGFloat = 1.3
    /// Where the body rocks from (its bottom middle).
    static let pivot = p(500, 780)
    /// The three lines beside the hand.
    static let lines: [(CGPoint, CGPoint)] = [
        (p(792, 352), p(818, 298)),
        (p(838, 384), p(884, 340)),
        (p(864, 428), p(924, 408)),
    ]

    static func body(_ w: CGFloat) -> Path {
        func q(_ x: CGFloat, _ y: CGFloat) -> CGPoint { let u = p(x, y); return CGPoint(x: u.x * w, y: u.y * w) }
        var b = Path()
        b.move(to: q(262, 778))
        b.addCurve(to: q(702, 772), control1: q(420, 792), control2: q(600, 790))
        b.addCurve(to: q(760, 640), control1: q(742, 764), control2: q(752, 700))
        b.addCurve(to: q(826, 502), control1: q(800, 598), control2: q(828, 562))
        b.addCurve(to: q(764, 428), control1: q(825, 446), control2: q(795, 418))
        b.addCurve(to: q(712, 494), control1: q(736, 440), control2: q(722, 468))
        b.addCurve(to: q(560, 304), control1: q(690, 440), control2: q(650, 350))
        b.addCurve(to: q(254, 382), control1: q(470, 260), control2: q(330, 288))
        b.addCurve(to: q(168, 672), control1: q(184, 462), control2: q(158, 582))
        b.addCurve(to: q(262, 778), control1: q(176, 742), control2: q(206, 772))
        b.closeSubpath()
        return b
    }

    /// The icon's highlight on the top left of the head.
    static func gloss(_ w: CGFloat) -> Path {
        func q(_ x: CGFloat, _ y: CGFloat) -> CGPoint { let u = p(x, y); return CGPoint(x: u.x * w, y: u.y * w) }
        var g = Path()
        g.move(to: q(312, 380))
        g.addQuadCurve(to: q(412, 334), control: q(352, 346))
        g.move(to: q(288, 414))
        g.addLine(to: q(290, 411))
        return g
    }

    /// Everything but the eyes (views, so they can blink) on a side of `w`: the shadow, the body, the cheek, the
    /// gloss, and the motion lines at the given opacities (none: no lines). Offline closes the eyes here.
    static func draw(_ ctx: inout GraphicsContext, side w: CGFloat, ink: MochiInk, compact: Bool, lines: [Double]) {
        let lw = max(strokeMin, w * stroke)
        ctx.drawLayer { shadowCtx in
            shadowCtx.addFilter(.blur(radius: w * 0.02))
            let c = p(490, 800)
            shadowCtx.fill(Path(ellipseIn: CGRect(x: (c.x - 0.3) * w, y: (c.y - 0.022) * w, width: 0.6 * w, height: 0.044 * w)), with: .color(ink.shadow))
        }
        let body = body(w)
        ctx.fill(body, with: .linearGradient(Gradient(colors: ink.body), startPoint: CGPoint(x: 0.4 * w, y: 0.3 * w), endPoint: CGPoint(x: 0.5 * w, y: 0.82 * w)))
        ctx.stroke(body, with: .color(ink.outline), style: StrokeStyle(lineWidth: lw, lineCap: .round, lineJoin: .round))
        let k: CGFloat = compact ? compactFeatures : 1
        let r = cheekRadius * k
        ctx.fill(Path(ellipseIn: CGRect(x: (cheek.x - r) * w, y: (cheek.y - r * 0.9) * w, width: 2 * r * w, height: 1.8 * r * w)), with: .color(ink.cheek))
        if !compact {
            ctx.stroke(gloss(w), with: .color(ink.gloss), style: StrokeStyle(lineWidth: lw * 0.6, lineCap: .round))
            for (i, (a, b)) in self.lines.enumerated() where i < lines.count {
                var l = Path()
                l.move(to: CGPoint(x: a.x * w, y: a.y * w))
                l.addLine(to: CGPoint(x: b.x * w, y: b.y * w))
                ctx.stroke(l, with: .color(ink.motion.opacity(lines[i])), style: StrokeStyle(lineWidth: lw * 0.85, lineCap: .round))
            }
        }
        if ink.offline {
            // Closed eyes, two short curves.
            for e in eyes {
                var l = Path()
                let half = eyeRadius * k * w
                l.move(to: CGPoint(x: e.x * w - half, y: e.y * w))
                l.addQuadCurve(to: CGPoint(x: e.x * w + half, y: e.y * w), control: CGPoint(x: e.x * w, y: e.y * w + half * 0.7))
                ctx.stroke(l, with: .color(ink.line), style: StrokeStyle(lineWidth: lw * 0.8, lineCap: .round))
            }
        }
    }
}

/// Circuit's geometry in fractions of the avatar's side, 0…1 from the top left: pads on the left at three rows, the
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

/// Pixel's grid: 8 × 8 cells of this share of the side, centred; the face's frame cells, the eyes' row, the mouth's columns.
nonisolated private enum Pixel {
    static let cell: CGFloat = 0.075
    static let eyeRow: CGFloat = 2.5
    static let mouth = [2, 3, 4, 5]
    /// A rounded square of cells: the face's edge.
    static let frame: [(Int, Int)] = {
        var cells: [(Int, Int)] = []
        for i in 1...6 { cells += [(i, 0), (i, 7), (0, i), (7, i)] }
        return cells
    }()
}

/// Facet's facets: triangles fanned from a point a little above the middle to these rim points, 0…1 from the top left.
nonisolated private enum Facet {
    static let center = CGPoint(x: 0.5, y: 0.46)
    static let rim: [CGPoint] = (0..<9).map { i in
        let a = Double(i) / 9 * 2 * .pi - .pi / 2
        let r = i % 2 == 0 ? 0.56 : 0.5
        return CGPoint(x: 0.5 + cos(a) * r, y: 0.5 + sin(a) * r)
    }
}

extension Color {
    /// `mix(with:by:)` is watchOS 11+, and the watch app still installs on watchOS 10 (older watches stop there):
    /// there it blends the resolved components instead.
    nonisolated func blended(with other: Color, by t: Double) -> Color {
        if #available(watchOS 11, *) { return mix(with: other, by: t) }
        let a = resolve(in: EnvironmentValues()), b = other.resolve(in: EnvironmentValues())
        func m(_ x: Float, _ y: Float) -> Double { Double(x) + (Double(y) - Double(x)) * t }
        return Color(.sRGBLinear, red: m(a.red, b.red), green: m(a.green, b.green), blue: m(a.blue, b.blue), opacity: m(a.opacity, b.opacity))
    }
}
