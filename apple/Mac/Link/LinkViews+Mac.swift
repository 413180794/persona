#if os(macOS)
import AppKit
import SwiftUI
import WebKit

// The Mac's versions of the UIKit views in LinkSheet.swift: same names, same call sites.

/// A WKWebView whose right-click menu on a selection leads with AI Edit and Add as Context.
final class SelectableWebView: WKWebView {
    var onPick: ((String, String) -> Void)?

    override func willOpenMenu(_ menu: NSMenu, with event: NSEvent) {
        super.willOpenMenu(menu, with: event)
        // WebKit puts Copy into the menu only when text is selected.
        guard menu.items.contains(where: { $0.identifier?.rawValue == "WKMenuItemIdentifierCopy" }) else { return }
        let ours = UIMenu(options: .displayInline, children: [
            UIAction(title: String(localized: "Edit with AI"), image: UIImage(systemName: Theme.Icon.ai)) { [weak self] _ in self?.pickSelection("edit") },
            UIAction(title: String(localized: "Add as Context"), image: UIImage(systemName: Theme.Icon.addContext)) { [weak self] _ in self?.pickSelection("context") },
        ])
        menu.insertItem(.separator(), at: 0)
        for item in ours.children.reversed() { menu.insertItem(item.nsMenuItem(), at: 0) }
    }

    private func pickSelection(_ kind: String) {
        evaluateJavaScript("window.getSelection().toString()") { [weak self] result, _ in
            let text = (result as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            guard !text.isEmpty else { return }
            self?.onPick?(text, kind)
        }
    }
}

/// The pooled (preloaded) web view for a link, as on iOS.
struct PooledWebView: NSViewRepresentable {
    var url: URL
    @Binding var title: String
    var onPick: (String, String) -> Void

    func makeNSView(context: Context) -> SelectableWebView {
        let v = WebViewPool.shared.view(for: url)
        v.removeFromSuperview()
        v.onPick = onPick
        context.coordinator.observe(v) { title = $0 }
        return v
    }

    func updateNSView(_ v: SelectableWebView, context: Context) { v.onPick = onPick }

    func makeCoordinator() -> Coordinator { Coordinator() }

    @MainActor
    final class Coordinator {
        private var token: NSKeyValueObservation?
        private var onTitle: ((String) -> Void)?
        func observe(_ v: WKWebView, title: @escaping (String) -> Void) {
            onTitle = title
            if let t = v.title, !t.isEmpty { title(t) }
            token = v.observe(\.title, options: [.new]) { [weak self] v, _ in
                let t = v.title ?? ""
                Task { @MainActor in if !t.isEmpty { self?.onTitle?(t) } }
            }
        }
    }
}

/// A paragraph in a selectable text view; right-click on a selection offers `actions` (AI Edit and Add as Context by default).
struct PickTextView: NSViewRepresentable {
    var text: String
    var actions = PickAction.ai
    var pick: (String, String) -> Void

    func makeNSView(context: Context) -> NSScrollView {
        let scroll = MenuTextView.scrollable()
        let t = scroll.documentView as? MenuTextView
        t?.string = text
        let coordinator = context.coordinator
        let actions = actions
        t?.selectionItems = { selected in
            UIMenu(options: .displayInline, children: actions.map { a in
                UIAction(title: a.title, image: UIImage(systemName: a.icon)) { _ in coordinator.pick(selected, a.kind) }
            }).children.map { $0.nsMenuItem() }
        }
        return scroll
    }

    func updateNSView(_ v: NSScrollView, context: Context) { context.coordinator.pick = pick }

    func makeCoordinator() -> Coordinator { Coordinator(pick: pick) }

    @MainActor
    final class Coordinator {
        var pick: (String, String) -> Void
        init(pick: @escaping (String, String) -> Void) { self.pick = pick }
    }
}
#endif
