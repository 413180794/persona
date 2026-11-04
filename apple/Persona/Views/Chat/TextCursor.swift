import SwiftUI

extension TextSelection {
    /// A cursor `offset` UTF-16 units into a TextField's text. The field reads an insertion point's index as a UTF-16
    /// offset into its own text, whatever string the index was made on: one made on Swift's (UTF-8) text lands further
    /// on when Chinese comes before it — at the end, after picking a name to @ in the middle (#1627).
    static func cursor(utf16 offset: Int) -> TextSelection {
        TextSelection(insertionPoint: encoded(String.Index.self, offset))
    }
}

/// String.Index(encodedOffset:) is deprecated for indexing Swift strings, which is not what it is used for here.
private protocol EncodedOffsetIndex { init(encodedOffset: Int) }
extension String.Index: EncodedOffsetIndex {}
private func encoded<I: EncodedOffsetIndex>(_: I.Type, _ offset: Int) -> I { I(encodedOffset: offset) }
