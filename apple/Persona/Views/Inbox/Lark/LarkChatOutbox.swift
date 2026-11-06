import SwiftUI

/// One durable composer and send queue per Hub group, Feishu account and conversation.
/// Disk transactions and network sends have separate serial queues: an echo never delays the next send.
@MainActor @Observable
final class LarkChatOutbox {
    private struct CacheKey: Hashable { var identity: LarkOutboxIdentity; var base: URL }
    private static var chats: [CacheKey: [String: LarkChatOutbox]] = [:]
    private static var cleanups: [URL: Task<Void, Never>] = [:]
    static func shared(identity: LarkOutboxIdentity, feed: String, base: URL = URL.applicationSupportDirectory) -> LarkChatOutbox {
        let key = CacheKey(identity: identity, base: base)
        if let existing = chats[key]?[feed] { return existing }
        let result = LarkChatOutbox(store: LarkOutboxStore(identity: identity, feed: feed, base: base))
        result.writes = cleanups[base]
        chats[key, default: [:]][feed] = result
        return result
    }

    /// Invalidate on the main actor before any asynchronous deletion. Late callbacks cannot recreate data.
    @discardableResult static func forget(groups: Set<String>? = nil, base: URL = URL.applicationSupportDirectory) -> Task<Void, Never> {
        let keys = chats.keys.filter { $0.base == base && (groups == nil || groups!.contains($0.identity.group)) }
        let queues = keys.flatMap { Array(chats.removeValue(forKey: $0)?.values ?? [:].values) }
        for queue in queues { queue.invalidate() }
        let before = cleanups[base]
        let next = Task {
            await before?.value
            for queue in queues { await queue.store?.invalidate() }
            try? await LarkOutboxAccounts.shared.remove(groups: groups, base: base)
        }
        cleanups[base] = next
        return next
    }

    static func waitForCleanup(base: URL = URL.applicationSupportDirectory) async {
        await cleanups[base]?.value
    }

    private(set) var snapshot = LarkOutboxSnapshot()
    private(set) var ready: Bool
    private(set) var storageProblem: String?
    private(set) var preparationProblem: String?
    private(set) var canRecover = false
    private(set) var invalidated = false
    var identity: LarkOutboxIdentity? { store?.identity }
    var items: [LarkOutgoing] { snapshot.items }
    var composer: (draft: LarkDraft, attachments: [LarkAttachment]) {
        if let pendingDraft { return pendingDraft }
        return (snapshot.draft, snapshot.attachments)
    }
    @ObservationIgnored private let store: LarkOutboxStore?
    @ObservationIgnored private var writes: Task<Void, Never>?
    @ObservationIgnored private var chain: Task<Void, Never>?
    @ObservationIgnored private var pendingDraft: (LarkDraft, [LarkAttachment])?
    @ObservationIgnored private var draftRevision = 0
    @ObservationIgnored private var draftTimer: Task<Void, Never>?
    @ObservationIgnored private var refreshTasks: [UUID: Task<Void, Never>] = [:]

    init(store: LarkOutboxStore? = nil) {
        self.store = store
        ready = store == nil
    }

    /// Old text-only drafts are imported once. A corrupt/newer record is never treated as an empty composer.
    func prepare(legacy: LarkDraft = LarkDraft()) async throws {
        let before = writes
        let next = Task {
            await before?.value
            guard !invalidated else { throw LarkOutboxIdentity.Failure.removed }
            guard !ready, let store else { return }
            let saved: LarkOutboxSnapshot
            if let existing = try await store.load() { saved = existing }
            else { saved = try await store.save(LarkOutboxSnapshot(draft: legacy)) }
            guard !invalidated else { throw LarkOutboxIdentity.Failure.removed }
            snapshot = saved
            ready = true
        }
        writes = Task { _ = try? await next.value }
        do {
            try await next.value
            preparationProblem = nil
            canRecover = false
        } catch {
            preparationProblem = (error as? LarkOutboxStore.Failure)?.localizedDescription ?? String(localized: "Couldn't read drafts on this device. Try again.")
            canRecover = false
            if let reason = error as? LarkOutboxStore.Failure {
                switch reason {
                case .version, .corrupt: canRecover = true
                default: canRecover = false
                }
            }
            throw error
        }
    }

    func recover() async throws {
        let before = writes
        let next = Task {
            await before?.value
            guard !invalidated else { throw LarkOutboxIdentity.Failure.removed }
            guard canRecover, let store else { throw LarkOutboxStore.Failure.notReady }
            let saved = try await store.recover()
            guard !invalidated else { throw LarkOutboxIdentity.Failure.removed }
            snapshot = saved
            ready = true
            preparationProblem = nil
            canRecover = false
        }
        writes = Task { _ = try? await next.value }
        try await next.value
    }

