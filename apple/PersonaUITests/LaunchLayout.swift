import XCTest

extension XCUIApplication {
    /// Launches with the four tabs (`-ui.oldLayout YES`) unless the test chose a layout itself: the tests written for
    /// the tab bar keep running on it; the shell's tests (`ShellTests`) pass `-ui.oldLayout NO`.
    /// `PERSONA_LAYOUT=shell` (TEST_RUNNER_PERSONA_LAYOUT) runs them in the shell instead, the iPhone's default: the tests
    /// that hold what the user is doing still (LiveUpdateTests, ChatFollowTests) run in both (docs/testing.md).
    ///
    /// `-demo.on YES` without `-pairURL` is Demo Mode from a clean start (`-openWelcome YES` first): a pairing an earlier test
    /// left in the keychain wins over Demo Mode, and the test ran on that hub's data instead (2026-11-03: the mock's
    /// extra pending interactions filled the Agents list and the shell's tests found no agent row).
    func launchPersona() {
        let shell = ProcessInfo.processInfo.environment["PERSONA_LAYOUT"] == "shell"
        if !launchArguments.contains("-ui.oldLayout") { launchArguments += ["-ui.oldLayout", shell ? "NO" : "YES"] }
        if Self.wantsDemo(launchArguments) { launchArguments += ["-openWelcome", "YES"] }
        launch()
    }

    /// Launched into Demo Mode and not paired by the test itself.
    static func wantsDemo(_ args: [String]) -> Bool {
        let demo = zip(args, args.dropFirst()).contains { $0 == "-demo.on" && $1 == "YES" }
        return demo && !args.contains("-pairURL") && !args.contains("-openWelcome")
    }
}
