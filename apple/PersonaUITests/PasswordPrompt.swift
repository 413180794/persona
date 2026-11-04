import XCTest

/// The system's "Save Password?" sheet (Passwords / AutoFill), which iOS puts up over the app when a page with a
/// password-looking field goes away. A token or a plugin's secret is not the user's password: it must never ask (#1512).
enum PasswordPrompt {
    /// The sheet's buttons, in English and Chinese: the sheet is the system's, in the simulator's language.
    private static let labels = ["Save Password", "Not Now", "Never for This Website", "Never for This App",
                                 "存储密码", "保存密码", "以后", "不是现在", "永不用于此 App", "永不"]

    /// Waits a moment (the sheet comes up after the page has gone), then fails if the sheet is there, in the app or
    /// on the springboard.
    @MainActor static func assertNone(_ app: XCUIApplication, _ context: String, file: StaticString = #filePath, line: UInt = #line) {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let predicate = NSPredicate(format: "label IN %@", labels)
        let deadline = Date().addingTimeInterval(4)
        repeat {
            for host in [app, springboard] {
                let button = host.buttons.matching(predicate).firstMatch
                if button.exists {
                    XCTFail("\(context): the system offers to save it as a password:\n\(host.debugDescription)", file: file, line: line)
                    return
                }
                let title = host.staticTexts.matching(NSPredicate(format: "label CONTAINS[c] %@ OR label CONTAINS %@ OR label CONTAINS %@", "Save Password", "保存密码", "存储密码")).firstMatch
                if title.exists {
                    XCTFail("\(context): the system offers to save it as a password:\n\(host.debugDescription)", file: file, line: line)
                    return
                }
            }
            usleep(300_000)
        } while Date() < deadline
    }
}
