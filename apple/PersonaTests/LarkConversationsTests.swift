import Foundation
import Testing
@testable import Jarvis

@MainActor struct LarkConversationsTests {
    private func chat(_ id: String, _ preview: String = "") -> LarkConversation {
        LarkConversation(id: id, name: id, isGroup: true, preview: preview)
    }

    @Test func paintsCacheThenUpdatesWithoutDuplicatingRows() async {
        var saved: [LarkConversation] = []
        let store = LarkConversationsModel(deps: .init(
            page: { _ in .init(conversations: [chat("new"), chat("old", "updated")], nextCursor: "next") },
            status: { .init(configured: true, connected: true) },
            cached: { [chat("old"), chat("history")] }, save: { saved = $0 }))
        #expect(store.conversations.map(\.id) == ["old", "history"])
        await store.refresh()
        #expect(store.conversations.map(\.id) == ["new", "old"])
        #expect(store.conversations[1].preview == "updated")
        #expect(saved == store.conversations)
    }

    @Test func paginationDeduplicatesAndStopsOnRepeatedCursor() async {
        let store = LarkConversationsModel(deps: .init(
            page: { cursor in .init(conversations: cursor == nil ? [chat("a")] : [chat("a"), chat("b")], nextCursor: "same") },
            status: { .init(configured: true, connected: true) }, cached: { [] }, save: { _ in }))
        await store.refresh()
        await store.more()
        #expect(store.conversations.map(\.id) == ["a", "b"])
        #expect(store.nextCursor == nil)
    }

    @Test func offlineAndCancellationPreserveTheList() async {
        var offline = true
        let store = LarkConversationsModel(deps: .init(
            page: { _ in
                if offline { throw URLError(.notConnectedToInternet) }
                throw CancellationError()
            }, status: { .init(configured: true, connected: false) },
            cached: { [chat("cached")] }, save: { _ in Issue.record("Failed fetch must not overwrite cache") }))
        await store.refresh()
        #expect(store.failed)
        #expect(store.conversations.map(\.id) == ["cached"])
        offline = false
        await store.refresh()
        #expect(!store.loading)
        #expect(store.conversations.map(\.id) == ["cached"])
    }

    @Test func refreshReplacesLoadedTailAndKeepsItsCursor() async {
        var refreshed = false
        var cursors: [String?] = []
        let store = LarkConversationsModel(deps: .init(
            page: { cursor in
                cursors.append(cursor)
                if cursor == nil { return .init(conversations: [chat("head")], nextCursor: "tail") }
                if refreshed { return .init(conversations: [chat("tail", "read elsewhere")], nextCursor: "third") }
                return .init(conversations: [chat("tail", "unread"), chat("left-group")], nextCursor: "third")
            }, status: { .init(configured: true, connected: true) }, cached: { [] }, save: { _ in }))
        await store.refresh()
        await store.more()
        refreshed = true
        cursors = []
        await store.refresh()
        #expect(cursors == [nil, "tail"])
        #expect(store.conversations.map(\.id) == ["head", "tail"])
        #expect(store.conversations.last?.preview == "read elsewhere")
        #expect(store.nextCursor == "third")
    }

    @Test func failedTailRefreshDoesNotPublishPartialSnapshot() async {
        var fail = false
        var saves = 0
        let store = LarkConversationsModel(deps: .init(
            page: { cursor in
                if cursor != nil, fail { throw URLError(.networkConnectionLost) }
                return .init(conversations: [chat(cursor ?? "head", fail ? "changed" : "old")], nextCursor: cursor == nil ? "tail" : "third")
            }, status: { .init(configured: true, connected: true) }, cached: { [] }, save: { _ in saves += 1 }))
        await store.refresh()
        await store.more()
        let old = store.conversations
        fail = true
        await store.refresh()
        #expect(store.failed)
        #expect(store.conversations == old)
        #expect(store.nextCursor == "third")
        #expect(saves == 2)
    }

    @Test func footerRequestWaitsForRefreshInsteadOfBeingLost() async {
        var gate: CheckedContinuation<Void, Never>?
        var hold = false
        let store = LarkConversationsModel(deps: .init(
            page: { cursor in
                if hold, cursor == nil { await withCheckedContinuation { gate = $0 } }
                return .init(conversations: [chat(cursor ?? "head")], nextCursor: cursor == nil ? "tail" : nil)
            }, status: { .init(configured: true, connected: true) }, cached: { [] }, save: { _ in }))
        await store.refresh()
        hold = true
        let refresh = Task { await store.refresh() }
        while gate == nil { await Task.yield() }
        let footer = Task { await store.more() }
        await Task.yield()
        gate?.resume()
        await refresh.value
        await footer.value
        #expect(store.conversations.map(\.id) == ["head", "tail"])
        #expect(store.nextCursor == nil)
        #expect(!store.loadingMore)
    }

    @Test func reconnectRefreshSurvivesCancellationOfPreviousTask() async {
        var gate: CheckedContinuation<Void, Never>?
        var calls = 0
        let store = LarkConversationsModel(deps: .init(
            page: { _ in
                calls += 1
                if calls == 1 { await withCheckedContinuation { gate = $0 } }
                return .init(conversations: [chat("fresh")], nextCursor: nil)
            }, status: { .init(configured: true, connected: true) }, cached: { [] }, save: { _ in }))
        let old = Task { await store.refresh() }
        while gate == nil { await Task.yield() }
        old.cancel()
        let reconnected = Task { await store.refresh() }
        await Task.yield()
        gate?.resume()
        await old.value
        await reconnected.value
        #expect(calls == 2)
        #expect(store.conversations.map(\.id) == ["fresh"])
        #expect(!store.loading)
    }

    @Test func cacheSeparatesHubAndCredentialsWithoutPuttingTokenInFilename() {
        let a = ServerConfig(url: URL(string: "https://one.example.com")!, token: "first-secret")
        let b = ServerConfig(url: a.url, token: "other-secret")
        let c = ServerConfig(url: URL(string: "https://two.example.com")!, token: a.token)
        #expect(LarkConversationsModel.cacheName(a) != LarkConversationsModel.cacheName(b))
        #expect(LarkConversationsModel.cacheName(a) != LarkConversationsModel.cacheName(c))
        #expect(!LarkConversationsModel.cacheName(a).contains(a.token))
    }
}
