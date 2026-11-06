import CryptoKit
import Foundation
import Testing
@testable import Persona

@MainActor struct LarkNativeEncryptionTests {
    @Test(arguments: [404, 405], ["", "not_found", "NOT_FOUND", "method_not_allowed"]) func missingNativeRoutesExplainWhichServerNeedsUpdating(_ status: Int, _ code: String) async throws {
        let hub = FakeHub()
        let path = "/api/v1/lark/encryption"
        hub.on("GET", path) { _ in throw APIError(status: status, message: "not found", fields: code.isEmpty ? [:] : ["code": code]) }
        do {
            _ = try await hub.client.larkEncryptionStatus()
            Issue.record("old Hub was accepted")
        } catch let error as APIError {
            #expect(error.code == "lark_hub_upgrade_required")
            #expect(error.message.contains("升级 Hub"))
            #expect(!error.message.contains("not found"))
        }
        hub.on("GET", path, json: #"{"accountId":"123","configured":true,"locked":true,"unifiedPassword":false}"#)
        #expect(try await hub.client.larkEncryptionStatus().accountId == "123")
    }

    @Test func nativeFailuresPreserveAuthorizationAndAccountErrors() async throws {
        let hub = FakeHub()
        for (status, code) in [(403, "VAULT_NATIVE_NOT_AUTHORIZED"), (401, "lark_not_logged_in"), (404, "conversation_not_found"), (426, "lark_gateway_upgrade_required")] {
            hub.on("GET", "/api/v1/lark/encryption") { _ in throw APIError(status: status, message: "fixture", fields: ["code": code]) }
            do {
                _ = try await hub.client.larkEncryptionStatus()
                Issue.record("failure was accepted")
            } catch let error as APIError { #expect(error.code == code && error.status == status) }
        }
        hub.on("POST", "/api/v1/lark/conversations/789/encryption/covers") { _ in throw APIError(status: 404, message: "not found") }
        do {
            _ = try await hub.client.larkEncryptionCovers(account: "123", feed: "789")
            Issue.record("missing cover route was accepted")
        } catch let error as APIError { #expect(error.code == "lark_hub_upgrade_required") }
        #expect(hub.sent("POST", "/api/v1/lark/conversations/789/messages").isEmpty)
    }

    private func payload() -> LarkEncryptedPayload {
        LarkEncryptedPayload(accountId: "123", feedId: "789", messages: [.init(text: "ciphertext-one"), .init(text: "ciphertext-two")], cover: "下周再聊")
    }
    private func sealer() -> LarkOutboxSealer { LarkOutboxSealer(key: { _ in Data(repeating: 7, count: 32) }) }

    @Test func encryptedDraftAndPendingBodySurviveRestartWithoutPlaintextOnDisk() async throws {
        let root = URL.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: root) }
        let identity = LarkOutboxIdentity(group: "fixture-hub", account: "123")
        let store = LarkOutboxStore(identity: identity, feed: "789", base: root, sealer: sealer())
        let draft = LarkDraft(text: "private draft marker", encryption: .init(cover: "下周再聊"))
        let outgoing = LarkOutgoing(text: "private outgoing marker", encrypted: payload(), attempted: true)
        _ = try await store.save(.init(draft: draft, items: [outgoing]))
        let bytes = try Data(contentsOf: await store.directory.appending(path: "state.json"))
        #expect(bytes.range(of: Data("private".utf8)) == nil)
        #expect(bytes.starts(with: Data("OLKD1\n".utf8)))
        let reopened = LarkOutboxStore(identity: identity, feed: "789", base: root, sealer: sealer())
        let loaded = try #require(try await reopened.load())
        #expect(loaded.draft == draft && loaded.items.first?.encrypted == payload())
        #expect(loaded.items.first?.text == outgoing.text && loaded.items.first?.state == .unconfirmed)
        #expect(loaded.items.first?.clientID == outgoing.clientID)
    }

    @Test func keychainUnavailableCannotOverwriteAnEncryptedDraft() async throws {
        let root = URL.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: root) }
        let identity = LarkOutboxIdentity(group: "fixture-hub", account: "123")
        let store = LarkOutboxStore(identity: identity, feed: "789", base: root, sealer: sealer())
        _ = try await store.save(.init(draft: .init(text: "private", encryption: .init())))
        let url = await store.directory.appending(path: "state.json"), before = try Data(contentsOf: url)
        let unavailable = LarkOutboxStore(identity: identity, feed: "789", base: root,
            sealer: LarkOutboxSealer(key: { _ in throw LarkOutboxSealer.Failure.unavailable }))
        let queue = LarkChatOutbox(store: unavailable)
        await #expect(throws: (any Error).self) { try await queue.prepare() }
        #expect(!queue.ready && !queue.canRecover)
        await #expect(throws: (any Error).self) { try await queue.enqueue([.init(text: "must not overwrite")]) }
        #expect(try Data(contentsOf: url) == before)
    }

    @Test func encryptedRetryKeepsCiphertextAndIDsAndNeverUsesPlainSend() async throws {
        let hub = FakeHub()
        let path = "/api/v1/lark/conversations/789/encrypted-messages"
        hub.on("POST", path, json: #"{"message_id":"999"}"#)
        var outgoing = LarkOutgoing(text: "private body", encrypted: payload(), replyTo: .init(id: "555", senderName: "同事", text: "问题"))
        try await LarkOutbox.deliver(outgoing, feed: "789", api: hub.client)
        outgoing.retry()
        try await LarkOutbox.deliver(outgoing, feed: "789", api: hub.client)
        let calls = try hub.sent("POST", path).map { try JSONDecoder().decode([String: String].self, from: $0.body!) }
        #expect(calls.count == 4)
        #expect(calls[0] == calls[2] && calls[1] == calls[3])
        #expect(calls[0]["reply_id"] == nil && calls[1]["reply_id"] == "555")
        #expect(calls[1]["client_id"] == outgoing.clientID)
        #expect(calls[0]["ciphertext"] == "ciphertext-one" && calls[1]["ciphertext"] == "ciphertext-two")
        #expect(calls.allSatisfy { $0["text"] == nil && $0["account_id"] == "123" })
        let id = try #require(calls[0]["client_id"])
        #expect(UUID(uuidString: id) != nil && Array(id)[14] == "8" && "89ab".contains(Array(id)[19]))
        #expect(hub.sent("POST", "/api/v1/lark/conversations/789/messages").isEmpty)
        #expect(hub.sent("POST", "/api/v1/lark/conversations/789/encryption/prepare").isEmpty)
        let shown = outgoing.message(like: nil)
        #expect(shown.olk?.summary == "private body" && shown.olk?.publicParts.first?.text == "下周再聊")
    }

    @Test func aPartialBatchStopsAndAnAccountMismatchCannotPrepare() async throws {
        let hub = FakeHub()
        let path = "/api/v1/lark/conversations/789/encrypted-messages"
        hub.on("POST", path) { _ in throw APIError(status: 503, message: "unavailable") }
        let outgoing = LarkOutgoing(text: "private", encrypted: payload())
        await #expect(throws: (any Error).self) { try await LarkOutbox.deliver(outgoing, feed: "789", api: hub.client) }
        #expect(hub.sent("POST", path).count == 1)
        await #expect(throws: (any Error).self) { try await LarkOutbox.deliver(outgoing, feed: "456", api: hub.client) }
        #expect(hub.sent("POST", path).count == 1)
        hub.on("POST", "/api/v1/lark/conversations/789/encryption/prepare", json: #"{"accountId":"456","feedId":"789","messages":[{"text":"cipher"}]}"#)
        await #expect(throws: (any Error).self) {
            try await hub.client.larkPrepareEncryption(account: "123", feed: "789", text: "private", cover: "cover")
        }
    }

    @Test func legacyDraftRemainsPlainAndEncryptedModeCannotDisappearOnRestore() throws {
        let old = try JSONDecoder().decode(LarkDraft.self, from: Data(#"{"text":"old"}"#.utf8))
        #expect(old.encryption == nil && old.text == "old")
        let encrypted = LarkDraft(text: "", encryption: .init(cover: "下周再聊"))
        #expect(!encrypted.isEmpty)
        let restored = try JSONDecoder().decode(LarkDraft.self, from: JSONEncoder().encode(encrypted))
        #expect(restored.encryption?.cover == "下周再聊")
        var outgoing = LarkOutgoing(text: "private", encrypted: payload(), at: .distantPast)
        outgoing.state = .sent
        #expect(LarkOutbox.remaining([outgoing], messages: [], now: .now).count == 1)
    }
}
