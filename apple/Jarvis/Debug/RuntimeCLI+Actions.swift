#if DEBUG && os(iOS)
import SwiftUI
import UIKit

/// Acting on the app (docs/runtime-cli.md「动作和截图」): `activate`, `scroll`, `type`, `dismiss-keyboard`, `snapshot`.
/// A real touch (a menu's items, which take no accessibility activation) is `jarvisctl.py tap`'s fallback: `idb`.
extension RuntimeCLI {
    static let actionCommands: [String: Command] = [
        "activate": { a in activate(a) },
        "scroll": { a in scroll(a) },
        "type": { a in type(a) },
        "dismiss-keyboard": { _ in windows.forEach { $0.endEditing(true) }; return [:] },
        "snapshot": { a in snapshot(a) },
    ]

    /// What VoiceOver's double tap does to the element `pick` finds: a button's action, a menu opening. No touch, so
    /// no hit-testing; `activated` false when the element does not take it (a menu's items).
    private static func activate(_ a: Args) -> Reply {
        guard let e = pick(a) else { return fail("not found") }
        return ["activated": e.object.accessibilityActivate(), "element": e.json]
    }

    /// The largest scroll view on screen, moved by `pages` screens (negative: up; 0.8 of its height each) or `to` top /
    /// bottom, without animation (no deceleration: what is under test is where things land, not the fling).
    private static func scroll(_ a: Args) -> Reply {
        var views: [UIScrollView] = []
        func collect(_ v: UIView) {
            if let s = v as? UIScrollView, !s.isHidden, s.window != nil, s.contentSize.height > s.bounds.height + 1 { views.append(s) }
            v.subviews.forEach(collect)
        }
        windows.forEach(collect)
        guard let s = views.max(by: { $0.bounds.width * $0.bounds.height < $1.bounds.width * $1.bounds.height }) else {
            return fail("no scroll view")
        }
        let top = -s.adjustedContentInset.top
        let bottom = max(top, s.contentSize.height + s.adjustedContentInset.bottom - s.bounds.height)
        var y = s.contentOffset.y
        switch a["to"] as? String {
        case "top": y = top
        case "bottom": y = bottom
        default: y += CGFloat(a["pages"] as? Double ?? 1) * s.bounds.height * 0.8
        }
        s.setContentOffset(CGPoint(x: s.contentOffset.x, y: min(bottom, max(top, y))), animated: false)
        return ["offset": Double(s.contentOffset.y), "contentHeight": Double(s.contentSize.height)]
    }

    private static func firstResponder() -> UIResponder? {
        func find(_ v: UIView) -> UIResponder? {
            if v.isFirstResponder { return v }
            for s in v.subviews { if let r = find(s) { return r } }
            return nil
        }
        for w in windows { if let r = find(w) { return r } }
        return nil
    }

    /// Types `text` into the field that has focus; with element filters, activates that element first (a field takes
    /// focus that way).
    private static func type(_ a: Args) -> Reply {
        if ["id", "idPrefix", "label", "labelContains", "labelPrefix", "trait"].contains(where: { a[$0] != nil }) {
            guard let e = pick(a) else { return fail("not found") }
            _ = e.object.accessibilityActivate()
        }
        guard let r = firstResponder() as? UIKeyInput else { return fail("no text field has focus") }
        r.insertText(a["text"] as? String ?? "")
        return ["typed": true]
    }

    /// The app's windows as a PNG (no status bar, unlike `simctl io screenshot`) at `scale` (the screen's when 0),
    /// written to `path` (a simulator's files are the Mac's) or returned as base64.
    private static func snapshot(_ a: Args) -> Reply {
        guard let png = render(scale: a["scale"] as? Double ?? 0)?.pngData() else { return fail("no window") }
        if let p = a["path"] as? String {
            do { try png.write(to: URL(fileURLWithPath: p)) } catch { return fail("\(error)") }
            return ["path": p, "bytes": png.count]
        }
        return ["png": png.base64EncodedString()]
    }
}
#endif
