import Foundation
import Security
import SwiftUI
import Testing
import UIKit
@testable import Jarvis

/// The persona on the Live Activity (docs/live-activity.md「形象」): which state it shows for a fleet, and the look
/// the app hands the widget extension.
struct FleetPersonaTests {
    @Test func stateFollowsThePhase() {
        #expect(FleetPersona.state(FleetState(phase: .needsYou, needsYou: 1, working: 2), stale: false) == .needsYou)
        #expect(FleetPersona.state(FleetState(phase: .working, working: 2), stale: false) == .working)
        // The assistant answering does not make working agents "thinking".
        #expect(FleetPersona.state(FleetState(phase: .working, working: 2, assistant: true), stale: false) == .working)
        #expect(FleetPersona.state(FleetState(phase: .done, unread: 1), stale: false) == .idle)
        #expect(FleetPersona.state(FleetState(), stale: false) == .idle)
    }

    /// Only the assistant at work (no agent working or waiting): 思考中, the nearest the fleet has.
    @Test func assistantAloneIsThinking() {
        #expect(FleetPersona.state(FleetState(phase: .done, unread: 1, assistant: true), stale: false) == .thinking)
        #expect(FleetPersona.state(FleetState(assistant: true), stale: false) == .thinking)
    }

    /// Stale content (the hub stopped pushing) wins over everything: 离线, gray.
    @Test func staleIsOffline() {
        for phase in [FleetState.Phase.needsYou, .working, .done, .none] {
            #expect(FleetPersona.state(FleetState(phase: phase), stale: true) == .offline)
        }
    }

    /// The look carries the color the app resolved in each appearance, so the extension draws the same accent.
    @Test func lookResolvesLightAndDark() {
        let look = FleetLook(design: .circuit, tint: Palette.coral)
        #expect(look.design == "circuit")
        #expect(look.faceDesign == .circuit)
        #expect(look.light == 0xD4683F)
        #expect(look.dark == 0xE8835C)
    }

