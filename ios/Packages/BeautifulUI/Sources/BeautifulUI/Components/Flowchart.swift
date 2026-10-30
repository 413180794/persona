import SwiftUI

/// A dropdown value in a condition: its options and the current pick.
public struct FlowChoice: Sendable {
    public struct Option: Hashable, Sendable {
        public var name: String
        public var tag: String?

        public init(_ name: String, tag: String? = nil) {
            self.name = name
            self.tag = tag
        }
    }

    public var options: [Option]
    public var selected: String
    /// Show the amber value dot.
    public var dotted: Bool
    /// Menu width (the original's `w-36`, `w-44`, `w-64`).
    public var menuWidth: CGFloat
    /// Open the menu from the chip's trailing edge.
    public var alignsTrailing: Bool

    public init(_ options: [Option], selected: String, dotted: Bool = false, menuWidth: CGFloat = 144, alignsTrailing: Bool = false) {
        self.options = options
        self.selected = selected
        self.dotted = dotted
        self.menuWidth = menuWidth
        self.alignsTrailing = alignsTrailing
    }
}

/// One clause of an If/Else: `<conjunction> <source> <property> is <value>`.
public struct FlowClause: Identifiable, Sendable {
    public var id: String
    public var conjunction: String
    public var source: String
    public var property: FlowChoice
    public var value: FlowChoice
    /// Put the value on its own line (long values).
    public var valueWraps: Bool

    public init(id: String, conjunction: String, source: String, property: FlowChoice, value: FlowChoice, valueWraps: Bool = false) {
        self.id = id
        self.conjunction = conjunction
        self.source = source
        self.property = property
        self.value = value
        self.valueWraps = valueWraps
    }
}

/// A node on the canvas.
public struct FlowNode: Identifiable, Sendable {
    public enum Body: Sendable {
        case step(title: String, caption: String)
        case condition([FlowClause])
    }

    public var id: String
    public var kind: String
    public var hue: Color
    public var width: CGFloat
    public var body: Body

    public init(id: String, kind: String, hue: Color, width: CGFloat, body: Body) {
        self.id = id
        self.kind = kind
        self.hue = hue
        self.width = width
        self.body = body
    }
}

/// An agent workflow on a dotted canvas: nodes stacked in rows and joined by a curved
/// connector. Drag a card and the connector follows; tap a step to select it and light its
/// connector; condition chips open menus.
public struct Flowchart: View {
    var nodes: [FlowNode]

    @State private var heights: [String: CGFloat] = [:]
    @State private var offsets: [String: CGSize] = [:]
    @State private var dragStart: [String: CGSize] = [:]
    @State private var selected: String?
    @State private var picks: [String: String] = [:]
    @State private var openMenu: String?
    @State private var width: CGFloat = 0
    @Environment(\.buiTheme) private var theme

    static let padY: CGFloat = 24
    static let rowGap: CGFloat = 64
    /// Kind pill plus its gap above a card.
    static let pillOffset: CGFloat = 30

    public init(nodes: [FlowNode], selected: String? = nil) {
        self.nodes = nodes
        _selected = State(initialValue: selected)
    }

    /// Top of each node before dragging, from measured heights.
    nonisolated static func tops(heights: [CGFloat]) -> [CGFloat] {
        var out: [CGFloat] = []
        var y = padY
        for h in heights {
            out.append(y)
            y += h + rowGap
        }
        return out
    }

    /// The connector: a vertical-tangent cubic whose handle length tracks the gap (24–84 pt).
    nonisolated static func connector(from a: CGPoint, to b: CGPoint) -> Path {
        let k = min(max(abs(b.y - a.y) * 0.55, 24), 84)
        var p = Path()
        p.move(to: a)
        p.addCurve(to: b, control1: CGPoint(x: a.x, y: a.y + k), control2: CGPoint(x: b.x, y: b.y - k))
        return p
    }

