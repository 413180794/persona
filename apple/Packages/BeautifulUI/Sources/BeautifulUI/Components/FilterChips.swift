import SwiftUI

/// A status filter: an optional dot in the status's hue, the name and a count. The chosen one floats as a raised
/// capsule (`FilterTable`'s chips). Lay several out in an `HStack` or a `BUIFlowLayout`; the chip doesn't scroll.
public struct BUIFilterChip: View {
    var label: String
    var dot: Color?
    var count: Int?
    var isActive: Bool
    var action: () -> Void
    @Environment(\.buiTheme) private var theme

    public init(_ label: String, dot: Color? = nil, count: Int? = nil, isActive: Bool, action: @escaping () -> Void) {
        self.label = label
        self.dot = dot
        self.count = count
        self.isActive = isActive
        self.action = action
    }

    public var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                if let dot { Circle().fill(dot).frame(width: 6 * theme.typeScale, height: 6 * theme.typeScale) }
                Text(label)
                    .buiFont(BUITextStyle(size: 12, weight: .medium, lineHeight: 1.19))
                    .foregroundStyle(isActive ? theme.colors.ink : theme.colors.ink2)
                    .lineLimit(1)
                if let count {
                    Text("\(count)")
                        .buiFont(BUITextStyle(size: 10.5, lineHeight: 1.19))
                        .monospacedDigit()
                        .foregroundStyle(isActive ? theme.colors.ink2 : theme.colors.ink3)
                        .padding(.horizontal, 4)
                        .background(isActive ? theme.colors.field : .clear, in: .rect(cornerRadius: 4))
                }
            }
            .padding(.horizontal, 10)
            .frame(height: 26 * max(1, theme.typeScale * 0.9))
            .background {
                if isActive {
                    Capsule().fill(theme.colors.surface)
                        .overlay(Capsule().strokeBorder(BUIElevation.button.ring(theme.colors) ?? .clear, lineWidth: 1))
                        .shadow(color: .black.opacity(0.04), radius: 2)
                }
            }
            .contentShape(Capsule())
        }
        .buttonStyle(PressHighlightStyle(radius: 13 * theme.typeScale, color: theme.colors.hover))
        .fixedSize()
        .accessibilityAddTraits(isActive ? .isSelected : [])
    }
}

/// The site's pill segmented control (`rounded-full bg-field p-0.5`): the chosen segment is a raised capsule that
/// glides to the next one.
public struct BUISegmented<Value: Hashable>: View {
    public struct Segment {
        public var value: Value
        public var label: String

        public init(_ value: Value, _ label: String) {
            self.value = value
            self.label = label
        }
    }

    var segments: [Segment]
    @Binding var selection: Value
    var fills: Bool
    @Environment(\.buiTheme) private var theme
    @Namespace private var pill

    /// - Parameter fills: segments share the whole width (a settings row); otherwise the control hugs its words.
    public init(_ segments: [Segment], selection: Binding<Value>, fills: Bool = false) {
        self.segments = segments
        _selection = selection
        self.fills = fills
    }

    public var body: some View {
        HStack(spacing: 0) {
            ForEach(Array(segments.enumerated()), id: \.offset) { _, segment in
                let active = segment.value == selection
                Button {
                    withAnimation(BUIMotion.standard) { selection = segment.value }
                } label: {
                    Text(segment.label)
                        .buiFont(BUITextStyle(size: 11.5, weight: .medium))
                        .foregroundStyle(active ? theme.colors.ink : theme.colors.ink3)
                        .lineLimit(1)
                        .padding(.horizontal, 10)
                        .frame(maxWidth: fills ? .infinity : nil, minHeight: 24 * max(1, theme.typeScale * 0.9))
                        .background {
                            if active {
                                Capsule()
                                    .fill(theme.colors.surface)
                                    .overlay(Capsule().strokeBorder(BUIElevation.button.ring(theme.colors) ?? .clear, lineWidth: 1))
                                    .shadow(color: .black.opacity(0.06), radius: 1.5, y: 0.5)
                                    .matchedGeometryEffect(id: "pill", in: pill)
                            }
                        }
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(active ? .isSelected : [])
            }
        }
        .padding(2)
        .background(theme.colors.field, in: Capsule())
        // One element holding its segments, so a host's identifier names the control, not each segment.
        .accessibilityElement(children: .contain)
    }
}
