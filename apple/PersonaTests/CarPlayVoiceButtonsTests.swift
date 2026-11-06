#if canImport(CarPlay) && os(iOS)
import CarPlay
import Foundation
import Testing
@testable import Persona

/// The CarPlay voice states' buttons (CarPlayVoiceButtons): set through the runtime, so every SDK that builds the app
/// builds the buttons in. They used to sit behind `#if compiler(>=6.4)`, which every Xcode 26 toolchain failed.
@MainActor struct CarPlayVoiceButtonsTests {
    private static let carPlay = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "Persona/CarPlay")

    private static func button(_ title: String) -> CPButton {
        let button = CPButton(image: UIImage(), handler: { @Sendable _ in })
        button.title = title
        return button
    }

    @Test func setsTheButtonsOnAVoiceState() throws {
        guard #available(iOS 26.4, *) else { return }
        #expect(CarPlayVoiceButtons.supported)
        let state = CPVoiceControlState(identifier: "speaking", titleVariants: ["Persona 在说"], image: nil, repeats: true)
        #expect(CarPlayVoiceButtons.set([Self.button("打断"), Self.button("结束")], on: state))
        #expect(CarPlayVoiceButtons.buttons(of: state).map(\.title) == ["打断", "结束"])
    }

    @Test func somethingWithoutButtonsIsLeftAlone() {
        let other = NSObject()
        #expect(!CarPlayVoiceButtons.set([Self.button("结束")], on: other))
        #expect(CarPlayVoiceButtons.buttons(of: other).isEmpty)
    }

    /// No compiler-version guard decides whether the car shows buttons (Xcode 26.4–26.6 are Swift 6.3 with the 26.4 SDK).
    @Test func noCompilerGuardAroundTheButtons() throws {
        let files = try FileManager.default.contentsOfDirectory(at: Self.carPlay, includingPropertiesForKeys: nil)
            .filter { $0.pathExtension == "swift" }
        #expect(!files.isEmpty)
        for file in files {
            let code = try String(contentsOf: file, encoding: .utf8).components(separatedBy: "\n")
                .map { l in l.range(of: "//").map { String(l[..<$0.lowerBound]) } ?? l }
            for (n, line) in code.enumerated() {
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                #expect(!trimmed.hasPrefix("#if compiler"), "\(file.lastPathComponent):\(n + 1)")
                #expect(!line.contains(".actionButtons"), "\(file.lastPathComponent):\(n + 1) sets the SDK-only property")
            }
        }
    }
}
#endif
