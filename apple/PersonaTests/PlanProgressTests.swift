import Foundation
import Testing
@testable import Jarvis

/// A plan's (TodoWrite's) progress in the one sentence the web says it too (#5901).
@MainActor
struct PlanProgressTests {
    private func plan(_ status: [String]) -> [JSONValue] {
        status.enumerated().map { n, s in .object(["content": .string("step \(n)"), "status": .string(s)]) }
    }

    @Test func theCurrentStepIsTheFirstInProgressElseTheFirstToDo() {
        #expect(PlanProgress.text(plan(["completed", "completed", "in_progress", "pending"])) == String(localized: "Step \(3) of \(4) [Plan Progress]"))
        // Nothing in progress (it waits for an answer): the first still to do.
        #expect(PlanProgress.text(plan(["completed", "pending", "pending", "pending"])) == String(localized: "Step \(2) of \(4) [Plan Progress]"))
        // One in progress after one left to do: the one in progress.
        #expect(PlanProgress.text(plan(["pending", "in_progress"])) == String(localized: "Step \(2) of \(2) [Plan Progress]"))
    }

    @Test func allDoneSaysSo() {
        #expect(PlanProgress.text(plan(["completed", "completed", "completed", "completed"])) == String(localized: "All \(4) steps done [Plan Progress]"))
        #expect(PlanProgress.text([]) == "")
    }
}
