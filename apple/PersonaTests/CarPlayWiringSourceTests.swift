import Foundation
import Testing

/// The CarPlay scene delegate and the Siri intent use the rules tested in CarPlayVoiceScreenTests (CarPlayAutoStart,
/// CarPlayTalk, opening states). The delegate needs a real car connection, so these read the sources next to this file,
/// like `HoldSourceTests`: each bug came back by calling the session or the template straight from a lifecycle method.
struct CarPlayWiringSourceTests {
    private static let ios = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()

    /// The file's lines with `//` comments taken out.
    private static func code(_ path: String) throws -> [String] {
        try String(contentsOf: ios.appending(path: path), encoding: .utf8).components(separatedBy: "\n")
            .map { l in l.range(of: "//").map { String(l[..<$0.lowerBound]) } ?? l }
    }

    /// The body of the first `{ … }` starting on a line that contains `start`.
    private static func body(_ lines: [String], _ start: String) -> String? {
        guard let first = lines.firstIndex(where: { $0.contains(start) }) else { return nil }
        var depth = 0, opened = false, out = ""
        for line in lines[first...] {
            for c in line {
                if c == "{" { depth += 1; opened = true }
                if c == "}" { depth -= 1 }
            }
            out += line + "\n"
            if opened, depth == 0 { return out }
        }
        return nil
    }

    private static let delegate = "Persona/CarPlay/CarPlaySceneDelegate.swift"

    /// A banner, Siri or a call makes the scene active again: only an armed CarPlayAutoStart may start it.
    @Test func becomingActiveStartsOnlyWhenArmed() throws {
        let lines = try Self.code(Self.delegate)
        let active = try #require(Self.body(lines, "func sceneDidBecomeActive("))
        #expect(active.contains("autoStart.takeOnBecomingActive()"))
        let foreground = try #require(Self.body(lines, "func sceneWillEnterForeground("))
        #expect(foreground.contains("autoStart.arm()"))
        let connect = try #require(Self.body(lines, "didConnect interfaceController"))
        #expect(connect.contains("autoStart.arm()"))
    }

    /// Siri may only open the microphone with Persona on the car screen, so the foreground is tracked.
    @Test func siriChecksTheForeground() throws {
        let intent = try Self.code("Persona/Intents/TalkToPersonaIntent.swift").joined(separator: "\n")
        #expect(!intent.contains("CarPlayVoicePresence.isConnected"))
        #expect(intent.contains("CarPlayVoicePresence.talk"))
        let lines = try Self.code(Self.delegate)
        let background = try #require(Self.body(lines, "func sceneDidEnterBackground("))
        #expect(background.contains("CarPlayVoicePresence.isForeground = false"))
        let foreground = try #require(Self.body(lines, "func sceneWillEnterForeground("))
        #expect(foreground.contains("CarPlayVoicePresence.isForeground = true"))
    }

    /// CarPlay ignores state switches before the template is on screen: `show` never switches directly.
    @Test func statesSwitchOnlyOnceTheTemplateIsPresented() throws {
        let lines = try Self.code(Self.delegate)
        let show = try #require(Self.body(lines, "func showEndedOrCall("))
        #expect(!show.contains("activateVoiceControlState"))
        #expect(!show.contains("setRootTemplate"))
        let activate = try #require(Self.body(lines, "func activateCurrentState("))
        #expect(activate.contains("guard presented"))
        let present = try #require(Self.body(lines, "func present("))
        #expect(present.contains("presented = true"))
        let all = lines.joined(separator: "\n")
        #expect(all.components(separatedBy: "activateVoiceControlState").count == 2, "only activateCurrentState switches")
    }

    /// The car call goes through the shared engine, not the old DuplexVoiceSession.
    @Test func theCarUsesVoiceCalls() throws {
        let delegate = try Self.code(Self.delegate).joined(separator: "\n")
        #expect(delegate.contains("CarPlayVoiceSession"))
        #expect(delegate.contains("VoiceCalls"))
        #expect(!delegate.contains("DuplexVoiceSession"))
        let intent = try Self.code("Persona/Intents/TalkToPersonaIntent.swift").joined(separator: "\n")
        #expect(intent.contains("CarPlayVoiceSession"))
        #expect(!intent.contains("DuplexVoiceSession"))
    }
}
