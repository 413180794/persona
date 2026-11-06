#if os(macOS)
import AppKit
import SwiftUI

/// The Mac's `CodeLinesView` (the iOS one is a UIScrollView of reused labels, `Persona/Code/CodeLinesView.swift`):
/// a flipped document view inside an `NSScrollView` that draws only the lines in the dirty rect, so a file of
/// 100 000 lines opens and scrolls like one of ten. The gutter (line numbers, JSON's fold arrows) is drawn at the
/// visible left edge, so it stays put while the code scrolls sideways under it. A click goes to the source (JSON
/// opens or closes a node) and selects the line (⇧-click extends, ⌘A selects all, ⌘C copies); a right click
/// offers the line's menu (Copy Line…).
/// ⌘F (Edit › Find…, `MacFind`) opens a find bar over the lines (`CodeFindBar`): matches are filled behind the text,
/// the current one stronger, and scrolled to.
final class CodeLinesView: NSView, MacFindable {
    weak var source: CodeLineSource? {
        didSet { reload() }
    }
    /// The find bar's state (set by `CodeLines`); nil draws no matches.
    var find: CodeFindModel?
    var onNearEnd: (() -> Void)?
    /// Once, when the first lines are on screen.
    var onFirstLines: (() -> Void)?
    /// A line (0-based) to select and, once, scroll to: a message's `path:12`.
    var focus: Int? {
        didSet {
            guard focus != oldValue else { return }
            focusPending = focus != nil
            applyFocus()
        }
    }
    private var focusPending = false

    let font = CodeMetrics.font
    /// NSFont has no `lineHeight`: ascender + descender + leading is what UIFont's is.
    private(set) lazy var fontLineHeight: CGFloat = ceil(font.ascender - font.descender + font.leading)
    private(set) lazy var lineHeight: CGFloat = ceil(fontLineHeight * 1.25)
    private(set) lazy var charWidth: CGFloat = ("0" as NSString).size(withAttributes: [.font: font]).width
    // The same margins as the iOS view.
    private let pad = NSEdgeInsets(top: 10, left: 10, bottom: 24, right: 24)
    /// Between the gutter and the code (the iOS labels sit 8 pt right of the gutter's text box).
    private let gutterGap: CGFloat = 8
    private var gutterWidth: CGFloat = 0
    /// Lines whose attributed strings were made, for drawing them again without asking the source.
    private var lineCache: [Int: NSAttributedString] = [:]
    private var gutterCache: [Int: NSAttributedString] = [:]
    /// The selected lines (anchor where the selection started, for ⇧-click).
    private var selection: ClosedRange<Int>?
    private var anchor: Int?
    private var mouseDownLine: Int?
    private var dragged = false
    private var firstShown = false

    private let background = NSColor(Theme.codeBackground)
    private let rule = NSColor.separatorColor
    private let selectionFill = NSColor.selectedTextBackgroundColor
    private let matchFill = NSColor(Theme.Fill.macFindMatch)
    private let currentFill = NSColor(Theme.Fill.macFindCurrent)

    override var isFlipped: Bool { true }
    override var isOpaque: Bool { true }
    override var acceptsFirstResponder: Bool { true }
    /// The gutter is drawn where the visible rect is: every scroll redraws what is on screen, so the pre-drawn
    /// overdraw of responsive scrolling would show a gutter in the wrong place.
    override class var isCompatibleWithResponsiveScrolling: Bool { false }

    /// Everything again: the line count or the gutter changed.
    func reload() {
        lineCache = [:]
        gutterCache = [:]
        source?.changed = { [weak self] range in
            guard let self else { return }
            if let range { self.refresh(range) } else { self.reload() }
        }
        if let n = source?.lineCount, let s = selection, s.upperBound >= n {
            selection = n > 0 && s.lowerBound < n ? s.lowerBound...(n - 1) : nil
        }
        resize()
        needsDisplay = true
        find?.sourceChanged()
        applyFocus()
    }

