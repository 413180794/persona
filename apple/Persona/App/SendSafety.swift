import SwiftUI

// Sending into a terminal safely (docs/protocol.md「发消息」): the node refuses a message — nothing typed — when the
// computer's Claude input box holds a draft or a dialog has the keyboard. Each pending message carries its id as
// client_msg_id, so a retry after a lost response is not typed twice.

/// A send refused because the computer's input box holds a draft: the app shows it and may clear it and send.
struct SendRefusal: Identifiable {
    let key: SessionKey
    let messageID: UUID
    /// The input box as shown on the computer.
    let draft: String
    /// The draft the node can clear (one row of typed text); nil: only the user at the computer can.
    let clearable: String?
    var id: UUID { messageID }
}

extension AppModel {
    /// Sends a pending message, telling apart why it failed: a draft on the computer (an alert offers to clear it), an
    /// open dialog, a failure after typing began (it may have arrived; a retry with the same id cannot double it), or
    /// no answer at all (no network, the hub out of reach, a timeout): then it waits and goes again by itself, which
    /// the same id makes safe too.
    func sendPending(_ key: SessionKey, _ msg: PendingMessage, clearDraft: String? = nil) async -> SendOutcome {
        // Restored outbox entries wait for metadata; opaque IDs do not identify a writable source.
        guard let session = session(key) else { return .waiting }
        guard session.canSend else { return .failed }
        guard msg.mayHaveArrived != true else { return .failed }
        guard let api = api(key.group) else { return .failed }
        let socket = socket(key.group)
        do {
            // The message's id is its op id too (docs/protocol.md「操作回执」): however often it arrives, the hub
            // types it once. Through a short drop (a hub restart) it keeps trying with that id instead of going to
            // the outbox's clock; once the drop's grace is over the outbox takes it (flushOutbox).
            let launcher = assistantLaunchers[key], model = assistantModels[key]
            try await OpRetry.untilDelivered(keepTrying: { socket?.takesWrites == true }) {
                try await api.send(key, text: msg.text, images: msg.images, launcher: launcher, model: model,
                                   clientMsgID: msg.id.uuidString, clearDraft: clearDraft, opID: msg.id)
            }
            return .sent
        } catch let e as APIError where e.code == "input_draft" {
            Haptics.attention()
            sendRefusal = SendRefusal(key: key, messageID: msg.id, draft: e.fields["draft"] ?? "", clearable: e.fields["clearable"])
            return .failed
        } catch let e as APIError where e.fields["delivered"] == "unknown" || e.code == OpReceipt.unknownCode {
            toast = Toast(String(localized: "可能已经送到电脑上了：先看看对话里有没有，再决定要不要重发"), style: .error)
            if let i = chat(key).pending.firstIndex(where: { $0.id == msg.id }) { chat(key).pending[i].mayHaveArrived = true }
            return .failed
        } catch where error.isOffline || error.isTimeout || error.isGap {
            return .waiting
        } catch {
            show(error)
            return .failed
        }
    }

    /// The user chose Clear and Send: the node clears that draft (only if the box still shows it) and sends.
    func clearDraftAndSend(_ r: SendRefusal) {
        guard let msg = chat(r.key).pending.first(where: { $0.id == r.messageID }) else { return }
        retry(r.key, msg, clearDraft: r.clearable)
    }

    private static let draftsName = "drafts"

    static func savedDrafts() -> [SessionKey: String] {
        let list = StateCache.load(draftsName, as: [SavedDraft].self) ?? []
        return Dictionary(list.map { ($0.key, $0.text) }, uniquingKeysWith: { a, _ in a })
    }

    func saveDrafts() {
        draftWriter.run { [weak self] in
            guard let self else { return }
            StateCache.save(Self.draftsName, self.drafts.filter { !$0.value.isEmpty }.map { SavedDraft(key: $0.key, text: $0.value) })
        }
    }
}

extension AppModel {
    private static let contextsName = "composer-contexts"

    static func savedContexts() -> [SessionKey: [ContextRef]] {
        let list = StateCache.load(contextsName, as: [SavedContexts].self) ?? []
        return Dictionary(list.map { ($0.key, $0.refs) }, uniquingKeysWith: { a, _ in a })
    }

    func saveContexts() {
        contextWriter.run { [weak self] in
            guard let self else { return }
            StateCache.save(Self.contextsName, self.composerContexts.filter { !$0.value.isEmpty }.map { SavedContexts(key: $0.key, refs: $0.value) })
        }
    }

    nonisolated static func marks(_ drafts: [SessionKey: String], _ contexts: [SessionKey: [ContextRef]]) -> Set<SessionKey> {
        Set(drafts.filter { !$0.value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }.keys)
            .union(contexts.filter { !$0.value.isEmpty }.keys)
    }

    /// `draftMarks` from the drafts and quotes, set only when it differs.
    func markDrafts() {
        let now = Self.marks(drafts, composerContexts)
        if now != draftMarks { draftMarks = now }
    }
}

private nonisolated struct SavedContexts: Codable, Sendable {
    var key: SessionKey
    var refs: [ContextRef]
}

private nonisolated struct SavedDraft: Codable, Sendable {
    var key: SessionKey
    var text: String
}

extension View {
    /// The alert for a send the computer refused because of a draft in its input box.
    func sendRefusalAlert(_ model: AppModel) -> some View {
        alert("电脑上的输入框里有字", isPresented: Binding(
            get: { model.sendRefusal != nil },
            set: { if !$0 { model.sendRefusal = nil } }
        ), presenting: model.sendRefusal) { r in
            if r.clearable != nil {
                Button("清空并发送") { model.clearDraftAndSend(r); model.sendRefusal = nil }
            }
            Button("先不发", role: .cancel) { model.sendRefusal = nil }
        } message: { r in
            let shown = r.draft.count > 200 ? String(r.draft.prefix(200)) + "…" : r.draft
            Text(r.clearable != nil
                 ? "「\(shown)」\n这条消息还没发，免得和它拼在一起。清空电脑上的这段文字再发？"
                 : "「\(shown)」\n这条消息还没发，免得和它拼在一起。这段内容要在电脑上处理掉，再点消息旁的感叹号重发。")
        }
    }
}
