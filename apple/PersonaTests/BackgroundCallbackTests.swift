import Foundation
import Testing

/// Closures handed to system APIs that run them on their own queue. The app's default isolation is the main actor, so
/// a plain closure written in app code is main-actor code, and Swift checks that at run time: when Photos / Speech /
/// EventKit / WatchConnectivity call it on their queue, the app dies (保存图片 crashed, 2026-10-31). The SDK does not
/// mark these blocks `Sendable`, so the compiler says nothing. Each closure passed to them is written `{ @Sendable … in`
/// (a Sendable closure is not isolated to the main actor). Reads the sources next to this file, like
/// `IconVocabularyTests`.
struct BackgroundCallbackTests {
    private static let ios = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()

    /// Calls whose block runs off the main thread and is not `Sendable` in the SDK (checked in the headers).
    private static let api = try! NSRegularExpression(pattern:
        #"\.(performChanges|requestImage\w*|fetchReminders|sendMessage|getNotificationSettings|getDeliveredNotifications|getPendingNotificationRequests|evaluatePolicy|loadObject|loadDataRepresentation|loadFileRepresentation|prepareForDisplay|prepareThumbnail)\b|SFSpeechRecognizer\.requestAuthorization\b|\b(replyHandler|errorHandler): \{"#)

    @Test func closuresRunOnASystemQueueAreSendable() throws {
        var found: [String] = []
        for dir in ["Persona", "PersonaWatch", "Shared"] {
            let files = (FileManager.default.enumerator(at: Self.ios.appending(path: dir), includingPropertiesForKeys: nil)?
                .compactMap { $0 as? URL } ?? []).filter { $0.pathExtension == "swift" }
            for file in files {
                let lines = try String(contentsOf: file, encoding: .utf8).components(separatedBy: "\n")
                for (n, line) in lines.enumerated() where !line.trimmingCharacters(in: .whitespaces).hasPrefix("//") {
                    guard let m = Self.api.firstMatch(in: line, range: NSRange(line.startIndex..., in: line)),
                          let r = Range(m.range, in: line) else { continue }
                    // A closure literal after the call (a function passed by name is that function's business).
                    let rest = line[r.lowerBound...]
                    guard let brace = rest.firstIndex(of: "{") else { continue }
                    if !rest[brace...].dropFirst().trimmingCharacters(in: .whitespaces).hasPrefix("@Sendable") {
                        found.append("\(file.lastPathComponent):\(n + 1): \(line.trimmingCharacters(in: .whitespaces))")
                    }
                }
            }
        }
        #expect(found.isEmpty, "write these closures `{ @Sendable … in`:\n\(found.joined(separator: "\n"))")
    }
}
