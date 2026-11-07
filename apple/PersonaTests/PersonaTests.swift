import Foundation
import ImageIO
import SwiftUI
import Testing
import UIKit
import UniformTypeIdentifiers
@testable import Persona

/// Personas (docs/apple/persona.md): persona.json as the hub sends it, the colors held to the app's floors and laid over a
/// style, and the phone's copy of a package (downloaded whole, kept per rev, the old rev removed).
@MainActor
struct PersonaTests {
    private nonisolated static let packID = "nwverse"

    static let manifest = """
    {"format":1,"id":"nwverse","name":"奶蛙","subtitle":"圆滚滚",
     "avatar":{"sizes":{"64":"avatar/64.png","128":"avatar/128.png","512":"avatar/512.png"},"safe":1},
     "dot":{"size":144,"states":{"idle":{"file":"dot/idle.png","fps":8},"needs_you":{"file":"dot/needs_you.png","fps":6}}},
     "colors":{"light":{"primary":"#F2C230","accent":"#F2C230","bubble":"#F2C230","bubble_text":"#FFFFFF"},
               "dark":{"primary":"#F2C230","accent":"#F2C230"}}}
    """

    static func decode(_ json: String = manifest) throws -> PersonaManifest {
        try JSONDecoder().decode(PersonaManifest.self, from: Data(json.utf8))
    }

    // MARK: persona.json

    @Test func manifestAsTheHubSendsIt() throws {
        let m = try Self.decode()
        #expect(m.problem == nil)
        #expect(m.files == ["avatar/128.png", "avatar/512.png", "avatar/64.png", "dot/idle.png", "dot/needs_you.png"])
        // The smallest avatar with at least the pixels asked for, the largest when none has.
        #expect(m.avatarFile(points: 42, scale: 3) == "avatar/128.png")
        #expect(m.avatarFile(points: 20, scale: 3) == "avatar/64.png")
        #expect(m.avatarFile(points: 300, scale: 3) == "avatar/512.png")
        // A state the package lacks shows idle's frames; needs_you is read by its raw name.
        #expect(m.dotState(.working)?.file == "dot/idle.png")
        #expect(m.dotState(.needsYou)?.file == "dot/needs_you.png")
    }

