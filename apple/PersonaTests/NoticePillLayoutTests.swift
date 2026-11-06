import SwiftUI
import Testing
@testable import Persona

/// Render the actual notice row: its capsule begins at the row's leading edge, but does not stretch a short label.
@MainActor struct NoticePillLayoutTests {
    @Test(arguments: [ColorScheme.light, .dark])
    func wakeCapsulesAreLeadingAndContentSized(scheme: ColorScheme) throws {
        let summaries = ["Monitor 事件 · == upload", "Monitor 事件 · uploaded 0.1.0 (2)",
                         "Monitor 事件 · " + String(repeating: "upload validation completed successfully; ", count: 5)]
        var previousEnd = 0
        for (index, summary) in summaries.enumerated() {
            var item = Item(id: "notice-\(index)", seq: index, ts: nil, role: "system", kind: .notice, text: summary,
                            toolUseID: nil, toolName: nil, toolInput: nil, isError: nil, image: nil)
            item.wake = true
            let renderer = ImageRenderer(content: NoticeRow(item: item)
                .frame(width: 360)
                .environment(\.colorScheme, scheme)
                .background(scheme == .light ? Color.white : Color.black))
            renderer.scale = 2
            let image = try #require(renderer.cgImage)
            var pixels = [UInt8](repeating: 0, count: image.width * image.height * 4)
            let context = try #require(CGContext(data: &pixels, width: image.width, height: image.height,
                bitsPerComponent: 8, bytesPerRow: image.width * 4, space: CGColorSpaceCreateDeviceRGB(),
                bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
            context.draw(image, in: CGRect(x: 0, y: 0, width: image.width, height: image.height))
            let y = image.height / 2
            let painted = (0..<image.width).filter { x in
                let offset = (y * image.width + x) * 4
                let rgb = pixels[offset..<(offset + 3)]
                return scheme == .light ? rgb.contains { $0 < 250 } : rgb.contains { $0 > 5 }
            }
            let first = try #require(painted.first)
            let last = try #require(painted.last)
            #expect(first <= 2, "capsule starts at \(first) px: \(summary) (\(scheme))")
            if index < 2 {
                #expect(last < image.width - 40, "a short capsule stretched to fill the row")
            } else {
                // Truncation can fit to a narrower glyph boundary; content-sized does not mean full-width.
                #expect(last > previousEnd, "the long capsule should be wider than the short labels")
            }
            previousEnd = last
        }
    }
}
