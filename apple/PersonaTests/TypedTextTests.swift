import SwiftUI
import Testing
import UIKit
@testable import Persona

/// A send takes what the field shows, the keyboard's composition confirmed first (#2203: with the WeChat keyboard the
/// main conversation sent about half of what was typed).
@MainActor struct TypedTextTests {
    /// A draft held outside the view, as the composer's (`model.drafts`); counts its writes.
    final class Draft {
        var text: String
        var writes = 0
        init(_ text: String) { self.text = text }
        var binding: Binding<String> { Binding(get: { self.text }, set: { self.text = $0; self.writes += 1 }) }
    }

    private func field(_ committed: String) -> UITextView {
        let view = UITextView(frame: CGRect(x: 0, y: 0, width: 300, height: 44))
        view.insertText(committed)
        return view
    }

    @Test func theCompositionIsConfirmedAndSentWithTheRest() {
        // The first words confirmed, the rest still composing (marked) when send is tapped; the binding has the first.
        let view = field("今天天气")
        view.setMarkedText("很好我们去公园", selectedRange: NSRange(location: 7, length: 0))
        #expect(view.markedTextRange != nil)
        let draft = Draft("今天天气")
        TypedText.commit(view, into: draft.binding)
        #expect(draft.text == "今天天气很好我们去公园")
        // Nothing is left for the keyboard to confirm into the box once the send has emptied it.
        #expect(view.markedTextRange == nil)
        #expect(view.text == "今天天气很好我们去公园")
    }

    @Test func anEditTheBindingMissedIsSent() {
        // A keyboard extension's edit the field's bridge did not pass on.
        let view = field("今天天气很好我们去公园")
        let draft = Draft("今天天气很好")
        TypedText.commit(view, into: draft.binding)
        #expect(draft.text == "今天天气很好我们去公园")
    }

    @Test func aDraftThatMatchesIsLeftAlone() {
        let view = field("今天天气很好我们去公园")
        let draft = Draft("今天天气很好我们去公园")
        TypedText.commit(view, into: draft.binding)
        #expect(draft.writes == 0)
        TypedText.commit(nil, into: draft.binding)
        #expect(draft.writes == 0 && draft.text == "今天天气很好我们去公园")
    }

    @Test func theFocusedFieldIsTheOneRead() throws {
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        let view = field("今天天气")
        window.addSubview(view)
        window.makeKeyAndVisible()
        defer { view.resignFirstResponder(); window.isHidden = true }
        try #require(view.becomeFirstResponder())
        view.setMarkedText("很好", selectedRange: NSRange(location: 2, length: 0))
        let draft = Draft("今天天气")
        TypedText.commitFocused(into: draft.binding)
        #expect(draft.text == "今天天气很好")
        #expect(view.markedTextRange == nil)
    }
}
