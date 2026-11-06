import Foundation
import Testing
@testable import Persona

/// The phone's writes ask one at a time and each answer counts once, for the one it names (#4456).
@MainActor
struct PhoneConfirmationsTests {
    /// Starts `ask` and waits until it is on screen or queued.
    private func asking(_ q: PhoneConfirmations, _ summary: String, answers: AnswerBox) {
        Task { @MainActor in answers.got[summary] = await q.ask(summary) }
    }

    private func settle() async {
        for _ in 0..<20 { await Task.yield() }
    }

    @Test func aSecondWriteWaitsBehindTheFirst() async {
        let q = PhoneConfirmations(), answers = AnswerBox()
        asking(q, "Create event A", answers: answers)
        await settle()
        asking(q, "Create reminder B", answers: answers)
        await settle()
        let first = try! #require(q.current)
        #expect(first.summary == "Create event A" && q.queued == 1)

        q.answer(first.id, true)
        await settle()
        #expect(answers.got["Create event A"] == true)
        let second = try! #require(q.current)
        #expect(second.summary == "Create reminder B" && q.queued == 0)
        #expect(answers.got["Create reminder B"] == nil) // still waiting for the user

        q.answer(second.id, false)
        await settle()
        #expect(answers.got["Create reminder B"] == false && q.current == nil)
    }

    /// Allow on the first dialog, then its dismissal arrives late, naming the dialog it showed: the second one, now on
    /// screen, is not denied by it.
    @Test func aLateDismissalOfTheFirstDoesNotDenyTheNext() async {
        let q = PhoneConfirmations(), answers = AnswerBox()
        asking(q, "Create event A", answers: answers)
        await settle()
        asking(q, "Create reminder B", answers: answers)
        await settle()
        let first = try! #require(q.current)
        q.answer(first.id, true) // the Allow button
        await settle()
        let second = try! #require(q.current)
        q.answer(first.id, false) // the alert's isPresented = false for the dialog it showed, coming after
        await settle()
        #expect(answers.got["Create event A"] == true)
        #expect(q.current?.id == second.id && answers.got["Create reminder B"] == nil)
    }

    @Test func anAnswerCountsOnce() async {
        let q = PhoneConfirmations(), answers = AnswerBox()
        asking(q, "Write health", answers: answers)
        await settle()
        let c = try! #require(q.current)
        q.answer(c.id, true)
        q.answer(c.id, false) // a second answer to the same one (a resumed continuation would crash here)
        await settle()
        #expect(answers.got["Write health"] == true && q.current == nil)
    }

    @Test func dismissingTheOneOnScreenDeniesIt() async {
        let q = PhoneConfirmations(), answers = AnswerBox()
        asking(q, "Complete reminder", answers: answers)
        await settle()
        q.answer(try! #require(q.current).id, false) // swiped away / dismissed with no button
        await settle()
        #expect(answers.got["Complete reminder"] == false)
    }

    @Test func signingOutDeniesEveryOneLeft() async {
        let q = PhoneConfirmations(), answers = AnswerBox()
        asking(q, "A", answers: answers)
        asking(q, "B", answers: answers)
        await settle()
        q.denyAll()
        await settle()
        #expect(answers.got == ["A": false, "B": false] && q.current == nil && q.queued == 0)
    }
}

@MainActor final class AnswerBox {
    var got: [String: Bool] = [:]
}
