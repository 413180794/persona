import Foundation
import Observation
import Testing
@testable import Jarvis

@MainActor
private final class TodoObservationFlag { var changed = false }

@MainActor
struct TodoCacheTests {
    @Test func requestsDuringARefreshCoalesceIntoOneFollowup() async {
        let hub = FakeHub()
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: {})
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"first","title":"First"}]}"#)
        var client = hub.client
        client.transport = { request in
            let result = try await hub.client.transport!(request)
            if request.path == "/api/v1/todos", hub.sent("GET", request.path).count == 1 {
                await MainActor.run {
                    store.reload(api: hub.client)
                    store.reload(api: hub.client)
                }
                hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"second","title":"Second"}]}"#)
            }
            return result
        }
        await store.reload(api: client).value
        #expect(hub.sent("GET", "/api/v1/todos").count == 2)
        #expect(store.todos.map(\.id) == ["second"])
        #expect(store.byID["first"] == nil)
        #expect(store.byID["second"]?.title == "Second")
    }

    @Test func taskReadersShareCachedAndMutatedTodos() async throws {
        let cached = TodoItem(id: "cached", title: "Cached")
        let store = TodoStore(cachedTodos: [cached], cacheWriter: nil, hubListChanged: {})
        let tasks = TasksStore(todoStore: store)
        let hub = FakeHub()
        #expect(tasks.todos == ["cached": cached])
        hub.on("POST", "/api/v1/todos", json: #"{"todo":{"id":"new","title":"New"}}"#)
        _ = try await store.create(title: "New", api: hub.client)
        #expect(tasks.todos["new"]?.title == "New")
        #expect(tasks.todos == store.byID)
        hub.on("PATCH", "/api/v1/todos/new", json: #"{"todo":{"id":"new","title":"Edited"}}"#)
        _ = try await store.update(id: "new", title: "Edited", api: hub.client)
        #expect(tasks.todos["new"]?.title == "Edited")
        #expect(tasks.todos == store.byID)
        hub.on("DELETE", "/api/v1/todos/new", json: "{}")
        try await store.delete(id: "new", api: hub.client)
        #expect(tasks.todos["new"] == nil)
        #expect(tasks.todos == store.byID)
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"remote","title":"Remote"}]}"#)
        await store.reload(api: hub.client).value
        #expect(tasks.todos.keys.sorted() == ["remote"])
        #expect(tasks.todos == store.byID)
        tasks.forget()
        #expect(tasks.todos["remote"]?.title == "Remote")
        store.forget()
        #expect(tasks.todos.isEmpty && store.byID.isEmpty)
    }

    /// Calls the two stores manually in didConnect order; this does not execute AppModel.didConnect.
    @Test func reconnectionRefreshesTwoShownFeedsButOnlyOneTodoList() async {
        let hub = FakeHub()
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: {})
        let tasks = TasksStore(todoStore: store)
        for conversation in ["one", "two"] {
            hub.on("GET", "/api/v1/assistant/conversations/\(conversation)/feed", json: #"{"items":[]}"#)
            await tasks.load(conversation, refreshTodos: false, api: hub.client)?.value
        }
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[]}"#)
        let reload = store.reload(api: hub.client)
        let feeds = tasks.reconnected(api: hub.client)
        await reload.value
        for feed in feeds { await feed.value }
        #expect(hub.sent("GET", "/api/v1/todos").count == 1)
        #expect(hub.sent("GET", "/api/v1/assistant/conversations/one/feed").count == 2)
        #expect(hub.sent("GET", "/api/v1/assistant/conversations/two/feed").count == 2)
    }

    /// Exercises TodoStore.reload alone; AppModel's todos event wiring is outside this test's scope.
    @Test func oneReloadRequestsOneList() async {
        let hub = FakeHub()
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: {})
        let tasks = TasksStore(todoStore: store)
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"event","title":"Event"}]}"#)
        await store.reload(api: hub.client).value
        #expect(hub.sent("GET", "/api/v1/todos").count == 1)
        #expect(tasks.todos["event"]?.title == "Event")
    }

    @Test func onlyChangedHubListsPushTheWatchSummary() async throws {
        var pushes = 0
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: { pushes += 1 })
        let hub = FakeHub()
        hub.on("POST", "/api/v1/todos", json: #"{"todo":{"id":"t","title":"Local"}}"#)
        _ = try await store.create(title: "Local", api: hub.client)
        #expect(pushes == 0)
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"t","title":"Hub"}]}"#)
        await store.refresh(api: hub.client)
        #expect(pushes == 1)
        await store.refresh(api: hub.client)
        #expect(pushes == 1)
        hub.on("PATCH", "/api/v1/todos/t", json: #"{"todo":{"id":"t","title":"Edited"}}"#)
        _ = try await store.update(id: "t", title: "Edited", api: hub.client)
        #expect(pushes == 1)
        hub.on("DELETE", "/api/v1/todos/t", json: "{}")
        try await store.delete(id: "t", api: hub.client)
        #expect(pushes == 1)
    }

    @Test func observationFollowsTheSharedIndex() async throws {
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: {})
        let tasks = TasksStore(todoStore: store)
        let observed = TodoObservationFlag()
        withObservationTracking { _ = tasks.todos["t"] } onChange: {
            MainActor.assumeIsolated { observed.changed = true }
        }
        let hub = FakeHub()
        hub.on("POST", "/api/v1/todos", json: #"{"todo":{"id":"t","title":"New"}}"#)
        _ = try await store.create(title: "New", api: hub.client)
        #expect(observed.changed)
        #expect(tasks.todos["t"]?.title == "New")
    }

    @Test func aHubEchoAfterALocalEditPushesTheWatchSummary() async throws {
        var pushes = 0
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: { pushes += 1 })
        let hub = FakeHub()
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"t","title":"Hub"}]}"#)
        await store.refresh(api: hub.client)
        #expect(pushes == 1)
        hub.on("PATCH", "/api/v1/todos/t", json: #"{"todo":{"id":"t","title":"Edited"}}"#)
        _ = try await store.update(id: "t", title: "Edited", api: hub.client)
        #expect(pushes == 1)
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"t","title":"Edited"}]}"#)
        await store.refresh(api: hub.client)
        #expect(pushes == 2)
    }

    @Test func cachedTodosAndForgetKeepTheLastPushedComparison() async {
        var pushes = 0
        let store = TodoStore(cachedTodos: [TodoItem(id: "t", title: "Cached")], cacheWriter: nil,
                              hubListChanged: { pushes += 1 })
        let hub = FakeHub()
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"t","title":"Cached"}]}"#)
        await store.refresh(api: hub.client)
        #expect(pushes == 0)
        store.forget()
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"new","title":"New"}]}"#)
        await store.refresh(api: hub.client)
        #expect(pushes == 1)
        store.forget()
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[]}"#)
        await store.refresh(api: hub.client)
        #expect(pushes == 2)
    }

    @Test func forgetDiscardsAnOldResponseWithoutPersistingIt() async throws {
        var writes: [[TodoItem]] = []
        var pushes = 0
        let store = TodoStore(cachedTodos: [], cacheWriter: { writes.append($0) }, hubListChanged: { pushes += 1 })
        let hub = FakeHub()
        var old = hub.client
        old.transport = { _ in
            await MainActor.run { store.forget() }
            return Data(#"{"todos":[{"id":"old","title":"Old","spaces":[]}]}"#.utf8)
        }
        await store.refresh(api: old)
        #expect(store.todos.isEmpty && store.byID.isEmpty)
        #expect(store.error == nil && store.isSupported && !store.spacesSupported && !store.loading)
        #expect(pushes == 0)
        // Observe the real debounce through an in-memory writer, never the application cache file.
        for _ in 0..<150 where writes.isEmpty { try await Task.sleep(for: .milliseconds(20)) }
        #expect(writes == [[]]) // Only forget's empty snapshot, never the old response.
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"new","title":"New","spaces":[]}]}"#)
        await store.reload(api: hub.client).value
        #expect(store.byID.keys.sorted() == ["new"])
        #expect(store.spacesSupported)
        #expect(pushes == 1)
    }

    @Test(arguments: [404, 500]) func forgetDiscardsOldFetchErrors(_ status: Int) async {
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: {})
        let hub = FakeHub()
        var old = hub.client
        old.transport = { _ in
            await MainActor.run {
                store.forget()
                store.error = "Current hub error"
            }
            throw APIError(status: status, message: "Old hub error")
        }
        await store.refresh(api: old)
        #expect(store.todos.isEmpty && store.byID.isEmpty)
        #expect(store.isSupported && !store.spacesSupported && !store.loading)
        #expect(store.error == "Current hub error")
    }

    @Test(arguments: ["linkAgent", "unlinkAgent", "setSpace"])
    func membershipFollowupDiscardsAnOldResponseAfterForget(_ operation: String) async throws {
        var pushes = 0
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: { pushes += 1 })
        let hub = FakeHub()
        var client = hub.client
        client.transport = { request in
            if request.method == "GET" {
                await MainActor.run { store.forget() }
                return Data(#"{"todos":[{"id":"old","title":"Old","spaces":[]}]}"#.utf8)
            }
            return Data("{}".utf8)
        }
        switch operation {
        case "linkAgent": try await store.linkAgent(todoID: "t", machineID: "m", sessionID: "s", api: client)
        case "unlinkAgent": try await store.unlinkAgent(todoID: "t", machineID: "m", sessionID: "s", api: client)
        default: try await store.setSpace(todoID: "t", space: .init(machineID: "m", space: "p"), linked: true, api: client)
        }
        #expect(store.todos.isEmpty && store.byID.isEmpty)
        #expect(store.isSupported && !store.spacesSupported)
        #expect(store.error == nil && pushes == 0)
    }

    @Test func writeOperationsDiscardOldResponsesAndRollbacksAfterForget() async throws {
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: {})
        let hub = FakeHub()

        // 1. create in-flight when forget happens
        var createClient = hub.client
        createClient.transport = { _ in
            await MainActor.run { store.forget() }
            return Data(#"{"todo":{"id":"created","title":"Created"}}"#.utf8)
        }
        _ = try await store.create(title: "Created", api: createClient)
        #expect(store.todos.isEmpty)
        #expect(store.byID["created"] == nil)

        // 2. update in-flight when forget happens
        var updateClient = hub.client
        updateClient.transport = { _ in
            await MainActor.run { store.forget() }
            return Data(#"{"todo":{"id":"updated","title":"Updated"}}"#.utf8)
        }
        _ = try await store.update(id: "updated", title: "Updated", api: updateClient)
        #expect(store.todos.isEmpty)
        #expect(store.byID["updated"] == nil)

        // 3. toggleDone in-flight when forget happens and PATCH fails (revert must not restore original)
        let originalItem = TodoItem(id: "orig", title: "Original", doneAt: nil)
        let storeWithItem = TodoStore(cachedTodos: [originalItem], cacheWriter: nil, hubListChanged: {})
        var toggleClient = hub.client
        toggleClient.transport = { _ in
            await MainActor.run { storeWithItem.forget() }
            throw APIError(status: 500, message: "Server error")
        }
        storeWithItem.toggleDone(originalItem, api: toggleClient)
        // Give background Task a chance to hit the catch block
        for _ in 0..<50 where !storeWithItem.todos.isEmpty {
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(storeWithItem.todos.isEmpty)
        #expect(storeWithItem.byID.isEmpty)

        // 4. setSpace in-flight when forget happens and link fails (revert must not restore original)
        let spaceStore = TodoStore(cachedTodos: [originalItem], cacheWriter: nil, hubListChanged: {})
        var spaceClient = hub.client
        spaceClient.transport = { _ in
            await MainActor.run { spaceStore.forget() }
            throw APIError(status: 500, message: "Link failure")
        }
        do {
            _ = try await spaceStore.setSpace(todoID: "orig", space: .init(machineID: "m", space: "sp"), linked: true, api: spaceClient)
        } catch {}
        #expect(spaceStore.todos.isEmpty)
        #expect(spaceStore.byID.isEmpty)
    }

    @Test func reloadDoesNotWaitForSlowPriorGenerationFetch() async throws {
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: {})
        let hub = FakeHub()

        let slowFetchStarted = AsyncStream<Void>.makeStream()
        let allowSlowFetchToFinish = AsyncStream<Void>.makeStream()

        var slowClient = hub.client
        slowClient.transport = { request in
            if request.method == "GET" && request.path == "/api/v1/todos" {
                slowFetchStarted.continuation.yield()
                // Wait for explicit signal before finishing old fetch
                for await _ in allowSlowFetchToFinish.stream { break }
                return Data(#"{"todos":[{"id":"old","title":"Old"}]}"#.utf8)
            }
            return Data("{}".utf8)
        }

        // Start old reload
        let oldTask = store.reload(api: slowClient)
        for await _ in slowFetchStarted.stream { break }

        // Switch hub
        store.forget()
        #expect(store.todos.isEmpty)

        // New hub reload starts immediately without waiting for oldTask
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"new","title":"New"}]}"#)
        let newTask = store.reload(api: hub.client)
        await newTask.value

        #expect(store.todos.map(\.id) == ["new"])
        #expect(store.byID["new"]?.title == "New")

        // Now allow old fetch to finish and verify it does not overwrite new hub's todos
        allowSlowFetchToFinish.continuation.yield()
        await oldTask.value

        #expect(store.todos.map(\.id) == ["new"])
        #expect(store.byID["new"]?.title == "New")
        #expect(store.byID["old"] == nil)
    }

    @Test func loadingRemainsTrueWhenOldGenerationFetchCompletesDuringNewFetch() async throws {
        let store = TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: {})
        let hub = FakeHub()

        let oldFetchStarted = AsyncStream<Void>.makeStream()
        let allowOldFetchToFinish = AsyncStream<Void>.makeStream()

        var oldClient = hub.client
        oldClient.transport = { request in
            if request.method == "GET" && request.path == "/api/v1/todos" {
                oldFetchStarted.continuation.yield()
                for await _ in allowOldFetchToFinish.stream { break }
                return Data(#"{"todos":[{"id":"old","title":"Old"}]}"#.utf8)
            }
            return Data("{}".utf8)
        }

        let oldTask = store.reload(api: oldClient)
        for await _ in oldFetchStarted.stream { break }
        #expect(store.loading == true)

        store.forget()
        #expect(store.loading == false)

        let newFetchStarted = AsyncStream<Void>.makeStream()
        let allowNewFetchToFinish = AsyncStream<Void>.makeStream()

        var newClient = hub.client
        newClient.transport = { request in
            if request.method == "GET" && request.path == "/api/v1/todos" {
                newFetchStarted.continuation.yield()
                for await _ in allowNewFetchToFinish.stream { break }
                return Data(#"{"todos":[{"id":"new","title":"New"}]}"#.utf8)
            }
            return Data("{}".utf8)
        }

        let newTask = store.reload(api: newClient)
        for await _ in newFetchStarted.stream { break }
        #expect(store.loading == true)

        // Old fetch returns while new fetch is still in flight
        allowOldFetchToFinish.continuation.yield()
        await oldTask.value

        // store.loading must remain true because old fetch generation mismatch did not clear loading
        #expect(store.loading == true)

        // New fetch finishes and clears loading
        allowNewFetchToFinish.continuation.yield()
        await newTask.value
        #expect(store.loading == false)
        #expect(store.todos.map(\.id) == ["new"])
    }
}