    func saveDraft(_ draft: LarkDraft, attachments: [LarkAttachment]) async throws {
        try await change { $0.draft = draft; $0.attachments = attachments }
    }

    /// Typing coalesces into one transaction. Attachments and lifecycle boundaries flush immediately.
    func scheduleDraft(_ draft: LarkDraft, attachments: [LarkAttachment], immediately: Bool = false) {
        guard !invalidated else { return }
        draftRevision += 1
        pendingDraft = (draft, attachments)
        draftTimer?.cancel()
        draftTimer = Task {
            if !immediately {
                do { try await Task.sleep(for: .seconds(1)) } catch { return }
            }
            await flushDraft()
        }
    }

    func flushDraft() async {
        draftTimer?.cancel()
        draftTimer = nil
        guard let (draft, attachments) = pendingDraft else { return }
        let revision = draftRevision
        pendingDraft = nil
        do { try await saveDraft(draft, attachments: attachments) }
        catch {
            if !invalidated, draftRevision == revision, pendingDraft == nil { pendingDraft = (draft, attachments) }
            storageProblem = String(localized: "Draft not saved. Check this device's storage.")
        }
    }

    /// Acknowledged on disk before clearing the input or performing the first network write.
    func enqueue(_ batch: [LarkOutgoing]) async throws {
        draftRevision += 1
        draftTimer?.cancel()
        draftTimer = nil
        pendingDraft = nil
        try await change {
            $0.items += batch
            $0.draft = LarkDraft()
            $0.attachments = []
        }
    }

    func retry(_ id: UUID) async throws {
        try await change { state in
            guard let index = state.items.firstIndex(where: { $0.id == id }) else { return }
            guard state.items[index].canRetry else { throw LarkOutboxStore.Failure.unconfirmedFile }
            state.items[index].retry()
        }
    }

    func discard(_ id: UUID) async throws {
        try await change { $0.items.removeAll { $0.id == id } }
    }

    func restore(_ id: UUID, draft: LarkDraft, attachments: [LarkAttachment]) async throws {
        draftRevision += 1
        draftTimer?.cancel(); draftTimer = nil; pendingDraft = nil
        try await change {
            $0.items.removeAll { $0.id == id }
            $0.draft = draft
            $0.attachments = attachments
        }
    }

    /// Legacy records have no verified owner. The UI requires explicit account confirmation before import.
    func importLegacy(_ legacy: LarkOutboxSnapshot) async throws {
        await flushDraft()
        try await change {
            guard $0.isEmpty else { throw LarkOutboxStore.Failure.notReady }
            $0 = legacy
        }
    }

    func deliver(_ id: UUID, using api: APIClient, feed: String, refresh: @escaping () async -> Void) {
        deliver(id, client: { api }, feed: feed, refresh: refresh)
    }

    func deliver(_ id: UUID, client: @escaping () -> APIClient?, feed: String, refresh: @escaping () async -> Void) {
        let before = chain
        chain = Task {
            await before?.value
            guard !invalidated, !Task.isCancelled,
                  let outgoing = items.first(where: { $0.id == id }), outgoing.state == .sending else { return }
            let api: APIClient
            do {
                guard let current = client() else { throw LarkOutboxIdentity.Failure.removed }
                api = current
                try await checkAccount(using: api)
                if let encrypted = outgoing.encrypted, encrypted.accountId != identity?.account || encrypted.feedId != feed {
                    throw LarkOutboxIdentity.Failure.changed
                }
            } catch {
                // No write has been attempted: even a preflight timeout is safe to retry explicitly.
                try? await change { state in
                    guard let i = state.items.firstIndex(where: { $0.id == id }) else { return }
                    state.items[i].state = .failed(error.shown ?? error.localizedDescription)
                }
                return
            }
            do {
                try await change { state in
                    guard let i = state.items.firstIndex(where: { $0.id == id }) else { return }
                    state.items[i].attempted = true
                }
            } catch {
                guard !invalidated else { return }
                if let i = snapshot.items.firstIndex(where: { $0.id == id }) {
                    snapshot.items[i].state = .failed(String(localized: "Message not submitted. Check this device's storage, then try again."))
                }
                storageProblem = String(localized: "Message not sent: couldn't save its progress. Check this device's storage.")
                return
            }
            guard !invalidated, !Task.isCancelled, items.contains(where: { $0.id == id && $0.state == .sending }) else { return }
            do {
                let receipt = try await LarkOutbox.deliver(outgoing, feed: feed, api: api)
                // A lost disk write is not a failed network send. Keep the acknowledged state in memory,
                // and the previous durable client ID will reconcile it after a restart.
                do {
                    try await change { state in
                        guard let i = state.items.firstIndex(where: { $0.id == id }) else { return }
                        state.items[i].messageID = receipt.messageID
                        state.items[i].state = .sent
                    }
                } catch {
                    guard !invalidated else { return }
                    if let i = snapshot.items.firstIndex(where: { $0.id == id }) {
                        snapshot.items[i].messageID = receipt.messageID
                        snapshot.items[i].state = .sent
                    }
                    storageProblem = String(localized: "The send result isn't saved on this device yet. Stay on this page until it syncs.")
                }
                refreshTasks[id] = Task {
                    defer { refreshTasks[id] = nil }
                    guard !invalidated, !Task.isCancelled else { return }
                    await refresh()
                    do { try await Task.sleep(for: .seconds(2)) } catch { return }
                    guard !invalidated else { return }
                    await refresh()
                    do { try await Task.sleep(for: .seconds(LarkOutbox.copyWait)) } catch { return }
                    guard !invalidated else { return }
                    await refresh()
                }
            } catch {
                guard !invalidated else { return }
                let offline = (error as? URLError).map { [.notConnectedToInternet, .cannotFindHost, .cannotConnectToHost, .dnsLookupFailed].contains($0.code) } ?? false
                let localFailure = error is CocoaError || error is LarkAttachment.Failure || offline
                let ambiguousFile = outgoing.attachment?.kind == .file && !localFailure && ((error as? APIError)?.status ?? 500) >= 500
                let state: LarkOutgoing.State = (ambiguousFile || (!offline && error is URLError) || error is CancellationError)
                    ? .unconfirmed : .failed(error.shown ?? error.localizedDescription)
                do {
                    try await change { snapshot in
                        guard let i = snapshot.items.firstIndex(where: { $0.id == id }) else { return }
                        snapshot.items[i].state = state
                    }
                } catch { storageProblem = String(localized: "Send status not saved. Check this device's storage.") }
                Haptics.error()
            }
        }
    }

