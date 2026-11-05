import Foundation
import SwiftUI
import Testing
import UIKit
@testable import Jarvis

/// The home screen icon, the 形象's (#3203): which icon each 形象 puts on, the model over a stand-in for UIApplication
/// (following the 形象 and 深浅色, the migration from the old App 图标 setting), and that every icon's assets are in the
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

    @Test func startsOnWhatTheSystemHas() {
        #expect(AppIconModel(system: FakeSystem("AppIcon-Visor")).current == .visor)
        #expect(AppIconModel(system: FakeSystem("AppIcon-PaperLight")).current == .standard)
        // An icon this build dropped (余烬 / 薄荷 / 石墨, #709; 蜜桃 / p. / 气场, #3203) reads as the default.
        #expect(AppIconModel(system: FakeSystem("AppIcon-Graphite")).current == .standard)
        #expect(AppIconModel(system: FakeSystem("AppIcon-Aura")).current == .standard)
        #expect(AppIconModel(system: FakeSystem()).current == .standard)
        #expect(!AppIconModel(system: nil).available)
    }

    /// Every built-in 形象 has its own icon; a package takes the built-in it names in `app_icon_fallback`, the default's
    /// when it names none or one that is not there.
    @Test func eachLookPutsOnItsIcon() throws {
        #expect(Persona.builtin.appIcon == .standard)
        #expect(Persona.visor.appIcon == .visor)
        #expect(Persona.circuit.appIcon == .circuit)
        for p in Persona.builtins { #expect(p.appIcon.lookID == p.id, "\(p.id) has no icon of its own") }
        #expect(Set(Persona.builtins.map(\.appIcon)).count == Persona.builtins.count)

        func package(_ fallback: String?) throws -> Persona {
            var json = #"{"format":1,"id":"mint","name":"小薄荷","avatar":{"sizes":{"128":"a.png"}},"dot":{"states":{"idle":{"file":"i.png"}}}"#
            if let fallback { json += #","app_icon_fallback":"\#(fallback)""# }
            let m = try JSONDecoder().decode(PersonaManifest.self, from: Data((json + "}").utf8))
            return Persona(info: PersonaInfo(id: m.id, rev: "1", bytes: 1, manifest: m))
        }
        #expect(try package("jarvis-circuit").appIcon == .circuit)
        #expect(try package("jarvis").appIcon == .visor)
        #expect(try package(nil).appIcon == .standard)
        #expect(try package("gone").appIcon == .standard)
        #expect(try package("jarvis").manifest?.problem == nil)
    }

    /// Choosing a 形象 puts its icon on; the same again does nothing (the system would alert again); back to 团子.
    @Test func followsTheLook() async {
        let sys = FakeSystem()
        let m = AppIconModel(system: sys)
        await m.follow(.visor)
        #expect(m.current == .visor && sys.alternateIconName == "AppIcon-Visor")
        await m.follow(.visor)
        await m.follow(nil) // 形象 not known yet: the last one stays
        await m.follow(.standard)
        #expect(m.current == .standard && sys.calls == ["AppIcon-Visor", nil])
        #expect(!m.switching && m.failure == nil)
    }

    /// The migration from the separate App 图标 setting (#3203): the 形象 wins. Someone on 面罩 with the default icon gets
    /// 面罩's the first time the app is in front; someone on 团子 with a dropped icon (气场) is left alone, as the home
    /// screen already shows the default; someone on 团子 with 面罩's icon goes back to the default.
    @Test func theLookDecidesForTheOldSetting() async {
        let plain = FakeSystem()
        await AppIconModel(system: plain).follow(.visor, scheme: .system)
        #expect(plain.calls == ["AppIcon-Visor"])

        let dropped = FakeSystem("AppIcon-Aura")
        let d = AppIconModel(system: dropped)
        await d.follow(.standard, scheme: .system)
        #expect(dropped.calls.isEmpty && d.current == .standard)
        await d.follow(.circuit)
        #expect(dropped.calls == ["AppIcon-Circuit"])

        let other = FakeSystem("AppIcon-Visor")
        await AppIconModel(system: other).follow(.standard, scheme: .system)
        #expect(other.calls == [nil])
    }

    /// 团子's 纸色 takes the app's 深浅色 (#710): under 浅色 it goes on in its light-only set, and a change of 深浅色 moves
    /// it; an icon with one set stays put.
    @Test func paperFollowsTheAppsScheme() async {
        #expect(AppIconChoice.standard.iconName(for: .system) == nil)
        #expect(AppIconChoice.standard.iconName(for: .dark) == "AppIcon-PaperDark")
        #expect(AppIconChoice.standard.iconName(for: .light) == "AppIcon-PaperLight")
        #expect(AppIconChoice.visor.iconName(for: .dark) == "AppIcon-Visor")

        let sys = FakeSystem()
        let m = AppIconModel(system: sys)
        await m.follow(.standard, scheme: .dark)
        #expect(m.current == .standard && sys.alternateIconName == "AppIcon-PaperDark")
        await m.follow(nil, scheme: .dark) // already on: no second call
        await m.follow(.circuit)
        await m.follow(nil, scheme: .light) // 汇流 has one set
        #expect(sys.calls == ["AppIcon-PaperDark", "AppIcon-Circuit"])
        await m.follow(.standard, scheme: .system)
        #expect(sys.calls == ["AppIcon-PaperDark", "AppIcon-Circuit", nil])
    }

    /// The look an icon is drawn in (Persona's logo, #1312): 纸色 takes a set 深浅色, the others and 跟随系统 the system's.
    @Test func homeScreenLook() {
        #expect(AppIconChoice.standard.homeScreenLook(.light, system: .dark) == .light)
        #expect(AppIconChoice.standard.homeScreenLook(.dark, system: .light) == .dark)
        #expect(AppIconChoice.standard.homeScreenLook(.system, system: .dark) == .dark)
        for c in [AppIconChoice.visor, .circuit] {
            #expect(c.homeScreenLook(.light, system: .dark) == .dark)
            #expect(c.homeScreenLook(.dark, system: .light) == .light)
        }
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

    /// 深色 then quickly 浅色 (geminiwen's review of #714): the second change comes while the system is still asking
    /// about the first, and the icon still ends on 浅色's set.
    @Test func aChangeWhileSwitchingIsNotLost() async {
        let sys = SlowSystem()
        let m = AppIconModel(system: sys)
        let first = Task { await m.follow(.standard, scheme: .dark) }
        await until { sys.holding }
        await m.follow(nil, scheme: .light) // the switch to 深色 is in flight: remembered, not dropped
        sys.letGo()
        await until { sys.calls.count == 2 && sys.holding }
        sys.letGo()
        await first.value
        #expect(sys.calls == ["AppIcon-PaperDark", "AppIcon-PaperLight"])
        #expect(sys.alternateIconName == "AppIcon-PaperLight" && !m.switching && m.current == .standard)
    }

    /// Another 形象 chosen while the system is still asking about the first: the icon ends on the last one's.
    @Test func aLookChosenWhileSwitchingIsNotLost() async {
        let sys = SlowSystem()
        let m = AppIconModel(system: sys)
        let first = Task { await m.follow(.visor) }
        await until { sys.holding }
        await m.follow(.circuit)
        sys.letGo()
        await until { sys.calls.count == 2 && sys.holding }
        sys.letGo()
        await first.value
        #expect(sys.calls == ["AppIcon-Visor", "AppIcon-Circuit"] && m.current == .circuit)
    }

    /// An icon this build dropped (余烬…) is left as it is: the home screen shows the primary icon already, and a switch
    /// on coming to the front would pop the system's alert.
    @Test func aDroppedIconIsNotSwitchedOnItsOwn() async {
        let sys = FakeSystem("AppIcon-Ember")
        let m = AppIconModel(system: sys)
        await m.follow(.standard)
        #expect(sys.calls.isEmpty && m.current == .standard)
    }

    @Test func aFailureKeepsTheIconAndSaysSo() async {
        let sys = FakeSystem()
        sys.fail = NSError(domain: "test", code: 1, userInfo: [NSLocalizedDescriptionKey: "不行"])
        let m = AppIconModel(system: sys)
        await m.follow(.circuit)
        #expect(m.current == .standard)
        #expect(m.failure?.contains("不行") == true)
    }

    @Test func notWhereTheSystemCannot() async {
        let sys = FakeSystem()
        sys.supportsAlternateIcons = false
        let m = AppIconModel(system: sys)
        await m.follow(.circuit)
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

    /// Two 形象 on one icon set, or one picture for two icons, would make the logo lie about what is on.
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
