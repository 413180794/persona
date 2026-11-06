import Foundation
import Testing
@testable import Jarvis

/// Supervised tasks (#3124, docs/unified-entry.md ("Supervision")) and their agents' reports (#5112): `supervision` decodes
/// leniently, the chip, the checklist and the lines say what the web says (web/src/lib/supervision.test.ts). Tests run
/// in Chinese (the scheme pins zh-Hans).
struct SupervisionTests {
    private func todo(_ json: String) throws -> TodoItem { try JSON.decoder.decode(TodoItem.self, from: Data(json.utf8)) }

    private func sup(_ s: TaskSupervision = .init(supervisor: "main", doneWhen: ["PR 合并到 master", "截图贴进 PR"]),
                     done: Bool = false, doneBy: String? = nil) -> TodoItem {
        TodoItem(id: "todo_1", title: "部署 hub", doneAt: done ? Date() : nil, doneBy: doneBy, supervision: s)
    }

    // MARK: Decoding

    @Test func decodesAFullSupervision() throws {
        let t = try todo(#"""
        {"id":"todo_1","title":"部署 hub","agents":[],"supervision":{"supervisor":"main","done_when":["PR 合并到 master","截图贴进 PR"],
         "expect_minutes":30,"state":"rework","round":1,"updated_at":"2026-11-05T08:00:00Z",
         "reviews":[{"at":"2026-11-05T08:30:00Z","verdict":"rework","round":1,
                     "checks":[{"criterion":"PR 合并到 master","met":true},{"criterion":"截图贴进 PR","met":false,"evidence":"只有浅色"}],
                     "note":"补深色截图"}],
         "progress":{"kind":"progress","step":3,"steps":5,"text":"在跑迁移","at":"2026-11-05T08:40:00Z"}}}
        """#)
        let s = try #require(t.supervision)
        #expect(s.supervisor == "main" && s.doneWhen.count == 2 && s.expectMinutes == 30 && s.state == "rework" && s.round == 1)
        #expect(s.reviews.count == 1 && s.reviews[0].checks.count == 2 && s.reviews[0].note == "补深色截图" && s.reviews[0].round == 1)
        #expect(s.progress == TaskSupervision.Progress(kind: "progress", step: 3, steps: 5, text: "在跑迁移", at: s.progress?.at))
        #expect(s.updatedAt != nil && s.progress?.at != nil)
    }

    @Test func aTodoWithoutSupervisionHasNone() throws {
        let t = try todo(#"{"id":"todo_2","title":"手动的","agents":[]}"#)
        #expect(t.supervision == nil && t.reviewBadge == nil && t.reviewState == nil)
        let null = try todo(#"{"id":"todo_3","title":"x","agents":[],"supervision":null}"#)
        #expect(null.supervision == nil)
    }

    /// Fields left out take their empty value: only the supervisor, or not even that.
    @Test func aPartialSupervisionDecodes() throws {
        let t = try todo(#"{"id":"todo_1","title":"x","agents":[],"supervision":{"supervisor":"main"}}"#)
        let s = try #require(t.supervision)
        #expect(s.doneWhen.isEmpty && s.reviews.isEmpty && s.state == "" && s.round == 0 && s.expectMinutes == nil && s.progress == nil)
        #expect(t.reviewBadge?.text == "督办中")
        let empty = try todo(#"{"id":"todo_1","title":"x","agents":[],"supervision":{}}"#)
        #expect(empty.supervision?.supervisor == "")
    }

    /// A malformed supervision is as if there were none; the todo itself still decodes.
    @Test func aMalformedSupervisionIsDropped() throws {
        for bad in [#""yes""#, "[1,2]", #"{"done_when":"PR 合并"}"#, #"{"state":5}"#] {
            let t = try todo(#"{"id":"todo_1","title":"部署 hub","agents":[],"supervision":"# + bad + "}")
            #expect(t.title == "部署 hub" && t.supervision == nil, "supervision \(bad)")
        }
    }

    /// A broken progress or reviews list is left out, the rest of the supervision stays.
    @Test func aMalformedProgressOrReviewIsLeftOut() throws {
        let t = try todo(#"""
        {"id":"todo_1","title":"x","agents":[],"supervision":{"supervisor":"main","done_when":["A"],
         "reviews":"none","progress":["not","an","object"],"expect_minutes":"soon","round":"two"}}
        """#)
        let s = try #require(t.supervision)
        #expect(s.doneWhen == ["A"] && s.reviews.isEmpty && s.progress == nil && s.expectMinutes == nil && s.round == 0)
        let partial = try todo(#"{"id":"todo_1","title":"x","agents":[],"supervision":{"progress":{"text":"在跑迁移","step":"3"}}}"#)
        #expect(partial.supervision?.progress == TaskSupervision.Progress(kind: "", text: "在跑迁移"))
    }

    @Test func aSupervisionSurvivesTheCache() throws {
        let t = sup(.init(supervisor: "main", doneWhen: ["A"], state: "pending_review",
                          progress: .init(kind: "question", text: "要不要停写？")))
        let back = try JSON.decoder.decode(TodoItem.self, from: JSON.encoder.encode(t))
        #expect(back.supervision == t.supervision)
    }

    // MARK: Feed

    @Test func feedKindsReviewAndReport() throws {
        let res = try JSON.decoder.decode(APIClient.FeedResponse.self, from: Data(#"""
        {"items":[
          {"id":"f1","conversation":"main","at":"2026-11-05T09:00:00Z","kind":"review","ref":"todo_1",
           "body":{"title":"部署 hub","verdict":"rework","round":2,"note":"补深色截图","checks":[]}},
          {"id":"f2","conversation":"main","at":"2026-11-05T09:01:00Z","kind":"report","ref":"todo_1",
           "body":{"kind":"question","text":"A 先灰度\n还是 B 全量？","evidence":["https://github.com/o/r/pull/902"," ",""],"step":2,"steps":5}},
          {"id":"f3","conversation":"main","at":"2026-11-05T09:02:00Z","kind":"routed","ref":"todo_1","body":{"text":"先修测试","interrupted":true}},
          {"id":"f4","conversation":"main","at":"2026-11-05T09:03:00Z","kind":"routed","ref":"todo_1","body":{"text":"先修测试"}},
          {"id":"f5","conversation":"main","at":"2026-11-05T09:04:00Z","kind":"something_newer","ref":"todo_1"}]}
        """#.utf8))
        #expect(res.items.map(\.kind) == [.review, .report, .routed, .routed, .unknown])
        let review = res.items[0]
        #expect(review.verdict == "rework" && review.round == 2 && review.note == "补深色截图" && review.title == "部署 hub")
        #expect(review.reportKind == nil)
        let report = res.items[1]
        #expect(report.reportKind == "question" && report.text == "A 先灰度\n还是 B 全量？" && report.step == 2 && report.steps == 5)
        #expect(report.evidence == ["https://github.com/o/r/pull/902"])
        #expect(res.items[2].interrupted && !res.items[3].interrupted && !review.interrupted)
    }

    // MARK: The chip

    @Test func badgeTextsAndTones() {
        func badge(_ state: String, round: Int = 0, done: Bool = false, checks: [TaskSupervision.Check] = []) -> ReviewBadge? {
            sup(.init(supervisor: "main", doneWhen: ["PR 合并到 master", "截图贴进 PR"], state: state, round: round,
                      reviews: checks.isEmpty ? [] : [.init(verdict: "rework", checks: checks)]), done: done).reviewBadge
        }
        #expect(badge("pending_review")?.text == "待验收 · 0/2")
        #expect(badge("pending_review")?.tone == .caution)
        let rework = badge("rework", round: 1, checks: [.init(criterion: "PR 合并到 master", met: true)])
        #expect(rework?.text == "返工第 1 轮 · 1/2" && rework?.tone == .caution)
        #expect(badge("needs_user")?.label == "等你决定" && badge("needs_user")?.tone == .danger)
        #expect(badge("accepted", done: true)?.label == "已验收" && badge("accepted", done: true)?.tone == .positive)
        #expect(badge("cancelled", done: true)?.label == "已取消" && badge("cancelled", done: true)?.tone == .neutral)
        #expect(badge("")?.label == "督办中" && badge("")?.tone == .neutral)
        #expect(badge("a_state_from_later")?.label == "督办中")
        // No criteria: no count.
        #expect(sup(.init(supervisor: "main", state: "needs_user")).reviewBadge?.text == "等你决定")
        #expect(TodoItem(id: "t", title: "x").reviewBadge == nil)
    }

    /// An accepted or cancelled task the user reopened goes on again.
    @Test func reopenedAcceptedCountsAsInProgress() {
        let accepted = TaskSupervision(supervisor: "main", doneWhen: ["A"], state: "accepted")
        #expect(sup(accepted, done: true, doneBy: "review").reviewState == .accepted)
        let reopened = sup(accepted, done: false)
        #expect(reopened.reviewState == .ongoing && reopened.reviewBadge?.label == "督办中")
        let cancelled = TaskSupervision(supervisor: "main", state: "cancelled")
        #expect(sup(cancelled, done: false).reviewState == .ongoing)
    }

    @Test func statusLabelOfAReviewedTask() {
        let accepted = sup(.init(supervisor: "main", state: "accepted"), done: true, doneBy: "review")
        #expect(accepted.statusLabel(.done) == "已验收" && accepted.reviewDone)
        let cancelled = sup(.init(supervisor: "main", state: "cancelled"), done: true, doneBy: "review")
        #expect(cancelled.statusLabel(.done) == "已取消")
        let auto = TodoItem(id: "t", title: "x", doneAt: Date(), doneBy: "auto", doneReason: "PR 已合并")
        #expect(auto.statusLabel(.done) == "自动完成" && auto.closedReason == "PR 已合并")
        let byHand = TodoItem(id: "t", title: "x", doneAt: Date(), doneBy: nil, doneReason: "x")
        #expect(byHand.statusLabel(.done) == "已完成" && byHand.closedReason == nil)
        var reviewed = cancelled
        reviewed.doneReason = "  不需要了 "
        #expect(reviewed.closedReason == "不需要了")
        // Open: the state's own label, the chip says the rest.
        #expect(sup(.init(supervisor: "main", state: "rework")).statusLabel(.working) == "进行中")
    }

    // MARK: The checklist

    /// Each criterion takes the verdict of the latest review that judged it; criteria judged but not given follow.
    @Test func checklistUsesTheLatestReviewPerCriterion() {
        let s = TaskSupervision(supervisor: "main", doneWhen: ["PR 合并到 master", "截图贴进 PR", "部署"], reviews: [
            .init(verdict: "rework", checks: [.init(criterion: "PR 合并到 master", met: false, evidence: "还没合"),
                                               .init(criterion: "截图贴进 PR", met: false, evidence: "没截图"),
                                               .init(criterion: "顺手改了文档", met: true)]),
            .init(verdict: "rework", round: 2, checks: [.init(criterion: "PR 合并到 master", met: true, evidence: " "),
                                                         .init(criterion: "顺手改了文档", met: false, evidence: "又删了")]),
        ])
        #expect(s.checklist == [
            ReviewCheckItem(criterion: "PR 合并到 master", mark: .met, evidence: nil),
            ReviewCheckItem(criterion: "截图贴进 PR", mark: .missing, evidence: "没截图"),
            ReviewCheckItem(criterion: "部署", mark: .open, evidence: nil),
            ReviewCheckItem(criterion: "顺手改了文档", mark: .missing, evidence: "又删了"),
        ])
        #expect(s.checkCount == "1/4")
        #expect(TaskSupervision(supervisor: "main").checklist.isEmpty && TaskSupervision(supervisor: "main").checkCount == "")
        #expect(s.latestReview?.round == 2)
    }

    // MARK: Lines

    @Test func progressAndReportLines() {
        func line(_ p: TaskSupervision.Progress?) -> String? { TaskSupervision(supervisor: "main", progress: p).progressLine }
        #expect(line(nil) == nil)
        #expect(line(.init(kind: "progress", text: "")) == nil)
        #expect(line(.init(kind: "progress", step: 3, steps: 5, text: "在跑迁移")) == "第 3/5 步 · 在跑迁移")
        #expect(line(.init(kind: "progress", step: 2, text: "在跑迁移")) == "第 2 步 · 在跑迁移")
        #expect(line(.init(kind: "progress", step: 0, steps: 5, text: "在跑迁移")) == "在跑迁移")
        #expect(line(.init(kind: "progress", text: "在跑迁移")) == "在跑迁移")
        #expect(line(.init(kind: "question", step: 2, steps: 5, text: "要不要停写？")) == "问 Persona：要不要停写？")
        #expect(ReviewText.reportLine(kind: "blocked", text: "等 CI") == "卡住了：等 CI")
        #expect(ReviewText.reportLine(kind: "done", text: "PR 已合并") == "说做完了：PR 已合并")
        #expect(ReviewText.reportWantsAttention("question") && ReviewText.reportWantsAttention("blocked") && !ReviewText.reportWantsAttention("done"))
    }

    @Test func reviewVerdictLabels() {
        #expect(ReviewText.verdict("accepted", round: nil) == "已验收")
        #expect(ReviewText.verdict("rework", round: 2) == "返工第 2 轮")
        #expect(ReviewText.verdict("rework", round: nil) == "返工第 1 轮")
        #expect(ReviewText.verdict("needs_user", round: nil) == "等你决定")
        #expect(ReviewText.verdict("cancelled", round: nil) == "已取消")
        #expect(ReviewText.verdict("", round: nil) == "验收")
    }
}
