import Foundation
import Testing
@testable import Jarvis

@MainActor struct LarkConversationsTests {
    private func chat(_ id: String, _ preview: String = "") -> LarkConversation {
        LarkConversation(id: id, name: id, isGroup: true, preview: preview)
    }

    private func snapshot(_ rows: [LarkConversation], pinned: [LarkConversation] = [], owner: String = "one") -> LarkConversationsModel.Snapshot {
        .init(accountID: owner, conversations: rows, pinned: pinned)
    }
    private func reply(conversations: [LarkConversation], nextCursor: String? = nil, owner: String = "one") -> APIClient.LarkConversations {
        .init(conversations: conversations, nextCursor: nextCursor, accountID: owner)
    }

    @Test func paintsCacheThenUpdatesWithoutDuplicatingRows() async {
        var saved: [LarkConversation] = []
        let store = LarkConversationsModel(deps: .init(
            page: { _, _ in reply(conversations: [chat("new"), chat("old", "updated")], nextCursor: "next") },
            pinned: { reply(conversations: []) }, status: { .init(configured: true, connected: true) },
            cached: { snapshot([chat("old"), chat("history")]) }, save: { saved = $0?.conversations ?? [] }))
        #expect(store.conversations.map(\.id) == ["old", "history"])
        await store.refresh()
        #expect(store.conversations.map(\.id) == ["new", "old"])
        #expect(store.conversations[1].preview == "updated")
        #expect(saved == store.conversations)
    }

