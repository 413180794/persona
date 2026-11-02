import SwiftUI

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
