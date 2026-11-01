import XCTest

extension XCUIApplication {
    /// Launches with the four tabs (`-ui.oldLayout YES`) unless the test chose a layout itself: the tests written for
    /// the tab bar keep running on it; the shell's tests (`ShellTests`) pass `-ui.oldLayout NO`.
    /// `JARVIS_LAYOUT=shell` (TEST_RUNNER_JARVIS_LAYOUT) runs them in the shell instead, the iPhone's default: the tests
    /// that hold what the user is doing still (LiveUpdateTests, ChatFollowTests) run in both (docs/testing.md).
    func launchJarvis() {
        let shell = ProcessInfo.processInfo.environment["JARVIS_LAYOUT"] == "shell"
        if !launchArguments.contains("-ui.oldLayout") { launchArguments += ["-ui.oldLayout", shell ? "NO" : "YES"] }
        launch()
    }
}
