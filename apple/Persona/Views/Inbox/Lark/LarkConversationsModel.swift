import CryptoKit
import Foundation
import Observation

/// The conversation browser uses the same Hub API and chat caches as the inbox.
@MainActor @Observable
final class LarkConversationsModel {
    struct Deps {
        var page: (String?) async throws -> APIClient.LarkConversations
        var status: () async throws -> LarkStatus
        var cached: () -> [LarkConversation]
        var save: ([LarkConversation]) -> Void

        static func live(_ config: ServerConfig) -> Self {
            let api = APIClient(config: config)
            let name = cacheName(config)
            return Self(page: { try await api.larkConversations(cursor: $0) },
                        status: { try await api.larkStatus() },
                        cached: { StateCache.load(name) ?? [] },
                        save: { StateCache.save(name, $0) })
        }
    }

    private(set) var conversations: [LarkConversation]
    private(set) var loading = false
    private(set) var loadingMore = false
    private(set) var nextCursor: String?
    private(set) var status: LarkStatus?
    private(set) var failed = false
    private(set) var pageFailed = false
    @ObservationIgnored private let deps: Deps
    @ObservationIgnored private var loadedPages = 1
    @ObservationIgnored private var pageWaiters: [CheckedContinuation<Void, Never>] = []

    init(deps: Deps) {
        self.deps = deps
        conversations = deps.cached()
    }

    /// Switching Hub or device credentials cannot show the previous connection's list.
    static func cacheName(_ config: ServerConfig) -> String {
        let digest = SHA256.hash(data: Data((config.url.absoluteString + "\n" + config.token).utf8))
        return "lark-conversations-" + digest.map { String(format: "%02x", $0) }.joined()
    }

    /// Replace the loaded window atomically: old tail rows must not override fresh unread counts or
    /// survive indefinitely after leaving a group. Keep the old snapshot visible until all pages succeed.
    func refresh() async {
        while loading || loadingMore {
            await withCheckedContinuation { pageWaiters.append($0) }
            guard !Task.isCancelled else { return }
        }
        guard !Task.isCancelled else { return }
        loading = true
        defer { loading = false; wakePageWaiters() }
        do {
            var rows: [LarkConversation] = []
            var cursor: String?
            var seen = Set<String>()
            var count = 0
            repeat {
                let page = try await deps.page(cursor)
                try Task.checkCancellation()
                rows = Self.merge(rows, page.conversations)
                count += 1
                cursor = page.nextCursor.flatMap { $0.isEmpty || !seen.insert($0).inserted ? nil : $0 }
            } while cursor != nil && count < loadedPages
            conversations = rows
            loadedPages = count
            nextCursor = cursor
            failed = false
            pageFailed = false
            deps.save(conversations)
        } catch {
            if !Self.cancelled(error) { failed = true }
        }
        if !Task.isCancelled { status = try? await deps.status() }
    }

    func more() async {
        // The visible footer may ask while refresh is running. Resume it on the new cursor instead
        // of silently dropping the request (its SwiftUI task id may not change after refresh).
        while loading {
            await withCheckedContinuation { pageWaiters.append($0) }
            guard !Task.isCancelled else { return }
        }
        guard let cursor = nextCursor, !loadingMore, !Task.isCancelled else { return }
        loadingMore = true
        do {
            let page = try await deps.page(cursor)
            try Task.checkCancellation()
            conversations = Self.merge(conversations, page.conversations)
            loadedPages += 1
            nextCursor = page.nextCursor.flatMap { $0.isEmpty || $0 == cursor ? nil : $0 }
            pageFailed = false
            deps.save(conversations)
        } catch {
            if !Self.cancelled(error) { pageFailed = true }
        }
        loadingMore = false
        wakePageWaiters()
    }

    private func wakePageWaiters() {
        let waiting = pageWaiters
        pageWaiters = []
        waiting.forEach { $0.resume() }
    }

    private static func cancelled(_ error: Error) -> Bool {
        error is CancellationError || (error as? URLError)?.code == .cancelled || Task.isCancelled
    }

    /// Keep the server's order, but a fresh copy always replaces an older row with the same id.
    static func merge(_ existing: [LarkConversation], _ fresh: [LarkConversation]) -> [LarkConversation] {
        var rows = existing
        var indices = Dictionary(existing.enumerated().map { ($0.element.id, $0.offset) }, uniquingKeysWith: { first, _ in first })
        for row in fresh {
            if let index = indices[row.id] { rows[index] = row }
            else { indices[row.id] = rows.count; rows.append(row) }
        }
        return rows
    }
}
