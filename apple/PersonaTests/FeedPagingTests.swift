import Foundation
import Testing
@testable import Persona

/// When reading a conversation's feed asks for one more page (#4642).
struct FeedPagingTests {
    private let t0 = Date(timeIntervalSince1970: 1_800_000_000)

    private func page(_ count: Int, newest: TimeInterval) -> [FeedItem] {
        (0..<count).map { i in
            FeedItem(id: "f\(i)", conversation: "c", at: t0.addingTimeInterval(newest - TimeInterval(count - 1 - i)), kind: .message)
        }
    }

    @Test func aFullPageGoesOnFromItsNewest() {
        #expect(FeedPaging.next(after: page(500, newest: 499), since: nil) == t0.addingTimeInterval(499))
        #expect(FeedPaging.next(after: page(500, newest: 999), since: t0.addingTimeInterval(499)) == t0.addingTimeInterval(999))
    }

    @Test func aShortPageIsTheLast() {
        #expect(FeedPaging.next(after: page(499, newest: 499), since: nil) == nil)
        #expect(FeedPaging.next(after: [], since: t0) == nil)
    }

    @Test func aPageThatDidNotMoveOnIsTheLast() {
        // Every item at the same instant as `since`: asking again would bring the same page back.
        let stuck = (0..<500).map { FeedItem(id: "s\($0)", conversation: "c", at: t0, kind: .message) }
        #expect(FeedPaging.next(after: stuck, since: t0) == nil)
    }
}
