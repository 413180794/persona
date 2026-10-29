import SwiftUI

/// A machine's OS as a small mark (docs/machines.md「系统图标」): the Apple logo for macOS, a drawn circle of friends
/// for Ubuntu, simple symbols or a letter in the distribution's color for the rest. No images: a few shapes scale to
/// any size and both appearances.
struct OSBadge: View {
    var system: MachineSystem?
    /// The machine's own symbol, for one that never said what it runs.
    var fallback: String = "desktopcomputer"
    var size: CGFloat = 44
    var online = true

    var body: some View {
        let family = system?.family
        let tint = online ? (family.map(Self.color) ?? Theme.accent) : Color.secondary
        ZStack {
            RoundedRectangle(cornerRadius: size * 0.27, style: .continuous)
                .fill(tint.opacity(0.13))
            mark(family, tint: tint)
                .frame(width: size * 0.56, height: size * 0.56)
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true) // the OS name is written next to it everywhere; a card's label starts with the machine
    }

    @ViewBuilder private func mark(_ f: OSFamily?, tint: Color) -> some View {
        switch f {
        case .macos:
            Image(systemName: "apple.logo").resizable().scaledToFit().padding(size * 0.02).foregroundStyle(online ? Color.primary : .secondary)
        case .ubuntu:
            UbuntuMark().foregroundStyle(tint)
        case .arch:
            Image(systemName: "triangle.fill").resizable().scaledToFit().foregroundStyle(tint)
        case .alpine:
            Image(systemName: "mountain.2.fill").resizable().scaledToFit().foregroundStyle(tint)
        case .nixos:
            Image(systemName: "snowflake").resizable().scaledToFit().foregroundStyle(tint)
        case .linux, .none, .other:
            Image(systemName: f == .linux ? "terminal.fill" : fallback).resizable().scaledToFit().foregroundStyle(tint)
        case .some(let f):
            Text(Self.letter(f))
                .font(.system(size: size * 0.5, weight: .heavy, design: .rounded))
                .foregroundStyle(tint)
                .minimumScaleFactor(0.5)
        }
    }

    static func letter(_ f: OSFamily) -> String {
        switch f {
        case .debian: "d"
        case .fedora: "f"
        case .redhat, .rocky: "R"
        case .centos: "C"
        case .alma: "A"
        case .opensuse: "S"
        default: "?"
        }
    }

    /// The distributions' own colors, stepped for each appearance.
    static func color(_ f: OSFamily) -> Color {
        switch f {
        case .macos: Color.primary
        case .ubuntu: Color(light: 0xE95420, dark: 0xF26B3A)
        case .debian: Color(light: 0xA80030, dark: 0xE0245E)
        case .fedora: Color(light: 0x3C6EB4, dark: 0x51A2DA)
        case .redhat: Color(light: 0xCC0000, dark: 0xFF4D4D)
        case .centos: Color(light: 0x932279, dark: 0xC14FA8)
        case .rocky: Color(light: 0x10B981, dark: 0x34D399)
        case .alma: Color(light: 0x0F4266, dark: 0x6CA6D9)
        case .arch: Color(light: 0x1793D1, dark: 0x3BAEE8)
        case .alpine: Color(light: 0x0D597F, dark: 0x5AA9D6)
        case .opensuse: Color(light: 0x4E8A1E, dark: 0x73BA25)
        case .nixos: Color(light: 0x5277C3, dark: 0x7EBAE4)
        case .linux, .other: Theme.accent
        }
    }
}

/// Ubuntu's circle of friends, simplified: a ring broken in three places, a dot at each break.
struct UbuntuMark: View {
    var body: some View {
        Canvas { ctx, size in
            let s = min(size.width, size.height)
            let c = CGPoint(x: size.width / 2, y: size.height / 2)
            let r = s * 0.31, dot = s * 0.13, line = s * 0.13
            for i in 0..<3 {
                let mid = Double(i) * 2 * .pi / 3 - .pi / 2
                var arc = Path()
                arc.addArc(center: c, radius: r, startAngle: .radians(mid + 0.42), endAngle: .radians(mid + 2 * .pi / 3 - 0.42), clockwise: false)
                ctx.stroke(arc, with: .foreground, style: StrokeStyle(lineWidth: line, lineCap: .butt))
                let p = CGPoint(x: c.x + cos(mid) * (r + dot * 0.95), y: c.y + sin(mid) * (r + dot * 0.95))
                ctx.fill(Path(ellipseIn: CGRect(x: p.x - dot, y: p.y - dot, width: dot * 2, height: dot * 2)), with: .foreground)
            }
        }
    }
}
