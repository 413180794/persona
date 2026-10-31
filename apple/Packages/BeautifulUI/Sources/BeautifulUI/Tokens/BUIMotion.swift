import SwiftUI

/// A CSS `cubic-bezier(x1, y1, x2, y2)` timing curve that can be sampled directly,
/// for animations driven by `TimelineView` rather than SwiftUI's animation system.
public struct BUICurve: Sendable, Hashable {
    public var x1, y1, x2, y2: Double

    public init(_ x1: Double, _ y1: Double, _ x2: Double, _ y2: Double) {
        self.x1 = x1; self.y1 = y1; self.x2 = x2; self.y2 = y2
    }

    public static let linear = BUICurve(0, 0, 1, 1)
    /// CSS `ease`.
    public static let ease = BUICurve(0.25, 0.1, 0.25, 1)
    /// CSS `ease-in-out`.
    public static let easeInOut = BUICurve(0.42, 0, 0.58, 1)
    /// CSS `ease-out`.
    public static let easeOut = BUICurve(0, 0, 0.58, 1)
    /// `--ease-out` (Tailwind).
    public static let twEaseOut = BUICurve(0, 0, 0.2, 1)
    /// `--ease-in-out` (Tailwind) — the site's default transition curve.
    public static let twEaseInOut = BUICurve(0.4, 0, 0.2, 1)
    /// `--ease-out-strong` — entrances, pop-ins, row reveals.
    public static let outStrong = BUICurve(0.23, 1, 0.32, 1)
    /// `--ease-in-out-strong`.
    public static let inOutStrong = BUICurve(0.77, 0, 0.175, 1)
    /// `--ease-link` — underline and disclosure motion.
    public static let link = BUICurve(0.16, 1, 0.3, 1)

    /// Progress (y) at time fraction `t` in 0...1.
    public func callAsFunction(_ t: Double) -> Double {
        if t <= 0 { return 0 }
        if t >= 1 { return 1 }
        // Solve x(s) = t for s with Newton steps, falling back to bisection.
        var s = t
        for _ in 0..<8 {
            let x = sample(x1, x2, s) - t
            if abs(x) < 1e-6 { return sample(y1, y2, s) }
            let dx = slope(x1, x2, s)
            if abs(dx) < 1e-6 { break }
            s -= x / dx
        }
        var lo = 0.0, hi = 1.0
        s = t
        for _ in 0..<32 {
            let x = sample(x1, x2, s)
            if abs(x - t) < 1e-6 { break }
            if x < t { lo = s } else { hi = s }
            s = (lo + hi) / 2
        }
        return sample(y1, y2, s)
    }

    private func sample(_ a: Double, _ b: Double, _ s: Double) -> Double {
        ((1 - 3 * b + 3 * a) * s + (3 * b - 6 * a)) * s * s + 3 * a * s
    }

    private func slope(_ a: Double, _ b: Double, _ s: Double) -> Double {
        3 * (1 - 3 * b + 3 * a) * s * s + 2 * (3 * b - 6 * a) * s + 3 * a
    }

    /// The same curve as a SwiftUI animation.
    public func animation(duration: Double) -> Animation {
        .timingCurve(x1, y1, x2, y2, duration: duration)
    }
}

/// A CSS `@keyframes` track for one numeric property; the timing curve applies per segment,
/// as in CSS.
public struct BUIKeyframes: Sendable {
    public var stops: [(offset: Double, value: Double)]
    public var curve: BUICurve

    public init(_ stops: [(Double, Double)], curve: BUICurve = .ease) {
        self.stops = stops.map { (offset: $0.0, value: $0.1) }.sorted { $0.offset < $1.offset }
        self.curve = curve
    }

    /// Value at cycle fraction `p` in 0...1.
    public func value(at p: Double) -> Double {
        guard let first = stops.first, let last = stops.last else { return 0 }
        if p <= first.offset { return first.value }
        if p >= last.offset { return last.value }
        for (a, b) in zip(stops, stops.dropFirst()) where p <= b.offset {
            let span = b.offset - a.offset
            let local = span > 0 ? (p - a.offset) / span : 1
            return a.value + (b.value - a.value) * curve(local)
        }
        return last.value
    }

    /// Value of an infinite CSS animation `elapsed` seconds after it started, with an
    /// `animation-delay`. Before the delay ends the property keeps `rest`.
    public func value(elapsed: Double, duration: Double, delay: Double = 0, rest: Double) -> Double {
        let t = elapsed - delay
        guard t >= 0, duration > 0 else { return rest }
        return value(at: t.truncatingRemainder(dividingBy: duration) / duration)
    }
}

/// Durations and animations the site uses.
public enum BUIMotion {
    /// `--default-transition-duration` (150 ms, `--ease-in-out`).
    public static let standard = BUICurve.twEaseInOut.animation(duration: 0.15)
    /// `pop-in` entrance (160–300 ms, `--ease-out-strong`).
    public static let popIn = BUICurve.outStrong.animation(duration: 0.2)
    /// `fade-up` reveal (400–600 ms, `--ease-out-strong`).
    public static let fadeUp = BUICurve.outStrong.animation(duration: 0.45)
    /// Disclosure expand / collapse.
    public static let disclosure = BUICurve.outStrong.animation(duration: 0.28)
}