    @Test func unknownDesignFallsBackToTheDefaultFace() throws {
        let look = try JSONDecoder().decode(FleetLook.self, from: Data(#"{"design":"later","light":1,"dark":2}"#.utf8))
        #expect(look.faceDesign == .visor)
    }

    @Test func lookRoundTrips() throws {
        let look = FleetLook(design: .visor, tint: Palette.systemBlue)
        #expect(try JSONDecoder().decode(FleetLook.self, from: JSONEncoder().encode(look)) == look)
        #expect(look == FleetLook.standard)
    }

    /// A look saved by a build before packages had faces still reads: a built-in.
    @Test func anOlderLookStillReads() throws {
        let look = try JSONDecoder().decode(FleetLook.self, from: Data(#"{"design":"circuit","light":1,"dark":2}"#.utf8))
        #expect(look.package == nil && look.faceDesign == .circuit)
    }

    /// The persona rides in the ContentState (build 98: the extension did not get it from the keychain on a phone):
    /// content from an older hub, without it, still decodes and draws from the keychain; with it, the extension draws
    /// what the content says, and the app's own updates carry it.
    @Test func lookRidesInTheContent() throws {
        let json = #"{"phase":"needsYou","needsYou":1,"working":0,"unread":0,"items":[],"details":false,"assistant":true,"left":"status","right":"tpm"}"#
        let old = try JSONDecoder().decode(FleetState.self, from: Data(json.utf8))
        #expect(FleetPersona.state(old, stale: false) == .needsYou && old.look == nil)
        let out = try JSONSerialization.jsonObject(with: JSONEncoder().encode(old)) as? [String: Any]
        #expect(out?["look"] == nil)

        let pushed = #"{"phase":"working","working":1,"look":{"design":"circuit","light":13919295,"dark":15238012}}"#
        let state = try JSONDecoder().decode(FleetState.self, from: Data(pushed.utf8))
        #expect(state.look == FleetLook(design: "circuit", light: 0xD4643F, dark: 0xE8837C))
        #expect(state.drawnLook.faceDesign == .circuit)
        // A look the extension cannot read (a later hub's) is dropped, not the whole content.
        let odd = try JSONDecoder().decode(FleetState.self, from: Data(#"{"phase":"working","look":{"design":7}}"#.utf8))
        #expect(odd.phase == .working && odd.look == nil)

        let mine = FleetLook(design: "visor", light: 1, dark: 2, package: "a/1")
        #expect(old.tailored(island: IslandPrefs(), details: true, look: mine).look == mine)
        #expect(state.tailored(island: IslandPrefs(), details: true).look == state.look)
    }

    // MARK: 设置 › 外观 › 形象 → what the extension draws

    /// The keychain group the app and the extension share, as a dictionary: what one saves, the other reads.
    nonisolated final class Shared: @unchecked Sendable {
        private var items: [String: Data] = [:]
        private let lock = NSLock()
        var store: FleetLookStore {
            FleetLookStore(read: { k in self.lock.withLock { self.items[k] } },
                           write: { d, k in self.lock.withLock { self.items[k] = d }; return true })
        }
        func has(_ account: String) -> Bool { lock.withLock { items[account] != nil } }
    }

    /// A placeholder package (the real ones are the user's, on the hub only): a disc of one color per picture, frames
    /// for idle / working / thinking and none for needs you or offline.
    static let colors: [String: UInt32] = ["idle": 0x40B2A0, "working": 0xE08A2E, "thinking": 0x7D5BDC, "avatar": 0x3D89BC]
    static let manifest = """
    {"format":1,"id":"mint","name":"小薄荷","avatar":{"sizes":{"128":"avatar/128.png"}},
     "dot":{"size":144,"states":{"idle":{"file":"dot/idle.png"},"working":{"file":"dot/working.png"},"thinking":{"file":"dot/thinking.png"}}},
     "colors":{"light":{"primary":"#40B2A0"},"dark":{"primary":"#40B2A0"}}}
    """

    static func disc(_ rgb: UInt32) -> Data {
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let color = UIColor(red: CGFloat(rgb >> 16 & 0xFF) / 255, green: CGFloat(rgb >> 8 & 0xFF) / 255, blue: CGFloat(rgb & 0xFF) / 255, alpha: 1)
        return UIGraphicsImageRenderer(size: CGSize(width: 144, height: 144), format: format).pngData { _ in
            color.setFill()
            UIBezierPath(ovalIn: CGRect(x: 0, y: 0, width: 144, height: 144)).fill()
        }
    }

    /// The package chosen in 设置 the way the app does it: listed by the hub, downloaded, on screen.
    @MainActor private func chosen() async throws -> PersonaStore {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/personas", json: #"{"personas":[{"id":"mint","rev":"r1","bytes":10,"manifest":\#(Self.manifest)}]}"#)
        for (name, file) in [("avatar", "avatar/128.png"), ("idle", "dot/idle.png"), ("working", "dot/working.png"), ("thinking", "dot/thinking.png")] {
            let png = Self.disc(Self.colors[name]!)
            hub.on("GET", "/api/v1/personas/mint/\(file)") { _ in png }
        }
        let root = FileManager.default.temporaryDirectory.appending(path: "fleet-personas-\(UUID().uuidString)")
        let personas = PersonaStore(root: root, defaults: UserDefaults(suiteName: "fleet-persona-\(UUID().uuidString)")!)
        await personas.refresh(hub.client)
        await personas.choose(try #require(personas.offered.first { $0.id == "mint" }), api: hub.client)
        #expect(personas.current.id == "mint")
        return personas
    }

    /// The color at a point of a view as drawn (0…1 of its side), and its alpha.
    @MainActor static func pixel(_ view: some View, size: CGFloat, at p: CGPoint) throws -> (rgb: UInt32, alpha: UInt8) {
        let renderer = ImageRenderer(content: view.environment(\.colorScheme, .dark))
        renderer.scale = 2
        let cg = try #require(renderer.cgImage)
        var px = [UInt8](repeating: 0, count: 4)
        let ctx = try #require(CGContext(data: &px, width: 1, height: 1, bitsPerComponent: 8, bytesPerRow: 4,
                                         space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        let x = (p.x * CGFloat(cg.width)).rounded(.down), y = (p.y * CGFloat(cg.height)).rounded(.down)
        ctx.draw(cg, in: CGRect(x: -x, y: -(CGFloat(cg.height) - 1 - y), width: CGFloat(cg.width), height: CGFloat(cg.height)))
        let a = max(px[3], 1)
        func un(_ v: UInt8) -> UInt32 { UInt32(min(255, (Int(v) * 255 + Int(a) / 2) / Int(a))) }
        return (un(px[0]) << 16 | un(px[1]) << 8 | un(px[2]), px[3])
    }

    static func near(_ a: UInt32, _ b: UInt32, _ by: Int = 10) -> Bool {
        [16, 8, 0].allSatisfy { abs(Int(a >> UInt32($0) & 0xFF) - Int(b >> UInt32($0) & 0xFF)) <= by }
    }

    static let center = CGPoint(x: 0.5, y: 0.5)

    /// The whole chain: the package chosen in 设置 → the look and faces the app saves → what the extension reads →
    /// the face it draws, for every state: the package's own frame, or its avatar with the state's cue.
    @MainActor @Test func aPackageChosenInSettingsIsWhatTheExtensionDraws() async throws {
        let personas = try await chosen()
        let shared = Shared()
        let look = FleetLook(persona: personas.current, builtinAccent: Palette.systemBlue)
        #expect(look.package == "mint/r1")
        #expect(FleetLook.save(look, faces: { FleetFaces(persona: personas.current, personas: personas) }, to: shared.store))

        // The extension's side: only what is in the shared store.
        let read = FleetLook.load(shared.store)
        #expect(read == look)
        #expect(read.light == 0x40B2A0) // the package's primary, for the moments its faces cannot be read
        let faces = try #require(read.faces(shared.store))
        #expect(faces.package == "mint/r1")
        let size = IslandMetrics.face
        for (state, name) in [(PersonaState.idle, "idle"), (.working, "working"), (.thinking, "thinking")] {
            #expect(faces.face(state)?.own == true)
            let drawn = try Self.pixel(FleetFace(size: size, state: state, look: read, faces: faces), size: size, at: Self.center)
            #expect(Self.near(drawn.rgb, Self.colors[name]!), "\(state): \(String(drawn.rgb, radix: 16))")
        }
        // No frames for needs you: the avatar in the built-ins' red ring.
        #expect(faces.face(.needsYou)?.own == false)
        let needs = FleetFace(size: size, state: .needsYou, look: read, faces: faces)
        #expect(Self.near(try Self.pixel(needs, size: size, at: Self.center).rgb, Self.colors["avatar"]!))
        let ring = try Self.pixel(needs, size: size, at: CGPoint(x: 0.5, y: 0.03)).rgb
        #expect(Self.near(ring, Self.hex(FleetPalette.needsYou), 24), "ring: \(String(ring, radix: 16))")
        // None for offline either: the avatar, gray and faded.
        let off = try Self.pixel(FleetFace(size: size, state: .offline, look: read, faces: faces), size: size, at: Self.center)
        let (r, g, b) = (Int(off.rgb >> 16 & 0xFF), Int(off.rgb >> 8 & 0xFF), Int(off.rgb & 0xFF))
        #expect(abs(r - g) < 6 && abs(g - b) < 6 && off.alpha < 220, "offline: \(String(off.rgb, radix: 16)) \(off.alpha)")

        // The same choice again saves nothing and makes no faces: the running activity is not redrawn for nothing.
        #expect(!FleetLook.save(look, faces: { Issue.record("faces made again"); return nil }, to: shared.store))
    }

    /// Back to a built-in: the extension draws it in the app's accent, and no picture of the package stays behind.
    @MainActor @Test func backToABuiltIn() async throws {
        let personas = try await chosen()
        let shared = Shared()
        FleetLook.save(FleetLook(persona: personas.current, builtinAccent: Palette.systemBlue),
                       faces: { FleetFaces(persona: personas.current, personas: personas) }, to: shared.store)
        #expect(shared.has(FleetLookStore.facesAccount))

        let circuit = FleetLook(persona: .circuit, builtinAccent: Palette.coral)
        #expect(FleetLook.save(circuit, faces: { nil }, to: shared.store))
        let read = FleetLook.load(shared.store)
        #expect(read.faceDesign == .circuit && read.package == nil && read.light == 0xD4683F)
        #expect(read.faces(shared.store) == nil)
        #expect(!shared.has(FleetLookStore.facesAccount))
    }

    /// Another package's faces are never drawn for this one; faces that cannot be made leave the package's color.
    @Test func facesBelongToTheirPackage() throws {
        let shared = Shared()
        let a = FleetFaces(package: "a/1", states: ["idle": Self.disc(0x40B2A0)], avatar: nil)
        FleetLook.save(FleetLook(design: "visor", light: 1, dark: 1, package: "a/1"), faces: { a }, to: shared.store)
        let b = FleetLook(design: "visor", light: 0x24418F, dark: 0x24418F, package: "b/1")
        #expect(FleetLook.save(b, faces: { nil }, to: shared.store))
        let read = FleetLook.load(shared.store)
        #expect(read.package == "b/1" && read.light == 0x24418F)
        #expect(read.faces(shared.store) == nil)
    }

    /// Nothing written yet, or a build whose extension cannot read the group: the default face in system blue, the
    /// one the user saw whatever they chose (build 92, 2026-11-02).
    @Test func nothingReadableIsTheDefault() {
        let empty = FleetLookStore(read: { _ in nil }, write: { _, _ in false })
        #expect(FleetLook.load(empty) == .standard)
        #expect(!FleetLook.save(FleetLook(persona: .circuit, builtinAccent: Palette.coral), faces: { nil }, to: empty))
    }

    /// The minimal island (and a limited side) with nothing to count: the persona, not an empty island.
    @MainActor @Test func minimalWithNoCountIsThePersona() throws {
        let thinking = FleetState(phase: .done, assistant: true)
        #expect(IslandCell.minimal(thinking) == .empty)
        let side = IslandMetrics.limitedContent
        let face = try Self.pixel(IslandMinimal(state: thinking, stale: false, limited: true, look: .standard), size: side, at: CGPoint(x: 0.5, y: 0.15))
        #expect(face.alpha > 200)
        #expect(IslandCell.minimal(FleetState(phase: .working, working: 3)) == .working(3))
    }

    static func hex(_ c: Color) -> UInt32 { FleetLook.hex(c, .dark) }

    // MARK: What a phone draws (build 98, 2026-11-02)

    /// A placeholder as busy as a real persona's frame: blocks of a few colors, fixed by a seed.
    static func busy(_ side: Int = 144, block: Int = 14, seed: UInt64 = 7) -> UIImage {
        var rng = seed
        func next() -> UInt64 { rng = rng &* 6364136223846793005 &+ 1442695040888963407; return rng >> 33 }
        let colors: [UIColor] = [.systemTeal, .systemOrange, .systemPurple, .white, .black, .systemYellow]
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        return UIGraphicsImageRenderer(size: CGSize(width: side, height: side), format: format).image { ctx in
            for y in stride(from: 0, to: side, by: block) {
                for x in stride(from: 0, to: side, by: block) {
                    colors[Int(next() % UInt64(colors.count))].setFill()
                    ctx.fill(CGRect(x: x, y: y, width: block, height: block))
                }
            }
        }
    }

    /// Every picture is drawn as 26 points whatever its pixels, and none is over 4 KB: a busy one is made with fewer
    /// pixels. A phone does not draw a Live Activity whose picture is larger than where it shows (a PNG decoded at 1×
    /// is as many points as it has pixels: 78) or over 4 KB; the simulator drew both.
    @Test func facesAreSmallEnoughForAPhone() throws {
        let png = try #require(FleetFaces.png(Self.busy()))
        #expect(png.count <= FleetFaces.maxBytes)
        let image = try #require(FleetFaces.image(png))
        #expect(image.size == CGSize(width: FleetFaces.points, height: FleetFaces.points))
        let cg = try #require(image.cgImage)
        #expect(cg.width < Int(FleetFaces.pixels), "made with fewer pixels")
        #expect(cg.bitsPerComponent == 8)
        // A simple one keeps all of them, and is drawn at the same size.
        let plain = try #require(UIImage(data: Self.disc(0x40B2A0)))
        let disc = try #require(FleetFaces.png(plain))
        let drawn = try #require(FleetFaces.image(disc))
        #expect(drawn.cgImage?.width == Int(FleetFaces.pixels) && drawn.size.width == FleetFaces.points)
        // Noise fits at no size: no picture rather than one a phone would refuse.
        if let noise = FleetFaces.png(Self.busy(block: 1)) { #expect(noise.count <= FleetFaces.maxBytes) }
    }

    /// The extension's report is written again only when what it read changed.
    @Test func reportComparesWhatWasRead() {
        var a = FleetLookReport()
        a.look(errSecSuccess, .standard, bytes: 60)
        var b = a
        b.at = .now
        #expect(a.same(b))
        b.faces(errSecItemNotFound, nil, bytes: nil)
        #expect(!a.same(b))
    }

    /// 灵动岛诊断 names the first step that fails, in the order the look travels from 设置 to the island.
    @Test func diagnosticsNameTheFailingStep() {
        let look = FleetLook(design: "visor", light: 1, dark: 1, package: "a/1")
        var i = IslandDiagnostics.Input(look: look, group: "g")
        i.hub = .some(nil)
        #expect(IslandDiagnostics.verdict(i).contains("hub 没有这台手机的形象"))
        i.hub = .some(.standard)
        #expect(IslandDiagnostics.verdict(i).contains("hub 记下的不是设置里的形象"))
        i.hub = .some(look)
        i.activity = .some(nil)
        #expect(IslandDiagnostics.verdict(i).contains("实时活动内容里不是设置里的形象"))
        i.activity = .some(look)
        i.writes = [FleetLookStore.facesAccount: (.now, errSecMissingEntitlement, 9000)]
        #expect(IslandDiagnostics.verdict(i).contains("签名里没有这个钥匙串组"))
        i.writes = [:]
        #expect(IslandDiagnostics.verdict(i).contains("扩展没留下报告"))
        var seen = FleetLookReport()
        seen.content(.standard)
        i.seen = seen
        #expect(IslandDiagnostics.verdict(i).contains("扩展上次画的不是设置里的形象"))
        seen.content(look)
        seen.faces(errSecItemNotFound, nil, bytes: nil)
        i.seen = seen
        #expect(IslandDiagnostics.verdict(i) == "扩展读不到这个形象包的图（没有这一条），画的是默认形象换成它的颜色")
        seen.faces(errSecSuccess, "a/1", bytes: 12000)
        i.seen = seen
        #expect(IslandDiagnostics.verdict(i) == "扩展画的和设置一致")
        let rows = IslandDiagnostics.sections(i, now: .now)
        #expect(rows.map(\.title) == ["结论", "形象", "App 这次启动写进钥匙串", "钥匙串里", "扩展最近一次"])
        #expect(IslandDiagnostics.text(rows).contains("实时活动的内容里：visor · a/1"))
    }
}
