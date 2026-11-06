import SwiftUI
import Testing
@testable import Persona

/// The task dock's pills stay inside their own frames (#3010): the dock's horizontal ScrollView offers no width, and a
/// pill whose cap was a flexible frame outside its capsule took its title's full width, so the capsule spilled over
/// the pills beside it. Here the pill is laid out the way the ScrollView lays it out (`fixedSize()`), between two
/// transparent gutters on white: nothing may be drawn in the gutters.
@MainActor struct DockPillLayoutTests {
    private static let long = "Workflow 支持：hub / 网页 / 安卓 / Apple 端都能看到后台工作流的状态和结果"
    private static let gutter: CGFloat = 60

    @Test(arguments: [DynamicTypeSize.large, .xxxLarge, .accessibility3], [false, true])
    func longTitleStaysInsideItsFrame(size: DynamicTypeSize, waiting: Bool) throws {
        let pill = DockPillLabel(title: Self.long, state: .needsYou, waiting: waiting) {
            Circle().fill(Theme.caution).frame(width: Theme.Size.dot, height: Theme.Size.dot)
        }
        let row = HStack(spacing: 0) {
            Color.clear.frame(width: Self.gutter)
            pill.fixedSize()
            Color.clear.frame(width: Self.gutter)
        }
        .environment(\.dynamicTypeSize, size)
        .environment(\.colorScheme, .light)
        let px = try Pixels(row.background(Color.white))
        let edge = Int(Self.gutter * px.scale)
        let mid = px.height / 2
        for x in 0..<(edge - 1) {
            #expect(px.isWhite(x: x, y: mid), "drawn in the leading gutter at \(x) px (\(size), waiting \(waiting))")
            #expect(px.isWhite(x: px.width - 1 - x, y: mid), "drawn in the trailing gutter at \(x) px (\(size), waiting \(waiting))")
        }
        // The pill itself is drawn and capped: its width (between the gutters) is at most the scaled cap.
        let pillWidth = CGFloat(px.width - 2 * edge) / px.scale
        #expect(pillWidth <= Theme.Size.box200 * Theme.sizeScale(size) + 0.5, "\(pillWidth) pt wide at \(size)")
        #expect(!px.isWhite(x: edge + 2, y: mid), "no capsule at the pill's leading edge")
    }

    /// A short title keeps its own width: the cap offers the most, it does not stretch.
    @Test func shortTitleKeepsItsWidth() throws {
        let pill = DockPillLabel(title: "Apple", state: .working, waiting: false) { EmptyView() }
        let capped = try Pixels(pill.fixedSize().background(Color.white))
        let free = try Pixels(HStack(spacing: Theme.Space.sm) { Text("Apple").lineLimit(1) }
            .font(Theme.Font.captionStrong)
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.sm)
            .fixedSize()
            .background(Color.white))
        #expect(capped.width == free.width)
    }

    @Test func capOffersTheNarrowerWidth() {
        let cap = WidthCap(width: 200)
        #expect(cap.offer(.unspecified).width == 200)
        #expect(cap.offer(ProposedViewSize(width: .infinity, height: nil)).width == 200)
        #expect(cap.offer(ProposedViewSize(width: 120, height: 30)) == ProposedViewSize(width: 120, height: 30))
        #expect(cap.offer(ProposedViewSize(width: 300, height: nil)).width == 200)
    }
}

/// A rendered view's RGBA pixels.
private struct Pixels {
    let width: Int, height: Int, scale: CGFloat
    private let data: [UInt8]

    @MainActor init(_ view: some View) throws {
        let r = ImageRenderer(content: view)
        r.scale = 2
        let image = try #require(r.cgImage)
        width = image.width; height = image.height; scale = r.scale
        var buf = [UInt8](repeating: 0, count: width * height * 4)
        let ctx = try #require(CGContext(data: &buf, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
                                         space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        ctx.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
        data = buf
    }

    func isWhite(x: Int, y: Int) -> Bool {
        let i = (y * width + x) * 4
        return data[i] >= 250 && data[i + 1] >= 250 && data[i + 2] >= 250
    }
}
