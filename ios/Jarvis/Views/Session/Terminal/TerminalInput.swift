import UIKit

// Typing straight into the terminal: the screen view takes the keyboard (UITextInput, so input methods with a
// composition — pinyin — and dictation work), and every key becomes a key press for herdr. Nothing is kept here but
// the composition the input method is still working on, which shows over the cursor until it is chosen.

/// What typing on the terminal sends: text as typed, or a key by herdr's name ("enter", "backspace", "up").
nonisolated enum TerminalInputEvent: Equatable, Sendable {
    case text(String)
    case key(String)
}

/// Typing, batched for the keys API: one request carries text and then keys, so while one is on its way the next fills
/// up, and text typed after a key starts another request. A backspace still waiting here takes back the character
/// before it instead of going out.
nonisolated struct KeyBatcher {
    private(set) var pending: [TerminalInputEvent] = []

    var isEmpty: Bool { pending.isEmpty }

    mutating func push(_ e: TerminalInputEvent) {
        switch e {
        case .text(let t):
            guard !t.isEmpty else { return }
            if case .text(let last)? = pending.last { pending[pending.count - 1] = .text(last + t) } else { pending.append(e) }
        case .key("backspace"):
            guard case .text(var last)? = pending.last else { pending.append(e); return }
            last.removeLast()
            if last.isEmpty { pending.removeLast() } else { pending[pending.count - 1] = .text(last) }
        case .key:
            pending.append(e)
        }
    }

    /// The next request: the text at the front, then the keys after it up to the next text.
    mutating func take() -> (keys: [String], text: String)? {
        guard !pending.isEmpty else { return nil }
        var text = "", keys: [String] = [], i = 0
        if case .text(let t) = pending[0] { text = t; i = 1 }
        while i < pending.count, case .key(let k) = pending[i] { keys.append(k); i += 1 }
        pending.removeFirst(i)
        return (keys, text)
    }
}

/// A position in the composition (UTF-16 offset); the "document" the input method sees is only that.
final class TermTextPosition: UITextPosition {
    let offset: Int
    init(_ offset: Int) { self.offset = offset }
}

final class TermTextRange: UITextRange {
    let from: TermTextPosition, to: TermTextPosition
    init(_ a: Int, _ b: Int) { from = TermTextPosition(min(a, b)); to = TermTextPosition(max(a, b)) }
    override var start: UITextPosition { from }
    override var end: UITextPosition { to }
    override var isEmpty: Bool { from.offset == to.offset }
}

extension TerminalScrollView: UITextInput {
    override var canBecomeFirstResponder: Bool { true }

    override func becomeFirstResponder() -> Bool {
        let ok = super.becomeFirstResponder()
        if ok { onFocus?(true); canvas.setNeedsDisplay() }
        return ok
    }

    override func resignFirstResponder() -> Bool {
        let ok = super.resignFirstResponder()
        if ok {
            if !marked.isEmpty { setMarked("", selected: NSRange()) }
            onFocus?(false)
            canvas.setNeedsDisplay()
        }
        return ok
    }

    // MARK: UIKeyInput

    var hasText: Bool { true } // backspace always reaches the terminal, and the keyboard does not shift on its own

    func insertText(_ text: String) {
        if !marked.isEmpty { setMarked("", selected: NSRange()) } // a candidate chosen: it replaces the composition
        emit(text)
    }

    func deleteBackward() {
        onInput?(.key("backspace"))
    }

    /// Return, tab and newlines in a paste go out as keys; the rest as text.
    private func emit(_ text: String) {
        var run = ""
        for ch in text {
            let key: String? = ch == "\n" || ch == "\r" || ch == "\r\n" ? "enter" : ch == "\t" ? "tab" : nil
            guard let key else { run.append(ch); continue }
            if !run.isEmpty { onInput?(.text(run)); run = "" }
            onInput?(.key(key))
        }
        if !run.isEmpty { onInput?(.text(run)) }
    }

    // MARK: Composition

    private var markedLength: Int { (marked as NSString).length }

    var markedTextRange: UITextRange? { marked.isEmpty ? nil : TermTextRange(0, markedLength) }

    var selectedTextRange: UITextRange? {
        get { TermTextRange(markedSelection.location, markedSelection.location + markedSelection.length) }
        set {
            guard let r = newValue as? TermTextRange else { return }
            markedSelection = NSRange(location: r.from.offset, length: r.to.offset - r.from.offset)
        }
    }

    func setMarkedText(_ markedText: String?, selectedRange: NSRange) {
        setMarked(markedText ?? "", selected: selectedRange)
    }

    func unmarkText() {
        let t = marked
        setMarked("", selected: NSRange())
        if !t.isEmpty { emit(t) }
    }

    func text(in range: UITextRange) -> String? {
        guard let r = range as? TermTextRange else { return nil }
        let ns = marked as NSString
        let a = min(r.from.offset, ns.length), b = min(r.to.offset, ns.length)
        return ns.substring(with: NSRange(location: a, length: b - a))
    }

