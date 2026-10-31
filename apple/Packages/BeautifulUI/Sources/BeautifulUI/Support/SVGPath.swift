import Foundation
import Synchronization
import SwiftUI

/// Parses SVG path data (`d` attributes) so components can reuse the original's inline icons
/// verbatim. Supports every path command, including elliptical arcs.
public enum SVGPath {
    private static let cache = Mutex<[String: Path]>([:])

    /// The path in its own coordinate space (the original icons use a 24×24 viewBox).
    public static func path(_ d: String) -> Path {
        if let hit = cache.withLock({ $0[d] }) { return hit }
        let parsed = parse(d)
        cache.withLock { $0[d] = parsed }
        return parsed
    }

    static func parse(_ d: String) -> Path {
        var scanner = Tokens(d)
        var path = Path()
        var current = CGPoint.zero
        var start = CGPoint.zero
        var lastControl: CGPoint?
        var lastCommand: Character = " "
        var command: Character = " "

        while let next = scanner.nextCommandOrNumber() {
            if case .command(let c) = next {
                command = c
            } else {
                scanner.pushBack()
                // Implicit repeat; a repeated moveto becomes lineto.
                if command == "M" { command = "L" } else if command == "m" { command = "l" }
            }
            let relative = command.isLowercase
            func point() -> CGPoint? {
                guard let x = scanner.number(), let y = scanner.number() else { return nil }
                return relative ? CGPoint(x: current.x + x, y: current.y + y) : CGPoint(x: x, y: y)
            }
            switch command.uppercased().first! {
            case "M":
                guard let p = point() else { return path }
                path.move(to: p); current = p; start = p; lastControl = nil
            case "L":
                guard let p = point() else { return path }
                path.addLine(to: p); current = p; lastControl = nil
            case "H":
                guard let x = scanner.number() else { return path }
                current = CGPoint(x: relative ? current.x + x : x, y: current.y)
                path.addLine(to: current); lastControl = nil
            case "V":
                guard let y = scanner.number() else { return path }
                current = CGPoint(x: current.x, y: relative ? current.y + y : y)
                path.addLine(to: current); lastControl = nil
            case "C":
                guard let c1 = point(), let c2 = point(), let p = point() else { return path }
                path.addCurve(to: p, control1: c1, control2: c2); current = p; lastControl = c2
            case "S":
                let c1 = reflect(lastControl, around: current, if: "CScs".contains(lastCommand))
                guard let c2 = point(), let p = point() else { return path }
                path.addCurve(to: p, control1: c1, control2: c2); current = p; lastControl = c2
            case "Q":
                guard let c = point(), let p = point() else { return path }
                path.addQuadCurve(to: p, control: c); current = p; lastControl = c
            case "T":
                let c = reflect(lastControl, around: current, if: "QTqt".contains(lastCommand))
                guard let p = point() else { return path }
                path.addQuadCurve(to: p, control: c); current = p; lastControl = c
            case "A":
                guard let rx = scanner.number(), let ry = scanner.number(), let rotation = scanner.number(),
                      let large = scanner.flag(), let sweep = scanner.flag(), let p = point() else { return path }
                addArc(to: &path, from: current, to: p, rx: rx, ry: ry, rotation: rotation, large: large, sweep: sweep)
                current = p; lastControl = nil
            case "Z":
                path.closeSubpath(); current = start; lastControl = nil
            default:
                return path
            }
            lastCommand = command
        }
        return path
    }

    private static func reflect(_ control: CGPoint?, around p: CGPoint, if valid: Bool) -> CGPoint {
        guard valid, let control else { return p }
        return CGPoint(x: 2 * p.x - control.x, y: 2 * p.y - control.y)
    }

