import Foundation

extension APIClient {
    /// A receipt can predate the loaded history window. Read that exact message instead of pinning an old
    /// attachment at the bottom forever, or discarding it because another file has the same name.
    func larkConfirmsDelivery(messageID: String, feed: String) async throws -> Bool {
        guard !messageID.isEmpty, messageID.count <= 128,
              messageID.utf8.allSatisfy({ (48...57).contains($0) || (65...90).contains($0) || (97...122).contains($0) || $0 == 45 || $0 == 95 }) else { return false }
        struct Proof: Decodable {
            var id: String
            var feedId: String
            var isMine: Bool
            var localState: String?
        }
        struct Reply: Decodable { var item: Proof }
        let reply: Reply = try await get("/api/v1/lark/raw/messages/" + messageID)
        return reply.item.id == messageID && reply.item.feedId == feed && reply.item.isMine &&
            reply.item.localState != "pending" && reply.item.localState != "failed"
    }
}
