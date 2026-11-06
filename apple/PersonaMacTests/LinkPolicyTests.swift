import Foundation
import Testing
@testable import Persona

/// Which links a click hands to macOS. On the Mac `NSWorkspace.open` runs whatever a URL names (a `file://` app, an
/// `smb://` share, another app's scheme) without asking, and the text comes from agents and chats: only the web and
/// mail go out; the rest (pairing links too) are copied.
struct LinkPolicyTests {
    @Test func webAndMailLinksOpen() {
        for s in ["https://example.com/a", "HTTP://example.com", "mailto:me@example.com"] {
            #expect(LinkPolicy.opensOnMac(URL(string: s)!), "\(s)")
        }
    }

    /// The app's own 「打开系统设置」 buttons go through UIApplication.open with System Settings' URLs: those it built
    /// itself, so they open (a message's link to System Settings is still copied).
    @MainActor @Test func theAppsOwnSystemSettingsButtonsOpen() {
        #expect(LinkPolicy.opensFromApp(URL(string: UIApplication.openSettingsURLString)!))
        #expect(LinkPolicy.opensFromApp(URL(string: UIApplication.openNotificationSettingsURLString)!))
        #expect(LinkPolicy.opensFromApp(URL(string: "https://example.com")!))
        #expect(!LinkPolicy.opensFromApp(URL(string: "file:///Applications/Calculator.app")!))
        #expect(!LinkPolicy.opensOnMac(URL(string: UIApplication.openSettingsURLString)!))
    }

    @Test func everythingElseIsCopied() {
        for s in ["file:///Applications/Calculator.app", "smb://host/share", "x-apple.systempreferences:com.apple",
                  "vscode://file/etc/passwd", "javascript:alert(1)", "relative/path"] {
            #expect(!LinkPolicy.opensOnMac(URL(string: s)!), "\(s)")
        }
    }
}