    /// Endpoint-to-centre arc conversion (SVG 1.1 implementation notes, F.6.5), drawn as cubic segments.
    static func addArc(to path: inout Path, from p0: CGPoint, to p1: CGPoint, rx: CGFloat, ry: CGFloat, rotation: CGFloat, large: Bool, sweep: Bool) {
        var rx = abs(rx), ry = abs(ry)
        guard rx > 0, ry > 0, p0 != p1 else { path.addLine(to: p1); return }
        let phi = rotation * .pi / 180
        let cosPhi = cos(phi), sinPhi = sin(phi)
        let dx = (p0.x - p1.x) / 2, dy = (p0.y - p1.y) / 2
        let x1 = cosPhi * dx + sinPhi * dy
        let y1 = -sinPhi * dx + cosPhi * dy
        let lambda = (x1 * x1) / (rx * rx) + (y1 * y1) / (ry * ry)
        if lambda > 1 { rx *= lambda.squareRoot(); ry *= lambda.squareRoot() }
        let num = rx * rx * ry * ry - rx * rx * y1 * y1 - ry * ry * x1 * x1
        let den = rx * rx * y1 * y1 + ry * ry * x1 * x1
        var coef = (max(0, num) / den).squareRoot()
        if large == sweep { coef = -coef }
        let cx1 = coef * rx * y1 / ry
        let cy1 = -coef * ry * x1 / rx
        let cx = cosPhi * cx1 - sinPhi * cy1 + (p0.x + p1.x) / 2
        let cy = sinPhi * cx1 + cosPhi * cy1 + (p0.y + p1.y) / 2
        func angle(_ ux: CGFloat, _ uy: CGFloat, _ vx: CGFloat, _ vy: CGFloat) -> CGFloat {
            let a = atan2(ux * vy - uy * vx, ux * vx + uy * vy)
            return a
        }
        let theta1 = angle(1, 0, (x1 - cx1) / rx, (y1 - cy1) / ry)
        var delta = angle((x1 - cx1) / rx, (y1 - cy1) / ry, (-x1 - cx1) / rx, (-y1 - cy1) / ry)
        if !sweep && delta > 0 { delta -= 2 * .pi } else if sweep && delta < 0 { delta += 2 * .pi }

        let segments = Int(ceil(abs(delta) / (.pi / 2)))
        let step = delta / CGFloat(segments)
        let t = 4 / 3 * tan(step / 4)
        var theta = theta1
        func pointAt(_ a: CGFloat) -> CGPoint {
            CGPoint(x: cx + rx * cos(a) * cosPhi - ry * sin(a) * sinPhi, y: cy + rx * cos(a) * sinPhi + ry * sin(a) * cosPhi)
        }
        func derivative(_ a: CGFloat) -> CGPoint {
            CGPoint(x: -rx * sin(a) * cosPhi - ry * cos(a) * sinPhi, y: -rx * sin(a) * sinPhi + ry * cos(a) * cosPhi)
        }
        for _ in 0..<segments {
            let a = pointAt(theta), b = pointAt(theta + step)
            let da = derivative(theta), db = derivative(theta + step)
            path.addCurve(
                to: b,
                control1: CGPoint(x: a.x + t * da.x, y: a.y + t * da.y),
                control2: CGPoint(x: b.x - t * db.x, y: b.y - t * db.y)
            )
            theta += step
        }
    }

    private struct Tokens {
        enum Token { case command(Character), number }
        let chars: [Character]
        var index = 0
        var lastStart = 0

        init(_ s: String) { chars = Array(s) }

        mutating func skipSeparators() {
            while index < chars.count, chars[index] == " " || chars[index] == "," || chars[index].isNewline || chars[index] == "\t" {
                index += 1
            }
        }

        mutating func nextCommandOrNumber() -> Token? {
            skipSeparators()
            guard index < chars.count else { return nil }
            lastStart = index
            let c = chars[index]
            if c.isLetter && c != "e" && c != "E" {
                index += 1
                return .command(c)
            }
            return .number
        }

        mutating func pushBack() { index = lastStart }

        mutating func number() -> CGFloat? {
            skipSeparators()
            var s = ""
            var seenDot = false, seenExp = false
            while index < chars.count {
                let c = chars[index]
                if c.isNumber {
                    s.append(c)
                } else if (c == "-" || c == "+") && (s.isEmpty || s.last == "e" || s.last == "E") {
                    s.append(c)
                } else if c == "." && !seenDot && !seenExp {
                    seenDot = true; s.append(c)
                } else if (c == "e" || c == "E") && !seenExp && !s.isEmpty {
                    seenExp = true; s.append(c)
                } else {
                    break
                }
                index += 1
            }
            return Double(s).map { CGFloat($0) }
        }

        /// Arc flags may be written without separators ("a1 1 0 01 2 2").
        mutating func flag() -> Bool? {
            skipSeparators()
            guard index < chars.count, chars[index] == "0" || chars[index] == "1" else { return nil }
            defer { index += 1 }
            return chars[index] == "1"
        }
    }
}
