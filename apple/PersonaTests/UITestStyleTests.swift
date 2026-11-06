import Foundation
import Testing

/// Every UI test launches in a style it names (`UITestStyle` in PersonaUITests): the app keeps the style the last run
/// left, and the BeautifulUI style draws its own components, so a test that leaves it out finds another test's looks.
/// A launch is `launchPinned()` (not `launch()` or `launchPersona()`), or the file passes `-ui.style` itself (a test about
/// one style). Another app (`XCUIApplication(bundleIdentifier:)`, e.g. LiveActivityShots' peer) has no style to pin.
/// Reads the sources.
struct UITestStyleTests {
    @Test func everyLaunchPinsAStyle() throws {
        let dir = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().appending(path: "PersonaUITests")
        let files = try FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil).filter { $0.pathExtension == "swift" }
        #expect(files.count > 20, "the UI tests are not at \(dir.path)")
        var found: [String] = []
        for f in files {
            let text = try String(contentsOf: f, encoding: .utf8)
            guard !text.contains("\"-ui.style\"") else { continue }
            let others = try NSRegularExpression(pattern: #"let (\w+) = XCUIApplication\(bundleIdentifier:"#)
                .matches(in: text, range: NSRange(text.startIndex..., in: text))
                .compactMap { Range($0.range(at: 1), in: text).map { String(text[$0]) + ".launch()" } }
            for (i, line) in text.components(separatedBy: "\n").enumerated() where line.contains(".launch()") || line.contains(".launchPersona()") || line.contains(".launchPersona()") {
                if others.contains(where: { line.contains($0) }) { continue }
                found.append("\(f.lastPathComponent):\(i + 1): \(line.trimmingCharacters(in: .whitespaces))")
            }
        }
        #expect(found.isEmpty, "launch with launchPinned() (or pass -ui.style):\n\(found.joined(separator: "\n"))")
    }
}