    /// Dictation and some keyboards replace a range instead of inserting: whatever they put in is typed.
    func replace(_ range: UITextRange, withText text: String) {
        if !marked.isEmpty { setMarked("", selected: NSRange()) }
        emit(text)
    }

    var beginningOfDocument: UITextPosition { TermTextPosition(0) }
    var endOfDocument: UITextPosition { TermTextPosition(markedLength) }

    func textRange(from fromPosition: UITextPosition, to toPosition: UITextPosition) -> UITextRange? {
        guard let a = fromPosition as? TermTextPosition, let b = toPosition as? TermTextPosition else { return nil }
        return TermTextRange(a.offset, b.offset)
    }

    func position(from position: UITextPosition, offset: Int) -> UITextPosition? {
        guard let p = position as? TermTextPosition else { return nil }
        let o = p.offset + offset
        return o < 0 || o > markedLength ? nil : TermTextPosition(o)
    }

    func position(from position: UITextPosition, in direction: UITextLayoutDirection, offset: Int) -> UITextPosition? {
        switch direction {
        case .left: self.position(from: position, offset: -offset)
        case .right: self.position(from: position, offset: offset)
        default: nil
        }
    }

    func compare(_ position: UITextPosition, to other: UITextPosition) -> ComparisonResult {
        let a = (position as? TermTextPosition)?.offset ?? 0, b = (other as? TermTextPosition)?.offset ?? 0
        return a < b ? .orderedAscending : a > b ? .orderedDescending : .orderedSame
    }

    func offset(from: UITextPosition, to toPosition: UITextPosition) -> Int {
        ((toPosition as? TermTextPosition)?.offset ?? 0) - ((from as? TermTextPosition)?.offset ?? 0)
    }

    var tokenizer: UITextInputTokenizer { UITextInputStringTokenizer(textInput: self) }

    func position(within range: UITextRange, farthestIn direction: UITextLayoutDirection) -> UITextPosition? {
        direction == .left || direction == .up ? range.start : range.end
    }

    func characterRange(byExtending position: UITextPosition, in direction: UITextLayoutDirection) -> UITextRange? {
        guard let p = position as? TermTextPosition else { return nil }
        return direction == .left || direction == .up ? TermTextRange(0, p.offset) : TermTextRange(p.offset, markedLength)
    }

    func baseWritingDirection(for position: UITextPosition, in direction: UITextStorageDirection) -> NSWritingDirection { .leftToRight }
    func setBaseWritingDirection(_ writingDirection: NSWritingDirection, for range: UITextRange) {}

    /// The input method's candidates sit by the terminal's cursor.
    func firstRect(for range: UITextRange) -> CGRect { cursorRect() }
    func caretRect(for position: UITextPosition) -> CGRect { cursorRect() }
    func selectionRects(for range: UITextRange) -> [UITextSelectionRect] { [] }
    func closestPosition(to point: CGPoint) -> UITextPosition? { TermTextPosition(markedLength) }
    func closestPosition(to point: CGPoint, within range: UITextRange) -> UITextPosition? { range.end }
    func characterRange(at point: CGPoint) -> UITextRange? { nil }

    // MARK: Hardware keyboard

    override var keyCommands: [UIKeyCommand]? {
        var list: [UIKeyCommand] = [
            UIKeyCommand(input: UIKeyCommand.inputUpArrow, modifierFlags: [], action: #selector(hardwareKey(_:))),
            UIKeyCommand(input: UIKeyCommand.inputDownArrow, modifierFlags: [], action: #selector(hardwareKey(_:))),
            UIKeyCommand(input: UIKeyCommand.inputLeftArrow, modifierFlags: [], action: #selector(hardwareKey(_:))),
            UIKeyCommand(input: UIKeyCommand.inputRightArrow, modifierFlags: [], action: #selector(hardwareKey(_:))),
            UIKeyCommand(input: UIKeyCommand.inputEscape, modifierFlags: [], action: #selector(hardwareKey(_:))),
            UIKeyCommand(input: "\t", modifierFlags: [], action: #selector(hardwareKey(_:))),
            UIKeyCommand(input: "\t", modifierFlags: .shift, action: #selector(hardwareKey(_:))),
        ]
        for c in "abcdefghijklmnopqrstuvwxyz[]\\" {
            list.append(UIKeyCommand(input: String(c), modifierFlags: .control, action: #selector(hardwareKey(_:))))
        }
        for c in list { c.wantsPriorityOverSystemBehavior = true }
        return list
    }

    @objc private func hardwareKey(_ c: UIKeyCommand) {
        guard let input = c.input else { return }
        let names = [UIKeyCommand.inputUpArrow: "up", UIKeyCommand.inputDownArrow: "down", UIKeyCommand.inputLeftArrow: "left",
                     UIKeyCommand.inputRightArrow: "right", UIKeyCommand.inputEscape: "esc", "\t": "tab"]
        var name = names[input] ?? input
        if c.modifierFlags.contains(.control) { name = "ctrl+" + name }
        if c.modifierFlags.contains(.shift) { name = "shift+" + name }
        onInput?(.key(name))
    }
}
