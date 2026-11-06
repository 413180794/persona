#if os(macOS)
import AppKit
import CoreText
import PersonaKit
import SwiftUI

/// The terminal tab's screen on a Mac: the same grid the phone draws (`LineRender`, `CellMetrics`), in an
/// `NSScrollView` whose flipped document view is the whole scrollback. Same interface as the iOS `TerminalView`
/// (TerminalView.swift), so the session page is shared:
/// - the keyboard goes to the terminal through `NSTextInputClient` (input methods compose over the cursor);
/// - scrolling is the terminal's wheel while driving (`onWheel`), else it scrolls the scrollback;
/// - a click is a click on the program's screen (`onClick`) or opens a link (`onOpenURL`); a drag selects, ⌘C copies;
/// - with `onMouse` (driving, on a hub that takes it) presses, drags and releases of every button go to the program as
///   they are (MacTerminalMouse.swift); Shift keeps them here for selecting and the menu (the other way round in Select Locally);
/// - ⌘+ / ⌘− / ⌘0 and a trackpad pinch change the font size (`onFontSize`); ⌘V pastes (`onDoubleTap`, the phone's paste);
/// - files and pictures pasted with ⌘V or dragged in go to `onFiles` (uploaded, their paths typed: TerminalFiles.swift).
struct TerminalView: NSViewRepresentable {
    var content: TerminalContent
    var wrap: Bool
    var fontSize: CGFloat
    var font: TerminalFont = .jetbrains
    var theme: TerminalTheme
    /// Whether the keyboard is on the terminal; it follows the view as well (a click, focus moving elsewhere).
    var focused: Binding<Bool>?
    var onInput: ((TerminalInputEvent) -> Void)?
    /// Called while the user scrolls near the top (load the page before; the view keeps its place when it arrives).
    var onNearTop: (() -> Void)?
    var onOpenURL: ((URL) -> Void)?
    var onCopy: (() -> Void)?
    /// The font size to use now (pinch, ⌘+ / ⌘−).
    var onFontSize: ((CGFloat) -> Void)?
    /// A trackpad swipe: left / right / up / down.
    var onSwipe: ((UISwipeGestureRecognizer.Direction) -> Void)?
    /// The phone's double tap, which pastes: ⌘V here.
    var onDoubleTap: (() -> Void)?
    /// Driving the terminal: the scroll wheel is the program's wheel, in rows (> 0 back), over the cell under the pointer.
    var onWheel: ((Int, (col: Int, row: Int)?) -> Void)?
    /// Driving the terminal: a click at a cell of its screen (column, row; 0-based).
    var onClick: ((Int, Int) -> Void)?
    /// The cols × rows that fit the view at the font size.
    var onGrid: ((TerminalGridSize) -> Void)?
    /// Files dropped on the terminal, or files / a picture on the pasteboard at ⌘V (set with `files(_:dropping:)`).
    var onFiles: (([TerminalFileSource]) -> Void)?
    /// Files are being dragged over the terminal (true) or not any more.
    var onDropping: ((Bool) -> Void)?
    /// Driving, on a hub with `terminal_mouse`: the pointer's events for the program (set with `mouse(_:mode:)`).
    var onMouse: ((MacTerminalMouse.Event) -> Void)?
    var mouseMode = MacTerminalMouse.Mode.program

    /// Forwards the mouse to the program while driving (nil: the older click only).
    func mouse(_ forward: ((MacTerminalMouse.Event) -> Void)?, mode: MacTerminalMouse.Mode) -> TerminalView {
        var v = self
        v.onMouse = forward
        v.mouseMode = mode
        return v
    }

    /// Takes pasted and dropped files (the iOS view has no such thing: there the key bar picks them).
    func files(_ take: (([TerminalFileSource]) -> Void)?, dropping: ((Bool) -> Void)? = nil) -> TerminalView {
        var v = self
        v.onFiles = take
        v.onDropping = dropping
        return v
    }

    func makeNSView(context: Context) -> TerminalScrollView { TerminalScrollView() }

    func updateNSView(_ v: TerminalScrollView, context: Context) {
        v.onNearTop = onNearTop
        v.onOpenURL = onOpenURL
        v.onCopy = onCopy
        v.onFontSize = onFontSize
        v.onSwipe = onSwipe
        v.onDoubleTap = onDoubleTap
        v.setWheel(onWheel)
        v.onClick = onClick
        v.onInput = onInput
        v.onGrid = onGrid
        v.onFiles = onFiles
        v.onDropping = onDropping
        v.setMouse(onMouse, mode: mouseMode)
        v.onFocus = focused.map { b in { on in if b.wrappedValue != on { b.wrappedValue = on } } }
        v.update(content: content, wrap: wrap, fontSize: fontSize, font: font, theme: theme,
                 dark: context.environment.colorScheme == .dark)
        if let focused, onInput != nil, focused.wrappedValue != v.isFocused {
            let on = focused.wrappedValue
            DispatchQueue.main.async { // not while SwiftUI is updating views
                guard on != v.isFocused, let window = v.window else { return }
                window.makeFirstResponder(on ? v.screen : nil)
            }
        }
    }

    static func dismantleNSView(_ v: TerminalScrollView, coordinator: ()) {
        if v.isFocused { v.window?.makeFirstResponder(nil) }
    }
}

