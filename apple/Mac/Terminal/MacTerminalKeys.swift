#if os(macOS)
import AppKit

/// A key press on the Mac terminal as herdr's key name ("up", "shift+tab", "ctrl+c", "alt+b"), or nil when it is typed
/// text (it goes through the input method, `interpretKeyEvents`, and arrives as `insertText`).
nonisolated enum MacTerminalKeys {
    static let special: [UInt16: String] = [
        126: "up", 125: "down", 123: "left", 124: "right", 53: "esc", 48: "tab", 36: "enter", 76: "enter",
        51: "backspace", 117: "delete", 115: "home", 119: "end", 116: "pageup", 121: "pagedown",
        122: "f1", 120: "f2", 99: "f3", 118: "f4", 96: "f5", 97: "f6", 98: "f7", 100: "f8", 101: "f9", 109: "f10",
        103: "f11", 111: "f12",
    ]

    /// - Parameters:
    ///   - characters: the event's `charactersIgnoringModifiers`.
    ///   - optionAsMeta: Terminal Settings › Use Option as Meta Key; off, Option keeps typing the layout's characters.
    static func name(keyCode: UInt16, characters: String?, flags: NSEvent.ModifierFlags, optionAsMeta: Bool) -> String? {
        let f = flags.intersection(.deviceIndependentFlagsMask)
        var base = special[keyCode]
        let isSpecial = base != nil
        let meta = optionAsMeta && f.contains(.option)
        // Shift on a letter is a modifier herdr encodes; on a digit or symbol it is already in the character ("alt+>").
        var shiftedLetter = false
        if base == nil, f.contains(.control) || meta, let raw = characters, raw.count == 1 {
            let ch = raw.lowercased()
            if f.contains(.control) {
                base = ch == " " ? "space" : ch
            } else if raw.unicodeScalars.count == 1, let scalar = raw.unicodeScalars.first, (0x20...0x7E).contains(scalar.value) {
                // Meta only for what has a herdr key name: printable ASCII. Anything else stays text.
                base = ch == " " ? "space" : ch
                shiftedLetter = f.contains(.shift) && CharacterSet.letters.contains(scalar)
            }
        }
        guard let base else { return nil }
        var mods: [String] = []
        if f.contains(.control) { mods.append("ctrl") }
        if f.contains(.option), isSpecial || meta { mods.append("alt") }
        if f.contains(.shift), isSpecial || shiftedLetter { mods.append("shift") }
        return (mods + [base]).joined(separator: "+")
    }
}
#endif
