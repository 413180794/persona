import SwiftUI
import Testing
import UIKit
@testable import Persona

@MainActor
struct ShellNavigationClearanceTests {
    /// The navigation bar with the blank prompt line (UIKit's, the same at every text size) holds the top bar at every
    /// text size: its buttons stop growing at `Theme.Shell.topBarLargestTypeSize` for that (#340).
    @Test func thePromptLineHoldsTheTopBarAtEveryTextSize() {
        let bar = barHeight(prompt: ShellNavigationClearance.line)
        #expect(bar > barHeight(prompt: nil), "the prompt line makes the bar no taller")
        for size in DynamicTypeSize.allCases {
            #expect(bar >= ShellNavigationClearance.needed(at: size), "\(size): the bar \(bar), the top bar needs \(ShellNavigationClearance.needed(at: size))")
        }
    }

    @Test func theRoomNeverShrinksAsTextGrows() {
        let heights = DynamicTypeSize.allCases.map { ShellNavigationClearance.needed(at: $0) }
        for (smaller, larger) in zip(heights, heights.dropFirst()) {
            #expect(larger >= smaller)
        }
    }

    private func barHeight(prompt: String?) -> CGFloat {
        let page = UIViewController()
        page.navigationItem.prompt = prompt
        let nav = UINavigationController(rootViewController: page)
        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 402, height: 874))
        window.rootViewController = nav
        window.isHidden = false
        nav.view.layoutIfNeeded()
        defer { window.isHidden = true }
        return nav.navigationBar.frame.height
    }
}
