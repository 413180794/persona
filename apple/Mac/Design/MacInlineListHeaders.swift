import AppKit
import SwiftUI

/// Attached inside a List's section header, so only that header's own table is changed.
/// AppKit otherwise floats its first group in an NSBannerView, with a system fill and separator
/// that `scrollContentBackground` and `scrollEdgeEffectHidden` do not control.
struct MacInlineListHeaders: NSViewRepresentable {
    func makeNSView(context: Context) -> Probe { Probe() }
    func updateNSView(_ view: Probe, context: Context) { view.schedule() }

    final class Probe: NSView {
        override func hitTest(_ point: NSPoint) -> NSView? { nil }
        override func viewDidMoveToSuperview() { super.viewDidMoveToSuperview(); schedule() }
        override func viewDidMoveToWindow() { super.viewDidMoveToWindow(); schedule() }

        func schedule() {
            // The table finishes attaching its header after the hosting view's update.
            DispatchQueue.main.async { [weak self] in self?.apply() }
        }

        private func apply() {
            var ancestor = superview
            while let view = ancestor {
                if let table = view as? NSTableView {
                    if table.floatsGroupRows { table.floatsGroupRows = false }
                    return
                }
                ancestor = view.superview
            }
        }
    }
}
