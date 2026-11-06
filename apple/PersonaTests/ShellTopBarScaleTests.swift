import Foundation
import Testing

/// The shell's top bars grow with the text only up to `Theme.Shell.topBarLargestTypeSize` (#340): Assistant's, Agents' and
/// the one that stands in for both while the strip slides (`ShellMovingBar`). One left on `.shellScaled()` is larger
/// than the others at the accessibility sizes, and the bar visibly jumps as the strip starts or stops.
struct ShellTopBarScaleTests {
    private static let apple = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()

    @Test(arguments: [
        ("Persona/Views/Assistant/AssistantTopBar.swift", "struct AssistantTopBar: View"),
        ("Persona/Views/Chats/ChatsView.swift", "struct AgentsTopBar: View"),
        ("Persona/Views/Shell/ShellView.swift", "private struct ShellMovingBar: View"),
    ])
    func everyTopBarStopsGrowingAtTheSameSize(file: String, bar: String) throws {
        let source = try String(contentsOf: Self.apple.appending(path: file), encoding: .utf8)
        let start = try #require(source.range(of: bar), "\(bar) not in \(file)")
        // The bar's declaration: up to the next top-level declaration.
        let rest = source[start.upperBound...]
        let end = rest.range(of: "\n}\n")?.upperBound ?? rest.endIndex
        let body = rest[..<end]
        #expect(body.contains(".shellTopBarScaled()"), "\(bar) does not stop growing at the top bars' size")
        #expect(!body.contains(".shellScaled()"), "\(bar) grows past the other top bars")
    }
}
