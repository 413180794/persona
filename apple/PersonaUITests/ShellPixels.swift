import XCTest

extension XCUIApplication {
    /// Whether the shell's Agents page is what is drawn: its selected 全部 chip (an accent-tinted capsule) is on screen
    /// where it is laid out. Both pages stay built and the hidden one stays in the accessibility tree (its glass and
    /// lists are UIKit), so `exists` / `isHittable` say nothing about which page shows; only the pixels do.
    func agentsDrawn() -> Bool {
        let chip = buttons["agents.filter.all"].firstMatch
        guard chip.waitForExistence(timeout: 3) else { return false }
        let frame = chip.frame
        guard let cg = screenshot().image.cgImage else { return false }
        let scale = CGFloat(cg.width) / windows.firstMatch.frame.width
        let r = frame.insetBy(dx: 4, dy: 4)
        let w = Int(r.width * scale), h = Int(r.height * scale)
        guard w > 0, h > 0 else { return false }
        var px = [UInt8](repeating: 0, count: w * h * 4)
        guard let ctx = CGContext(data: &px, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w * 4,
                                  space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
        else { return false }
        ctx.draw(cg, in: CGRect(x: -r.minX * scale, y: -(CGFloat(cg.height) - r.maxY * scale), width: CGFloat(cg.width), height: CGFloat(cg.height)))
        var tinted = 0
        for i in stride(from: 0, to: px.count, by: 4) where Int(px[i + 2]) - Int(px[i]) > 20 { tinted += 1 }
        return tinted * 2 > w * h
    }

    /// Waits (the pages slide and fade) until Agents is drawn, or is not.
    func waitAgentsDrawn(_ shown: Bool) -> Bool {
        var now = agentsDrawn()
        for _ in 0..<10 where now != shown {
            usleep(300_000)
            now = agentsDrawn()
        }
        return now == shown
    }
}
