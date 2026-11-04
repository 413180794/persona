#if canImport(UIKit)
import UIKit
#endif

#if os(iOS) || os(visionOS)
// Pages that draw their own header hide the navigation bar (the Agents chat), and UIKit then switches off the
// edge swipe back. Keep it on whenever there is somewhere to go back to.
extension UINavigationController: @retroactive UIGestureRecognizerDelegate {
    override open func viewDidLoad() {
        super.viewDidLoad()
        interactivePopGestureRecognizer?.delegate = self
    }

    public func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
        viewControllers.count > 1
    }
}
#endif