    public var body: some View {
        let hs = nodes.map { heights[$0.id] ?? 100 }
        let tops = Self.tops(heights: hs)
        let canvasHeight = (tops.last ?? 0) + (hs.last ?? 0) + Self.padY
        let cw = width > 0 ? width : 338
        ZStack(alignment: .topLeading) {
            DotGrid(color: theme.colors.lineStrong)
            Canvas { gc, _ in
                for i in nodes.indices.dropLast() {
                    let a = nodes[i], b = nodes[i + 1]
                    let from = CGPoint(x: cw / 2 + (offsets[a.id]?.width ?? 0), y: tops[i] + hs[i] + (offsets[a.id]?.height ?? 0))
                    let to = CGPoint(x: cw / 2 + (offsets[b.id]?.width ?? 0), y: tops[i + 1] + Self.pillOffset + (offsets[b.id]?.height ?? 0))
                    let lit = selected == a.id || selected == b.id
                    gc.stroke(Self.connector(from: from, to: to), with: .color(lit ? theme.colors.accent : theme.colors.lineStrong), lineWidth: 1.25)
                }
            }
            .allowsHitTesting(false)
            ForEach(Array(nodes.enumerated()), id: \.element.id) { index, node in
                let w = min(node.width, cw * 0.92)
                nodeView(node)
                    .frame(width: w)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { heights[node.id] = $0 }
                    .offset(x: (cw - w) / 2 + (offsets[node.id]?.width ?? 0), y: tops[index] + (offsets[node.id]?.height ?? 0))
                    .zIndex(openMenu?.hasPrefix(node.id) == true ? 2 : 1)
                    .gesture(drag(node.id))
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: canvasHeight)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .background(theme.colors.page)
        .clipShape(.rect(cornerRadius: theme.radii.card, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: theme.radii.card, style: .continuous).strokeBorder(theme.colors.line, lineWidth: 1))
        .animation(BUIMotion.standard, value: selected)
    }

    private func drag(_ id: String) -> some Gesture {
        DragGesture(minimumDistance: 4)
            .onChanged { value in
                let base = dragStart[id] ?? offsets[id] ?? .zero
                if dragStart[id] == nil { dragStart[id] = base }
                offsets[id] = CGSize(width: base.width + value.translation.width, height: base.height + value.translation.height)
            }
            .onEnded { _ in dragStart[id] = nil }
    }

    @ViewBuilder
    private func nodeView(_ node: FlowNode) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(node.kind)
                .buiFont(BUITextStyle(size: 11.5, weight: .medium, lineHeight: 1.19))
                .foregroundStyle(node.hue.mix(with: theme.colors.ink, by: 0.2))
                .padding(.horizontal, 8)
                .frame(height: 24)
                .background(node.hue.mix(with: theme.colors.page, by: 0.86), in: .rect(cornerRadius: 6, style: .continuous))
            switch node.body {
            case .step(let title, let caption):
                let active = selected == node.id
                Button {
                    selected = active ? nil : node.id
                } label: {
                    HStack(spacing: 10) {
                        BUIIcon(Self.cone, size: 16, lineWidth: 1.8)
                            .foregroundStyle(node.hue)
                            .frame(width: 36, height: 36)
                            .background(node.hue.mix(with: theme.colors.surface, by: 0.88), in: .rect(cornerRadius: 8, style: .continuous))
                            .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(node.hue.mix(with: theme.colors.surface, by: 0.8), lineWidth: 1))
                        VStack(alignment: .leading, spacing: 2) {
                            Text(title)
                                .buiFont(BUITextStyle(size: 13, weight: .semibold, lineHeight: 1.25))
                                .foregroundStyle(theme.colors.ink)
                                .lineLimit(1)
                            Text(caption)
                                .buiFont(BUITextStyle(size: 12, lineHeight: 1.375))
                                .foregroundStyle(theme.colors.ink2)
                        }
                        Spacer(minLength: 0)
                    }
                    .padding(10)
                    .background(theme.colors.surface, in: .rect(cornerRadius: 18, style: .continuous))
                    .overlay {
                        RoundedRectangle(cornerRadius: 18, style: .continuous)
                            .strokeBorder(active ? theme.colors.accent : (BUIElevation.card.ring(theme.colors) ?? .clear), lineWidth: active ? 1.5 : 1)
                    }
                    .shadow(color: .black.opacity(0.045), radius: 5, y: 2)
                    .contentShape(.rect(cornerRadius: 18))
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(active ? .isSelected : [])
            case .condition(let clauses):
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(clauses) { clause in clauseRow(clause, node: node) }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .buiSurface(RoundedRectangle(cornerRadius: 18, style: .continuous), elevation: .card)
            }
        }
    }

    private func clauseRow(_ clause: FlowClause, node: FlowNode) -> some View {
        let head = HStack(spacing: 6) {
            DragHandle()
            Text(clause.conjunction)
                .buiFont(BUITextStyle(size: 12.5, lineHeight: 1.19))
                .foregroundStyle(theme.colors.ink2)
                .frame(width: 28, alignment: .leading)
            HStack(spacing: 4) {
                BUIIcon(Self.cone, size: 12, lineWidth: 1.8).foregroundStyle(theme.colors.ink2)
                Text(clause.source)
            }
            .buiFont(BUITextStyle(size: 12, weight: .medium, lineHeight: 1.19))
            .foregroundStyle(theme.colors.ink)
            .padding(.horizontal, 6)
            .frame(height: 24)
            .buiSurface(radius: 6, elevation: .button)
            .fixedSize()
            chip("\(node.id).\(clause.id).property", clause.property)
            Text("is")
                .buiFont(BUITextStyle(size: 12.5, lineHeight: 1.19))
                .foregroundStyle(theme.colors.ink2)
        }
        return VStack(alignment: .leading, spacing: 6) {
            if clause.valueWraps {
                head
                chip("\(node.id).\(clause.id).value", clause.value)
                    .padding(.leading, 49)
            } else {
                HStack(spacing: 6) {
                    head
                    chip("\(node.id).\(clause.id).value", clause.value)
                }
            }
        }
    }

    private func chip(_ key: String, _ choice: FlowChoice) -> some View {
        let value = picks[key] ?? choice.selected
        let open = openMenu == key
        return Button {
            withAnimation(BUICurve.outStrong.animation(duration: 0.18)) { openMenu = open ? nil : key }
        } label: {
            HStack(spacing: 4) {
                if choice.dotted {
                    Circle().fill(Color(hex: 0xF09A2F)).frame(width: 6, height: 6)
                }
                Text(value).lineLimit(1)
                BUIIcon(.chevronDown, size: 11, lineWidth: 2.4).foregroundStyle(theme.colors.ink3)
            }
            .buiFont(BUITextStyle(size: 12, weight: .medium, lineHeight: 1.19))
            .foregroundStyle(theme.colors.ink)
            .padding(.horizontal, 6)
            .frame(height: 24)
            .background(open ? theme.colors.hover2 : theme.colors.field, in: .rect(cornerRadius: 6, style: .continuous))
        }
        .buttonStyle(.plain)
        .overlay(alignment: choice.alignsTrailing ? .topTrailing : .topLeading) {
            if open {
                // Menu sits above the chip: a zero-height slot that it overflows upward from.
                ChoiceMenu(choice: choice, value: value) { name in
                    picks[key] = name
                    withAnimation(BUICurve.outStrong.animation(duration: 0.18)) { openMenu = nil }
                }
                .frame(width: choice.menuWidth)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.bottom, 6)
                .frame(height: 0, alignment: .bottom)
                .transition(.scale(scale: 0.95, anchor: choice.alignsTrailing ? .bottomTrailing : .bottomLeading).combined(with: .opacity))
            }
        }
    }

    static let cone = BUIGlyph("m7 11 4.08 10.35a1 1 0 0 0 1.84 0L17 11M17 7A5 5 0 0 0 7 7M17 7a2 2 0 0 1 0 4H7a2 2 0 0 1 0-4")
}

