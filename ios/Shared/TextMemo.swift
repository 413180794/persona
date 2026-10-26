import Foundation

/// Results derived from a message's text, computed once per distinct text. Chat rows redraw all the time while
/// the list scrolls or streams; the regexes, link detectors and parsers behind these were the main thread's
/// hottest work (a profile of a long chat: ~15% of all main-thread time). Hashing the text is far cheaper.
nonisolated final class TextMemo<Value>: @unchecked Sendable {
    private var values: [String: Value] = [:]
    private let lock = NSLock()
    private let limit: Int

    init(limit: Int = 4000) { self.limit = limit }

    func callAsFunction(_ text: String, _ make: (String) -> Value) -> Value {
        lock.lock()
        if let v = values[text] { lock.unlock(); return v }
        lock.unlock()
        let v = make(text)
        lock.lock()
        if values.count >= limit { values.removeAll(keepingCapacity: true) } // a long session's worth; then start over
        values[text] = v
        lock.unlock()
        return v
    }
}
