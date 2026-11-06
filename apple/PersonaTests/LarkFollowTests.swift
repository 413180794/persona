import Foundation
import Testing
@testable import Persona

/// Where a Feishu chat's list goes as it changes (LarkFollow, #672). The numbers are the chat measured on an
/// iPhone 17 Pro opened from the inbox: a 202 pt top bar, the box 89 pt over the bottom, the keyboard adding 311.
struct LarkFollowTests {
    private func g(offset: CGFloat, content: CGFloat = 431, container: CGFloat = 583, top: CGFloat = 202,
                   bottom: CGFloat = 89) -> LarkFollow.Geometry {
        LarkFollow.Geometry(offset: offset, content: content, container: container, top: top, bottom: bottom)
    }

    @Test func theEndIsTheContentLessWhatShowsBetweenTheInsets() {
        #expect(LarkFollow.endOffset(g(offset: 0, container: 272)) == CGFloat(431 - 272 - 202))
        // All of it fits: the end is the top.
        #expect(LarkFollow.endOffset(g(offset: 0)) == -202)
        #expect(LarkFollow.atEnd(g(offset: -202)))
        #expect(LarkFollow.atEnd(g(offset: -43, container: 272, bottom: 400)))
        #expect(!LarkFollow.atEnd(g(offset: -202, container: 272, bottom: 400)))
        #expect(LarkFollow.atEnd(g(offset: -43.4, content: 431, container: 272.4, bottom: 400)))
    }

    /// The bug: the keyboard came up over a chat that showed its last message, and nothing moved the list.
    @Test func theKeyboardOverAListShowingItsEndKeepsTheEndInView() {
        let before = g(offset: -202)
        let keyboard = g(offset: -202, container: 272, bottom: 400)
        #expect(LarkFollow.goesToEnd(old: before, new: keyboard, following: false))
        #expect(LarkFollow.goesToEnd(old: before, new: keyboard, following: true))
    }

    @Test func theQuoteBarComingAndGoingKeepsTheEndInView() {
        let typing = g(offset: -43, container: 272, bottom: 400)
        let quoted = g(offset: -43, container: 220, bottom: 452)
        #expect(LarkFollow.goesToEnd(old: typing, new: quoted, following: false))
        // It goes: the end comes back down instead of leaving a gap over the box.
        let back = g(offset: 9, container: 272, bottom: 400)
        #expect(LarkFollow.goesToEnd(old: quoted, new: back, following: true))
    }

    /// A sent row (or one arriving) grows the content: following, the list goes to the new end.
    @Test func followingGoesToTheNewEnd() {
        let typing = g(offset: -43, container: 272, bottom: 400)
        let sent = g(offset: -43, content: 526, container: 272, bottom: 400)
        #expect(LarkFollow.goesToEnd(old: typing, new: sent, following: true))
        // Not following (scrolled up reading): new rows do not pull the list down.
        #expect(!LarkFollow.goesToEnd(old: typing, new: sent, following: false))
    }

    @Test func aListReadFurtherUpStaysWhereItIs() {
        let reading = g(offset: -150)
        let keyboard = g(offset: -150, container: 272, bottom: 400)
        #expect(!LarkFollow.goesToEnd(old: reading, new: keyboard, following: false))
        // At the end already: nothing to do.
        #expect(!LarkFollow.goesToEnd(old: nil, new: g(offset: -43, container: 272, bottom: 400), following: true))
        // Loading changes the content, not the room: no pin for a list that is not following.
        #expect(!LarkFollow.goesToEnd(old: g(offset: -202, content: 24), new: g(offset: -202, content: 900), following: false))
    }
}
