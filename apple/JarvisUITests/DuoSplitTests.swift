import XCTest

/// iPhone Duo open (regular width): the Agents list is the split's sidebar and stays put while the agents it opens
/// replace each other in the detail column (docs/ios.md「iPhone Duo」). Needs an unfolded iPhone Duo simulator and
/// scripts/mock_hub.py:
///   TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<port> xcodebuild test … -only-testing:JarvisUITests/DuoSplitTests
final class DuoSplitTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-dev.openChat", "", "-tab", "dev", "-openAgentsFresh", "YES"]
        app.launch()
        guard app.windows.firstMatch.frame.width > 600 else { throw XCTSkip("not a split: needs iPhone Duo open") }
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let a = XCTAttachment(screenshot: app.screenshot())
            a.lifetime = .keepAlways
            add(a)
        }
        super.tearDown()
    }

    /// The sidebar's list: the collection view at the leading edge.
    private var sidebar: XCUIElement {
        app.collectionViews.allElementsBoundByIndex.first { $0.frame.minX < 40 && $0.frame.width < app.windows.firstMatch.frame.width * 0.7 }
            ?? app.collectionViews.firstMatch
    }

    /// Agent rows in the sidebar, top to bottom.
    private func rows() -> [XCUIElement] {
        let list = sidebar.frame
        return sidebar.cells.allElementsBoundByIndex
            .filter { $0.isHittable && $0.frame.height > 40 && $0.frame.maxY < list.maxY - 20 && $0.frame.minY > list.minY + 60 }
    }

    func testSwitchingAgentsKeepsTheSidebarStill() {
        XCTAssertTrue(app.collectionViews.firstMatch.waitForExistence(timeout: 15))
        sleep(2)
        let candidates = rows()
        guard candidates.count >= 2 else {
            print("SIDEBAR TREE", sidebar.debugDescription.prefix(6000))
            return XCTFail("fewer than two agent rows in the sidebar")
        }
        print("DUO ROWS", candidates.map { "\($0.label) @\(Int($0.frame.minX)),\(Int($0.frame.minY)) \(Int($0.frame.width))x\(Int($0.frame.height))" })
        print("DUO WINDOW", app.windows.firstMatch.frame, "SIDEBAR", sidebar.frame)
        candidates[0].tap()
        sleep(2)
        print("DUO AFTER FIRST", app.staticTexts.allElementsBoundByIndex.filter { $0.frame.minX > sidebar.frame.maxX }.prefix(8).map { $0.label })
        let still = sidebar.frame

        // The switch: shoot the screen through the transition and compare the sidebar's pixels with the still frame
        // (XCUITest's frames do not follow the columns' animation).
        let before = app.screenshot().image
        rows().first { $0.frame.minY > candidates[0].frame.maxY - 1 }?.tap() ?? candidates[1].tap()
        var frames: [UIImage] = []
        let end = Date.now.addingTimeInterval(1.5)
        while Date.now < end { frames.append(app.screenshot().image) }
        print("DUO AFTER SECOND", app.staticTexts.allElementsBoundByIndex.filter { $0.frame.minX > sidebar.frame.maxX }.prefix(8).map { $0.label })
        let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"]
        if let dir {
            try? before.pngData()?.write(to: URL(fileURLWithPath: dir).appendingPathComponent("duo-0.png"))
            for (i, f) in frames.enumerated() {
                try? f.pngData()?.write(to: URL(fileURLWithPath: dir).appendingPathComponent("duo-\(i + 1).png"))
            }
        }
        let side = CGRect(x: still.minX + 8, y: still.minY + 120, width: still.width - 16, height: min(still.height - 240, 500))
        let base = Self.luma(before, in: side)
        let worst = frames.map { Self.difference(base, Self.luma($0, in: side)) }.max() ?? 0
        XCTAssertLessThan(worst, 0.05, "the sidebar changed (\(worst) of its pixels) while switching agents, \(frames.count) frames")
    }

    /// Grey levels of a region (points), sampled every 4 points.
    private static func luma(_ image: UIImage, in rect: CGRect) -> [UInt8] {
        guard let cg = image.cgImage else { return [] }
        let scale = CGFloat(cg.width) / image.size.width
        let w = Int(image.size.width / 4), h = Int(image.size.height / 4)
        var px = [UInt8](repeating: 0, count: w * h)
        let ctx = CGContext(data: &px, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w, space: CGColorSpaceCreateDeviceGray(),
                            bitmapInfo: CGImageAlphaInfo.none.rawValue)!
        ctx.draw(cg, in: CGRect(x: 0, y: 0, width: w, height: h))
        _ = scale
        var out: [UInt8] = []
        for y in Int(rect.minY / 4)..<min(h, Int(rect.maxY / 4)) {
            for x in Int(rect.minX / 4)..<min(w, Int(rect.maxX / 4)) { out.append(px[y * w + x]) }
        }
        return out
    }

    /// The share of samples that differ by more than a little.
    private static func difference(_ a: [UInt8], _ b: [UInt8]) -> Double {
        guard a.count == b.count, !a.isEmpty else { return 1 }
        return Double(zip(a, b).filter { abs(Int($0) - Int($1)) > 24 }.count) / Double(a.count)
    }
}
