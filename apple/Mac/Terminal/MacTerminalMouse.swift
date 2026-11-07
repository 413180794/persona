import AppKit

/// The pointer on a terminal the Mac drives (docs/integrations/terminal.md section 25, as the web's `lib/terminal-mouse.ts`): which
/// presses go to herdr as `terminal_input.mouse` (and so to a program that takes the mouse: vim, Claude Code, Codex) and
/// which stay here (selection, the right-click menu), the button and modifier bits herdr takes, and one motion event
/// per cell. Pure, so the decisions are tested without a window.
nonisolated enum MacTerminalMouse {
    enum Button: String, Equatable { case left, middle, right }
    enum Action: String, Equatable { case down, up, drag }

    /// Who gets the mouse (Terminal Settings › Mouse): herdr does not say whether the program in the pane takes it, so it is the user's.
    /// program (the default): presses go to herdr, Shift keeps them here; local: they stay here, Shift sends them on.
    enum Mode: String, CaseIterable { case program, local }

    struct Event: Equatable {
        var action: Action
        var button: Button
        var column: Int
        var row: Int
        var mods: Int
    }

    /// herdr's modifier bits on a mouse event (measured: docs/integrations/terminal.md section 25).
    static let shift = 1, ctrl = 2, alt = 4
    /// herdr drops a cell past this; the hub refuses it too.
    static let maxCell = 4096

    static func mode(_ raw: String?) -> Mode { raw == Mode.local.rawValue ? .local : .program }

    /// NSEvent.buttonNumber → herdr's button (0 left, 1 right, 2 middle); the others (back, forward) have none.
    static func button(_ number: Int) -> Button? {
        switch number {
        case 0: .left
        case 1: .right
        case 2: .middle
        default: nil
        }
    }

    /// The button of an AppKit mouse event: by its type for the left and right buttons, by number for the others.
    static func button(type: NSEvent.EventType, number: Int) -> Button? {
        switch type {
        case .leftMouseDown, .leftMouseDragged, .leftMouseUp: .left
        case .rightMouseDown, .rightMouseDragged, .rightMouseUp: .right
        default: button(number)
        }
    }

    /// Whether a press goes to herdr. Shift flips the route in both modes (so it is not also sent as a modifier); ⌘
    /// always stays here: ⌘-click opens a link, and a program has no use for it.
    static func toProgram(_ mode: Mode, flags: NSEvent.ModifierFlags) -> Bool {
        if flags.contains(.command) { return false }
        return flags.contains(.shift) == (mode == .local)
    }

    /// The modifier bits sent with an event routed to the program: ctrl and option (Shift chose the route).
    static func modifiers(_ flags: NSEvent.ModifierFlags) -> Int {
        (flags.contains(.control) ? ctrl : 0) | (flags.contains(.option) ? alt : 0)
    }

    /// One motion event per cell: a drag is a stream of points, a program wants cells.
    struct CellThrottle {
        private var last: [Int]?

        init(_ cell: (col: Int, row: Int)? = nil) { last = cell.map { [$0.col, $0.row] } }

        /// True when this cell is not the one the last event was sent for.
        mutating func step(_ cell: (col: Int, row: Int)) -> Bool {
            let key = [cell.col, cell.row]
            guard key != last else { return false }
            last = key
            return true
        }
    }

    /// A screen cell from a column and row that may fall outside it: nil when outside (a press there is not the
    /// program's), or with `clamp` the nearest edge cell (a drag that left the terminal keeps pointing at its edge).
    static func cell(col: Int, row: Int, cols: Int, rows: Int, clamp: Bool) -> (col: Int, row: Int)? {
        let maxCol = min(max(cols, 1), maxCell) - 1, maxRow = min(max(rows, 1), maxCell) - 1
        if clamp { return (min(max(0, col), maxCol), min(max(0, row), maxRow)) }
        guard col >= 0, row >= 0, col <= maxCol, row <= maxRow else { return nil }
        return (col, row)
    }
}
