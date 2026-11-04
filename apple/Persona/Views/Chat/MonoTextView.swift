import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

#if os(iOS) || os(visionOS)
/// Monospaced, unwrapped text that scrolls both ways — files, diffs, the terminal. A UIScrollView around a
/// non-scrolling UITextView sized to the longest line: SwiftUI's horizontal ScrollView around a LazyVStack cannot size
/// its content width, and a scrolling UITextView (TextKit 2) wraps no matter how wide its text container is.
struct MonoTextView: UIViewRepresentable {
    var text: NSAttributedString
    /// Keep the same distance from the end when the text changes (a live terminal follows its last line).
    var followsEnd = false
    var inset = UIEdgeInsets(top: 10, left: 10, bottom: 10, right: 10)
    /// Called while the user scrolls near the top (load the page before; the view keeps its place when it arrives).
    var onNearTop: (() -> Void)? = nil

    func makeUIView(context: Context) -> MonoScrollView {
        let v = MonoScrollView()
        v.inset = inset
        return v
    }

    func updateUIView(_ v: MonoScrollView, context: Context) {
        v.onNearTop = onNearTop
        v.set(text, followsEnd: followsEnd)
    }
}

final class MonoScrollView: UIScrollView, UIScrollViewDelegate {
    var onNearTop: (() -> Void)?
    private let textView = UITextView()
    private var measured: CGSize = .zero
    private var first = true
    var inset = UIEdgeInsets.zero

    override init(frame: CGRect) {
        super.init(frame: frame)
        alwaysBounceVertical = true
        showsHorizontalScrollIndicator = false
        showsVerticalScrollIndicator = false
        textView.isEditable = false
        textView.isSelectable = true
        textView.isScrollEnabled = false
        textView.backgroundColor = .clear
        textView.dataDetectorTypes = []
        textView.textContainerInset = .zero
        textView.textContainer.lineFragmentPadding = 0
        textView.textContainer.lineBreakMode = .byClipping
        textView.textContainer.widthTracksTextView = true
        addSubview(textView)
        delegate = self
    }

    func scrollViewDidScroll(_ scrollView: UIScrollView) {
        if (isDragging || isDecelerating), contentOffset.y < 600 { onNearTop?() }
    }

    required init?(coder: NSCoder) { fatalError() }

    func set(_ text: NSAttributedString, followsEnd: Bool) {
        guard first || textView.attributedText.string != text.string else { return }
        let fromEnd = contentSize.height - contentOffset.y
        textView.attributedText = text
        let r = text.boundingRect(with: CGSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude),
                                  options: [.usesLineFragmentOrigin, .usesFontLeading], context: nil)
        measured = CGSize(width: ceil(r.width) + 2, height: ceil(r.height) + 2)
        setNeedsLayout()
        layoutIfNeeded()
        if followsEnd {
            let maxY = max(0, contentSize.height - bounds.height)
            let y = first ? maxY : contentSize.height - fromEnd
            contentOffset = CGPoint(x: contentOffset.x, y: min(max(0, y), maxY))
        }
        first = false
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        let w = max(measured.width, bounds.width - inset.left - inset.right)
        let frame = CGRect(x: inset.left, y: inset.top, width: w, height: measured.height)
        if textView.frame != frame { textView.frame = frame }
        let size = CGSize(width: w + inset.left + inset.right, height: measured.height + inset.top + inset.bottom)
        if contentSize != size { contentSize = size }
    }
}

#endif

enum MonoText {
    static func font(_ size: CGFloat = 12) -> UIFont { .monospacedSystemFont(ofSize: size, weight: .regular) }

    /// Source with a gray line-number gutter.
    static func numbered(_ text: String, size: CGFloat = 12) -> NSAttributedString {
        let lines = text.components(separatedBy: "\n")
        let digits = String(lines.count).count
        let out = NSMutableAttributedString()
        let num: [NSAttributedString.Key: Any] = [.font: font(size), .foregroundColor: UIColor.tertiaryLabel]
        let body: [NSAttributedString.Key: Any] = [.font: font(size), .foregroundColor: UIColor.label]
        for (i, line) in lines.enumerated() {
            let n = String(i + 1)
            out.append(NSAttributedString(string: String(repeating: " ", count: digits - n.count) + n + "  ", attributes: num))
            out.append(NSAttributedString(string: line + (i < lines.count - 1 ? "\n" : ""), attributes: body))
        }
        return out
    }

    static func plain(_ text: String, color: UIColor, size: CGFloat = 10.5) -> NSAttributedString {
        NSAttributedString(string: text, attributes: [.font: font(size), .foregroundColor: color])
    }
}
