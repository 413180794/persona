import Foundation

/// Text made safe to go inside a `<persona-context>` block: nothing left in it closes the
/// block, so what follows cannot sit outside it as the user's own words (an inbox item handed to the AI comes from
/// someone else, #3831). The same output as the hub's `prompts.ContextBody`, the web's `contextBody` and Android's.
nonisolated enum ContextText {
    /// What a reader may take as the end of a block: any case, spaces around the slash and before `>`.
    static let closingTag = try! NSRegularExpression(
        pattern: #"<(\s*/\s*"# + HubNaming.contextPattern + #"\s*>)"#, options: [.caseInsensitive])

    /// Closing tags are removed, and one that the removal itself puts together (`</persona-</persona-context>context>`)
    /// has its `<` written as `&lt;`, which joins nothing. Two passes, linear in the text.
    static func inBlock(_ text: String) -> String {
        let removed = replace(text, with: "")
        return replace(removed, with: "&lt;$1")
    }

    /// Whether `text` has anything a reader would take as the end of a block.
    static func closes(_ text: String) -> Bool {
        closingTag.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)) != nil
    }

    private static func replace(_ text: String, with template: String) -> String {
        closingTag.stringByReplacingMatches(in: text, range: NSRange(text.startIndex..., in: text), withTemplate: template)
    }
}