final class TerminalScrollView: NSScrollView {
    var onNearTop: (() -> Void)?
    var onOpenURL: ((URL) -> Void)?
    var onCopy: (() -> Void)?
    var onFontSize: ((CGFloat) -> Void)?
    var onSwipe: ((UISwipeGestureRecognizer.Direction) -> Void)?
    var onDoubleTap: (() -> Void)?
    private(set) var onWheel: ((Int, (col: Int, row: Int)?) -> Void)?
    var onClick: ((Int, Int) -> Void)?
    var onInput: ((TerminalInputEvent) -> Void)?
    var onFocus: ((Bool) -> Void)?
    var onGrid: ((TerminalGridSize) -> Void)?
    var onFiles: (([TerminalFileSource]) -> Void)?
    var onDropping: ((Bool) -> Void)?
    private(set) var onMouse: ((MacTerminalMouse.Event) -> Void)?
    private var mouseMode = MacTerminalMouse.Mode.program
    /// The button herdr holds pressed, where it last was, and the drag's per-cell throttle.
    private var held: (button: MacTerminalMouse.Button, cell: (col: Int, row: Int), throttle: MacTerminalMouse.CellThrottle)?
    private var reportedGrid: TerminalGridSize?
    private var wheelRows: CGFloat = 0
    private var wheelAt: (col: Int, row: Int)?
    private var pinchFrom: CGFloat = TerminalPrefs.defaultFontSize
    private var pinchScale: CGFloat = 1

    /// The input method's composition, drawn over the cursor until it is chosen.
    private(set) var marked = ""
    var markedSelection = NSRange()

    let screen = TerminalScreenView()
    private var content = TerminalContent()
    private var wrap = true
    private var fontSize: CGFloat = TerminalPrefs.defaultFontSize
    private var font = TerminalFont.jetbrains
    private var theme = TerminalTheme.ink
    private var dark = true
    private var metrics = CellMetrics(size: TerminalPrefs.defaultFontSize)
    private var laidOutHeight: CGFloat = 0
    private var laidOutWidth: CGFloat = 0
    private var lines: [DisplayLine] = []
    private var lineLayout = TerminalLineLayout()
    private var maxCells = 0
    private var first = true
    // The same margins as the phone's terminal (TerminalView.swift).
    private let inset = NSEdgeInsets(top: 8, left: 10, bottom: 10, right: 10)
    private var cache: [CacheKey: LineRender] = [:]
    /// The selection, in display-line coordinates: anchor and focus cells (inclusive).
    private var selection: (anchor: CellPoint, focus: CellPoint)? { didSet { screen.needsDisplay = true } }
    /// Where a press went down, while it may still become a drag (a selection) or stay a click.
    private var pressedAt: CellPoint?
    private var dragged = false
    /// The press only cleared a selection: it is not a click for the program.
    private var pressCleared = false

    struct CellPoint: Comparable {
        var line: Int; var col: Int
        static func < (a: CellPoint, b: CellPoint) -> Bool { a.line < b.line || (a.line == b.line && a.col < b.col) }
    }
    private struct CacheKey: Hashable { var row: Int; var from: Int; var to: Int }

    static let fontRange: ClosedRange<CGFloat> = 7...20
    private static let fontStep: CGFloat = 1

    override init(frame: NSRect) {
        super.init(frame: frame)
        hasVerticalScroller = false // scroll bars stay hidden, as everywhere in the app
        hasHorizontalScroller = false
        automaticallyAdjustsContentInsets = false
        contentInsets = .zero
        verticalScrollElasticity = .allowed
        horizontalScrollElasticity = .none
        drawsBackground = true
        screen.host = self
        documentView = screen
    }

    required init?(coder: NSCoder) { fatalError() }

    /// Whether typing goes to the terminal.
    var isFocused: Bool { window?.firstResponder === screen }
    private var typing: Bool { isFocused && onInput != nil }

    private var viewport: CGSize { contentView.bounds.size }
    private var offset: CGPoint { contentView.bounds.origin }
    private var docSize: CGSize { screen.frame.size }

    private func scroll(toY y: CGFloat) {
        contentView.scroll(to: NSPoint(x: offset.x, y: y))
        reflectScrolledClipView(contentView)
    }

    // MARK: Content

    func update(content new: TerminalContent, wrap: Bool, fontSize: CGFloat, font: TerminalFont, theme: TerminalTheme, dark: Bool) {
        let styleChanged = wrap != self.wrap || fontSize != self.fontSize || font != self.font || theme.id != self.theme.id || dark != self.dark
        guard first || styleChanged || new != content else { return }
        if styleChanged || new.rows.count != content.rows.count {
            cache.removeAll(keepingCapacity: true)
        } else {
            // Only rows that changed are drawn again.
            for i in new.rows.indices where new.rows[i] != content.rows[i] { invalidate(row: i) }
        }
        if fontSize != self.fontSize || font != self.font || first { metrics = CellMetrics(size: fontSize, font: font) }
        let fromEnd = docSize.height - offset.y
        let atEnd = isAtEnd
        // Rows added under what is read (the scrollback grows) keep it where it is; pages added above, from the end.
        let grewBelow = !content.rows.isEmpty && new.rows.count > content.rows.count && new.rows.first == content.rows.first
        let top = offset.y
        self.wrap = wrap; self.fontSize = fontSize; self.font = font; self.theme = theme; self.dark = dark
        content = new
        backgroundColor = UIColor(hex: theme.side(dark).background)
        horizontalScrollElasticity = wrap ? .none : .automatic
        relayout()
        let maxY = max(0, docSize.height - viewport.height)
        if first || atEnd || onWheel != nil {
            scroll(toY: maxY)
        } else if grewBelow {
            scroll(toY: min(top, maxY))
        } else {
            scroll(toY: min(max(0, docSize.height - fromEnd), maxY)) // pages added above keep what you read in place
        }
        first = false
        screen.setAccessibilityValue(content.rows.suffix(12).map(TerminalGrid.plain).joined(separator: "\n"))
        reportGrid()
        screen.needsDisplay = true
    }

