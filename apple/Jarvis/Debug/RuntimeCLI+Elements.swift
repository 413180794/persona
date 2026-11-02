#if DEBUG && os(iOS)
import SwiftUI
import UIKit

/// Reading the app (docs/runtime-cli.md「读状态」): the accessibility tree XCUITest sees (`tree`, `find`, `wait`), the
/// view-body counts (`perf`), a pull from the hub (`refresh`).
extension RuntimeCLI {
    static let elementCommands: [String: Command] = [
        "tree": { a in ["elements": elements(matching: a).map(\.json)] },
        "find": { a in find(a) },
        "wait": { a in await wait(a) },
        "perf": { _ in perf() },
        "refresh": { _ in await AppModel.shared.refresh(); return [:] },
    ]

    /// What XCUITest turns on: with no assistive technology running SwiftUI builds no accessibility elements. Called
    /// once when the CLI starts (`startIfAsked`), never otherwise.
    static func enableAccessibilityTree() {
        guard let h = dlopen("/usr/lib/libAccessibility.dylib", RTLD_NOW),
              let f = dlsym(h, "_AXSSetAutomationEnabled") else { return }
        typealias Enable = @convention(c) (Int32) -> Void
        unsafeBitCast(f, to: Enable.self)(1)
    }

    struct Element {
        let object: NSObject
        let id: String
        let label: String
        let value: String
        let frame: CGRect
        let traits: UIAccessibilityTraits

        /// On screen (not hit-tested: the shell keeps both pages in the tree, the one behind counts as visible).
        var visible: Bool {
            let screen = RuntimeCLI.window?.screen.bounds ?? .zero
            return !frame.isEmpty && frame.minX.isFinite && screen.intersects(frame)
        }

        var json: Reply {
            var t: [String] = []
            if traits.contains(.button) { t.append("button") }
            if traits.contains(.selected) { t.append("selected") }
            if traits.contains(.header) { t.append("header") }
            if traits.contains(.staticText) { t.append("text") }
            if traits.contains(.notEnabled) { t.append("disabled") }
            if traits.contains(.searchField) { t.append("search") }
            if traits.contains(.image) { t.append("image") }
            // A frame can be NaN or infinite (an element mid-layout): 0, never a value JSON cannot hold.
            let f = [frame.minX, frame.minY, frame.width, frame.height].map { $0.isFinite ? (Double($0) * 10).rounded() / 10 : 0 }
            return ["id": id, "label": label, "value": value, "traits": t, "visible": visible, "frame": f]
        }
    }

    /// Every accessibility element of the app's windows, top-most window first (menus, sheets), in reading order.
    static func allElements() -> [Element] {
        var out: [Element] = []
        var seen = Set<ObjectIdentifier>()
        func visit(_ o: NSObject, depth: Int) {
            guard depth < 80, seen.insert(ObjectIdentifier(o)).inserted else { return }
            if let v = o as? UIView, v.isHidden || v.alpha < 0.01 { return }
            if o.isAccessibilityElement {
                let id = (o as? UIAccessibilityIdentification)?.accessibilityIdentifier
                    ?? (o.responds(to: Selector(("accessibilityIdentifier"))) ? o.value(forKey: "accessibilityIdentifier") as? String : nil)
                out.append(Element(object: o, id: id ?? "", label: o.accessibilityLabel ?? "", value: o.accessibilityValue ?? "",
                                   frame: o.accessibilityFrame, traits: o.accessibilityTraits))
            }
            if let children = o.accessibilityElements as? [NSObject] {
                for c in children { visit(c, depth: depth + 1) }
            } else {
                let n = o.accessibilityElementCount()
                if n > 0, n != NSNotFound {
                    for i in 0..<n { if let c = o.accessibilityElement(at: i) as? NSObject { visit(c, depth: depth + 1) } }
                }
            }
            if let v = o as? UIView { for s in v.subviews { visit(s, depth: depth + 1) } }
        }
        for w in windows.reversed() where !w.isHidden { visit(w, depth: 0) }
        return out
    }

    /// Filters: `id` (exact), `idPrefix`, `label` (exact), `labelContains`, `labelPrefix`, `trait` ("button"…),
    /// `visibleOnly`.
    static func elements(matching a: Args) -> [Element] {
        allElements().filter { e in
            if let id = a["id"] as? String, e.id != id { return false }
            if let p = a["idPrefix"] as? String, !e.id.hasPrefix(p) { return false }
            if let l = a["label"] as? String, e.label != l { return false }
            if let l = a["labelContains"] as? String, !e.label.contains(l) { return false }
            if let l = a["labelPrefix"] as? String, !e.label.hasPrefix(l) { return false }
            if let t = a["trait"] as? String, !(e.json["traits"] as? [String] ?? []).contains(t) { return false }
            if a["visibleOnly"] as? Bool == true, !e.visible { return false }
            return true
        }
    }

    /// The `index`-th match, or without `index` the first one on screen (then the first at all).
    static func pick(_ a: Args) -> Element? {
        let all = elements(matching: a)
        if let i = a["index"] as? Int { return i >= 0 && i < all.count ? all[i] : nil }
        return all.first(where: \.visible) ?? all.first
    }

    private static func find(_ a: Args) -> Reply {
        ["count": elements(matching: a).count, "element": pick(a)?.json ?? NSNull()]
    }

    /// Polls every 100 ms until an element matches (with `gone`, until none does), or `timeout` seconds (10) pass.
    private static func wait(_ a: Args) async -> Reply {
        let deadline = Date.now.addingTimeInterval(a["timeout"] as? Double ?? 10)
        let gone = a["gone"] as? Bool ?? false
        while true {
            let e = pick(a)
            if gone ? e == nil : e != nil { return ["element": e?.json ?? NSNull()] }
            if Date.now > deadline { return fail(gone ? "still there" : "not found") }
            try? await Task.sleep(for: .milliseconds(100))
        }
    }

    /// View bodies evaluated, events, main-thread time (PerfCount; the app launched with `-perfCounts YES`).
    private static func perf() -> Reply {
        guard PerfCount.enabled else { return fail("launch with -perfCounts YES") }
        return ["counts": (try? JSONSerialization.jsonObject(with: Data(PerfCount.summary.utf8))) ?? PerfCount.summary]
    }
}
#endif
