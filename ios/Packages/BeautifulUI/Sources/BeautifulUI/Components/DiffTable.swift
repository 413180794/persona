import SwiftUI

/// A row in a proposed table edit.
public struct DiffTableRow: Identifiable, Sendable {
    public enum Change: Sendable { case unchanged, removal, addition }

    public var id: String
    public var name: String
    public var tag: String
    public var tagTint: Color
    public var detail: String
    public var change: Change

    public init(id: String? = nil, name: String, tag: String, tagTint: Color, detail: String, change: Change = .unchanged) {
        self.id = id ?? name
        self.name = name
        self.tag = tag
        self.tagTint = tagTint
        self.detail = detail
        self.change = change
    }
}

/// Which proposed edits are included, and the summary of them.
public struct DiffTableSelection: Equatable, Sendable {
    public var included: Set<String>

    public init(included: Set<String>) {
        self.included = included
    }

    public func removals(in rows: [DiffTableRow]) -> Int {
        rows.filter { $0.change == .removal && included.contains($0.id) }.count
    }

    public func additions(in rows: [DiffTableRow]) -> Int {
        rows.filter { $0.change == .addition && included.contains($0.id) }.count
    }

    public mutating func toggle(_ id: String) {
        if included.contains(id) { included.remove(id) } else { included.insert(id) }
    }

    /// "2 removals · 1 addition".
    public static func summary(removals: Int, additions: Int) -> String {
        "\(removals) \(removals == 1 ? "removal" : "removals") · \(additions) \(additions == 1 ? "addition" : "additions")"
    }
}

/// AI-proposed edits sweeping through a table: removals tint red and strike through, an added
/// row slides in green, and each changed row toggles whether that edit is applied.
public struct DiffTable: View {
    var title: String
    var columns: [String]
    var rows: [DiffTableRow]
    var hint: String
    var onApply: ((DiffTableSelection) -> Void)?

    @State private var stage: Int
    @State private var selection: DiffTableSelection
    @State private var accepted = false
    @Environment(\.buiTheme) private var theme

    private static let fractions: [CGFloat] = [0.34, 0.30, 0.36]

    /// - Parameter animatesIn: play the original's sweep (removals at 180 ms, additions at 440 ms).
    public init(
        title: String,
        columns: [String],
        rows: [DiffTableRow],
        hint: String = "Tap changed rows to toggle",
        animatesIn: Bool = true,
        onApply: ((DiffTableSelection) -> Void)? = nil
    ) {
        self.title = title
        self.columns = columns
        self.rows = rows
        self.hint = hint
        self.onApply = onApply
        _stage = State(initialValue: animatesIn ? 0 : 2)
        _selection = State(initialValue: DiffTableSelection(included: Set(rows.filter { $0.change != .unchanged }.map(\.id))))
    }

    private var settled: Bool { stage >= 2 }

    public var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(title)
                    .buiFont(BUITextStyle(size: 12.5, weight: .medium))
                    .foregroundStyle(theme.colors.ink)
                Spacer(minLength: 8)
                if settled && !accepted {
                    Text(hint)
                        .buiFont(BUITextStyle(size: 11))
                        .foregroundStyle(theme.colors.ink3)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .overlay(alignment: .bottom) { divider }

            BUIColumns(Self.fractions) {
                ForEach(columns, id: \.self) { column in
                    Text(column)
                        .buiFont(BUITextStyle(size: 12, weight: .medium))
                        .foregroundStyle(theme.colors.ink3)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            .overlay(alignment: .bottom) { divider }

            ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                if row.change == .addition {
                    if settled {
                        addedRow(row)
                            .transition(.asymmetric(insertion: .push(from: .top).combined(with: .opacity), removal: .opacity))
                    }
                } else {
                    existingRow(row, isLast: index == rows.count - 1)
                }
            }

            if settled {
                footer
                    .overlay(alignment: .top) { divider }
                    .buiEntrance(.fadeUp, duration: 0.18)
            }
        }
        .frame(maxWidth: 380)
        .buiSurface(radius: theme.radii.card, elevation: .card)
        .task {
            guard stage < 2 else { return }
            try? await Task.sleep(for: .milliseconds(180))
            withAnimation(.easeInOut(duration: 0.2)) { stage = 1 }
            try? await Task.sleep(for: .milliseconds(260))
            withAnimation(BUICurve.outStrong.animation(duration: 0.2)) { stage = 2 }
        }
    }

    private var divider: some View {
        Rectangle().fill(theme.colors.line).frame(height: 1)
    }

