import SwiftUI

/// A small square or round source/app mark: an SF Symbol on a tint, or an image.
public enum BUIAvatar: Sendable {
    case symbol(String, tint: Color)
    case image(Image)
    /// Drawn on demand into a square of the given size (e.g. an app's brand mark).
    case drawing(@Sendable (inout GraphicsContext, CGSize) -> Void)

    @ViewBuilder
    func view(size: CGFloat) -> some View {
        switch self {
        case .symbol(let name, let tint):
            ZStack {
                tint
                Image(systemName: name)
                    .font(.system(size: size * 0.52, weight: .bold))
                    .foregroundStyle(.white)
            }
            .frame(width: size, height: size)
        case .image(let image):
            image.resizable().scaledToFill().frame(width: size, height: size)
        case .drawing(let draw):
            Canvas { gc, canvasSize in draw(&gc, canvasSize) }
                .frame(width: size, height: size)
        }
    }

    /// A non-square mark (brand logos); drawings get the exact size, others fill it.
    @ViewBuilder
    func view(width: CGFloat, height: CGFloat) -> some View {
        switch self {
        case .drawing(let draw):
            Canvas { gc, size in draw(&gc, size) }
                .frame(width: width, height: height)
        default:
            view(size: max(width, height)).frame(width: width, height: height).clipped()
        }
    }

    func draw(in gc: GraphicsContext, rect: CGRect, radius: CGFloat) {
        var gc = gc
        gc.clip(to: Path(roundedRect: rect, cornerRadius: radius))
        switch self {
        case .symbol(let name, let tint):
            gc.fill(Path(rect), with: .color(tint))
            gc.draw(
                Text(Image(systemName: name)).font(.system(size: rect.width * 0.52, weight: .bold)).foregroundStyle(.white),
                at: CGPoint(x: rect.midX, y: rect.midY)
            )
        case .image(let image):
            gc.draw(image, in: rect)
        case .drawing(let draw):
            gc.translateBy(x: rect.minX, y: rect.minY)
            draw(&gc, rect.size)
        }
    }
}
