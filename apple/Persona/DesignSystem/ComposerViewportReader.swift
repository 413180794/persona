#if os(iOS)
import SwiftUI
import UIKit

/// Measures the viewport down to a bottom-docked composer, including keyboard avoidance and sheet/window resizing.
/// Use window coordinates: some composers live in their own hosting controller, whose SwiftUI global origin is local.
struct ComposerViewportReader: UIViewRepresentable {
    var update: (CGFloat) -> Void

    func makeUIView(context: Context) -> Probe { Probe(frame: .zero) }

    func updateUIView(_ view: Probe, context: Context) {
        view.update = update
        view.setNeedsLayout()
    }

    final class Probe: UIView {
        var update: ((CGFloat) -> Void)?
        private var reported: CGFloat = 0

        override init(frame: CGRect) {
            super.init(frame: frame)
            observeKeyboard()
        }

        required init?(coder: NSCoder) {
            super.init(coder: coder)
            observeKeyboard()
        }

        private func observeKeyboard() {
            // A dock can move with the keyboard without changing this probe's local frame or size.
            for name in [UIResponder.keyboardDidChangeFrameNotification, UIResponder.keyboardDidHideNotification] {
                NotificationCenter.default.addObserver(self, selector: #selector(keyboardMoved(_:)), name: name, object: nil)
            }
        }

        @objc private func keyboardMoved(_ notification: NSNotification) { setNeedsLayout() }

        override var frame: CGRect {
            didSet { setNeedsLayout() }
        }

        override func didMoveToWindow() {
            super.didMoveToWindow()
            setNeedsLayout()
        }

        override func safeAreaInsetsDidChange() {
            super.safeAreaInsetsDidChange()
            setNeedsLayout()
        }

        override func layoutSubviews() {
            super.layoutSubviews()
            guard let window else { return }
            let bottom = min(convert(bounds, to: window).maxY, window.bounds.maxY - window.safeAreaInsets.bottom)
            let height = max(0, bottom - window.safeAreaInsets.top)
            guard height != reported else { return }
            reported = height
            // Layout can run inside SwiftUI's update; deliver only the latest value after that update completes.
            DispatchQueue.main.async { [weak self] in
                guard let self, self.window != nil, self.reported == height else { return }
                self.update?(height)
            }
        }
    }
}
#endif
