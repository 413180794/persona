import CryptoKit
import Foundation
import Testing
import UserNotifications
@testable import Jarvis

/// Pushes through the relay (docs/push-relay.md): what the hub seals in Go opens here, and only then does the
/// notification show more than 「有新消息」.
struct PushCryptoTests {
    // Sealed by the hub's pushcrypto package (server/internal/pushcrypto/pushcrypto_test.go, the same vector).
    static let priv = "el2PlP5fa1q1OuNd1M8/leos7rSG4OzZEtfcyKlg6+c="
    static let sealed = "BCggIsspkEoE9Iv+kSeqwGyPHxVNL0Cuto0IV/ZcubpAjXlNzTrF6T60wHSyaJiGj9b4udL2LnPDJEydaRAUnm/4syS2NCzzS/iffzfQTIx+WpKnYO5yqff6oDa/z7t7ow2jocpUOakeY4YKHS7ExN0Vwf5lnqWtOl282TY2JPHGUhI1ZKSiwB0RM8CzPIEzgHF0NWJldVg2pWRuHMGetwweyi8TUM2Zq0AzFPWvfbp8XWjshd1QjOQtoarB/ICzOKNLgXgpyJumYwxSvLw8m1+A6IZpF7Xl8UQwOK1Ytf/OoXJWgenoHY1XmFXBTr66RK0A3QgkM/1scEeQIvyL28MeDJzHdDO5u6n5Em3BuZLvaB8OSehyZHbPHPeMlabYso/z/lTsI4mPA0cP+6vNd+I5M6yUAPmi"
    static let plain = #"{"aps":{"alert":{"title":"Claude 在问你","subtitle":"重构 · 笔记本","body":"要继续吗？"},"category":"INTERACTION_QUESTION","thread-id":"s1"},"interaction_id":"ix1","machine_id":"m1","session_id":"s1","iat":1798000000}"#
    static let sealedAt = Date(timeIntervalSince1970: 1_798_000_000)

    var key: P256.KeyAgreement.PrivateKey { try! P256.KeyAgreement.PrivateKey(rawRepresentation: Data(base64Encoded: Self.priv)!) }
    var push: [AnyHashable: Any] { ["aps": ["alert": ["title": "Jarvis", "body": "有新消息"], "mutable-content": 1], "e": Self.sealed] }

    @Test func opensWhatTheHubSealed() throws {
        let out = try PushCrypto.open(Data(base64Encoded: Self.sealed)!, with: key)
        #expect(String(decoding: out, as: UTF8.self) == Self.plain)
    }

    @Test func otherKeyOrTamperingDoesNotOpen() {
        #expect(PushCrypto.payload(push, key: P256.KeyAgreement.PrivateKey(), now: Self.sealedAt) == nil)
        var bytes = Data(base64Encoded: Self.sealed)!
        bytes[bytes.count - 1] ^= 1
        #expect(PushCrypto.payload(["e": bytes.base64EncodedString()], key: key, now: Self.sealedAt) == nil)
        #expect(PushCrypto.payload(["e": "not base64"], key: key, now: Self.sealedAt) == nil)
        #expect(PushCrypto.payload(push, key: nil, now: Self.sealedAt) == nil) // locked since boot: no key
    }

    @Test func tooOldShowsOnlyTheFallback() {
        #expect(PushCrypto.payload(push, key: key, now: Self.sealedAt.addingTimeInterval(3600)) != nil)
        #expect(PushCrypto.payload(push, key: key, now: Self.sealedAt.addingTimeInterval(PushCrypto.maxAge + 60)) == nil)
    }

    @Test func appliesTextsButtonsAndIds() throws {
        let payload = try #require(PushCrypto.payload(push, key: key, now: Self.sealedAt))
        let content = UNMutableNotificationContent()
        content.title = "Jarvis"
        content.body = "有新消息"
        content.userInfo = push
        PushCrypto.apply(payload, to: content)
        #expect(content.title == "Claude 在问你")
        #expect(content.subtitle == "重构 · 笔记本")
        #expect(content.body == "要继续吗？")
        #expect(content.categoryIdentifier == "INTERACTION_QUESTION") // the 允许 / 拒绝 buttons
        #expect(content.threadIdentifier == "s1")
        // What a tap or a button reads (AppDelegate.sessionKey, resolveFromNotification); the sealed blob is gone.
        #expect(content.userInfo["interaction_id"] as? String == "ix1")
        #expect(content.userInfo["machine_id"] as? String == "m1")
        #expect(content.userInfo["session_id"] as? String == "s1")
        #expect(content.userInfo["e"] == nil)
        #expect(content.userInfo["iat"] == nil)
    }

    @Test func timeSensitiveSurvives() {
        let content = UNMutableNotificationContent()
        PushCrypto.apply(["aps": ["interruption-level": "time-sensitive", "alert": ["body": "x"]]], to: content)
        #expect(content.interruptionLevel == .timeSensitive)
    }

    @Test func roundTripWithAFreshKey() throws {
        let k = P256.KeyAgreement.PrivateKey()
        var sender = try HPKE.Sender(recipientKey: k.publicKey, ciphersuite: PushCrypto.suite, info: PushCrypto.info)
        let ct = try sender.seal(Data("hi".utf8))
        #expect(try PushCrypto.open(sender.encapsulatedKey + ct, with: k) == Data("hi".utf8))
        #expect(Data(base64Encoded: PushCrypto.publicKey(k))?.count == 65)
    }
}
