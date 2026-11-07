import Foundation
import Security
import Testing
@testable import Persona

/// The live screen's address (ScreenViewer.url, #1498): this device's token reaches the page as it is. It went out
/// encoded twice (a `jd_` token as `jd%255F`), the page read `jd%5F…`, and the hub said invalid token.
struct ScreenViewerTests {
    /// The fragment's values as the page reads them (URLSearchParams: %XX decoded once, a bare + read as a space).
    private func read(_ url: URL) -> [String: String] {
        let fragment = URLComponents(url: url, resolvingAgainstBaseURL: false)?.percentEncodedFragment ?? ""
        var out: [String: String] = [:]
        for pair in fragment.split(separator: "&") {
            let kv = pair.split(separator: "=", maxSplits: 1).map { String($0).replacingOccurrences(of: "+", with: " ").removingPercentEncoding ?? "" }
            out[kv[0]] = kv.count > 1 ? kv[1] : ""
        }
        return out
    }

    @Test func thePageGetsTheDeviceTokenAsItIs() throws {
        let page = try #require(URL(string: "http://127.0.0.1:18660/screen/macstudio"))
        for token in ["jd_cafe00a1", "a+b/c=&d%e~f.g-h"] {
            let url = try #require(ScreenViewer.url(page, token: token, name: "Mac Studio"))
            let got = read(url)
            #expect(got["token"] == token, "\(url.absoluteString)")
            #expect(got["name"] == "Mac Studio" && got["embed"] == "1", "\(url.absoluteString)")
            #expect(url.path == "/screen/macstudio" && url.query == nil, "the token stays out of what is sent to the hub")
        }
        let plain = try #require(ScreenViewer.url(page, token: "jd_cafe00a1", name: nil))
        #expect(plain.absoluteString == "http://127.0.0.1:18660/screen/macstudio#token=jd_cafe00a1&embed=1")
        let kept = try #require(ScreenViewer.url(page, token: "jd_cafe00a1", name: nil, keep: "phone"))
        #expect(read(kept)["keep"] == "phone", "this device remembers the Mac's sign-in (#1322)")
        let unlock = try #require(ScreenViewer.url(page, token: "jd_cafe00a1", name: nil, keep: "phone", unlock: true))
        #expect(read(unlock)["unlock"] == "1" && read(kept)["unlock"] == nil, "一键解锁 asks the page to type it (#1507 a-2)")
    }

    /// The Mac's Screen Sharing sign-in, kept on this device (#1322): by machine, readable only while it is unlocked,
    /// never synced; forgotten for good.
    @Test func theSignInStaysOnThisDevice() throws {
        let ref = MachineRef(group: "", id: "unit-test-mac")
        ScreenCredentials.forget(ref)
        #expect(ScreenCredentials.load(ref) == nil)
        #expect(ScreenCredentials.save(.init(account: "me", password: "p+w/ord"), for: ref))
        #expect(ScreenCredentials.load(ref) == .init(account: "me", password: "p+w/ord"))
        #expect(ScreenCredentials.load(MachineRef(group: "other", id: "unit-test-mac")) == nil, "another hub's Mac is another")

        let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: ScreenCredentials.service,
                                kSecAttrAccount as String: ScreenCredentials.key(ref), kSecReturnAttributes as String: true,
                                kSecMatchLimit as String: kSecMatchLimitOne]
        var out: AnyObject?
        #expect(SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess)
        let attributes = try #require(out as? [String: Any])
        #expect(attributes[kSecAttrAccessible as String] as? String == kSecAttrAccessibleWhenUnlockedThisDeviceOnly as String)
        #expect((attributes[kSecAttrSynchronizable as String] as? Bool ?? false) == false)

        // One-Tap Unlock is this device's, per Mac, and only with the sign-in kept (#1507 a-2).
        ScreenUnlock.set(true, for: ref)
        #expect(ScreenUnlock.isOn(ref) && !ScreenUnlock.isOn(MachineRef(group: "", id: "another-mac")))
        ScreenUnlock.set(false, for: ref)
        #expect(!ScreenUnlock.isOn(ref))
        ScreenUnlock.set(true, for: ref)

        #expect(!ScreenCredentials.save(.init(account: "me", password: ""), for: ref), "nothing to keep")
        ScreenCredentials.forget(ref)
        #expect(ScreenCredentials.load(ref) == nil)
        #expect(!ScreenUnlock.isOn(ref), "nothing kept: nothing to type")
        ScreenUnlock.set(false, for: ref)
    }
}