    /// The grid that fits, when it changed (not while SwiftUI is updating: the feed it goes to is observed).
    private func reportGrid() {
        let w = viewport.width - inset.left - inset.right, h = viewport.height - inset.top - inset.bottom
        guard w > 0, h > 0, let onGrid, metrics.cell.width > 0, metrics.cell.height > 0 else { return }
        let g = TerminalGridSize(cols: Int(w / metrics.cell.width), rows: Int(h / metrics.cell.height))
        guard g != reportedGrid else { return }
        reportedGrid = g
        DispatchQueue.main.async { onGrid(g) }
    }

    private var isAtEnd: Bool { offset.y >= docSize.height - viewport.height - 4 }

    private func invalidate(row: Int) {
        for k in cache.keys where k.row == row { cache[k] = nil }
    }

    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        needsLayout = true
    }

    override func layout() {
        super.layout()
        let size = viewport
        guard size.width != laidOutWidth || size.height != laidOutHeight else { return }
        // A smaller window keeps what was at the bottom (the cursor) there.
        let wasAtEnd = offset.y >= docSize.height - laidOutHeight - 4
        laidOutHeight = size.height
        if size.width != laidOutWidth { relayout() }
        if wasAtEnd || onWheel != nil { scroll(toY: max(0, docSize.height - size.height)) }
        reportGrid()
        screen.needsDisplay = true
    }

    /// Cuts rows into display lines and sizes the document.
    private func relayout() {
        laidOutWidth = viewport.width
        let perLine = max(10, Int((viewport.width - inset.left - inset.right) / metrics.cell.width))
        let out = lineLayout.lines(content.rows, cursor: content.cursor, perLine: perLine, wrap: wrap)
        maxCells = out.reduce(content.cols) { max($0, $1.to + $1.indent) }
        if out != lines { selection = nil }
        lines = out
        let width = wrap ? viewport.width : max(viewport.width, CGFloat(maxCells) * metrics.cell.width + inset.left + inset.right)
        let size = CGSize(width: width, height: CGFloat(lines.count) * metrics.cell.height + inset.top + inset.bottom)
        if screen.frame.size != size { screen.setFrameSize(size) }
    }

    // MARK: Composition

    func setMarked(_ text: String, selected: NSRange) {
        marked = text
        markedSelection = selected
        screen.needsDisplay = true
    }

    /// The terminal cursor's cell in the document (the end of the last line when it is hidden).
    func cursorRect() -> CGRect {
        let w = metrics.cell.width, h = metrics.cell.height
        var at: (line: Int, col: Int)?
        if let c = content.cursor {
            if let i = lines.lastIndex(where: { $0.row == c.row && c.col >= $0.from }) { at = (i, c.col - lines[i].from + lines[i].indent) }
        }
        let point = at ?? (max(0, lines.count - 1), lines.last.map { $0.to - $0.from + $0.indent } ?? 0)
        return CGRect(x: inset.left + CGFloat(point.col) * w, y: inset.top + CGFloat(point.line) * h, width: w, height: h)
    }

    /// Return, tab and newlines in a paste go out as keys; the rest as text (as on the phone).
    func emit(_ text: String) {
        var run = ""
        for ch in text {
            let key: String? = ch == "\n" || ch == "\r" || ch == "\r\n" ? "enter" : ch == "\t" ? "tab" : nil
            guard let key else { run.append(ch); continue }
            if !run.isEmpty { onInput?(.text(run)); run = "" }
            onInput?(.key(key))
        }
        if !run.isEmpty { onInput?(.text(run)) }
    }

    func focusChanged(_ on: Bool) {
        if !on, !marked.isEmpty { setMarked("", selected: NSRange()) }
        onFocus?(on)
        screen.needsDisplay = true
    }

    // MARK: Drawing

    /// A continuation line's cells: the borders of the row's indent again, blanks (on the row's background), then its
    /// own cells.
    private static func hanging(_ row: TermRow, _ line: DisplayLine) -> TermRow {
        var cells = row.prefix(line.indent).map { TerminalWrap.borders.contains($0.text) ? $0 : TermCell(text: " ", width: 1, style: $0.style) }
        cells += row[line.from..<min(line.to, row.count)]
        return cells
    }

    /// Draws the lines in `visible` (document coordinates, top-left origin: the document view is flipped).
    fileprivate func draw(in ctx: CGContext, visible: CGRect) {
        let side = theme.side(dark)
        let h = metrics.cell.height, w = metrics.cell.width
        let firstLine = max(0, Int((visible.minY - inset.top) / h))
        let lastLine = min(lines.count - 1, Int((visible.maxY - inset.top) / h))
        if firstLine <= lastLine {
            ctx.saveGState()
            ctx.textMatrix = CGAffineTransform(scaleX: 1, y: -1)
            let sel = selection.map { ($0.anchor < $0.focus ? $0.anchor : $0.focus, $0.anchor < $0.focus ? $0.focus : $0.anchor) }
            for i in firstLine...lastLine {
                let line = lines[i]
                let origin = CGPoint(x: inset.left, y: inset.top + CGFloat(i) * h)
                let key = CacheKey(row: line.row, from: line.from, to: line.to)
                let render = cache[key] ?? {
                    let row = content.rows[line.row]
                    let r = line.indent == 0
                        ? LineRender(row, from: line.from, to: line.to, metrics: metrics, side: side)
                        : LineRender(Self.hanging(row, line), from: 0, to: line.indent + line.to - line.from, metrics: metrics, side: side)
                    cache[key] = r
                    return r
                }()
                if let (a, b) = sel, i >= a.line, i <= b.line {
                    let from = i == a.line ? a.col : 0
                    let to = i == b.line ? b.col + 1 : max(line.to - line.from, 1)
                    ctx.setFillColor(UIColor(hex: side.selection).withAlphaComponent(0.32).cgColor)
                    ctx.fill(CGRect(x: origin.x + CGFloat(line.indent + from) * w, y: origin.y, width: CGFloat(max(0, to - from)) * w, height: h))
                }
                render.draw(in: ctx, at: origin)
                if let c = content.cursor, c.row == line.row, c.col >= line.from, c.col < max(line.to, line.from + 1) {
                    // Typing here: a solid block; otherwise an outline.
                    let rect = CGRect(x: origin.x + CGFloat(line.indent + c.col - line.from) * w, y: origin.y + 1, width: w, height: h - 2)
                    if typing {
                        ctx.setFillColor(UIColor(hex: side.cursor).withAlphaComponent(0.7).cgColor)
                        ctx.fill(rect)
                    } else {
                        ctx.setStrokeColor(UIColor(hex: side.cursor).withAlphaComponent(0.8).cgColor)
                        ctx.setLineWidth(1)
                        ctx.stroke(rect.insetBy(dx: 0.5, dy: 0.5))
                    }
                }
            }
            ctx.restoreGState()
        }
        drawMarked(side)
    }

    /// The composition over the cursor, underlined in the cursor's color (the phone's marked label).
    private func drawMarked(_ side: TerminalTheme.Side) {
        guard !marked.isEmpty else { return }
        let text = NSAttributedString(string: marked, attributes: [
            .font: metrics.regular, .foregroundColor: UIColor(hex: side.foreground),
            .underlineStyle: NSUnderlineStyle.single.rawValue, .underlineColor: UIColor(hex: side.cursor),
        ])
        let size = text.size()
        let r = cursorRect()
        let frame = CGRect(x: min(r.minX, max(inset.left, docSize.width - size.width - inset.right)), y: r.minY,
                           width: size.width + 2, height: r.height)
        UIColor(hex: side.background).withAlphaComponent(0.92).setFill()
        NSBezierPath(roundedRect: frame, xRadius: 3, yRadius: 3).fill()
        text.draw(at: CGPoint(x: frame.minX + 1, y: frame.minY + (frame.height - size.height) / 2))
    }

    // MARK: Scrolling, the wheel, pinch, swipe

    /// Driving: the scroll wheel goes to the terminal and this view stays at the end; otherwise it scrolls itself.
    func setWheel(_ wheel: ((Int, (col: Int, row: Int)?) -> Void)?) {
        let was = onWheel != nil
        onWheel = wheel
        if wheel != nil, !was { scroll(toY: max(0, docSize.height - viewport.height)) }
    }

    override func scrollWheel(with event: NSEvent) {
        guard onWheel != nil else {
            super.scrollWheel(with: event)
            if offset.y < 600 { onNearTop?() }
            return
        }
        // A new gesture (or a plain mouse wheel's notch): over the cell under the pointer, as a mouse wheel is.
        if event.phase == .began || event.phase == .mayBegin || (event.phase.isEmpty && event.momentumPhase.isEmpty) {
            if event.phase == .began { wheelRows = 0 }
            wheelAt = screenCell(at: screen.convert(event.locationInWindow, from: nil))
        }
        // Trackpads give points (with the system's own momentum after the fingers lift); a wheel gives lines.
        let dy = event.hasPreciseScrollingDeltas ? event.scrollingDeltaY : event.scrollingDeltaY * metrics.cell.height
        wheel(by: dy)
    }

    /// Points scrolled to whole rows of the font (the rest waits for the next event): content moving down is back in history.
    private func wheel(by dy: CGFloat) {
        guard let onWheel, metrics.cell.height > 0 else { return }
        wheelRows += dy / metrics.cell.height
        let n = Int(wheelRows)
        guard n != 0 else { return }
        wheelRows -= CGFloat(n)
        onWheel(n, wheelAt)
    }

    override func magnify(with event: NSEvent) {
        guard let onFontSize else { return }
        if event.phase == .began || pinchScale <= 0 { pinchFrom = fontSize; pinchScale = 1 }
        pinchScale *= 1 + event.magnification
        onFontSize(Self.clampFont(pinchFrom * pinchScale))
        if event.phase == .ended || event.phase == .cancelled { pinchScale = 1 }
    }

    override func swipe(with event: NSEvent) {
        guard let onSwipe else { return super.swipe(with: event) }
        // deltaX is +1 for a swipe to the left, deltaY +1 for a swipe up.
        if event.deltaX > 0 { onSwipe(.left) } else if event.deltaX < 0 { onSwipe(.right) }
        else if event.deltaY > 0 { onSwipe(.up) } else if event.deltaY < 0 { onSwipe(.down) }
    }

    /// ⌘+ / ⌘− / ⌘0 on the terminal: one step bigger, smaller, the default.
    fileprivate func fontKey(_ chars: String) -> Bool {
        guard let onFontSize else { return false }
        switch chars {
        case "=", "+": onFontSize(Self.clampFont(fontSize + Self.fontStep))
        case "-", "_": onFontSize(Self.clampFont(fontSize - Self.fontStep))
        case "0": onFontSize(TerminalPrefs.defaultFontSize)
        default: return false
        }
        return true
    }

    private static func clampFont(_ s: CGFloat) -> CGFloat {
        (min(max(s, fontRange.lowerBound), fontRange.upperBound) * 10).rounded() / 10
    }

    // MARK: Mouse

    func setMouse(_ forward: ((MacTerminalMouse.Event) -> Void)?, mode: MacTerminalMouse.Mode) {
        onMouse = forward
        mouseMode = mode
        if forward == nil { held = nil } // no longer driving: nothing to release (the program lost the pointer with it)
    }

    /// A press of any button: the program's (true, sent) or this view's to handle (false). Another button while one is
    /// held is swallowed.
    fileprivate func forwardDown(_ event: NSEvent) -> Bool {
        if held != nil { return true }
        guard let onMouse, let button = MacTerminalMouse.button(type: event.type, number: event.buttonNumber),
              MacTerminalMouse.toProgram(mouseMode, flags: event.modifierFlags),
              let at = mouseCell(event, clamp: false) else { return false }
        selection = nil // what was selected with Shift goes: the program has the pointer now
        pressedAt = nil
        held = (button, at, MacTerminalMouse.CellThrottle(at))
        onMouse(.init(action: .down, button: button, column: at.col, row: at.row, mods: MacTerminalMouse.modifiers(event.modifierFlags)))
        return true
    }

    /// Motion with the held button: one event per cell, following the pointer out of the view (clamped to the screen).
    fileprivate func forwardDrag(_ event: NSEvent) -> Bool {
        guard var h = held else { return false }
        guard MacTerminalMouse.button(type: event.type, number: event.buttonNumber) == h.button, let onMouse, let at = mouseCell(event, clamp: true),
              h.throttle.step(at) else { return true }
        h.cell = at
        held = h
        onMouse(.init(action: .drag, button: h.button, column: at.col, row: at.row, mods: MacTerminalMouse.modifiers(event.modifierFlags)))
        return true
    }

    fileprivate func forwardUp(_ event: NSEvent) -> Bool {
        guard let h = held else { return false }
        guard MacTerminalMouse.button(type: event.type, number: event.buttonNumber) == h.button else { return true }
        held = nil
        let at = mouseCell(event, clamp: true) ?? h.cell
        onMouse?(.init(action: .up, button: h.button, column: at.col, row: at.row, mods: MacTerminalMouse.modifiers(event.modifierFlags)))
        return true
    }

    /// The live screen's cell under an event. Below the last row drawn (the feed leaves out blank rows at the end) it
    /// counts on in rows; above the screen (the history kept while driving) it is outside.
    private func mouseCell(_ event: NSEvent, clamp: Bool) -> (col: Int, row: Int)? {
        guard !lines.isEmpty, metrics.cell.height > 0, metrics.cell.width > 0 else { return nil }
        let p = screen.convert(event.locationInWindow, from: nil)
        let i = Int(((p.y - inset.top) / metrics.cell.height).rounded(.down))
        let x = Int(((p.x - inset.left) / metrics.cell.width).rounded(.down))
        let row: Int, col: Int
        if lines.indices.contains(i) {
            row = lines[i].row - content.screenStart
            col = lines[i].from + x - lines[i].indent
        } else if i >= lines.count {
            row = lines[lines.count - 1].row - content.screenStart + i - lines.count + 1
            col = x
        } else {
            row = lines[0].row - content.screenStart + i
            col = x
        }
        let cols = content.cols > 0 ? content.cols : reportedGrid?.cols ?? MacTerminalMouse.maxCell
        let rows = reportedGrid?.rows ?? MacTerminalMouse.maxCell
        return MacTerminalMouse.cell(col: col, row: row, cols: cols, rows: rows, clamp: clamp)
    }

    private func cell(at p: CGPoint) -> CellPoint? {
        guard !lines.isEmpty else { return nil }
        let line = min(max(0, Int((p.y - inset.top) / metrics.cell.height)), lines.count - 1)
        let span = max(1, lines[line].to - lines[line].from)
        var col = min(max(0, Int((p.x - inset.left) / metrics.cell.width) - lines[line].indent), span - 1)
        let row = content.rows[lines[line].row]
        if lines[line].from + col < row.count, row[lines[line].from + col].width == 0, col > 0 { col -= 1 }
        return CellPoint(line: line, col: col)
    }

    fileprivate func mouseDown(at p: CGPoint, clicks: Int) {
        dragged = false
        pressCleared = false
        pressedAt = nil
        guard let c = cell(at: p) else { return }
        switch clicks {
        case 2: // a word, as text views do
            selection = (wordStart(c), wordEnd(c))
        case 3...: // the whole line
            selection = (CellPoint(line: c.line, col: 0), CellPoint(line: c.line, col: max(0, lines[c.line].to - lines[c.line].from - 1)))
        default:
            if selection != nil { selection = nil; pressCleared = true }
            pressedAt = c
        }
    }

    fileprivate func mouseDragged(to p: CGPoint) {
        guard let start = pressedAt, let c = cell(at: p) else { return }
        if !dragged, c == start { return }
        dragged = true
        selection = (start, c)
    }

    fileprivate func mouseUp(at p: CGPoint, clicks: Int) {
        defer { pressedAt = nil }
        guard clicks == 1, !dragged, !pressCleared, pressedAt != nil else { return }
        if let c = cell(at: p), let url = link(at: c) {
            onOpenURL?(url)
            return
        }
        if let onClick, let at = screenCell(at: p) { onClick(at.col, at.row) }
    }

    /// The cell of the live screen under a point (not clamped to the text: a click may go anywhere on the row).
    private func screenCell(at p: CGPoint) -> (col: Int, row: Int)? {
        guard !lines.isEmpty, metrics.cell.height > 0, metrics.cell.width > 0 else { return nil }
        let i = Int((p.y - inset.top) / metrics.cell.height)
        guard lines.indices.contains(i) else { return nil }
        let line = lines[i]
        let row = line.row - content.screenStart
        let col = line.from + Int((p.x - inset.left) / metrics.cell.width) - line.indent
        guard row >= 0, col >= 0, content.cols == 0 || col < content.cols else { return nil }
        return (col, row)
    }

    /// The OSC 8 link under a cell, else a URL written in its row.
    private func link(at c: CellPoint) -> URL? {
        let line = lines[c.line]
        let row = content.rows[line.row]
        let col = line.from + c.col
        if col < row.count, let s = row[col].style.link, let url = URL(string: s) { return url }
        let text = row.map { $0.width == 0 ? "" : $0.text }.joined()
        // Cell columns to string offsets: wide characters take one character but two cells.
        var offset = 0
        for i in 0..<min(col, row.count) where row[i].width > 0 { offset += row[i].text.utf16.count }
        return Links.scan(text).first { NSLocationInRange(offset, $0.range) }?.url // where links end: `Links`
    }

    private func isWordCell(_ c: CellPoint) -> Bool {
        let line = lines[c.line], row = content.rows[line.row], i = line.from + c.col
        guard i < row.count else { return false }
        let t = row[i].width == 0 && i > 0 ? row[i - 1].text : row[i].text
        return !(t == " " || t.isEmpty)
    }

    private func wordStart(_ c: CellPoint) -> CellPoint {
        guard isWordCell(c) else { return c }
        var p = c
        while p.col > 0, isWordCell(CellPoint(line: p.line, col: p.col - 1)) { p.col -= 1 }
        return p
    }

    private func wordEnd(_ c: CellPoint) -> CellPoint {
        guard isWordCell(c) else { return c }
        var p = c
        let span = lines[c.line].to - lines[c.line].from
        while p.col + 1 < span, isWordCell(CellPoint(line: p.line, col: p.col + 1)) { p.col += 1 }
        return p
    }

    // MARK: Copy, select all, paste

    var hasSelection: Bool { selection != nil }

    /// The selected text: lines of the same row join, rows end in a newline, trailing blanks go.
    func selectedText() -> String {
        guard let s = selection else { return "" }
        return text(from: min(s.anchor, s.focus), to: max(s.anchor, s.focus))
    }

    private func text(from a: CellPoint, to b: CellPoint) -> String {
        var out = ""
        var current = ""
        for i in a.line...b.line {
            let line = lines[i], row = content.rows[line.row]
            let from = line.from + (i == a.line ? a.col : 0)
            let to = i == b.line ? min(line.from + b.col + 1, row.count) : min(line.to, row.count)
            if from < to { for c in row[from..<to] where c.width > 0 { current += c.text } }
            if i == b.line || lines[i + 1].row != line.row {
                while current.hasSuffix(" ") { current.removeLast() }
                out += current + (i == b.line ? "" : "\n")
                current = ""
            }
        }
        return out
    }

    fileprivate func copySelection() {
        guard selection != nil else { return }
        UIPasteboard.general.string = selectedText()
        onCopy?()
        selection = nil
    }

    fileprivate func selectEverything() {
        guard !lines.isEmpty else { return }
        selection = (CellPoint(line: 0, col: 0),
                     CellPoint(line: lines.count - 1, col: max(0, lines[lines.count - 1].to - lines[lines.count - 1].from - 1)))
    }

    /// ⌘V: files copied in Finder or a picture alone (a screenshot) are uploaded (`onFiles`); text is the page's paste
    /// (it asks first for several lines), else the clipboard's text typed.
    fileprivate func paste() {
        if let onFiles {
            switch MacPaste.current() {
            case .files(let urls): return onFiles(urls.map { .file($0) })
            case .image:
                if let png = Self.pastedPicture() { return onFiles([.data(png, name: TerminalPaste.pictureName(.png))]) }
            case .text: break
            }
        }
        if let onDoubleTap { onDoubleTap(); return }
        if onInput != nil, let t = UIPasteboard.general.string, !t.isEmpty { emit(t) }
    }

    /// The picture on the pasteboard as PNG (screenshots come as PNG, pictures copied from apps often as TIFF).
    private static func pastedPicture(_ board: NSPasteboard = .general) -> Data? {
        if let png = board.data(forType: .png) { return png }
        guard let tiff = board.data(forType: .tiff) ?? NSImage(pasteboard: board)?.tiffRepresentation else { return nil }
        return NSBitmapImageRep(data: tiff)?.representation(using: .png, properties: [:])
    }

    fileprivate var canPaste: Bool { onDoubleTap != nil || onInput != nil || onFiles != nil }

    // MARK: Dropped files

    fileprivate static func droppedFiles(_ info: NSDraggingInfo) -> [URL] {
        info.draggingPasteboard.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
    }
}

