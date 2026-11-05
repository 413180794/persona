import Foundation

/// Keys and pastes to the Mac in the order they were made (#2374). A paste waits on the Mac's clipboard being written
/// (through the hub) before its ⌘V, so everything made after it (keys, further pastes) waits behind it on one tail;
/// with nothing pending, keys go out at once. A paste whose clipboard write fails sends no ⌘V (it would paste whatever
/// was on the Mac's clipboard before); what comes after it still goes out.
@MainActor final class RFBInputQueue {
    /// One key event: its keysym, and down or up.
    typealias Event = (keysym: UInt32, down: Bool)

    private let send: @MainActor (Event) -> Void
    private let write: @MainActor (String) async -> Bool
    private var tail: Task<Bool, Never>?
    private var serial = 0
    /// A paste of typed text failed and was told: the next failures in the same run say nothing more.
    private var typedFailed = false

    /// `send`: a key event onto the wire; `write`: text onto the Mac's clipboard, true once it is there.
    init(send: @escaping @MainActor (Event) -> Void, write: @escaping @MainActor (String) async -> Bool) {
        self.send = send
        self.write = write
    }

    /// Nothing waiting: keys go out as they are made.
    var idle: Bool { tail == nil }

    /// These key events, in order, after whatever is pending.
    func keys(_ events: [Event]) {
        guard tail != nil else {
            for e in events { send(e) }
            return
        }
        enqueue { [send] in
            for e in events { send(e) }
            return true
        }
    }

    /// A key pressed and let go, with these modifiers held around it.
    func press(_ keysym: UInt32, with modifiers: [UInt32] = []) {
        keys(modifiers.map { ($0, true) } + [(keysym, true), (keysym, false)] + modifiers.reversed().map { ($0, false) })
    }

    /// Text typed: Latin-1 a key at a time, anything else (Chinese from the input method) pasted, in the order typed.
    /// `failed` once for a run of failed pastes: the run ends at a paste that goes through, or when everything has gone
    /// out before the next text.
    func type(_ text: String, failed: @escaping @MainActor () -> Void) {
        if idle { typedFailed = false }
        var plain: [Event] = []
        var other = ""
        func flushPlain() {
            if !plain.isEmpty { keys(plain) }
            plain = []
        }
        func flushOther() {
            guard !other.isEmpty else { return }
            paste(other) { [weak self] ok in
                guard let self else { return }
                if ok {
                    self.typedFailed = false
                } else if !self.typedFailed {
                    self.typedFailed = true
                    failed()
                }
            }
            other = ""
        }
        for ch in text.unicodeScalars {
            if ch.value < 256 || ch == "\n" || ch == "\t" {
                flushOther()
                let k = RFBSession.keysym(ch)
                plain += [(k, true), (k, false)]
            } else {
                flushPlain()
                other.unicodeScalars.append(ch)
            }
        }
        flushPlain()
        flushOther()
    }

    /// The text onto the Mac's clipboard and then ⌘V there, after whatever is pending; the task's value (and `done`,
    /// called in the queue's order before anything after it runs) is whether the clipboard took it (no ⌘V when not).
    @discardableResult func paste(_ text: String, done: (@MainActor (Bool) -> Void)? = nil) -> Task<Bool, Never> {
        enqueue { [send, write] in
            let ok = await write(text)
            if ok { for e in Self.commandV { send(e) } }
            done?(ok)
            return ok
        }
    }

    /// ⌘V as key events (macOS's Screen Sharing takes ⌘ as Super).
    static let commandV: [Event] = [(RFBSession.command, true), (0x76, true), (0x76, false), (RFBSession.command, false)]

    @discardableResult private func enqueue(_ op: @escaping @MainActor () async -> Bool) -> Task<Bool, Never> {
        let previous = tail
        serial &+= 1
        let id = serial
        let task = Task { @MainActor [weak self] in
            _ = await previous?.value
            let ok = await op()
            if let self, self.serial == id { self.tail = nil }
            return ok
        }
        tail = task
        return task
    }
}
