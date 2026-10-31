import Foundation
import Testing
@testable import Jarvis

/// Which links a click hands to macOS. On the Mac `NSWorkspace.open` runs whatever a URL names (a `file://` app, an
/// `smb://` share, another app's scheme) without asking, and the text comes from agents and chats: only the web and
/// mail go out; the rest (pairing links too) are copied.
struct LinkPolicyTests {
    @Test func webAndMailLinksOpen() {
        for s in ["https://example.com/a", "HTTP://example.com", "mailto:me@example.com"] {
            #expect(LinkPolicy.opensOnMac(URL(string: s)!), "\(s)")
        }
    }

    @Test func everythingElseIsCopied() {
        for s in ["file:///Applications/Calculator.app", "smb://host/share", "x-apple.systempreferences:com.apple",
                  "vscode://file/etc/passwd", "javascript:alert(1)", "relative/path"] {
            #expect(!LinkPolicy.opensOnMac(URL(string: s)!), "\(s)")
        }
    }
}