/// The scrollback as one flipped view (top-left origin, like UIKit): it draws the lines in the dirty rect, takes the
/// mouse and, being first responder, the keyboard.
final class TerminalScreenView: NSView, NSTextInputClient, NSMenuItemValidation {
    weak var host: TerminalScrollView?

    override init(frame: NSRect) {
        super.init(frame: frame)
        setAccessibilityElement(true)
        setAccessibilityRole(.staticText)
        setAccessibilityLabel(String(localized: "Terminal"))
        setAccessibilityIdentifier("terminal-screen")
        registerForDraggedTypes([.fileURL])
    }

    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }
    override var isOpaque: Bool { false }
    override var acceptsFirstResponder: Bool { true }
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    override func draw(_ dirtyRect: NSRect) {
        guard let ctx = NSGraphicsContext.current?.cgContext else { return }
        host?.draw(in: ctx, visible: dirtyRect)
    }

    override func resetCursorRects() { addCursorRect(visibleRect, cursor: .iBeam) }

    override func becomeFirstResponder() -> Bool {
        let ok = super.becomeFirstResponder()
        if ok { host?.focusChanged(true) }
        return ok
    }

    override func resignFirstResponder() -> Bool {
        let ok = super.resignFirstResponder()
        if ok { host?.focusChanged(false) }
        return ok
    }

    // MARK: Mouse

    private func point(_ event: NSEvent) -> CGPoint { convert(event.locationInWindow, from: nil) }

    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        if host?.forwardDown(event) == true { return }
        host?.mouseDown(at: point(event), clicks: event.clickCount)
    }

    override func mouseDragged(with event: NSEvent) {
        if host?.forwardDrag(event) == true { return }
        autoscroll(with: event)
        host?.mouseDragged(to: point(event))
    }

    override func mouseUp(with event: NSEvent) {
        if host?.forwardUp(event) == true { return }
        host?.mouseUp(at: point(event), clicks: event.clickCount)
    }

    // The right and middle buttons: the program's while forwarding, else the menu (right) as before.
    override func rightMouseDown(with event: NSEvent) {
        if host?.forwardDown(event) == true {
            window?.makeFirstResponder(self)
            return
        }
        super.rightMouseDown(with: event)
    }

    override func rightMouseDragged(with event: NSEvent) {
        if host?.forwardDrag(event) == true { return }
        super.rightMouseDragged(with: event)
    }

    override func rightMouseUp(with event: NSEvent) {
        if host?.forwardUp(event) == true { return }
        super.rightMouseUp(with: event)
    }

    override func otherMouseDown(with event: NSEvent) {
        if host?.forwardDown(event) == true {
            window?.makeFirstResponder(self)
            return
        }
        super.otherMouseDown(with: event)
    }

    override func otherMouseDragged(with event: NSEvent) {
        if host?.forwardDrag(event) == true { return }
        super.otherMouseDragged(with: event)
    }

    override func otherMouseUp(with event: NSEvent) {
        if host?.forwardUp(event) == true { return }
        super.otherMouseUp(with: event)
    }

    // MARK: Dragging files in

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        guard let host, host.onFiles != nil, !TerminalScrollView.droppedFiles(sender).isEmpty else { return [] }
        host.onDropping?(true)
        return .copy
    }

    override func draggingUpdated(_ sender: NSDraggingInfo) -> NSDragOperation {
        host?.onFiles != nil && !TerminalScrollView.droppedFiles(sender).isEmpty ? .copy : []
    }

    override func draggingExited(_ sender: NSDraggingInfo?) { host?.onDropping?(false) }

    override func draggingEnded(_ sender: NSDraggingInfo) { host?.onDropping?(false) }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        let urls = TerminalScrollView.droppedFiles(sender)
        guard let onFiles = host?.onFiles, !urls.isEmpty else { return false }
        host?.onDropping?(false)
        onFiles(urls.map { .file($0) })
        return true
    }

    override func menu(for event: NSEvent) -> NSMenu? {
        let menu = NSMenu()
        func item(_ title: String, _ icon: String, _ action: Selector, _ key: String) {
            let i = NSMenuItem(title: title, action: action, keyEquivalent: key)
            i.image = NSImage(systemSymbolName: icon, accessibilityDescription: nil)
            i.target = self
            menu.addItem(i)
        }
        item(String(localized: "Copy [Readiness Views d37380]"), Theme.Icon.copy, #selector(copy(_:)), "c")
        if host?.canPaste == true { item(String(localized: "Paste"), Theme.Icon.paste, #selector(paste(_:)), "v") }
        item(String(localized: "Select All"), Theme.Icon.select, #selector(selectAll(_:)), "a")
        return menu
    }

    @objc func copy(_ sender: Any?) { host?.copySelection() }
    @objc func paste(_ sender: Any?) { host?.paste() }
    @objc override func selectAll(_ sender: Any?) { host?.selectEverything() }

    func validateMenuItem(_ item: NSMenuItem) -> Bool {
        switch item.action {
        case #selector(copy(_:)): host?.hasSelection == true
        case #selector(paste(_:)): host?.canPaste == true
        default: true
        }
    }

    // MARK: Keyboard

    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        let flags = event.modifierFlags.intersection(.deviceIndependentFlagsMask)
        if window?.firstResponder === self, flags.contains(.command), !flags.contains(.control), !flags.contains(.option),
           let chars = event.charactersIgnoringModifiers, host?.fontKey(chars) == true {
            return true
        }
        return super.performKeyEquivalent(with: event)
    }

    override func keyDown(with event: NSEvent) {
        guard let host, host.onInput != nil, !event.modifierFlags.contains(.command) else {
            super.keyDown(with: event)
            return
        }
        // While composing, every key belongs to the input method.
        // Function keys, ctrl combinations and (Terminal Settings › Option as Meta) Option combinations go out as herdr keys.
        if host.marked.isEmpty,
           let name = MacTerminalKeys.name(keyCode: event.keyCode, characters: event.charactersIgnoringModifiers,
                                           flags: event.modifierFlags,
                                           optionAsMeta: UserDefaults.standard.bool(forKey: TerminalPrefs.optionAsMeta)) {
            host.onInput?(.key(name))
            return
        }
        interpretKeyEvents([event])
    }

    /// What the key bindings turn a key into when it was not caught above (no composition left to finish).
    override func doCommand(by selector: Selector) {
        let names: [Selector: String] = [
            #selector(insertNewline(_:)): "enter", #selector(insertLineBreak(_:)): "enter", #selector(insertTab(_:)): "tab",
            #selector(insertBacktab(_:)): "shift+tab", #selector(deleteBackward(_:)): "backspace",
            #selector(deleteForward(_:)): "delete", #selector(cancelOperation(_:)): "esc",
            #selector(moveUp(_:)): "up", #selector(moveDown(_:)): "down", #selector(moveLeft(_:)): "left", #selector(moveRight(_:)): "right",
        ]
        if let name = names[selector] { host?.onInput?(.key(name)) }
    }

    // MARK: NSTextInputClient

    private static func plain(_ string: Any) -> String {
        (string as? NSAttributedString)?.string ?? (string as? String) ?? ""
    }

    func insertText(_ string: Any, replacementRange: NSRange) {
        guard let host else { return }
        if !host.marked.isEmpty { host.setMarked("", selected: NSRange()) } // a candidate chosen: it replaces the composition
        host.emit(Self.plain(string))
    }

    func setMarkedText(_ string: Any, selectedRange: NSRange, replacementRange: NSRange) {
        host?.setMarked(Self.plain(string), selected: selectedRange)
    }

    func unmarkText() {
        guard let host else { return }
        let t = host.marked
        host.setMarked("", selected: NSRange())
        if !t.isEmpty { host.emit(t) }
    }

    func selectedRange() -> NSRange { host?.markedSelection ?? NSRange(location: 0, length: 0) }

    func markedRange() -> NSRange {
        guard let m = host?.marked, !m.isEmpty else { return NSRange(location: NSNotFound, length: 0) }
        return NSRange(location: 0, length: (m as NSString).length)
    }

    func hasMarkedText() -> Bool { host?.marked.isEmpty == false }

    func attributedSubstring(forProposedRange range: NSRange, actualRange: NSRangePointer?) -> NSAttributedString? {
        guard let m = host?.marked as NSString?, range.location != NSNotFound else { return nil }
        let a = min(range.location, m.length), b = min(range.location + range.length, m.length)
        actualRange?.pointee = NSRange(location: a, length: b - a)
        return NSAttributedString(string: m.substring(with: NSRange(location: a, length: b - a)))
    }

    func validAttributesForMarkedText() -> [NSAttributedString.Key] { [] }

    /// The input method's candidates sit by the terminal's cursor (in screen coordinates).
    func firstRect(forCharacterRange range: NSRange, actualRange: NSRangePointer?) -> NSRect {
        guard let host, let window else { return .zero }
        return window.convertToScreen(convert(host.cursorRect(), to: nil))
    }

    func characterIndex(for point: NSPoint) -> Int { 0 }
}
#endif
