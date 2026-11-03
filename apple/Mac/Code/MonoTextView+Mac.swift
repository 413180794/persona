#if os(macOS)
import AppKit
import SwiftUI

/// The Mac's `MonoTextView` (iOS: `Persona/Views/Chat/MonoTextView.swift`): monospaced, unwrapped text that scrolls
/// both ways — files, diffs, the terminal. An `NSScrollView` around a non-scrolling, selectable `NSTextView` sized to
/// the longest line, `inset` around it as on iOS. Selecting with the mouse and ⌘C / ⌘A / ⌘F work as in any text view.
struct MonoTextView: NSViewRepresentable {
    var text: NSAttributedString
    /// Keep the same distance from the end when the text changes (a live terminal follows its last line).
    var followsEnd = false
    var inset = UIEdgeInsets(top: 10, left: 10, bottom: 10, right: 10)
    /// Called while the user scrolls near the top (load the page before; the view keeps its place when it arrives).
    var onNearTop: (() -> Void)? = nil

    func makeNSView(context: Context) -> MonoScrollView {
        let v = MonoScrollView()
        v.inset = inset
        return v
    }

    func updateNSView(_ v: MonoScrollView, context: Context) {
        v.onNearTop = onNearTop
        if !NSEdgeInsetsEqual(v.inset, inset) { v.inset = inset }
        v.set(text, followsEnd: followsEnd)
    }
}

final class MonoScrollView: NSScrollView {
    var onNearTop: (() -> Void)?
    var inset = UIEdgeInsets.zero {
        didSet { layoutText() }
    }
    private let page = MonoPageView()
    private let textView: NSTextView
    private var measured: CGSize = .zero
    private var first = true
    /// How close to the top (points) a user's scroll asks for the page before, as on iOS.
    private let nearTop: CGFloat = 600

    override init(frame: NSRect) {
        // TextKit 1 with an unbounded container: the text never wraps, and the view is sized to it.
        let storage = NSTextStorage()
        let layout = NSLayoutManager()
        layout.allowsNonContiguousLayout = true
        storage.addLayoutManager(layout)
        let container = NSTextContainer(size: CGSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude))
        container.widthTracksTextView = false
        container.heightTracksTextView = false
        container.lineFragmentPadding = 0
        container.lineBreakMode = .byClipping
        layout.addTextContainer(container)
        textView = NSTextView(frame: .zero, textContainer: container)
        super.init(frame: frame)
        textView.isEditable = false
        textView.isSelectable = true
        textView.isRichText = true
        textView.drawsBackground = false
        textView.textContainerInset = .zero
        textView.isVerticallyResizable = false
        textView.isHorizontallyResizable = false
        textView.isAutomaticLinkDetectionEnabled = false
        textView.isAutomaticDataDetectionEnabled = false
        textView.usesFindBar = true
        textView.isIncrementalSearchingEnabled = true
        hasVerticalScroller = true
        hasHorizontalScroller = true
        autohidesScrollers = true
        scrollerStyle = .overlay
        drawsBackground = false
        borderType = .noBorder
        page.addSubview(textView)
        documentView = page
    }

    required init?(coder: NSCoder) { fatalError() }

    override func scrollWheel(with event: NSEvent) {
        super.scrollWheel(with: event)
        if contentView.bounds.minY < nearTop { onNearTop?() }
    }

    func set(_ text: NSAttributedString, followsEnd: Bool) {
        guard first || textView.string != text.string else { return }
        let fromEnd = page.frame.height - contentView.bounds.minY
        textView.textStorage?.setAttributedString(text)
        if let layout = textView.layoutManager, let container = textView.textContainer {
            layout.ensureLayout(for: container)
            let r = layout.usedRect(for: container)
            measured = CGSize(width: ceil(r.width) + 2, height: ceil(r.height) + 2)
        }
        layoutText()
        if followsEnd {
            let maxY = max(0, page.frame.height - contentView.bounds.height)
            let y = first ? maxY : page.frame.height - fromEnd
            contentView.scroll(to: CGPoint(x: contentView.bounds.minX, y: min(max(0, y), maxY)))
            reflectScrolledClipView(contentView)
        }
        first = false
    }

    override func tile() {
        super.tile()
        layoutText()
    }

    /// The text view at the inset, the page around it at least as big as what is visible.
    private func layoutText() {
        let clip = contentView.bounds.size
        let w = max(measured.width, clip.width - inset.left - inset.right)
        let frame = CGRect(x: inset.left, y: inset.top, width: w, height: measured.height)
        if textView.frame != frame { textView.frame = frame }
        let size = CGSize(width: w + inset.left + inset.right, height: max(measured.height + inset.top + inset.bottom, clip.height))
        if page.frame.size != size { page.setFrameSize(size) }
    }
}

/// A top-left origin document view, as UIKit's are.
final class MonoPageView: NSView {
    override var isFlipped: Bool { true }
}
#endif
