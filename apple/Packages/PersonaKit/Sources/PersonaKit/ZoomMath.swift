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

    /// How far a pull-down to close has gone, 0…1: half the view's height is all the way (the image viewer fades its
    /// backdrop and shrinks the picture with it). An upward drag is 0.
    public static func pullProgress(translation: CGFloat, height: CGFloat) -> CGFloat {
        guard height > 0 else { return 0 }
        return min(1, max(0, translation / (height / 2)))
    }

    /// Whether a pull-down let go here closes: far enough, or a quick flick downward.
    public static func pullCloses(translation: CGFloat, velocity: CGFloat) -> Bool {
        translation > 120 || (translation > 30 && velocity > 700)
    }
}
