import SwiftUI

/// A scrubbable number property.
public struct FineTuneField: Identifiable, Sendable {
    public var id: String
    public var label: String
    public var value: Int
    public var range: ClosedRange<Int>
    public var step: Int
    public var suffix: String?

    public init(id: String, label: String, value: Int, range: ClosedRange<Int>, step: Int = 1, suffix: String? = nil) {
        self.id = id
        self.label = label
        self.value = value
        self.range = range
        self.step = step
        self.suffix = suffix
    }

    /// The value after dragging `dx` points from `start` (1 step per 2 pt), clamped. Rounds
    /// halves up like the original's `Math.round`.
    public func scrubbed(from start: Int, dx: CGFloat) -> Int {
        clamp(Int((Double(start) + Double(dx) / 2 * Double(step) + 0.5).rounded(.down)))
    }

    public func clamp(_ v: Int) -> Int { min(range.upperBound, max(range.lowerBound, v)) }
}

/// The agent adjusts design properties in an inspector: a layout segmented control, number
/// fields that scrub (drag the label) or take typing, and a type menu. Edited fields light
/// up, and the header turns from "Adjust" to "Edited".
public struct FineTuneCard: View {
    public enum Layout: Int, CaseIterable, Sendable { case row, column, grid }

    var title: String
    var fields: [FineTuneField]
    var types: [String]
    var placeholder: String
    var onChange: ((Layout, [String: Int], String?) -> Void)?

    @State private var layout: Layout
    @State private var values: [String: Int]
    @State private var type: String?
    @State private var menuOpen: Bool
    @Environment(\.buiTheme) private var theme

    public init(
        title: String,
        fields: [FineTuneField],
        types: [String],
        placeholder: String = "Select type",
        layout: Layout = .row,
        values: [String: Int] = [:],
        type: String? = nil,
        showsTypeMenu: Bool = false,
        onChange: ((Layout, [String: Int], String?) -> Void)? = nil
    ) {
        self.title = title
        self.fields = fields
        self.types = types
        self.placeholder = placeholder
        self.onChange = onChange
        _layout = State(initialValue: layout)
        _values = State(initialValue: Dictionary(uniqueKeysWithValues: fields.map { ($0.id, values[$0.id] ?? $0.value) }))
        _type = State(initialValue: type)
        _menuOpen = State(initialValue: showsTypeMenu)
    }

    private var edited: Bool {
        layout != .row || type != nil || fields.contains { values[$0.id] != $0.value }
    }

    public var body: some View {
        VStack(spacing: 0) {
            header
            VStack(alignment: .leading, spacing: 8) {
                Text("Layout")
                    .buiFont(BUITextStyle(size: 12.5, weight: .medium, lineHeight: 1.19))
                    .foregroundStyle(theme.colors.ink)
                segmented
                ForEach(Array(stride(from: 0, to: fields.count, by: 2)), id: \.self) { i in
                    HStack(spacing: 8) {
                        ForEach(fields[i..<min(i + 2, fields.count)]) { field in
                            ScrubField(field: field, value: Binding(
                                get: { values[field.id] ?? field.value },
                                set: { values[field.id] = $0; notify() }
                            ))
                        }
                    }
                }
            }
            .padding(12)
            .overlay(alignment: .bottom) { Rectangle().fill(theme.colors.line).frame(height: 1) }
            footer
        }
        .frame(maxWidth: 240)
        .buiSurface(radius: theme.radii.card, elevation: .raised)
        .overlay(alignment: .bottomTrailing) {
            if menuOpen {
                typeMenu
                    .padding(.trailing, 8)
                    .padding(.bottom, 44)
                    .transition(.scale(scale: 0.95, anchor: .bottomTrailing).combined(with: .opacity))
            }
        }
        .animation(BUICurve.outStrong.animation(duration: 0.2), value: menuOpen)
    }