    /// The focused line selected and brought into view (a third down), once it is there.
    private func applyFocus() {
        guard focusPending, let f = focus, let source, f < source.lineCount else { return }
        focusPending = false
        anchor = f
        select(f...f)
        DispatchQueue.main.async { [weak self] in // after the scroll view has sized the document
            guard let self, let clip = self.enclosingScrollView?.contentView else { return }
            let y = max(0, self.pad.top + CGFloat(f) * self.lineHeight - clip.bounds.height / 3)
            clip.scroll(to: CGPoint(x: 0, y: min(y, max(0, self.bounds.height - clip.bounds.height))))
            self.enclosingScrollView?.reflectScrolledClipView(clip)
            self.scrolled()
        }
    }

    // MARK: Find

    func showFind() { find?.show() }

    func findNext(forward: Bool) { find?.next(forward: forward) }

    /// The first line at the top of what is visible (where a new query starts looking).
    var topLine: Int { max(0, Int((visibleRect.minY - pad.top) / lineHeight)) }

    /// The matches changed: draw them again, and bring the current one into view.
    func findChanged(scroll: Bool) {
        needsDisplay = true
        guard scroll, let source, let m = find?.currentMatch, m.line < source.lineCount else { return }
        let r = matchRect(m.range, line: m.line, source)
        // Clear of the pinned gutter on the left, a few lines of context above and below.
        let left = pad.left + gutterWidth + gutterGap
        scrollToVisible(CGRect(x: r.minX - left, y: r.minY - lineHeight * 3, width: r.width + left + pad.right,
                               height: lineHeight * 7))
    }

    /// Where `range` of line `i` is drawn.
    private func matchRect(_ range: NSRange, line i: Int, _ source: CodeLineSource) -> CGRect {
        let a = line(i, source)
        let lo = min(range.location, a.length), hi = min(NSMaxRange(range), a.length)
        func x(_ n: Int) -> CGFloat { n == 0 ? 0 : a.attributedSubstring(from: NSRange(location: 0, length: n)).size().width }
        let codeX = pad.left + gutterWidth + gutterGap
        return CGRect(x: codeX + x(lo), y: pad.top + CGFloat(i) * lineHeight, width: max(x(hi) - x(lo), charWidth / 2),
                      height: lineHeight)
    }

    /// The lines in `range` again (their colors came in).
    func refresh(_ range: Range<Int>) {
        for i in range { lineCache[i] = nil }
        guard !range.isEmpty else { return }
        setNeedsDisplay(CGRect(x: visibleRect.minX, y: pad.top + CGFloat(range.lowerBound) * lineHeight,
                               width: visibleRect.width, height: CGFloat(range.count) * lineHeight))
    }

    /// The content's size, at least the scroll view's.
    func resize() {
        let clip = enclosingScrollView?.contentView.bounds.size ?? .zero
        guard let source else {
            setFrameSize(clip)
            return
        }
        gutterWidth = CGFloat(source.gutterColumns) * charWidth + 12
        let w = pad.left + gutterWidth + CGFloat(source.columns) * charWidth + pad.right
        let h = pad.top + CGFloat(source.lineCount) * lineHeight + pad.bottom
        let size = CGSize(width: max(w, clip.width), height: max(h, clip.height))
        if frame.size != size { setFrameSize(size) }
    }

    /// The visible rect moved: the gutter moves with it, and near the end the next page is wanted.
    func scrolled() {
        needsDisplay = true
        let v = visibleRect
        if v.maxY > bounds.height - v.height * 2 { onNearEnd?() }
    }

    private func line(_ i: Int, _ source: CodeLineSource) -> NSAttributedString {
        if let have = lineCache[i] { return have }
        let a = source.line(i)
        if lineCache.count > 800 { lineCache = [:] }
        lineCache[i] = a
        return a
    }

    private func gutter(_ i: Int, _ source: CodeLineSource) -> NSAttributedString {
        if let have = gutterCache[i] { return have }
        let a = source.gutter(i)
        if gutterCache.count > 800 { gutterCache = [:] }
        gutterCache[i] = a
        return a
    }

