import Foundation
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
        for c in AppIconChoice.allCases { #expect(AppIconChoice(iconName: c.iconName) == c) }
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
            let set = Self.assets.appending(path: "\(c.iconName ?? "AppIcon").appiconset")
            for f in ["icon.png", "icon-dark.png", "icon-tinted.png"] {
                let img = try #require(UIImage(contentsOfFile: set.appending(path: f).path), "\(c) \(f)")
                #expect(img.size.width * img.scale == 1024, "\(c) \(f)")
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
        for c in AppIconChoice.allCases {
            let set = Self.assets.appending(path: "\(c.iconName ?? "AppIcon").appiconset")
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
        #expect(listed == Set(AppIconChoice.allCases.compactMap(\.iconName)))
    }
}
