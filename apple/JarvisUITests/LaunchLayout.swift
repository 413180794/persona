import XCTest

extension XCUIApplication {
    /// Launches with the four tabs (`-ui.oldLayout YES`) unless the test chose a layout itself: the tests written for
    /// the tab bar keep running on it; the shell's tests (`ShellTests`) pass `-ui.oldLayout NO`.
    func launchJarvis() {
        if !launchArguments.contains("-ui.oldLayout") { launchArguments += ["-ui.oldLayout", "YES"] }
        launch()
    }
}
