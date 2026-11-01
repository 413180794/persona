import Testing
@testable import BeautifulUI

@Suite struct HostRowsTests {
    @Test func questionsFromPlainOptionsHaveBareChoices() {
        let q = ApprovalQuestion("Which?", options: ["A", "B"])
        #expect(q.choices == [.init("A"), .init("B")])
        #expect(q.options == ["A", "B"])
        #expect(q.allowsCustom)
        #expect(q.eyebrow == nil)
    }

    @Test func choicesKeepTheirLineAndTag() {
        let q = ApprovalQuestion("Which?", choices: [.init("A", detail: "first", tag: "Recommended"), .init("B")], allowsCustom: false)
        #expect(q.options == ["A", "B"])
        #expect(q.choices[0].detail == "first")
        #expect(q.choices[0].tag == "Recommended")
        #expect(!q.allowsCustom)
    }

    @Test func approvalOptionsDefaultToTheOriginal() {
        let o = ApprovalCardOptions()
        #expect(o.drawsSurface && o.autoAdvance && o.showsDismiss && o.allowsStartOver && !o.isSending)
        #expect(o.maxWidth == 320)
    }

    @Test func tasksWithoutASubtitleStayOneLine() {
        let t = TaskItem(id: "a", title: "Build", amount: "7 SKUs", status: .running(step: 2))
        #expect(t.subtitle == nil)
        #expect(TaskItem(id: "b", title: "Ship", subtitle: "last step", status: .completed).subtitle == "last step")
    }
}
