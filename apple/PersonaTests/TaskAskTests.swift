import Foundation
import Testing
@testable import Persona

/// A task card waiting for the user says what its agent asks, not only "Waiting for your answer" (#3998).
struct TaskAskTests {
    private func interaction(
        _ id: String, kind: InteractionKind, toolName: String? = nil, summary: String? = nil, questions: [Question]? = nil
    ) -> Interaction {
        Interaction(
            id: id, machineID: "m1", sessionID: "s1", sessionTitle: nil, cwd: nil,
            kind: kind, createdAt: nil, expiresAt: nil, toolName: toolName, toolUseID: nil, questions: questions,
            plan: nil, toolInput: nil, summary: summary, screen: nil, dialog: nil
        )
    }

    @Test func permissionCardSaysTheCommand() {
        let ix = interaction("p", kind: .permission, toolName: "Bash", summary: "npm test")
        #expect(TaskAsk.waitingLine([ix]) == "npm test")
    }

    @Test func questionCardSaysTheQuestionOnOneLine() {
        let q = Question(question: "用哪个测试框架？\n  Vitest 还是 Jest", header: nil, multiSelect: false, options: [])
        #expect(TaskAsk.line([interaction("q", kind: .question, questions: [q])]) == "用哪个测试框架？ Vitest 还是 Jest")
    }

    @Test func firstCardThatSaysAnythingWins() {
        let blank = interaction("blank", kind: .permission, summary: "  ")
        let later = interaction("later", kind: .permission, toolName: "Bash", summary: "git push")
        #expect(TaskAsk.line([blank, later]) == "git push")
    }

    @Test func noCardKeepsTheGenericWord() {
        #expect(TaskAsk.line([]) == nil)
        #expect(TaskAsk.waitingLine([]) == "有事等你回答")
    }
}