    private func existingRow(_ row: DiffTableRow, isLast: Bool) -> some View {
        let removal = row.change == .removal
        let out = removal && stage >= 1 && selection.included.contains(row.id)
        let interactive = removal && settled && !accepted
        return Button {
            withAnimation(.easeInOut(duration: 0.15)) { selection.toggle(row.id) }
        } label: {
            BUIColumns(Self.fractions) {
                Text(row.name)
                    .buiFont(.label)
                    .foregroundStyle(out ? theme.colors.red : theme.colors.ink)
                    .lineLimit(1)
                    .cell()
                BUITagPill(row.tag, dot: row.tagTint)
                    .opacity(out ? 0.55 : 1)
                    .cell()
                HStack(spacing: 8) {
                    Text(row.detail)
                        .buiFont(BUITextStyle(size: 12.5))
                        .foregroundStyle(out ? theme.colors.red : theme.colors.ink2)
                        .strikethrough(out, color: theme.colors.red.opacity(0.5))
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    if removal && settled {
                        IncludedMark(included: selection.included.contains(row.id), tint: theme.colors.red)
                    }
                }
                .cell()
            }
            .background(out ? theme.colors.redTint : .clear)
            .contentShape(.rect)
        }
        .buttonStyle(DimOnPressStyle())
        .disabled(!interactive)
        .overlay(alignment: .bottom) { if !isLast || settled { divider } }
        .accessibilityAddTraits(removal && selection.included.contains(row.id) ? .isSelected : [])
    }

    private func addedRow(_ row: DiffTableRow) -> some View {
        let on = selection.included.contains(row.id)
        return Button {
            withAnimation(.easeInOut(duration: 0.15)) { selection.toggle(row.id) }
        } label: {
            BUIColumns(Self.fractions) {
                Text(row.name)
                    .buiFont(.label)
                    .foregroundStyle(on ? theme.colors.green : theme.colors.ink3)
                    .lineLimit(1)
                    .cell()
                BUITagPill(row.tag, dot: row.tagTint, fill: theme.colors.surface)
                    .cell()
                HStack(spacing: 8) {
                    Text(row.detail)
                        .buiFont(.body)
                        .foregroundStyle(on ? theme.colors.green : theme.colors.ink3)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    IncludedMark(included: on, tint: theme.colors.green)
                }
                .cell()
            }
            .background(on ? theme.colors.greenTint : .clear)
            .contentShape(.rect)
        }
        .buttonStyle(DimOnPressStyle())
        .disabled(accepted)
        .accessibilityLabel("Include adding \(row.name)")
        .accessibilityAddTraits(on ? .isSelected : [])
    }

    private var footer: some View {
        let removals = selection.removals(in: rows)
        let additions = selection.additions(in: rows)
        let total = removals + additions
        return HStack {
            if accepted {
                HStack(spacing: 6) {
                    Circle()
                        .fill(theme.colors.green)
                        .overlay { BUIIcon(.check, size: 11, lineWidth: 3).foregroundStyle(.white) }
                        .frame(width: 18, height: 18)
                    Text("\(total) \(total == 1 ? "edit" : "edits") applied")
                        .buiFont(BUITextStyle(size: 12.5, weight: .medium))
                        .foregroundStyle(theme.colors.green)
                }
                .padding(.leading, 4)
                .padding(.trailing, 10)
                .padding(.vertical, 4)
                .background(theme.colors.greenTint, in: Capsule())
                .buiEntrance(.popIn, duration: 0.18)
                Spacer()
            } else {
                Text(DiffTableSelection.summary(removals: removals, additions: additions))
                    .buiFont(BUITextStyle(size: 11.5))
                    .monospacedDigit()
                    .foregroundStyle(theme.colors.ink3)
                Spacer()
                Button("Apply \(total) \(total == 1 ? "change" : "changes")") {
                    withAnimation(BUIMotion.standard) { accepted = true }
                    onApply?(selection)
                }
                .buttonStyle(.bui(.accent, fontSize: 12))
                .disabled(total == 0)
            }
        }
        .padding(10)
        .frame(minHeight: 44)
    }
}

private extension View {
    /// `.primitive-table-cell`: 10 × 12 pt padding, leading-aligned.
    func cell() -> some View {
        padding(.horizontal, 12)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// 18 pt include/exclude mark: a filled check when included, an empty inset tile when not.
private struct IncludedMark: View {
    var included: Bool
    var tint: Color
    @Environment(\.buiTheme) private var theme

    var body: some View {
        RoundedRectangle(cornerRadius: 5, style: .continuous)
            .fill(included ? tint : theme.colors.inset)
            .overlay {
                if included {
                    BUIIcon(.check, size: 11, lineWidth: 3).foregroundStyle(.white)
                } else {
                    RoundedRectangle(cornerRadius: 5, style: .continuous).strokeBorder(theme.colors.line, lineWidth: 1)
                }
            }
            .frame(width: 18, height: 18)
            .scaleEffect(included ? 1 : 0.92)
            .animation(.easeInOut(duration: 0.15), value: included)
    }
}

/// `hover:brightness-[0.985]` as a press state.
private struct DimOnPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .brightness(configuration.isPressed ? -0.015 : 0)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}
