#if os(macOS)
import AppKit
import QuartzCore
import SwiftUI

/// The Mac's `PersonaFramesView` (iOS: a UIImageView's animationImages): a layer-backed view whose contents a
/// discrete keyframe animation swaps, so Core Animation plays the loop and nothing in the app runs per frame.
/// Frame 0 alone when not animating.
struct PersonaFramesView: NSViewRepresentable {
    var frames: PersonaFrames
    var animating: Bool

    final class Coordinator {
        var shown: PersonaFrames?
        var animating = false
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> FramesView { FramesView() }

    func updateNSView(_ v: FramesView, context: Context) {
        let c = context.coordinator
        guard c.shown !== frames || c.animating != animating else { return }
        c.shown = frames
        c.animating = animating
        v.show(frames, animating: animating)
    }

    func sizeThatFits(_ proposal: ProposedViewSize, nsView: FramesView, context: Context) -> CGSize? {
        CGSize(width: proposal.width ?? Theme.Size.tapTarget, height: proposal.height ?? Theme.Size.tapTarget)
    }

    final class FramesView: NSView {
        private static let key = "persona.frames"

        override init(frame: NSRect) {
            super.init(frame: frame)
            wantsLayer = true
            layer?.contentsGravity = .resizeAspect
            layer?.masksToBounds = true
        }

        required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

        override var wantsUpdateLayer: Bool { true }

        override func viewDidChangeBackingProperties() {
            super.viewDidChangeBackingProperties()
            layer?.contentsScale = window?.backingScaleFactor ?? 2
        }

        func show(_ frames: PersonaFrames, animating: Bool) {
            guard let layer else { return }
            layer.removeAnimation(forKey: Self.key)
            let images = frames.images.compactMap(\.cgImage)
            layer.contents = images.first
            guard animating, images.count > 1 else { return }
            let a = CAKeyframeAnimation(keyPath: "contents")
            a.values = images
            a.calculationMode = .discrete
            a.duration = frames.duration
            a.repeatCount = .infinity
            a.isRemovedOnCompletion = false
            layer.add(a, forKey: Self.key)
        }
    }
}
#endif