    private var header: some View {
        HStack {
            Text(title)
                .buiFont(BUITextStyle(size: 13, weight: .medium, lineHeight: 1.19))
                .foregroundStyle(theme.colors.ink)
            Spacer()
            if edited {
                HStack(spacing: 6) {
                    BUIIcon(.check, size: 10, lineWidth: 3)
                    Text("Edited")
                }
                .buiFont(BUITextStyle(size: 12, weight: .medium, lineHeight: 1.19))
                .foregroundStyle(theme.colors.green)
                .buiEntrance(.popIn, duration: 0.25)
            } else {
                HStack(spacing: 6) {
                    BUIIcon(.sparkle, size: 9)
                        .foregroundStyle(theme.colors.accent)
                        .frame(width: 18, height: 18)
                        .background(theme.colors.accentTint, in: .rect(cornerRadius: 5))
                        .overlay(RoundedRectangle(cornerRadius: 5).strokeBorder(theme.colors.accent.opacity(0.3), lineWidth: 1))
                    Text("Adjust")
                        .buiFont(BUITextStyle(size: 12, weight: .medium, lineHeight: 1.19))
                        .buiShimmer(base: theme.colors.accent, highlight: theme.colors.accentInk)
                }
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .overlay(alignment: .bottom) { Rectangle().fill(theme.colors.line).frame(height: 1) }
    }

    private var segmented: some View {
        GeometryReader { proxy in
            let w = (proxy.size.width - 4) / 3
            ZStack(alignment: .leading) {
                RoundedRectangle(cornerRadius: 6, style: .continuous)
                    .fill(theme.colors.surface)
                    .overlay(RoundedRectangle(cornerRadius: 6, style: .continuous).strokeBorder(BUIElevation.button.ring(theme.colors) ?? .clear, lineWidth: 1))
                    .frame(width: w, height: 24)
                    .offset(x: 2 + w * CGFloat(layout.rawValue))
                HStack(spacing: 0) {
                    ForEach(Layout.allCases, id: \.self) { kind in
                        Button {
                            withAnimation(BUICurve.outStrong.animation(duration: 0.3)) { layout = kind }
                            notify()
                        } label: {
                            LayoutGlyph(kind: kind)
                                .foregroundStyle(kind == layout ? theme.colors.accent : theme.colors.ink3)
                                .frame(width: w, height: 24)
                                .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(["Row", "Column", "Grid"][kind.rawValue])
                        .accessibilityAddTraits(kind == layout ? .isSelected : [])
                    }
                }
                .padding(.horizontal, 2)
            }
        }
        .frame(height: 28)
        .background(theme.colors.field, in: .rect(cornerRadius: theme.radii.control, style: .continuous))
    }

    private var footer: some View {
        HStack {
            Text("Type")
                .buiFont(BUITextStyle(size: 12, lineHeight: 1.19))
                .foregroundStyle(theme.colors.ink3)
            Spacer()
            Button {
                menuOpen.toggle()
            } label: {
                HStack {
                    Text(type ?? placeholder)
                        .buiFont(BUITextStyle(size: 12, lineHeight: 1.19))
                        .foregroundStyle(type == nil ? theme.colors.ink3 : theme.colors.ink)
                    Spacer(minLength: 4)
                    BUIIcon(.chevronDown, size: 11, lineWidth: 2.5)
                        .foregroundStyle(theme.colors.ink3)
                        .rotationEffect(.degrees(menuOpen ? 180 : 0))
                }
                .padding(.leading, 8)
                .padding(.trailing, 4)
                .frame(width: 120, height: 26)
                .background(theme.colors.inset, in: .rect(cornerRadius: theme.radii.chip))
                .overlay(RoundedRectangle(cornerRadius: theme.radii.chip).strokeBorder(menuOpen ? theme.colors.accent : theme.colors.line, lineWidth: 1))
            }
            .buttonStyle(.plain)
            .padding(.trailing, -2)
        }
        .padding(10)
    }

    private var typeMenu: some View {
        VStack(spacing: 1) {
            ForEach(types, id: \.self) { item in
                Button {
                    type = item
                    menuOpen = false
                    notify()
                } label: {
                    Text(item)
                        .buiFont(BUITextStyle(size: 12.5, lineHeight: 1.19))
                        .foregroundStyle(theme.colors.ink)
                        .padding(.horizontal, 8)
                        .frame(maxWidth: .infinity, minHeight: 26, alignment: .leading)
                        .background(item == type ? theme.colors.field : .clear, in: .rect(cornerRadius: 6))
                        .contentShape(.rect(cornerRadius: 6))
                }
                .buttonStyle(PressHighlightStyle(radius: 6, color: theme.colors.field))
            }
        }
        .padding(4)
        .frame(width: 120)
        .buiSurface(radius: theme.radii.card, elevation: .raised)
    }

    private func notify() {
        onChange?(layout, values, type)
    }
}

/// A number field whose label scrubs: drag sideways to change the value; tap the number to type.
private struct ScrubField: View {
    var field: FineTuneField
    @Binding var value: Int
    @State private var dragStart: Int?
    @Environment(\.buiTheme) private var theme

    var body: some View {
        let active = value != field.value
        HStack(spacing: 4) {
            Text(field.label)
                .buiFont(BUITextStyle(size: 12, lineHeight: 1.19))
                .foregroundStyle(dragStart == nil ? theme.colors.ink3 : theme.colors.accentInk)
                .fixedSize()
                .padding(.horizontal, 2)
                .frame(maxHeight: .infinity)
                .contentShape(.rect)
                .gesture(
                    DragGesture(minimumDistance: 1)
                        .onChanged { g in
                            if dragStart == nil { dragStart = value }
                            value = field.scrubbed(from: dragStart ?? value, dx: g.translation.width)
                        }
                        .onEnded { _ in dragStart = nil }
                )
                .accessibilityRepresentation {
                    Stepper(field.label, value: $value, in: field.range, step: field.step)
                }
            TextField("", value: $value, format: .number.grouping(.never))
                .buiFont(BUITextStyle(size: 12, lineHeight: 1.19))
                .monospacedDigit()
                .foregroundStyle(theme.colors.ink)
                #if os(iOS)
                .keyboardType(.numberPad)
                #endif
                .onChange(of: value) { _, v in
                    let c = field.clamp(v)
                    if c != v { value = c }
                }
            if let suffix = field.suffix {
                Text(suffix)
                    .buiFont(BUITextStyle(size: 11.5, lineHeight: 1.19))
                    .foregroundStyle(theme.colors.ink3)
                    .padding(.trailing, 2)
            }
        }
        .padding(.leading, 2)
        .padding(.trailing, 4)
        .frame(height: 26)
        .background(active ? theme.colors.accentTint : theme.colors.field, in: .rect(cornerRadius: theme.radii.chip, style: .continuous))
        .overlay {
            if active {
                RoundedRectangle(cornerRadius: theme.radii.chip, style: .continuous).strokeBorder(theme.colors.accent, lineWidth: 1)
            }
        }
        .animation(.easeInOut(duration: 0.2), value: active)
    }
}

/// Row (3 squares), column (2 stacked) or grid (2 × 2) of 6 pt outlined squares.
private struct LayoutGlyph: View {
    var kind: FineTuneCard.Layout

    var body: some View {
        let square = RoundedRectangle(cornerRadius: 2).stroke(lineWidth: 1.2).frame(width: 6, height: 6)
        switch kind {
        case .row: HStack(spacing: 2) { square; square; square }
        case .column: VStack(spacing: 2) { square; square }
        case .grid: VStack(spacing: 2) { HStack(spacing: 2) { square; square }; HStack(spacing: 2) { square; square } }
        }
    }
}
