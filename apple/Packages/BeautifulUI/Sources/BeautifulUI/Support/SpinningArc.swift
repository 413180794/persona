import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

/// An arc of a circle turning forever, over an optional still track: the spinners' moving part.
///
/// On iOS it is a Core Animation layer: the render server turns it, the main thread does nothing per frame, and the app
/// still counts as idle (a SwiftUI `repeatForever` keeps an animation in flight, so XCUITest waits out its idle timeout
/// on every step while one is on screen). Elsewhere it is the SwiftUI rotation.
struct SpinningArc: View {
    var track: Color?
    var head: Color
    var lineWidth: CGFloat
    var period: Double
    /// The arc's part of the circle, clockwise from three o'clock (as `Shape.trim`).
    var from: CGFloat
    var to: CGFloat
    var roundCap = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        #if canImport(UIKit)
        ArcLayer(track: track, head: head, lineWidth: lineWidth, period: period, from: from, to: to, roundCap: roundCap,
                 moving: !reduceMotion)
        #else
        SwiftUIArc(track: track, head: head, lineWidth: lineWidth, period: period, from: from, to: to, roundCap: roundCap,
                   moving: !reduceMotion)
        #endif
    }
}

#if canImport(UIKit)
private struct ArcLayer: UIViewRepresentable {
    var track: Color?
    var head: Color
    var lineWidth: CGFloat
    var period: Double
    var from: CGFloat
    var to: CGFloat
    var roundCap: Bool
    var moving: Bool

    func makeUIView(context: Context) -> ArcView { ArcView() }

    func updateUIView(_ view: ArcView, context: Context) {
        // Resolved here, in the view's environment: a layer's CGColor has no light / dark of its own.
        view.trackColor = track.map { UIColor(cgColor: $0.resolve(in: context.environment).cgColor) }
        view.headColor = UIColor(cgColor: head.resolve(in: context.environment).cgColor)
        view.lineWidth = lineWidth
        view.period = period
        view.arc = (from, to)
        view.roundCap = roundCap
        view.moving = moving
        view.refresh()
    }
}

private final class ArcView: UIView {
    private let trackLayer = CAShapeLayer()
    private let headLayer = CAShapeLayer()
    var trackColor: UIColor?
    var headColor: UIColor = .label
    var lineWidth: CGFloat = 1.5
    var period: Double = 0.7
    var arc: (CGFloat, CGFloat) = (0, 0.25)
    var roundCap = false
    var moving = true

    override init(frame: CGRect) {
        super.init(frame: frame)
        isUserInteractionEnabled = false
        backgroundColor = .clear
        for l in [trackLayer, headLayer] {
            l.fillColor = UIColor.clear.cgColor
            layer.addSublayer(l)
        }
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not used") }

    override func layoutSubviews() {
        super.layoutSubviews()
        refresh()
    }

    /// Core Animation drops a layer's animations when its view leaves the window; a row scrolled back in turns again.
    override func didMoveToWindow() {
        super.didMoveToWindow()
        refresh()
    }

    func refresh() {
        let side = min(bounds.width, bounds.height)
        guard side > 0 else { return }
        let rect = CGRect(x: (bounds.width - side) / 2, y: (bounds.height - side) / 2, width: side, height: side)
        let path = UIBezierPath(arcCenter: CGPoint(x: side / 2, y: side / 2), radius: max(0, (side - lineWidth) / 2),
                                startAngle: 0, endAngle: .pi * 2, clockwise: true).cgPath
        for l in [trackLayer, headLayer] {
            l.frame = rect
            l.path = path
            l.lineWidth = lineWidth
        }
        trackLayer.strokeColor = (trackColor ?? .clear).cgColor
        headLayer.strokeColor = headColor.cgColor
        headLayer.lineCap = roundCap ? .round : .butt
        headLayer.strokeStart = arc.0
        headLayer.strokeEnd = arc.1
        if moving && window != nil {
            if headLayer.animation(forKey: "spin") == nil {
                let spin = CABasicAnimation(keyPath: "transform.rotation.z")
                spin.fromValue = 0
                spin.toValue = Double.pi * 2
                spin.duration = period
                spin.repeatCount = .infinity
                spin.isRemovedOnCompletion = false
                headLayer.add(spin, forKey: "spin")
            }
        } else {
            headLayer.removeAnimation(forKey: "spin")
        }
    }
}
#endif

/// The SwiftUI rotation, for platforms without UIKit.
private struct SwiftUIArc: View {
    var track: Color?
    var head: Color
    var lineWidth: CGFloat
    var period: Double
    var from: CGFloat
    var to: CGFloat
    var roundCap: Bool
    var moving: Bool
    @State private var turning = false