    override func draw(_ dirtyRect: NSRect) {
        background.setFill()
        dirtyRect.fill()
        guard let source else { return }
        let n = source.lineCount
        let first = max(0, Int((dirtyRect.minY - pad.top) / lineHeight))
        let last = min(n, Int((dirtyRect.maxY - pad.top) / lineHeight) + 1)
        guard first < last else { return }
        let textY = (lineHeight - fontLineHeight) / 2
        let codeX = pad.left + gutterWidth + gutterGap
        let v = visibleRect
        if let s = selection {
            let lo = max(s.lowerBound, first), hi = min(s.upperBound, last - 1)
            if lo <= hi {
                selectionFill.setFill()
                CGRect(x: dirtyRect.minX, y: pad.top + CGFloat(lo) * lineHeight, width: dirtyRect.width,
                       height: CGFloat(hi - lo + 1) * lineHeight).fill()
            }
        }
        if let find, !find.byLine.isEmpty {
            let current = find.currentMatch
            for i in first..<last {
                guard let ranges = find.byLine[i] else { continue }
                for r in ranges {
                    let isCurrent = current?.line == i && current?.range == r
                    (isCurrent ? currentFill : matchFill).setFill()
                    matchRect(r, line: i, source).fill()
                }
            }
        }
        for i in first..<last {
            line(i, source).draw(at: CGPoint(x: codeX, y: pad.top + CGFloat(i) * lineHeight + textY))
        }
        // Pinned: the gutter covers the code that scrolled under it.
        let gutterRect = CGRect(x: v.minX, y: dirtyRect.minY, width: pad.left + gutterWidth, height: dirtyRect.height)
        guard gutterRect.intersects(dirtyRect) else { return }
        background.setFill()
        gutterRect.fill()
        let scale = window?.backingScaleFactor ?? 2
        rule.setFill()
        CGRect(x: gutterRect.maxX, y: dirtyRect.minY, width: 1 / scale, height: dirtyRect.height).fill()
        let numberWidth = gutterWidth - gutterGap
        for i in first..<last {
            let g = gutter(i, source)
            let w = ceil(g.size().width)
            g.draw(at: CGPoint(x: v.minX + pad.left + numberWidth - w, y: pad.top + CGFloat(i) * lineHeight + textY))
        }
        if !firstShown, let f = onFirstLines {
            firstShown = true
            onFirstLines = nil
            DispatchQueue.main.async { f() } // not while drawing: it may change SwiftUI state
        }
    }

    func line(at p: CGPoint) -> Int? {
        guard let source else { return nil }
        let i = Int(floor((p.y - pad.top) / lineHeight))
        return i >= 0 && i < source.lineCount ? i : nil
    }

    private func point(_ event: NSEvent) -> CGPoint { convert(event.locationInWindow, from: nil) }

    // MARK: Clicks and selection

    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        dragged = false
        mouseDownLine = line(at: point(event))
        guard let i = mouseDownLine else {
            select(nil)
            return
        }
        if event.modifierFlags.contains(.shift), let a = anchor {
            select(min(a, i)...max(a, i))
        } else {
            anchor = i
            select(i...i)
        }
    }

    override func mouseDragged(with event: NSEvent) {
        guard let a = anchor, let source, source.lineCount > 0 else { return }
        dragged = true
        let i = line(at: point(event)) ?? (point(event).y < pad.top ? 0 : source.lineCount - 1)
        select(min(a, i)...max(a, i))
        autoscroll(with: event)
    }

    override func mouseUp(with event: NSEvent) {
        defer { mouseDownLine = nil }
        guard !dragged, !event.modifierFlags.contains(.shift), event.clickCount == 1,
              let i = mouseDownLine, line(at: point(event)) == i, source?.tap(i) == true else { return }
        reload()
    }

    private func select(_ range: ClosedRange<Int>?) {
        guard range != selection else { return }
        selection = range
        needsDisplay = true
    }

    override func selectAll(_ sender: Any?) {
        guard let n = source?.lineCount, n > 0 else { return }
        anchor = 0
        select(0...(n - 1))
    }

    @objc func copy(_ sender: Any?) {
        guard let source, let s = selection else { return }
        let n = source.lineCount
        let text = (s.lowerBound...min(s.upperBound, n - 1)).map { source.line($0).string }.joined(separator: "\n")
        UIPasteboard.general.string = text
    }

    /// Menu items (Edit › Copy / Select All) are on only when there is something for them.
    @objc func validateUserInterfaceItem(_ item: any NSValidatedUserInterfaceItem) -> Bool {
        switch item.action {
        case #selector(copy(_:)): selection != nil
        case #selector(selectAll(_:)): (source?.lineCount ?? 0) > 0
        default: true
        }
    }

    override func keyDown(with event: NSEvent) {
        if event.charactersIgnoringModifiers == "\u{1b}" { // Esc closes the find bar, then clears the selection
            if let find, find.visible { find.close() } else { select(nil) }
        } else {
            super.keyDown(with: event)
        }
    }

    override func menu(for event: NSEvent) -> NSMenu? {
        guard let i = line(at: point(event)), let menu = source?.menu(i) else { return nil }
        if selection.map({ !$0.contains(i) }) ?? true {
            anchor = i
            select(i...i)
        }
        return menu.nsMenu()
    }
}

