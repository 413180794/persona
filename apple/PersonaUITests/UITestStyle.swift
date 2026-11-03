import XCTest

/// The style a UI test runs in. The app keeps the style the last run left (`ui.style` in UserDefaults), and since the
/// BeautifulUI style draws its own components, a test that does not say which style it wants finds whichever looks
/// the previous test left. `launchPinned()` passes it: 液态玻璃 by default, another one with
/// `TEST_RUNNER_JARVIS_UI_STYLE=<style>` (`beautiful` runs a suite in the BeautifulUI style). `UITestStyleTests` checks
/// every launch pins one.
enum UITestStyle {
    static var name: String { ProcessInfo.processInfo.environment["JARVIS_UI_STYLE"] ?? "glass" }
    static var isBeautiful: Bool { name == "beautiful" }
}

extension XCUIApplication {
    /// Launches in the run's style (`UITestStyle`), on the four tabs unless the test chose a layout (`launchPersona()`).
    func launchPinned() {
        launchArguments += ["-ui.style", UITestStyle.name]
        launchPersona()
    }
}