    func settle(_ messages: [LarkMessage]) async throws {
        guard LarkOutbox.remaining(items, messages: messages) != items else { return }
        try await change { $0.items = LarkOutbox.remaining($0.items, messages: messages) }
    }

    func verifyAcknowledged(using api: APIClient, feed: String) async {
        guard (try? await checkAccount(using: api)) != nil else { return }
        for item in items where item.state == .sent {
            guard let id = item.messageID else { continue }
            do {
                if try await api.larkConfirmsDelivery(messageID: id, feed: feed) { try await discard(item.id) }
            } catch { /* Keep the receipt until its exact copy can be verified. */ }
        }
    }

    func checkAccount(using api: APIClient) async throws {
        guard !invalidated else { throw LarkOutboxIdentity.Failure.removed }
        if let identity {
            guard try await api.larkAccountID() == identity.account else { throw LarkOutboxIdentity.Failure.changed }
        }
        guard !invalidated, !Task.isCancelled else { throw LarkOutboxIdentity.Failure.removed }
    }

    private func invalidate() {
        invalidated = true
        draftRevision += 1
        ready = false
        draftTimer?.cancel(); draftTimer = nil; pendingDraft = nil
        chain?.cancel()
        for task in refreshTasks.values { task.cancel() }
        refreshTasks.removeAll()
        snapshot = LarkOutboxSnapshot()
    }

    /// The Lark gateway may publish its local pending copy before returning the receipt. Draw our row only once.
    func hidesEcho(_ message: LarkMessage) -> Bool {
        // The Lark gateway's unconfirmed local rows must not become ordinary sent bubbles after a local discard.
        if message.localState == "pending" || message.localState == "failed" || message.id.hasPrefix("local:") { return true }
        return message.isMine && items.contains { message.id == "local:" + $0.clientID || message.clientID?.lowercased() == $0.clientID }
    }

    private func change(_ mutate: @escaping @MainActor (inout LarkOutboxSnapshot) throws -> Void) async throws {
        let before = writes
        let next = Task {
            await before?.value
            guard !invalidated else { throw LarkOutboxIdentity.Failure.removed }
            guard ready else { throw LarkOutboxStore.Failure.notReady }
            var candidate = snapshot
            try mutate(&candidate)
            if let store { candidate = try await store.save(candidate) }
            guard !invalidated else { throw LarkOutboxIdentity.Failure.removed }
            withAnimation(Theme.Motion.standard) { snapshot = candidate }
            storageProblem = nil
        }
        writes = Task { _ = try? await next.value }
        try await next.value
    }
}

extension EnvironmentValues {
    /// The open chat's durable queue: a thread opened from the chat queues its pictures and files there, as replies in
    /// that thread (#1035), so they survive a restart and settle with the chat's own checks. Nil outside a chat.
    @Entry var larkChatOutbox: LarkChatOutbox? = nil
}