/// The scroll view around `CodeLinesView`: keeps the document at least its own size and tells it when it moved.
final class CodeLinesScrollView: NSScrollView {
    let lines = CodeLinesView()

    override init(frame: NSRect) {
        super.init(frame: frame)
        hasVerticalScroller = true
        hasHorizontalScroller = true
        autohidesScrollers = true
        scrollerStyle = .overlay
        drawsBackground = false
        borderType = .noBorder
        documentView = lines
        contentView.postsBoundsChangedNotifications = true
        NotificationCenter.default.addObserver(self, selector: #selector(moved), name: NSView.boundsDidChangeNotification, object: contentView)
    }

    required init?(coder: NSCoder) { fatalError() }

    @objc private func moved() {
        lines.scrolled()
        FrameMeter.shared.noteScroll()
    }

    override func tile() {
        super.tile()
        lines.resize()
    }
}

/// `CodeLinesView` in SwiftUI, with its find bar on top while finding (⌘F). `version` changes when the source is
/// a new one.
struct CodeLines: View {
    var source: CodeLineSource
    var version: Int = 0
    var onNearEnd: (() -> Void)? = nil
    var onFirstLines: (() -> Void)? = nil
    var focus: Int? = nil
    @State private var find = CodeFindModel()

    var body: some View {
        VStack(spacing: 0) {
            if find.visible {
                CodeFindBar(find: find)
                Divider()
            }
            CodeLinesHost(source: source, version: version, find: find, onNearEnd: onNearEnd, onFirstLines: onFirstLines, focus: focus)
        }
        // The scroll view has no size of its own: a sheet sized to its content (the file viewer) left the code a
        // sliver. Only the ideal size: inside a bigger container it fills what it is given, as before.
        .frame(idealWidth: Theme.Size.macSheetMinWidth, idealHeight: Theme.Size.macSheetMinHeight)
        #if DEBUG
        .onAppear {
            // `-macFileFind <query>` (with `-openFilePath`): the bar open with that typed in, for offscreen screenshots.
            if let q = UserDefaults.standard.string(forKey: "macFileFind") {
                // `-macFileFindOptions <case,word,regex>`: those switches on (any of the three, comma-separated).
                let on = Set((UserDefaults.standard.string(forKey: "macFileFindOptions") ?? "").split(separator: ",").map(String.init))
                DispatchQueue.main.asyncAfter(deadline: .now() + 1) {
                    find.options = CodeFind.Options(caseSensitive: on.contains("case"), wholeWord: on.contains("word"), regex: on.contains("regex"))
                    find.show()
                    find.query = q
                }
            }
        }
        #endif
    }
}

private struct CodeLinesHost: NSViewRepresentable {
    var source: CodeLineSource
    var version: Int
    var find: CodeFindModel
    var onNearEnd: (() -> Void)?
    var onFirstLines: (() -> Void)?
    var focus: Int?

    final class Coordinator {
        var version = -1
        var source: ObjectIdentifier?
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> CodeLinesScrollView {
        let v = CodeLinesScrollView()
        v.lines.onFirstLines = onFirstLines
        v.lines.find = find
        find.view = v.lines
        return v
    }

    func updateNSView(_ v: CodeLinesScrollView, context: Context) {
        v.lines.onNearEnd = onNearEnd
        defer { v.lines.focus = focus }
        let c = context.coordinator
        let id = ObjectIdentifier(source)
        guard c.source != id || c.version != version else { return }
        c.source = id
        c.version = version
        v.lines.source = source
    }
}
#endif
