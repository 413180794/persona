#if DEBUG && (os(iOS) || os(visionOS))
import QuartzCore
import SwiftUI
import UIKit

/// `-showSearchTrace YES -searchTracePath <file>` (DEBUG): the Agents page's top, frame by frame, from launch (#2478,
/// `AgentsTopMotionTests`). On each frame, where each marked block is drawn (its layer as on screen, `presentation()`,
/// top / height) and how opaque, the list's offset and top inset and the navigation bar; `note` adds what happened.
/// One line per frame: `SEARCH f <unix time> <name>=<y>/<h> <name>.a=<opacity> …`, `SEARCH note <unix time> <what>`.
@MainActor final class SearchTrace: NSObject {
    static let enabled = UserDefaults.standard.bool(forKey: "showSearchTrace")
    static let shared = SearchTrace()

    private var marks: [String: WeakView] = [:]
    private var link: CADisplayLink?
    private var out: FileHandle?
    private var lines: [String] = []

    final class WeakView { weak var view: UIView?; init(_ v: UIView) { view = v } }

    func mark(_ name: String, _ view: UIView) {
        marks[name] = WeakView(view)
        start()
    }

    func note(_ what: String) {
        guard Self.enabled else { return }
        write("SEARCH note \(Self.now) \(what)")
    }

    private static var now: String { String(format: "%.4f", Date.now.timeIntervalSince1970) }

    private func start() {
        guard Self.enabled, link == nil else { return }
        if let path = UserDefaults.standard.string(forKey: "searchTracePath") {
            FileManager.default.createFile(atPath: path, contents: nil)
            out = FileHandle(forWritingAtPath: path)
        }
        let l = CADisplayLink(target: self, selector: #selector(tick))
        l.add(to: .main, forMode: .common)
        link = l
    }

    /// Written in batches: a write per frame cost frames.
    private func write(_ s: String) {
        lines.append(s)
        if lines.count >= 30 { flush() }
    }

    private func flush() {
        guard !lines.isEmpty else { return }
        out?.write(Data((lines.joined(separator: "\n") + "\n").utf8))
        lines = []
    }

    /// Where `v` is drawn now, in its window (nil: not in one, or hidden).
    private static func drawn(_ v: UIView?) -> CGRect? {
        guard let v, let w = v.window, !v.isHidden else { return nil }
        let l = v.layer.presentation() ?? v.layer
        let wl = w.layer.presentation() ?? w.layer
        return l.convert(l.bounds, to: wl)
    }

    /// How opaque `v` is drawn: its and its superviews' opacity as on screen.
    private static func alpha(_ v: UIView?) -> Double {
        var a = 1.0, x = v
        while let y = x { a *= Double((y.layer.presentation() ?? y.layer).opacity) * (y.isHidden ? 0 : 1); x = y.superview }
        return a
    }

    private static func fmt(_ r: CGRect?) -> String { r.map { String(format: "%.1f/%.1f", $0.minY, $0.height) } ?? "-" }

    @objc private func tick() {
        var parts = ["SEARCH f \(Self.now)"]
        for name in marks.keys.sorted() {
            let v = marks[name]?.view
            parts.append("\(name)=\(Self.fmt(Self.drawn(v)))")
            if v?.window != nil { parts.append(String(format: "\(name).a=%.2f", Self.alpha(v))) }
        }
        // UIKit's side, from the filters' place: their list and navigation bar.
        if let anchor = marks["filter"]?.view ?? marks["topbar"]?.view {
            if let list = Self.enclosing(UICollectionView.self, of: anchor) {
                parts.append(String(format: "list.off=%.1f list.inset=%.1f", list.contentOffset.y, list.adjustedContentInset.top))
                // The first cells on screen, top first.
                let cells = list.visibleCells.compactMap { Self.drawn($0) }.sorted { $0.minY < $1.minY }.prefix(4)
                parts.append("cells=" + cells.map { Self.fmt($0) }.joined(separator: ","))
            }
            if let nav = Self.navigation(anchor) { parts.append("nav=\(Self.fmt(Self.drawn(nav.navigationBar)))") }
        }
        write(parts.joined(separator: " "))
    }

    private static func enclosing<T: UIView>(_ t: T.Type, of v: UIView) -> T? {
        var s = v.superview
        while let x = s { if let c = x as? T { return c }; s = x.superview }
        return nil
    }

    private static func navigation(_ v: UIView) -> UINavigationController? {
        var r: UIResponder? = v
        while let next = r?.next {
            if let vc = next as? UIViewController { return vc.navigationController ?? vc.children.compactMap { $0 as? UINavigationController }.first }
            r = next
        }
        return nil
    }
}

private struct SearchTraceMark: UIViewRepresentable {
    let name: String
    func makeUIView(context: Context) -> UIView {
        let v = UIView()
        v.isUserInteractionEnabled = false
        SearchTrace.shared.mark(name, v)
        return v
    }
    func updateUIView(_ v: UIView, context: Context) { SearchTrace.shared.mark(name, v) }
}

extension View {
    /// Where this block is, for `-showSearchTrace`.
    @ViewBuilder func searchTraceMark(_ name: String) -> some View {
        if SearchTrace.enabled { background { SearchTraceMark(name: name) } } else { self }
    }
}
#else
import SwiftUI
extension View {
    func searchTraceMark(_ name: String) -> some View { self }
}
#endif
