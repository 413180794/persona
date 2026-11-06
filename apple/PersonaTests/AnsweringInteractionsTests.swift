import Testing
@testable import Persona

/// A failed answer puts its card back only when the hub has not ended it meanwhile (#3922).
struct AnsweringInteractionsTests {
    @Test func endedByTheHubBeforeTheErrorStaysAway() {
        // A screen card: the hub broadcasts interaction_resolved, then the answer comes back 409 prompt_changed.
        var a = AnsweringInteractions()
        a.begin("ix1")
        a.hubEnded("ix1")
        #expect(!a.putsBack("ix1"))
    }

    @Test func stillPendingOnTheHubGoesBack() {
        // A Codex card the node could not answer: no interaction_resolved, the hub keeps it open.
        var a = AnsweringInteractions()
        a.begin("ix1")
        a.hubEnded("ix2")
        #expect(a.putsBack("ix1"))
    }

    @Test func otherGroupsAreTheirOwnCards() {
        var a = AnsweringInteractions()
        a.begin("work|ix1")
        a.hubEnded("ix1")
        #expect(a.putsBack("work|ix1"))
    }

    @Test func anEndBeforeTheAnswerStartedDoesNotCount() {
        var a = AnsweringInteractions()
        a.hubEnded("ix1") // not being answered: nothing to remember
        a.begin("ix1")
        #expect(a.putsBack("ix1"))
        a.hubEnded("ix1")
        a.finish("ix1")
        a.begin("ix1") // answered again later: a fresh start
        #expect(a.putsBack("ix1"))
    }
}
