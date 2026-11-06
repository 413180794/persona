#if os(macOS)
import AppKit
import PersonaKit
import SwiftUI

/// The Mac's `ZoomableImage` (iOS: UIScrollView in ImageViewer.swift): an NSScrollView with magnification, so a
/// trackpad pinch and ⌘ / ⌥-scroll zoom natively; a double click zooms to the clicked point (or back to fit), a single
/// click calls `onTap`. The pull-down to close has no Mac gesture (Esc and the Close button close the viewer).
struct ZoomableImage: NSViewRepresentable {
    var image: NSImage
    var onTap: () -> Void
    var onPull: (CGFloat) -> Void = { _ in }
    var onPullEnd: (Bool) -> Void = { _ in }

    func makeNSView(context: Context) -> ZoomScrollView {
        let v = ZoomScrollView(image: image)
        v.onTap = onTap
        return v
    }

    func updateNSView(_ v: ZoomScrollView, context: Context) {
        v.onTap = onTap
        if v.imageView.image !== image { v.setImage(image) }
    }
}

final class ZoomScrollView: NSScrollView {
    let imageView = ZoomImageView()
    var onTap: () -> Void = {}
    private var lastBounds: CGSize = .zero
    private var pendingTap: DispatchWorkItem?

    init(image: NSImage) {
        super.init(frame: .zero)
        contentView = CenteringClipView()
        contentView.drawsBackground = false
        drawsBackground = false
        hasVerticalScroller = false
        hasHorizontalScroller = false
        allowsMagnification = true
        minMagnification = 1
        imageView.imageScaling = .scaleProportionallyUpOrDown
        imageView.isEditable = false
        imageView.image = image
        documentView = imageView
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func setImage(_ image: NSImage) {
        // Paging swapped the image, so a single-tap close scheduled for the previous image is stale (#3481): without
        // this, tapping once and then paging closed the viewer after the double-click interval elapsed.
        pendingTap?.cancel()
        pendingTap = nil
        imageView.image = image
        lastBounds = .zero
        needsLayout = true
    }

    override func layout() {
        super.layout()
        guard bounds.size != lastBounds, let img = imageView.image, img.size.width > 0, img.size.height > 0 else { return }
        lastBounds = bounds.size
        // Fit at 1x, allow up to 5x of the image's pixels (at least 3x of the fitted size), as on iOS.
        let fit = ZoomMath.fittedSize(image: img.size, in: bounds.size)
        magnification = 1
        imageView.frame = CGRect(origin: .zero, size: fit)
        maxMagnification = ZoomMath.maxScale(image: img.size, fitted: fit, screenScale: window?.backingScaleFactor ?? 2)
    }

    /// Clicks on the image or the empty space around it. A single click waits out the double-click interval so a
    /// double click does not also count as a tap.
    func clicked(_ event: NSEvent) {
        pendingTap?.cancel()
        if event.clickCount >= 2 {
            pendingTap = nil
            if magnification > minMagnification + 0.01 {
                animator().magnification = minMagnification
            } else {
                let p = contentView.convert(event.locationInWindow, from: nil)
                animator().setMagnification(min(maxMagnification, 2.5), centeredAt: p)
            }
        } else if event.clickCount == 1 {
            let tap = DispatchWorkItem { [weak self] in self?.onTap() }
            pendingTap = tap
            DispatchQueue.main.asyncAfter(deadline: .now() + NSEvent.doubleClickInterval, execute: tap)
        }
    }
}

/// The image itself: no editing / dragging behaviour of NSImageView, clicks go to the scroll view.
final class ZoomImageView: NSImageView {
    override func mouseDown(with event: NSEvent) {}
    override func mouseUp(with event: NSEvent) { (enclosingScrollView as? ZoomScrollView)?.clicked(event) }
}

/// Keeps the document centered while it is smaller than the viewport (iOS does it with content insets).
final class CenteringClipView: NSClipView {
    override func constrainBoundsRect(_ proposedBounds: NSRect) -> NSRect {
        var r = super.constrainBoundsRect(proposedBounds)
        guard let doc = documentView else { return r }
        let inset = ZoomMath.centeringInset(content: doc.frame.size, bounds: r.size)
        if inset.width > 0 { r.origin.x = doc.frame.minX - inset.width }
        if inset.height > 0 { r.origin.y = doc.frame.minY - inset.height }
        return r
    }

    override func mouseDown(with event: NSEvent) {}
    override func mouseUp(with event: NSEvent) { (enclosingScrollView as? ZoomScrollView)?.clicked(event) }
}

/// What a page-style TabView does on iOS, for the Mac image viewers: one page at a time, ← / → keys and the arrow
/// buttons on the sides move between them.
struct ImagePager<Page: View>: View {
    var count: Int
    @Binding var index: Int
    @ViewBuilder var page: (Int) -> Page
    @FocusState private var focused: Bool

    var body: some View {
        page(index)
            .id(index)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .overlay(alignment: .leading) { if index > 0 { arrow(-1) } }
            .overlay(alignment: .trailing) { if index < count - 1 { arrow(1) } }
            .focusable()
            .focusEffectDisabled()
            .focused($focused)
            .onKeyPress(.leftArrow) { step(-1) }
            .onKeyPress(.rightArrow) { step(1) }
            .onAppear { focused = true }
    }

    private func step(_ by: Int) -> KeyPress.Result {
        let next = index + by
        guard next >= 0, next < count else { return .ignored }
        index = next
        return .handled
    }

    private func arrow(_ by: Int) -> some View {
        Button { _ = step(by) } label: {
            Image(systemName: by < 0 ? Theme.Icon.back : Theme.Icon.disclosure)
                .font(Theme.Font.heading)
                .scaledFrame(width: Theme.Size.avatar, height: Theme.Size.avatar)
        }
        .themedButton()
        .clipShape(.circle)
        .padding(.horizontal, Theme.Space.l)
        .accessibilityLabel(by < 0 ? String(localized: "Previous image") : String(localized: "Next image"))
    }
}
#endif