    var body: some View {
        ZStack {
            if let track { Circle().inset(by: lineWidth / 2).stroke(track, lineWidth: lineWidth) }
            Circle()
                .inset(by: lineWidth / 2)
                .trim(from: from, to: to)
                .stroke(head, style: StrokeStyle(lineWidth: lineWidth, lineCap: roundCap ? .round : .butt))
                .rotationEffect(.degrees(turning ? 360 : 0))
        }
        .onAppear {
            guard moving else { return }
            withAnimation(.linear(duration: period).repeatForever(autoreverses: false)) { turning = true }
        }
    }
}

/// The shimmer's bright band sweeping left to right across its frame forever (`buiShimmer`): on iOS a Core Animation
/// gradient layer, like `SpinningArc` and for the same reasons.
struct SweepBand: View {
    var color: Color
    var duration: Double

    var body: some View {
        #if canImport(UIKit)
        SweepLayer(color: color, duration: duration)
        #else
        SwiftUISweep(color: color, duration: duration)
        #endif
    }
}

#if canImport(UIKit)
private struct SweepLayer: UIViewRepresentable {
    var color: Color
    var duration: Double

    func makeUIView(context: Context) -> SweepView { SweepView() }

    func updateUIView(_ view: SweepView, context: Context) {
        view.color = UIColor(cgColor: color.resolve(in: context.environment).cgColor)
        view.duration = duration
        view.refresh()
    }
}

private final class SweepView: UIView {
    private let band = CAGradientLayer()
    var color: UIColor = .label
    var duration: Double = 1.4

    override init(frame: CGRect) {
        super.init(frame: frame)
        isUserInteractionEnabled = false
        backgroundColor = .clear
        band.startPoint = CGPoint(x: 0, y: 0.5)
        band.endPoint = CGPoint(x: 1, y: 0.5)
        band.locations = [0.35, 0.5, 0.65]
        layer.addSublayer(band)
        clipsToBounds = true
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not used") }

    override func layoutSubviews() {
        super.layoutSubviews()
        refresh()
    }

    override func didMoveToWindow() {
        super.didMoveToWindow()
        refresh()
    }

    /// The band is twice the width; its centre runs from -0.5 w to 1.5 w (the site's background-position 150 % → -50 %).
    func refresh() {
        let w = bounds.width
        guard w > 0 else { return }
        band.colors = [color.withAlphaComponent(0).cgColor, color.cgColor, color.withAlphaComponent(0).cgColor]
        band.bounds = CGRect(x: 0, y: 0, width: w * 2, height: bounds.height)
        band.position = CGPoint(x: -0.5 * w, y: bounds.height / 2)
        band.removeAnimation(forKey: "sweep")
        guard window != nil else { return }
        let sweep = CABasicAnimation(keyPath: "position.x")
        sweep.fromValue = -0.5 * w
        sweep.toValue = 1.5 * w
        sweep.duration = duration
        sweep.repeatCount = .infinity
        sweep.isRemovedOnCompletion = false
        band.add(sweep, forKey: "sweep")
    }
}
#endif

private struct SwiftUISweep: View {
    var color: Color
    var duration: Double
    @State private var phase: CGFloat = 0

    var body: some View {
        GeometryReader { proxy in
            LinearGradient(stops: [.init(color: color.opacity(0), location: 0.35), .init(color: color, location: 0.5),
                                   .init(color: color.opacity(0), location: 0.65)],
                           startPoint: .leading, endPoint: .trailing)
                .frame(width: proxy.size.width * 2)
                .offset(x: proxy.size.width * (phase * 2 - 1.5))
        }
        .onAppear { withAnimation(.linear(duration: duration).repeatForever(autoreverses: false)) { phase = 1 } }
    }
}
