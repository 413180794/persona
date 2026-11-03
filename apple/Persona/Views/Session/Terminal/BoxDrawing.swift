import CoreGraphics

/// Box-drawing and block characters drawn as lines and rects filling their cell, like terminals do: font glyphs for them
/// rarely span the cell exactly (fallback fonts are narrower), and frames then show as dashes with gaps.
nonisolated enum BoxDrawing {
    /// Arms from the cell's center: 0 none, 1 light, 2 heavy, 3 double.
    struct Arms { var up = 0, right = 0, down = 0, left = 0; var rounded = false }

    static func arms(_ v: UInt32) -> Arms? {
        switch v {
        case 0x2500, 0x2504, 0x2508, 0x254C: return Arms(right: 1, left: 1)
        case 0x2501, 0x2505, 0x2509, 0x254D: return Arms(right: 2, left: 2)
        case 0x2502, 0x2506, 0x250A, 0x254E: return Arms(up: 1, down: 1)
        case 0x2503, 0x2507, 0x250B, 0x254F: return Arms(up: 2, down: 2)
        case 0x250C: return Arms(right: 1, down: 1)
        case 0x2510: return Arms(down: 1, left: 1)
        case 0x2514: return Arms(up: 1, right: 1)
        case 0x2518: return Arms(up: 1, left: 1)
        case 0x256D: return Arms(right: 1, down: 1, rounded: true)
        case 0x256E: return Arms(down: 1, left: 1, rounded: true)
        case 0x2570: return Arms(up: 1, right: 1, rounded: true)
        case 0x256F: return Arms(up: 1, left: 1, rounded: true)
        case 0x251C: return Arms(up: 1, right: 1, down: 1)
        case 0x2524: return Arms(up: 1, down: 1, left: 1)
        case 0x252C: return Arms(right: 1, down: 1, left: 1)
        case 0x2534: return Arms(up: 1, right: 1, left: 1)
        case 0x253C: return Arms(up: 1, right: 1, down: 1, left: 1)
        case 0x250F: return Arms(right: 2, down: 2)
        case 0x2513: return Arms(down: 2, left: 2)
        case 0x2517: return Arms(up: 2, right: 2)
        case 0x251B: return Arms(up: 2, left: 2)
        case 0x2523: return Arms(up: 2, right: 2, down: 2)
        case 0x252B: return Arms(up: 2, down: 2, left: 2)
        case 0x2533: return Arms(right: 2, down: 2, left: 2)
        case 0x253B: return Arms(up: 2, right: 2, left: 2)
        case 0x254B: return Arms(up: 2, right: 2, down: 2, left: 2)
        case 0x2550: return Arms(right: 3, left: 3)
        case 0x2551: return Arms(up: 3, down: 3)
        case 0x2554: return Arms(right: 3, down: 3)
        case 0x2557: return Arms(down: 3, left: 3)
        case 0x255A: return Arms(up: 3, right: 3)
        case 0x255D: return Arms(up: 3, left: 3)
        case 0x2560: return Arms(up: 3, right: 3, down: 3)
        case 0x2563: return Arms(up: 3, down: 3, left: 3)
        case 0x2566: return Arms(right: 3, down: 3, left: 3)
        case 0x2569: return Arms(up: 3, right: 3, left: 3)
        case 0x256C: return Arms(up: 3, right: 3, down: 3, left: 3)
        case 0x2574: return Arms(left: 1)
        case 0x2575: return Arms(up: 1)
        case 0x2576: return Arms(right: 1)
        case 0x2577: return Arms(down: 1)
        default: return nil
        }
    }

    /// A block element as a part of the cell and how much of the color: █ ▀ ▄ ▌ ▐ ░ ▒ ▓ ▁…▇.
    static func block(_ v: UInt32, in r: CGRect) -> (CGRect, CGFloat)? {
        switch v {
        case 0x2588: return (r, 1)
        case 0x2580: return (CGRect(x: r.minX, y: r.minY, width: r.width, height: r.height / 2), 1)
        case 0x2584: return (CGRect(x: r.minX, y: r.midY, width: r.width, height: r.height / 2), 1)
        case 0x258C: return (CGRect(x: r.minX, y: r.minY, width: r.width / 2, height: r.height), 1)
        case 0x2590: return (CGRect(x: r.midX, y: r.minY, width: r.width / 2, height: r.height), 1)
        case 0x2591: return (r, 0.25)
        case 0x2592: return (r, 0.5)
        case 0x2593: return (r, 0.75)
        case 0x2581...0x2587:
            let h = r.height * CGFloat(v - 0x2580) / 8
            return (CGRect(x: r.minX, y: r.maxY - h, width: r.width, height: h), 1)
        default: return nil
        }
    }

    /// The strokes of a box-drawing character in the cell rect: (path, line width).
    static func strokes(_ a: Arms, in r: CGRect, light t: CGFloat) -> [(CGPath, CGFloat)] {
        let cx = (r.midX * 2).rounded() / 2, cy = (r.midY * 2).rounded() / 2
        var out: [(CGPath, CGFloat)] = []
        if a.rounded {
            let p = CGMutablePath()
            let rad = min(r.width, r.height) / 2
            let vEnd = a.up > 0 ? r.minY : r.maxY
            let hEnd = a.right > 0 ? r.maxX : r.minX
            p.move(to: CGPoint(x: cx, y: vEnd))
            p.addLine(to: CGPoint(x: cx, y: cy + (a.up > 0 ? -rad : rad)))
            p.addQuadCurve(to: CGPoint(x: cx + (a.right > 0 ? rad : -rad), y: cy), control: CGPoint(x: cx, y: cy))
            p.addLine(to: CGPoint(x: hEnd, y: cy))
            return [(p, t)]
        }
        func arm(_ kind: Int, from: CGPoint, to: CGPoint, vertical: Bool) {
            guard kind > 0 else { return }
            if kind == 3 {
                for d in [-t, t] {
                    let p = CGMutablePath()
                    p.move(to: vertical ? CGPoint(x: from.x + d, y: from.y) : CGPoint(x: from.x, y: from.y + d))
                    p.addLine(to: vertical ? CGPoint(x: to.x + d, y: to.y) : CGPoint(x: to.x, y: to.y + d))
                    out.append((p, t))
                }
                return
            }
            let p = CGMutablePath()
            p.move(to: from)
            p.addLine(to: to)
            out.append((p, kind == 2 ? t * 2 : t))
        }
        // Each arm runs past the center by half a line, so corners close.
        let ext = t
        arm(a.up, from: CGPoint(x: cx, y: r.minY), to: CGPoint(x: cx, y: cy + ext), vertical: true)
        arm(a.down, from: CGPoint(x: cx, y: cy - ext), to: CGPoint(x: cx, y: r.maxY), vertical: true)
        arm(a.left, from: CGPoint(x: r.minX, y: cy), to: CGPoint(x: cx + ext, y: cy), vertical: false)
        arm(a.right, from: CGPoint(x: cx - ext, y: cy), to: CGPoint(x: r.maxX, y: cy), vertical: false)
        // One path per width: stroked once, overlapping arms are not painted twice (a dim color would show the seams).
        var merged: [CGFloat: CGMutablePath] = [:]
        for (p, w) in out {
            let path = merged[w] ?? CGMutablePath() // a class: `merged[w, default:]` would never store it
            path.addPath(p)
            merged[w] = path
        }
        return merged.map { ($0.value, $0.key) }
    }
}
