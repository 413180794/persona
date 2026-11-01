import Foundation
import Testing

/// Every UI test launches in a style it names (`UITestStyle` in JarvisUITests): the app keeps the style the last run
/// left, and the BeautifulUI style draws its own components, so a test that leaves it out finds another test's looks.
/// A launch is `launchPinned()` (not `launch()` or `launchJarvis()`), or the file passes `-ui.style` itself (a test about
/// one style). Reads the sources.
struct UITestStyleTests {
    @Test func everyLaunchPinsAStyle() throws {
        let dir = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().appending(path: "JarvisUITests")
        let files = try FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil).filter { $0.pathExtension == "swift" }
        #expect(files.count > 20, "the UI tests are not at \(dir.path)")
        var found: [String] = []
        for f in files {
            let text = try String(contentsOf: f, encoding: .utf8)
            guard !text.contains("\"-ui.style\"") else { continue }
            for (i, line) in text.components(separatedBy: "\n").enumerated() where line.contains(".launch()") || line.contains(".launchJarvis()") {
                found.append("\(f.lastPathComponent):\(i + 1): \(line.trimmingCharacters(in: .whitespaces))")
            }
        }
        #expect(found.isEmpty, "launch with launchPinned() (or pass -ui.style):\n\(found.joined(separator: "\n"))")
    }
}