    /// A tier of a tiered look (Milk Frog) as the hub sends it: a plain manifest, its files under the tier's directory, and
    /// the few words its card shows in place of the colors.
    @Test func tierManifestWithAbout() throws {
        let m = try Self.decode(Self.manifest
            .replacingOccurrences(of: #""subtitle":"圆滚滚","#, with: #""subtitle":"蝌蚪","about":"NWVERSE 等级","#)
            .replacingOccurrences(of: #""avatar/"#, with: #""tiers/tadpole/avatar/"#)
            .replacingOccurrences(of: #""dot/"#, with: #""tiers/tadpole/dot/"#))
        #expect(m.problem == nil)
        #expect(m.about == "NWVERSE 等级")
        #expect(m.files.allSatisfy { $0.hasPrefix("tiers/tadpole/") })
        #expect(try Self.decode().about == nil)
    }

    @Test func manifestsTheHubWouldLeaveOut() throws {
        func problem(_ from: String, _ to: String) throws -> String? {
            try Self.decode(Self.manifest.replacingOccurrences(of: from, with: to)).problem
        }
        #expect(try problem(#""id":"nwverse""#, #""id":"persona""#) != nil) // the built-ins' ids
        #expect(try problem(#""id":"nwverse""#, #""id":"persona""#) != nil)
        #expect(try problem(#""id":"nwverse""#, #""id":"Dou Dou""#) != nil)
        #expect(try problem(#""format":1"#, #""format":2"#) != nil)
        #expect(try problem(#""idle":"#, #""thinking":"#) != nil)
        #expect(try problem("avatar/64.png", "../64.png") != nil)
        #expect(try problem("avatar/64.png", ".hidden/64.png") != nil)
    }

    // MARK: Colors

    /// A yellow (the character's own) can't carry white words: the accent and the bubble come out darker, at the
    /// floors StyleSpecTests holds every style to, in light and in dark.
    @Test func colorsAreHeldToTheFloors() throws {
        let p = try #require(try Self.decode().colors.flatMap { PersonaPalette(light: $0.light, dark: $0.dark) })
        let accent = try #require(p.accent), bubble = try #require(p.bubble)
        for (c, page) in [(accent.light, UInt32(0xFFFFFF)), (accent.dark, UInt32(0x000000))] {
            #expect(PersonaPalette.contrast(c, 0xFFFFFF) >= PersonaPalette.onAccent)
            #expect(PersonaPalette.contrast(c, page) >= PersonaPalette.link)
        }
        #expect(PersonaPalette.contrast(bubble.light, 0xFFFFFF) >= PersonaPalette.bubbleWords)
        #expect(PersonaPalette.contrast(bubble.dark, 0xFFFFFF) >= PersonaPalette.bubbleWords)
        // Decoration is not held to anything: the primary stays the character's yellow.
        #expect(p.primary == .init(light: 0xF2C230, dark: 0xF2C230))
        // A color that already passes is used as given.
        #expect(PersonaPalette.meeting(0x1D6FD0, page: 0xFFFFFF) == 0x1D6FD0)
        // Too dark to read as a link on black: lightened (white still reads on it).
        let dark = PersonaPalette.meeting(0x101820, page: 0x000000)
        #expect(PersonaPalette.contrast(dark, 0x000000) >= PersonaPalette.link)
        #expect(PersonaPalette.contrast(dark, 0xFFFFFF) >= PersonaPalette.onAccent)
    }

    /// As Theme Color › Match Avatar: the accent, the bubble and Lark's links are the persona's, on any style (Lark's own too);
    /// what it leaves out is the style's; the built-in (no colors) leaves the style as it is.
    @Test func colorsAsTheThemeColor() {
        let p = PersonaPalette(primary: .init(light: 0xF2C230, dark: 0xF2C230), accent: .init(light: 0x8A5A00, dark: 0xB07A00),
                               bubble: .init(light: 0x7A4E00, dark: 0x7A4E00))
        let hex = StyleSpecTests.hex
        let glass = StyleSpec.compose(style: .glass, color: .persona, persona: p)
        #expect(hex(glass.accent, .light) == 0x8A5A00)
        #expect(hex(glass.accent, .dark) == 0xB07A00)
        #expect(hex(glass.userBubble, .light) == 0x7A4E00)
        #expect(hex(glass.lark.blue, .light) == 0x8A5A00)
        #expect(glass.personaPrimary != nil)
        let lark = StyleSpec.compose(style: .lark, color: .persona, persona: p)
        #expect(hex(lark.lark.blue, .light) == 0x8A5A00)
        #expect(hex(lark.page, .light) == 0xF5F6F7)
        let accentOnly = StyleSpec.compose(style: .clay, color: .persona, persona: PersonaPalette(accent: p.accent))
        #expect(hex(accentOnly.userBubble, .light) == 0x6759D3)
        let builtin = StyleSpec.compose(style: .glass, color: .persona, persona: nil)
        #expect(hex(builtin.accent, .light) == 0x007AFF)
        #expect(builtin.personaPrimary == nil)
    }

    // MARK: The phone's copy

    private static func info(rev: String) -> String {
        #"{"personas":[{"id":"nwverse","rev":"\#(rev)","bytes":10,"manifest":\#(manifest)}]}"#
    }

    private func store() -> (PersonaStore, URL, UserDefaults) {
        let root = FileManager.default.temporaryDirectory.appending(path: "personas-\(UUID().uuidString)")
        let defaults = UserDefaults(suiteName: "persona-tests-\(UUID().uuidString)")!
        return (PersonaStore(root: root, defaults: defaults), root, defaults)
    }

    @Test func choosingDownloadsTheWholePackageAndKeepsIt() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/personas", json: Self.info(rev: "r1"))
        for f in try Self.decode().files { hub.on("GET", "/api/v1/personas/\(Self.packID)/\(f)", json: "png:\(f)") }
        let (s, root, defaults) = store()
        await s.refresh(hub.client)
        #expect(s.offered.map(\.id) == Persona.builtins.map(\.id) + ["nwverse"])
        #expect(s.current.isBuiltin) // offered, not chosen
        let p = try #require(s.offered.last)
        #expect(!s.isOnPhone(p))
        // Only its face for Settings' card: 64 pt at 3x, the smallest avatar that big (the 128 is too small).
        #expect(hub.sent("GET", "/api/v1/personas/\(Self.packID)/avatar/512.png").count == 1)
        #expect(hub.sent("GET", "/api/v1/personas/\(Self.packID)/avatar/128.png").isEmpty)
        #expect(hub.sent("GET", "/api/v1/personas/\(Self.packID)/dot/idle.png").isEmpty)

        await s.choose(p, api: hub.client)
        #expect(s.current.id == "nwverse")
        #expect(defaults.string(forKey: PersonaStore.chosenKey) == "nwverse")
        let idle = root.appending(path: "\(Self.packID)/r1/dot/idle.png")
        #expect((try? String(contentsOf: idle, encoding: .utf8)) == "png:dot/idle.png")

        // The next launch shows it from disk, before the hub answers.
        let again = PersonaStore(root: root, defaults: defaults)
        #expect(again.current.id == "nwverse" && again.current.rev == "r1")

        // A new rev on the hub: downloaded, shown, and the old one removed.
        hub.on("GET", "/api/v1/personas", json: Self.info(rev: "r2"))
        await s.refresh(hub.client)
        #expect(s.current.rev == "r2")
        #expect(FileManager.default.fileExists(atPath: root.appending(path: "\(Self.packID)/r2/dot/idle.png").path(percentEncoded: false)))
        #expect(!FileManager.default.fileExists(atPath: root.appending(path: "\(Self.packID)/r1").path(percentEncoded: false)))

        // Gone from the hub: the built-in again (the choice is kept for when it comes back).
        hub.on("GET", "/api/v1/personas", json: #"{"personas":[]}"#)
        await s.refresh(hub.client)
        #expect(s.current.isBuiltin)
        #expect(s.chosenID == "nwverse")
    }

    @Test func mochiIsTheOnlyBuiltIn() async {
        let (s, _, defaults) = store()
        #expect(Persona.builtins.map(\.id) == ["persona"])
        #expect(s.current.avatarDesign == .mochi && s.current.cardTitle == "团子")
        #expect(s.chosenID == "persona" && defaults.string(forKey: PersonaStore.chosenKey) == nil)
        await s.choose(.builtin, api: nil)
        #expect(PersonaStore(root: s.root, defaults: defaults).current.id == "persona")
    }

    /// Two refreshes at once (connecting to the hub and Settings opening, 2026-10-31): the second must not clean away the
    /// package the first is still downloading. It did, and the phone kept a package missing the files written first.
    @Test func refreshesAtOnceLeaveAWholePackage() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/personas", json: Self.info(rev: "r1"))
        let files = try Self.decode().files
        for f in files { hub.on("GET", "/api/v1/personas/\(Self.packID)/\(f)", json: "png:\(f)") }
        let (s, root, defaults) = store()
        defaults.set("nwverse", forKey: PersonaStore.chosenKey)
        // Every answer takes 20 ms, as over the network; the second refresh starts when the first has the list and
        // one file (at 40 ms) and is fetching the rest.
        var slow = hub.client
        let answer = slow.transport!
        slow.transport = { req in
            try await Task.sleep(for: .milliseconds(20))
            return try await answer(req)
        }
        let api = slow
        let first = Task { await s.refresh(api) }
        try await Task.sleep(for: .milliseconds(30))
        let second = Task { await s.refresh(api) }
        await first.value
        await second.value
        #expect(s.current.id == "nwverse")
        let missing = files.filter { !FileManager.default.fileExists(atPath: root.appending(path: "\(Self.packID)/r1/\($0)").path(percentEncoded: false)) }
        #expect(missing.isEmpty, "the package on the phone lacks \(missing)")
    }

    @Test func aFailedDownloadChangesNothing() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/personas", json: Self.info(rev: "r1"))
        hub.on("GET", "/api/v1/personas/\(Self.packID)/avatar/64.png", json: "png") // the others 404
        let (s, root, defaults) = store()
        await s.refresh(hub.client)
        await s.choose(try #require(s.offered.last), api: hub.client)
        #expect(s.current.isBuiltin)
        #expect(defaults.string(forKey: PersonaStore.chosenKey) == nil)
        #expect(s.error != nil)
        // Nothing half-written is left where a package would be read from.
        #expect(!FileManager.default.fileExists(atPath: root.appending(path: "\(Self.packID)/r1").path(percentEncoded: false)))
    }

    /// With no hub, a package not on the phone cannot be chosen, and Settings says why instead of doing nothing.
    @Test func choosingOfflineSaysWhy() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/personas", json: Self.info(rev: "r1"))
        let (s, _, defaults) = store()
        await s.refresh(hub.client)
        await s.choose(try #require(s.offered.last), api: nil)
        #expect(s.current.isBuiltin)
        #expect(defaults.string(forKey: PersonaStore.chosenKey) == nil)
        #expect(s.error?.contains("连上 hub") == true)
        // Choosing one that is on the phone then clears the message.
        await s.choose(Persona.builtin, api: nil)
        #expect(s.current.id == Persona.builtinID)
        #expect(s.error == nil)
    }

    /// An APNG's frames, composed whole, with its frame delays.
    @Test func framesFromAnAPNG() throws {
        let url = FileManager.default.temporaryDirectory.appending(path: "dot-\(UUID().uuidString).png")
        let dest = try #require(CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 4, nil))
        CGImageDestinationSetProperties(dest, [kCGImagePropertyPNGDictionary: [kCGImagePropertyAPNGLoopCount: 0]] as CFDictionary)
        for i in 0..<4 {
            let img = UIGraphicsImageRenderer(size: CGSize(width: 12, height: 12), format: .init(for: .init(displayScale: 1))).image { ctx in
                UIColor(white: CGFloat(i) / 4, alpha: 1).setFill()
                ctx.fill(CGRect(x: 0, y: 0, width: 12, height: 12))
            }
            CGImageDestinationAddImage(dest, img.cgImage!, [kCGImagePropertyPNGDictionary: [kCGImagePropertyAPNGDelayTime: 0.125]] as CFDictionary)
        }
        #expect(CGImageDestinationFinalize(dest))
        let f = try #require(PersonaFrames(url: url, fps: nil))
        #expect(f.images.count == 4)
        #expect(abs(f.duration - 0.5) < 0.01)
        #expect(f.images[0].cgImage?.width == 12)
    }
}
