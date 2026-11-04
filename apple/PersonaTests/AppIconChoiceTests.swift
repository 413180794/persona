import Foundation
import SwiftUI
import Testing
import UIKit
@testable import Jarvis

/// 设置 › 外观 › App 图标: the choices, the model over a stand-in for UIApplication, and that every choice's assets are in
/// the catalog and the build lists every alternate icon.
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
        #expect(AppIconModel(system: FakeSystem("AppIcon-Aura")).current == .aura)
        #expect(AppIconModel(system: FakeSystem("AppIcon-Peach")).current == .peach)
        // An icon this build dropped (余烬 / 薄荷 / 石墨, #709) reads as the default.
        #expect(AppIconModel(system: FakeSystem("AppIcon-Graphite")).current == .standard)
        #expect(AppIconModel(system: FakeSystem()).current == .standard)
        #expect(!AppIconModel(system: nil).available)
    }

    @Test func switchesAndBack() async {
        let sys = FakeSystem()
        let m = AppIconModel(system: sys)
        await m.select(.didot)
        #expect(m.current == .didot && sys.alternateIconName == "AppIcon-Didot")
        await m.select(.didot) // already on: no second call (the system would alert again)
        await m.select(.standard)
        #expect(m.current == .standard && sys.calls == ["AppIcon-Didot", nil])
        #expect(m.switching == nil && m.failure == nil)
    }

    /// 纸色 (the default) and 蜜桃 take the app's 深浅色 (#710): picked under 浅色 they go on in their light-only set, and a
    /// change of 深浅色 moves them; an icon with one set stays put.
    @Test func paperAndPeachFollowTheAppsScheme() async {
        #expect(AppIconChoice.standard.iconName(for: .system) == nil)
        #expect(AppIconChoice.standard.iconName(for: .dark) == "AppIcon-PaperDark")
        #expect(AppIconChoice.peach.iconName(for: .light) == "AppIcon-PeachLight")
        #expect(AppIconChoice.aura.iconName(for: .dark) == "AppIcon-Aura")

        let sys = FakeSystem()
        let m = AppIconModel(system: sys)
        await m.follow(.dark)
        #expect(m.current == .standard && sys.alternateIconName == "AppIcon-PaperDark")
        await m.follow(.dark) // already on: no second call (the system would alert again)
        await m.select(.peach, scheme: .dark)
        #expect(m.current == .peach && sys.alternateIconName == "AppIcon-PeachDark")
        await m.follow(.system)
        #expect(sys.calls == ["AppIcon-PaperDark", "AppIcon-PeachDark", "AppIcon-Peach"])

        let other = FakeSystem("AppIcon-Aura")
        let n = AppIconModel(system: other)
        await n.follow(.dark)
        #expect(other.calls.isEmpty && n.current == .aura)
    }

    /// The look an icon is drawn in (设置's header and the picker, #1312): 纸色 / 蜜桃 take a set 深浅色, the others
    /// and 跟随系统 the system's.
    @Test func homeScreenLook() {
        for c in [AppIconChoice.standard, .peach] {
            #expect(c.homeScreenLook(.light, system: .dark) == .light)
            #expect(c.homeScreenLook(.dark, system: .light) == .dark)
            #expect(c.homeScreenLook(.system, system: .dark) == .dark)
        }
        for c in [AppIconChoice.didot, .aura] {
            #expect(c.homeScreenLook(.light, system: .dark) == .dark)
            #expect(c.homeScreenLook(.dark, system: .light) == .light)
        }
    }

    @Test func startsOnAnyOfTheSets() {
        #expect(AppIconModel(system: FakeSystem("AppIcon-PaperLight")).current == .standard)
        #expect(AppIconModel(system: FakeSystem("AppIcon-PeachDark")).current == .peach)
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
        let first = Task { await m.follow(.dark) }
        await until { sys.holding }
        await m.follow(.light) // the switch to 深色 is in flight: remembered, not dropped
        sys.letGo()
        await until { sys.calls.count == 2 && sys.holding }
        sys.letGo()
        await first.value
        #expect(sys.calls == ["AppIcon-PaperDark", "AppIcon-PaperLight"])
        #expect(sys.alternateIconName == "AppIcon-PaperLight" && m.switching == nil && m.current == .standard)
    }

    /// An icon this build dropped (余烬…) is left as it is: the home screen shows the primary icon already, and a switch
    /// on coming to the front would pop the system's alert.
    @Test func aDroppedIconIsNotSwitchedOnItsOwn() async {
        let sys = FakeSystem("AppIcon-Ember")
        let m = AppIconModel(system: sys)
        await m.follow(.dark)
        #expect(sys.calls.isEmpty && m.current == .standard)
    }

    @Test func aFailureKeepsTheIconAndSaysSo() async {
        let sys = FakeSystem()
        sys.fail = NSError(domain: "test", code: 1, userInfo: [NSLocalizedDescriptionKey: "不行"])
        let m = AppIconModel(system: sys)
        await m.select(.aura)
        #expect(m.current == .standard)
        #expect(m.failure?.contains("不行") == true)
    }

    @Test func notWhereTheSystemCannot() async {
        let sys = FakeSystem()
        sys.supportsAlternateIcons = false
        let m = AppIconModel(system: sys)
        await m.select(.aura)
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

    /// Two tiles that read the same, or two choices on one icon set, would make the picker lie about what is on.
    @Test func everyChoiceIsDistinct() {
        let all = AppIconChoice.allCases
        #expect(Set(all.map(\.iconName)).count == all.count)
        #expect(Set(all.map(\.preview)).count == all.count)
        #expect(Set(all.map(\.title)).count == all.count)
        #expect(Set(all.map { "\($0.title) \($0.subtitle)" }).count == all.count)
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
