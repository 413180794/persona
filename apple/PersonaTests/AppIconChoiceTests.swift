import Foundation
import SwiftUI
import Testing
import UIKit
@testable import Persona

/// The home screen icon, the Avatar's (#3203): which icon each Avatar puts on, the model over a stand-in for UIApplication
/// (following the Avatar and Light & Dark, the migration from the old App Icon setting), and that every icon's assets are in the
/// catalog and the build lists every alternate icon.
@MainActor struct AppIconChoiceTests {
    private final class FakeSystem: AppIconSetting {
        var supportsAlternateIcons = true
        var alternateIconName: String?
        var fail: Error?
        var calls: [String?] = []

        init(_ name: String? = nil) { alternateIconName = name }

        func setAlternateIconName(_ name: String?) async throws {
            calls.append(name)
            if let fail { throw fail }
            alternateIconName = name
        }
    }

    private static let apple = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
    private static let assets = apple.appending(path: "Persona/Resources/Assets.xcassets")

    @Test func namesRoundTrip() {
        for c in AppIconChoice.allCases {
            #expect(AppIconChoice(iconName: c.iconName) == c)
            for s in ColorSchemeChoice.allCases { #expect(AppIconChoice(iconName: c.iconName(for: s)) == c, "\(c) \(s)") }
        }
        #expect(AppIconChoice.standard.iconName == nil)
        // An icon a later build dropped reads as the default, not a crash or a blank pick.
        #expect(AppIconChoice(iconName: "AppIcon-Gone") == .standard)
    }

    @Test func unitHostDoesNotTouchRealSystemIcons() async {
        let model = AppIconModel.live()
        #expect(!model.available && model.current == .standard)
        await model.follow(.standard, scheme: .dark)
        #expect(!model.switching && model.failure == nil)
        #expect(!AppModel().appIcon.available)
    }

    @Test func startsOnTheDefault() {
        #expect(AppIconModel(system: FakeSystem("AppIcon-PaperLight")).current == .standard)
        #expect(AppIconModel(system: FakeSystem()).current == .standard)
        #expect(!AppIconModel(system: nil).available)
    }

    @Test func bothPersonasUseTheDefaultIcon() throws {
        #expect(Persona.builtin.appIcon == .standard)
        let m = try JSONDecoder().decode(PersonaManifest.self, from: Data(#"{"format":1,"id":"nwverse","name":"奶蛙","avatar":{"sizes":{"128":"a.png"}},"dot":{"states":{"idle":{"file":"i.png"}}},"app_icon_fallback":"persona-circuit"}"#.utf8))
        #expect(Persona(info: PersonaInfo(id: m.id, rev: "1", bytes: 1, manifest: m)).appIcon == .standard)
    }

    @Test func followingTheSameIconDoesNotPromptAgain() async {
        let sys = FakeSystem()
        let m = AppIconModel(system: sys)
        await m.follow(.standard)
        await m.follow(nil)
        #expect(m.current == .standard && sys.calls.isEmpty)
    }

    @Test func paperFollowsTheAppsScheme() async {
        let sys = FakeSystem()
        let m = AppIconModel(system: sys)
        await m.follow(.standard, scheme: .dark)
        await m.follow(nil, scheme: .dark)
        await m.follow(nil, scheme: .light)
        await m.follow(.standard, scheme: .system)
        #expect(sys.calls == ["AppIcon-PaperDark", "AppIcon-PaperLight", nil])
    }

    @Test func homeScreenLook() {
        #expect(AppIconChoice.standard.homeScreenLook(.light, system: .dark) == .light)
        #expect(AppIconChoice.standard.homeScreenLook(.dark, system: .light) == .dark)
        #expect(AppIconChoice.standard.homeScreenLook(.system, system: .dark) == .dark)
    }

    /// A system that holds each switch until the test lets it go, as while its alert is up.
    private final class SlowSystem: AppIconSetting {
        var supportsAlternateIcons = true
        var alternateIconName: String?
        var calls: [String?] = []
        private var held: [CheckedContinuation<Void, Never>] = []
        var holding: Bool { !held.isEmpty }

        func setAlternateIconName(_ name: String?) async throws {
            calls.append(name)
            await withCheckedContinuation { held.append($0) }
            alternateIconName = name
        }

        func letGo() {
            let h = held
            held = []
            h.forEach { $0.resume() }
        }
    }

    /// Yields until `ready` or a bound (a broken model fails the expectations after, not the run).
    private func until(_ ready: () -> Bool) async {
        for _ in 0..<1000 where !ready() { await Task.yield() }
    }

    /// Dark then quickly Light: the second change comes while the system is still asking
    /// about the first, and the icon still ends on Light's set.
    @Test func aChangeWhileSwitchingIsNotLost() async {
        let sys = SlowSystem()
        let m = AppIconModel(system: sys)
        let first = Task { await m.follow(.standard, scheme: .dark) }
        await until { sys.holding }
        await m.follow(nil, scheme: .light) // the switch to Dark is in flight: remembered, not dropped
        sys.letGo()
        await until { sys.calls.count == 2 && sys.holding }
        sys.letGo()
        await first.value
        #expect(sys.calls == ["AppIcon-PaperDark", "AppIcon-PaperLight"])
        #expect(sys.alternateIconName == "AppIcon-PaperLight" && !m.switching && m.current == .standard)
    }

    @Test func aFailureKeepsTheIconAndSaysSo() async {
        let sys = FakeSystem()
        sys.fail = NSError(domain: "test", code: 1, userInfo: [NSLocalizedDescriptionKey: "不行"])
        let m = AppIconModel(system: sys)
        await m.follow(.standard, scheme: .light)
        #expect(m.current == .standard)
        #expect(m.failure?.contains("不行") == true)
    }

    @Test func notWhereTheSystemCannot() async {
        let sys = FakeSystem()
        sys.supportsAlternateIcons = false
        let m = AppIconModel(system: sys)
        await m.follow(.standard, scheme: .light)
        #expect(sys.calls.isEmpty && m.current == .standard)
    }

    @Test func everyChoiceHasItsArt() throws {
        for c in AppIconChoice.allCases {
            for name in (c.iconName == nil ? ["AppIcon"] : []) + c.iconNames {
                let set = Self.assets.appending(path: "\(name).appiconset")
                for f in ["icon.png", "icon-dark.png", "icon-tinted.png"] {
                    let img = try #require(UIImage(contentsOfFile: set.appending(path: f).path), "\(name) \(f)")
                    #expect(img.size.width * img.scale == 1024, "\(name) \(f)")
                }
            }
            #expect(UIImage(named: c.preview) != nil, "preview \(c.preview) not in the app's catalog")
        }
    }

    /// Two Avatars on one icon set, or one picture for two icons, would make the logo lie about what is on.
    @Test func everyChoiceIsDistinct() {
        let all = AppIconChoice.allCases
        #expect(Set(all.map(\.iconName)).count == all.count)
        #expect(Set(all.map(\.preview)).count == all.count)
        #expect(Set(all.map(\.lookID)).count == all.count)
        #expect(all.allSatisfy { c in Persona.builtins.contains { $0.id == c.lookID } })
    }

    /// App Store icons must be 1024 square and opaque. UIImage loads an icon with alpha just the same, so read the PNG's
    /// IHDR: width, height, colour type 2 (RGB, no alpha).
    @Test func everyIconIsOpaque1024() throws {
        for c in ["AppIcon"] + AppIconChoice.allCases.flatMap(\.iconNames) {
            let set = Self.assets.appending(path: "\(c).appiconset")
            #expect(FileManager.default.fileExists(atPath: set.appending(path: "Contents.json").path), "\(c)")
            for f in ["icon.png", "icon-dark.png", "icon-tinted.png"] {
                let head = try [UInt8](Data(contentsOf: set.appending(path: f)).prefix(26))
                try #require(head.count == 26 && head[..<8].elementsEqual([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]),
                             "\(c) \(f) is not a PNG")
                let be = { (i: Int) in head[i ..< i + 4].reduce(0) { $0 << 8 | Int($1) } }
                #expect(be(16) == 1024 && be(20) == 1024, "\(c) \(f) is \(be(16))x\(be(20))")
                #expect(head[25] == 2, "\(c) \(f) colour type \(head[25]), want 2 (RGB, no alpha)")
            }
        }
    }

    /// The build only ships the icon sets it is told to (ASSETCATALOG_COMPILER_ALTERNATE_APPICON_NAMES).
    @Test func theBuildListsEveryAlternate() throws {
        let yml = try String(contentsOf: Self.apple.appending(path: "project.yml"), encoding: .utf8)
        let line = try #require(yml.components(separatedBy: "\n").first { $0.contains("ASSETCATALOG_COMPILER_ALTERNATE_APPICON_NAMES") })
        let listed = Set(line.components(separatedBy: ":").last!.trimmingCharacters(in: .whitespaces.union(["\""]))
            .split(separator: " ").map(String.init))
        #expect(listed == Set(AppIconChoice.allCases.flatMap(\.iconNames)))
    }
}