    @Test func pinnedChatsSitInTheListWhereTheirTimePutsThem() {
        func at(_ id: String, _ minutes: Double, _ preview: String = "") -> LarkConversation {
            var c = chat(id, preview); c.updatedAt = Date(timeIntervalSince1970: 1_000_000 - minutes * 60); return c
        }
        // The hub's page: the pinned first in pin order, then the rest newest first.
        let rows = [at("pin-old", 50, "hello"), at("pin-new", 1), at("a", 5), at("b", 20, "hello"), at("c", 40)]
        let pinned = [at("pin-old", 50), at("pin-new", 1), at("pin-unloaded", 30)]
        // No page left: every pinned chat where its time puts it, the shelf's own copy for one not on a page.
        #expect(LarkConversationsModel.listed(rows, pinned: pinned, query: "").map(\.id)
                == ["pin-new", "a", "b", "pin-unloaded", "c", "pin-old"])
        // More pages: one older than the last loaded row waits for them.
        #expect(LarkConversationsModel.listed(rows, pinned: pinned, query: "", more: true).map(\.id)
                == ["pin-new", "a", "b", "pin-unloaded", "c"])
        // A page of nothing but pinned chats, more to come: nothing to place them against yet.
        #expect(LarkConversationsModel.listed(Array(rows.prefix(2)), pinned: pinned, query: "", more: true).isEmpty)
        // No shelf: the hub's order; a search: the hub's rows, pinned ones included.
        #expect(LarkConversationsModel.listed(rows, pinned: [], query: "").map(\.id) == rows.map(\.id))
        #expect(LarkConversationsModel.listed(rows, pinned: pinned, query: " hello ").map(\.id) == ["pin-old", "b"])
    }

    @Test func aPageDroppedByARefreshIsAskedForAgain() async {
        var asks = 0
        var refreshing: Task<Void, Never>?
        var store: LarkConversationsModel!
        store = LarkConversationsModel(deps: .init(
            page: { cursor, _ in
                guard cursor == "next" else { return reply(conversations: [chat("a")], nextCursor: "next") }
                asks += 1
                if asks == 1 {
                    // A push refreshes the list while this page is on its way.
                    refreshing = Task { await store.refresh() }
                    for _ in 0..<5 { await Task.yield() }
                }
                return reply(conversations: [chat("b")])
            },
            pinned: { reply(conversations: []) }, status: { .init(configured: true, connected: true) }, cached: { nil }, save: { _ in }))
        await store.refresh()
        await store.more()
        await refreshing?.value
        #expect(asks >= 2)
        #expect(store.conversations.map(\.id) == ["a", "b"])
        #expect(store.nextCursor == nil)
    }

    @Test func paginationDeduplicatesAndStopsOnRepeatedCursor() async {
        let store = LarkConversationsModel(deps: .init(
            page: { cursor, _ in reply(conversations: cursor == nil ? [chat("a")] : [chat("a"), chat("b")], nextCursor: "same") },
            pinned: { reply(conversations: []) }, status: { .init(configured: true, connected: true) }, cached: { nil }, save: { _ in }))
        await store.refresh()
        await store.more()
        #expect(store.conversations.map(\.id) == ["a", "b"])
        #expect(store.nextCursor == nil)
    }

    @Test func offlineAndCancellationPreserveTheList() async {
        var offline = true
        let store = LarkConversationsModel(deps: .init(
            page: { _, _ in
                if offline { throw URLError(.notConnectedToInternet) }
                throw CancellationError()
            }, pinned: { reply(conversations: []) }, status: { .init(configured: true, connected: false) },
            cached: { snapshot([chat("cached")]) }, save: { _ in Issue.record("Failed fetch must not overwrite cache") }))
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
            page: { cursor, _ in
                cursors.append(cursor)
                if cursor == nil { return reply(conversations: [chat("head")], nextCursor: "tail") }
                if refreshed { return reply(conversations: [chat("tail", "read elsewhere")], nextCursor: "third") }
                return reply(conversations: [chat("tail", "unread"), chat("left-group")], nextCursor: "third")
            }, pinned: { reply(conversations: []) }, status: { .init(configured: true, connected: true) }, cached: { nil }, save: { _ in }))
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
            page: { cursor, _ in
                if cursor != nil, fail { throw URLError(.networkConnectionLost) }
                return reply(conversations: [chat(cursor ?? "head", fail ? "changed" : "old")], nextCursor: cursor == nil ? "tail" : "third")
            }, pinned: { reply(conversations: []) }, status: { .init(configured: true, connected: true) }, cached: { nil }, save: { _ in saves += 1 }))
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
            page: { cursor, _ in
                if hold, cursor == nil { await withCheckedContinuation { gate = $0 } }
                return reply(conversations: [chat(cursor ?? "head")], nextCursor: cursor == nil ? "tail" : nil)
            }, pinned: { reply(conversations: []) }, status: { .init(configured: true, connected: true) }, cached: { nil }, save: { _ in }))
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
            page: { _, _ in
                calls += 1
                if calls == 1 { await withCheckedContinuation { gate = $0 } }
                return reply(conversations: [chat("fresh")], nextCursor: nil)
            }, pinned: { reply(conversations: []) }, status: { .init(configured: true, connected: true) }, cached: { nil }, save: { _ in }))
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
    @Test func pinsAndRowsCommitTogetherAndOfflineKeepsBoth() async {
        var fail = false
        let oldPins = [chat("old-pin")]
        let store = LarkConversationsModel(deps: .init(
            page: { _, _ in if fail { throw URLError(.networkConnectionLost) }; return reply(conversations: [chat("new")]) },
            pinned: { reply(conversations: [chat("new-pin")]) }, status: { .init(configured: true, connected: true) },
            cached: { snapshot([chat("old")], pinned: oldPins) }, save: { _ in }))
        #expect(store.pinned == oldPins)
        fail = true
        await store.refresh()
        #expect(store.pinned == oldPins)
        #expect(store.conversations.map(\.id) == ["old"])
        fail = false
        await store.refresh()
        #expect(store.pinned.map(\.id) == ["new-pin"])
        #expect(store.conversations.map(\.id) == ["new"])
    }

    @Test func changingAccountClearsOldRowsBeforeNewPagesFinish() async {
        var gate: CheckedContinuation<Void, Never>?
        var saves: [LarkConversationsModel.Snapshot?] = []
        let store = LarkConversationsModel(deps: .init(
            page: { _, owner in
                #expect(owner == "two")
                await withCheckedContinuation { gate = $0 }
                return reply(conversations: [chat("new")], owner: "two")
            }, pinned: { reply(conversations: [chat("new-pin")], owner: "two") },
            status: { .init(configured: true, connected: true) },
            cached: { snapshot([chat("old")], pinned: [chat("old-pin")]) }, save: { saves.append($0) }))
        let task = Task { await store.refresh() }
        while gate == nil { await Task.yield() }
        #expect(store.accountID == "two")
        #expect(store.conversations.isEmpty && store.pinned.isEmpty)
        #expect(saves.count == 1 && saves[0] == nil)
        gate?.resume(); await task.value
        #expect(store.conversations.map(\.id) == ["new"])
        #expect(saves.last??.accountID == "two")
    }

    @Test func accountChangeDuringPaginationNeverMergesUsers() async {
        let store = LarkConversationsModel(deps: .init(
            page: { cursor, _ in reply(conversations: [chat("row")], nextCursor: cursor == nil ? "tail" : nil,
                                      owner: cursor == nil ? "one" : "two") },
            pinned: { reply(conversations: [chat("pin")]) }, status: { .init(configured: true, connected: true) },
            cached: { nil }, save: { _ in }))
        await store.refresh(); await store.more()
        #expect(store.accountID == nil)
        #expect(store.conversations.isEmpty && store.pinned.isEmpty)
        #expect(store.pageFailed)
    }

    @Test func olderHubCannotWriteAnUnownedSnapshot() async {
        var saved = false
        let store = LarkConversationsModel(deps: .init(
            page: { _, _ in Issue.record("Must check owner before pages"); return reply(conversations: []) },
            pinned: { .init(conversations: [chat("unknown")], nextCursor: nil) },
            status: { .init(configured: true, connected: true) }, cached: { nil }, save: { _ in saved = true }))
        await store.refresh()
        #expect(store.needsUpgrade && store.failed)
        #expect(!saved && store.pinned.isEmpty)
    }

    @Test func liveChangesWaitForRestAndOnlyTheNewestSnapshotAppears() async {
        var version = 0
        let store = LarkConversationsModel(deps: .init(
            page: { _, _ in reply(conversations: [chat("row", "version \(version)")]) },
            pinned: { reply(conversations: [chat("pin-\(version)")]) }, status: { .init(configured: true, connected: true) },
            cached: { nil }, save: { _ in }))
        await store.refresh()
        store.holdScrolling(true)
        version = 1; await store.refresh()
        version = 2; await store.refresh()
        #expect(store.conversations.first?.preview == "version 0")
        #expect(store.pinned.first?.id == "pin-0")
        store.holdScrolling(false)
        #expect(store.conversations.first?.preview == "version 2")
        #expect(store.pinned.first?.id == "pin-2")
    }

    @Test func aNewerRefreshDiscardsLateResponsesEvenWhenTheyIgnoreCancellation() async {
        var gate: CheckedContinuation<Void, Never>?
        var calls = 0
        var published: [String] = []
        let store = LarkConversationsModel(deps: .init(
            page: { _, _ in
                calls += 1
                let id = calls == 1 ? "stale" : "fresh"
                if calls == 1 { await withCheckedContinuation { gate = $0 } }
                return reply(conversations: [chat(id)])
            }, pinned: { reply(conversations: []) }, status: { .init(configured: true, connected: true) },
            cached: { nil }, save: { published += $0?.conversations.map(\.id) ?? [] }))
        let old = Task { await store.refresh() }
        while gate == nil { await Task.yield() }
        let newer = Task { await store.refresh() }
        await Task.yield()
        gate?.resume()
        await old.value; await newer.value
        #expect(published == ["fresh"])
    }

    @Test func changingAccountWhileScrollingImmediatelyClearsOldAndPendingData() async {
        var owner = "one"
        var offline = false
        let store = LarkConversationsModel(deps: .init(
            page: { _, account in
                if offline { throw URLError(.networkConnectionLost) }
                return reply(conversations: [chat(account)], owner: account)
            }, pinned: { reply(conversations: [chat(owner + "-pin")], owner: owner) },
            status: { .init(configured: true, connected: true) }, cached: { nil }, save: { _ in }))
        await store.refresh()
        store.holdScrolling(true)
        await store.refresh()
        owner = "two"; offline = true
        await store.refresh()
        #expect(store.conversations.isEmpty && store.pinned.isEmpty)
        store.holdScrolling(false)
        #expect(store.conversations.isEmpty && store.pinned.isEmpty)
        #expect(store.accountID == "two")
    }

    @Test func signedOutRemovesCachedAccountButNetworkFailureDoesNot() async {
        let store = LarkConversationsModel(deps: .init(
            page: { _, _ in reply(conversations: []) },
            pinned: { throw APIError(status: 502, message: "signed out", fields: ["code": "lark_not_logged_in"]) },
            status: { .init(configured: true, connected: false) },
            cached: { snapshot([chat("old")], pinned: [chat("old-pin")]) }, save: { _ in }))
        await store.refresh()
        #expect(store.accountID == nil && store.conversations.isEmpty && store.pinned.isEmpty)
    }

    @Test func paginationArrivingDuringAnotherDragWaitsForRest() async {
        var gate: CheckedContinuation<Void, Never>?
        let store = LarkConversationsModel(deps: .init(
            page: { cursor, _ in
                if cursor != nil { await withCheckedContinuation { gate = $0 } }
                return reply(conversations: [chat(cursor ?? "head")], nextCursor: cursor == nil ? "tail" : nil)
            }, pinned: { reply(conversations: []) }, status: { .init(configured: true, connected: true) },
            cached: { nil }, save: { _ in }))
        await store.refresh()
        let more = Task { await store.more() }
        while gate == nil { await Task.yield() }
        store.holdScrolling(true)
        gate?.resume(); await more.value
        #expect(store.conversations.map(\.id) == ["head"])
        store.holdScrolling(false)
        #expect(store.conversations.map(\.id) == ["head", "tail"])
        #expect(store.nextCursor == nil)
    }
}
