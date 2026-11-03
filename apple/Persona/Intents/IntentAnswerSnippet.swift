import SwiftUI

/// The card under Siri's spoken answer: the question and the full answer text.
struct IntentAnswerSnippet: View {
    let title: String
    let answer: String

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            Text(title).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(2)
            Text(answer).font(Theme.Font.body)
        }
        .padding(Theme.Space.l)
    }
}
