import Testing
@testable import BeautifulUI

@Suite struct CodeHighlighterTests {
    @Test func coloursKeywordsCallsAndLiterals() {
        let tokens = CodeHighlighter.tokens("  const flavor = await getFlavor(\"pistachio\");")
        let kinds = tokens.filter { $0.kind != .plain }.map { "\($0.text):\($0.kind)" }
        #expect(kinds == ["const:keyword", "await:keyword", "getFlavor:function", "\"pistachio\":literal"])
    }

    @Test func roundTripsTheLine() {
        let line = "  await freezer.store(base, { temp: \"-16C\" }); // 12.5"
        #expect(CodeHighlighter.tokens(line).map(\.text).joined() == line)
    }

    @Test func numbersAreLiteralsButNotInsideIdentifiers() {
        let kinds = CodeHighlighter.tokens("x2 = 42").filter { $0.kind == .literal }.map(\.text)
        #expect(kinds == ["42"])
    }
}
