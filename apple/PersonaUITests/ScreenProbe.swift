import XCTest

/// Reads what the app's parts of the screen show (`probe.<area>`, the app's ScreenProbe, launched with `-probes YES`)
/// and checks it against the truth: consistent (the same, field by field, but for what may lag), timely (within a
/// bound) and converging (after any sequence of pushes and gestures, within a bound). docs/apple/ios-state.md.
extension XCUIApplication {
    /// The probe's whole line: `{"area","seq","page","v"}`. In the shell both pages are built: `page` picks one; nil
    /// takes the first.
    func probeEnvelope(_ area: String, page: String? = nil) -> [String: Any]? {
        let all = descendants(matching: .any).matching(identifier: "probe." + area)
        for i in 0..<all.count {
            guard let text = all.element(boundBy: i).value as? String,
                  let obj = (try? JSONSerialization.jsonObject(with: Data(text.utf8))) as? [String: Any] else { continue }
            if page == nil || obj["page"] as? String == page { return obj }
        }
        return nil
    }

    /// The probe's value `v`, decoded.
    func probe<T: Decodable>(_ area: String, as type: T.Type, page: String? = nil) -> T? {
        guard let v = probeEnvelope(area, page: page)?["v"], let data = try? JSONSerialization.data(withJSONObject: v) else { return nil }
        return try? JSONDecoder().decode(T.self, from: data)
    }

    func probeSeq(_ area: String, page: String? = nil) -> Int? { probeEnvelope(area, page: page)?["seq"] as? Int }

    /// When the probe's value last changed (the app's clock, the Mac's: compare with `Date.now` here).
    func probeChanged(_ area: String, page: String? = nil) -> Date? {
        (probeEnvelope(area, page: page)?["at"] as? NSNumber).map { Date(timeIntervalSince1970: $0.doubleValue / 1000) }
    }
}

/// Reads again and again until `check` finds no difference or `within` seconds have gone; the seconds it took, or nil
/// (the assertion fails with the last differences). `within: 0` checks once: consistent now.
@discardableResult
func eventually(_ what: String, within: TimeInterval, poll: TimeInterval = 0.1,
                file: StaticString = #filePath, line: UInt = #line, _ check: () -> [String]) -> TimeInterval? {
    let start = Date.now
    var last: [String] = []
    repeat {
        last = check()
        if last.isEmpty { return Date.now.timeIntervalSince(start) }
        if within <= 0 { break }
        Thread.sleep(forTimeInterval: poll)
    } while Date.now.timeIntervalSince(start) < within
    XCTFail("\(what): not within \(within) s:\n" + last.prefix(12).joined(separator: "\n"), file: file, line: line)
    return nil
}

/// Field by field: a difference for every key whose value differs (or is missing on one side), but those in
/// `deferrable` (allowed to lag).
func differences(shown: [String: String], truth: [String: String], deferrable: Set<String> = [], label: String = "") -> [String] {
    var out: [String] = []
    for key in Set(shown.keys).union(truth.keys).sorted() where !deferrable.contains(key) {
        let s = shown[key], t = truth[key]
        if s != t { out.append("\(label)\(key): shown \(s ?? "–"), hub \(t ?? "–")") }
    }
    return out
}
