import SwiftUI

/// Everything a component reads to draw itself. Inject a custom theme with
/// `.buiTheme(_:)` to map the library onto a host app's own tokens.
public struct BUITheme: Sendable {
    public var colors: BUIColors
    public var radii: BUIRadii
    /// Multiplies every `buiFont` size. The original's sizes are desktop sizes (13 pt labels);
    /// a phone host whose body text is 17 pt sets about 1.15–1.2.
    public var typeScale: CGFloat
    /// One-shot entrances (`buiEntrance`). A host whose lists rebuild rows as they scroll in
    /// turns them off, or every row of an opened history would fade up at once.
    public var entrances: Bool

    public init(colors: BUIColors = .standard, radii: BUIRadii = .standard, typeScale: CGFloat = 1, entrances: Bool = true) {
        self.colors = colors
        self.radii = radii
        self.typeScale = typeScale
        self.entrances = entrances
    }

    public static let standard = BUITheme()
}

public extension EnvironmentValues {
    @Entry var buiTheme: BUITheme = .standard
}

public extension View {
    func buiTheme(_ theme: BUITheme) -> some View {
        environment(\.buiTheme, theme)
    }
}
