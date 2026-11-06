import Foundation
import PersonaKit
import Testing
@testable import Persona

// What of the paths in messages is the app's: the bubble's links and the chips' list. PathRefs itself and its tests
// are in PersonaKit (apple/Packages/PersonaKit, #2095).

/// The user's bubble: plain text with the paths there as links.
@MainActor
struct PathTextTests {
    @Test func linksThePickedPathsAndKeepsTheRest() {
        let t = "看一下 server/handlers.go:120，还有 /tmp/x/y.md"
        let refs = PathRefs.pick(PathRefs.find(in: t)) { $0 == "server/handlers.go" }
        let a = PathText.attributed(t, refs, link: .blue)
        #expect(String(a.characters) == t)
        let links = a.runs.compactMap { run in run.link.map { (String(a[run.range].characters), $0) } }
        #expect(links.count == 1)
        #expect(links.first?.0 == "server/handlers.go:120")
        #expect(links.first.flatMap { PathLinkURL.parse($0.1) }.map { "\($0.path):\($0.line ?? 0)" } == "server/handlers.go:120")
    }
}

/// The chips' list keeps a span that is one path whole: not its folder or the words before a space as well.
struct FilePathsWholeSpanTests {
    @Test func asksWithoutTheLines() {
        #expect(FilePaths.find(in: "见 server/handlers.go:120 和 `README.md`") == ["server/handlers.go"])
    }

    @Test func aWholeSpanIsOneChip() {
        #expect(FilePaths.find(in: "`/tmp/a.md 和 /tmp/b.md`") == ["/tmp/a.md 和 /tmp/b.md"])
        #expect(FilePaths.find(in: "先看 /var/log/app.log:3，再看 `src/a.ts:12`") == ["/var/log/app.log", "src/a.ts"])
    }
}