/// Radial dots every 22 pt in `line-strong`, centred.
private struct DotGrid: View {
    var color: Color

    var body: some View {
        Canvas { gc, size in
            let step: CGFloat = 22
            let ox = (size.width / 2).truncatingRemainder(dividingBy: step)
            let oy = (size.height / 2).truncatingRemainder(dividingBy: step)
            var path = Path()
            var y = oy
            while y < size.height {
                var x = ox
                while x < size.width {
                    path.addEllipse(in: CGRect(x: x - 0.6, y: y - 0.6, width: 1.2, height: 1.2))
                    x += step
                }
                y += step
            }
            gc.fill(path, with: .color(color))
        }
        .allowsHitTesting(false)
    }
}

/// Six-dot grip.
private struct DragHandle: View {
    @Environment(\.buiTheme) private var theme

    var body: some View {
        Canvas { gc, _ in
            for y in [3.0, 8.0, 13.0] {
                for x in [3.0, 7.5] {
                    gc.fill(Path(ellipseIn: CGRect(x: x - 1.1, y: y - 1.1, width: 2.2, height: 2.2)), with: .color(theme.colors.ink3.opacity(0.7)))
                }
            }
        }
        .frame(width: 10, height: 16)
        .accessibilityHidden(true)
    }
}

/// The condition dropdown — same pattern as the Prompt Bar's model menu.
private struct ChoiceMenu: View {
    var choice: FlowChoice
    var value: String
    var onPick: (String) -> Void
    @Environment(\.buiTheme) private var theme

    var body: some View {
        VStack(spacing: 0) {
            ForEach(choice.options, id: \.self) { option in
                Button {
                    onPick(option.name)
                } label: {
                    HStack(spacing: 8) {
                        Text(option.name)
                            .buiFont(BUITextStyle(size: 12.5, weight: .medium, lineHeight: 1.19))
                            .foregroundStyle(theme.colors.ink)
                            .lineLimit(1)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        if let tag = option.tag {
                            Text(tag)
                                .buiFont(BUITextStyle(size: 11, lineHeight: 1.19))
                                .foregroundStyle(theme.colors.ink3)
                        }
                        BUIIcon(.check, size: 13, lineWidth: 2.5)
                            .foregroundStyle(theme.colors.ink)
                            .opacity(option.name == value ? 1 : 0)
                    }
                    .padding(.horizontal, 8)
                    .frame(height: 30)
                    .contentShape(.rect(cornerRadius: 6))
                }
                .buttonStyle(PressHighlightStyle(radius: 6, color: theme.colors.hover))
            }
        }
        .padding(4)
        .buiSurface(radius: theme.radii.card, elevation: .raised)
    }
}
