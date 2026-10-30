import Testing
@testable import BeautifulUI

@Suite struct PromptTokenTests {
    @Test func findsTheTrailingMentionOrCommand() {
        #expect(PromptToken.parse("@") == PromptToken(kind: .mention, query: "", start: 0))
        #expect(PromptToken.parse("ask @Sco") == PromptToken(kind: .mention, query: "sco", start: 4))
        #expect(PromptToken.parse("/churn-p") == PromptToken(kind: .command, query: "churn-p", start: 0))
    }

    @Test func ignoresTriggersInsideWordsOrFinishedTokens() {
        #expect(PromptToken.parse("mail@home") == nil)
        #expect(PromptToken.parse("@Slack ") == nil)
        #expect(PromptToken.parse("a/b") == nil)
        #expect(PromptToken.parse("") == nil)
    }

    @MainActor @Test func modelSendsTextOrAttachments() {
        let model = PromptBarModel(selectedModelID: "m")
        #expect(!model.canSend)
        model.text = "  \n"
        #expect(!model.canSend)
        model.attachments = ["a.png"]
        #expect(model.canSend)
    }

    @MainActor @Test func dismissingHidesTheMenuToken() {
        let model = PromptBarModel(text: "@", selectedModelID: "m")
        #expect(model.token != nil)
        model.isMenuDismissed = true
        #expect(model.token == nil)
    }
}
