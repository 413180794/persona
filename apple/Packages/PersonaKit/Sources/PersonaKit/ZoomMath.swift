import CoreGraphics
import Foundation

/// Pure geometry, unit-tested.
public nonisolated enum ZoomMath {
    public static func fittedSize(image: CGSize, in bounds: CGSize) -> CGSize {
        guard image.width > 0, image.height > 0, bounds.width > 0, bounds.height > 0 else { return .zero }
        let s = min(bounds.width / image.width, bounds.height / image.height)
        return CGSize(width: image.width * s, height: image.height * s)
    }

    public static func maxScale(image: CGSize, fitted: CGSize, screenScale: CGFloat) -> CGFloat {
        guard fitted.width > 0 else { return 3 }
        let pixelScale = image.width / (fitted.width * screenScale) * 5
        return max(3, min(pixelScale, 12))
    }

    /// Insets that keep content centered when it is smaller than the viewport.
    public static func centeringInset(content: CGSize, bounds: CGSize) -> CGSize {
        CGSize(width: max(0, (bounds.width - content.width) / 2), height: max(0, (bounds.height - content.height) / 2))
    }
}
