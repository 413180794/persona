import SwiftUI

/// One agent task and its progress.
public struct TaskItem: Identifiable, Sendable {
    public enum Status: Sendable, Equatable {
        /// Queued: a still ring around the step number.
        case pending(step: Int)
        /// In progress: a spinning ring around the step number.
        case running(step: Int)
        /// Failed: red badge and a pill with a spinning retry glyph.
        case failed
        /// Done: green check badge and a pill.
        case completed
    }

    /// A line in the task's expanded detail.
    public struct Detail: Hashable, Sendable {
        public var label: String
        public var meta: String

        public init(_ label: String, meta: String) {
            self.label = label
            self.meta = meta
        }
    }

    public var id: String
    public var title: String
    /// Right-aligned quantity, e.g. "12 suppliers".
    public var amount: String?
    public var status: Status
    public var details: [Detail]

    public init(id: String, title: String, amount: String? = nil, status: Status, details: [Detail] = []) {
        self.id = id
        self.title = title
        self.amount = amount
        self.status = status
        self.details = details
    }
}

/// Live agent task status: capsule rows (or a grouped list) with a progress ring or
/// result badge, a status pill, and a tap-to-expand detail list.
public struct TaskRows: View {
    public enum Style: String, CaseIterable, Sendable {
        /// Separate rounded capsules that square up when opened.
        case capsules
        /// One card with divided rows.
        case list
    }

    var tasks: [TaskItem]
    var style: Style
    var completedLabel: String
    var failedLabel: String
    var externalExpanded: Binding<Set<String>>?
    var onRetry: ((TaskItem) -> Void)?

    @State private var internalExpanded: Set<String> = []
    @Environment(\.buiTheme) private var theme

    public init(
        _ tasks: [TaskItem],
        style: Style = .capsules,
        expanded: Binding<Set<String>>? = nil,
        completedLabel: String = "Completed",
        failedLabel: String = "Failed",
        onRetry: ((TaskItem) -> Void)? = nil
    ) {
        self.tasks = tasks
        self.style = style
        self.externalExpanded = expanded
        self.completedLabel = completedLabel
        self.failedLabel = failedLabel
        self.onRetry = onRetry
    }

    private var expanded: Set<String> {
        externalExpanded?.wrappedValue ?? internalExpanded
    }

    private func toggle(_ id: String) {
        var next = expanded
        if next.contains(id) { next.remove(id) } else { next.insert(id) }
        withAnimation(BUICurve.outStrong.animation(duration: 0.3)) {
            if let externalExpanded { externalExpanded.wrappedValue = next } else { internalExpanded = next }
        }
    }

    public var body: some View {
        let list = style == .list
        VStack(spacing: list ? 0 : 8) {
            ForEach(Array(tasks.enumerated()), id: \.element.id) { index, task in
                row(task, isLast: index == tasks.count - 1)
                    .buiEntrance(.fadeUp, duration: 0.45, delay: Double(index) * 0.08)
            }
        }
        .frame(maxWidth: 440)
        .modifier(ListCard(active: list))
        .frame(minHeight: list ? nil : 196, alignment: .top)
    }

    private func row(_ task: TaskItem, isLast: Bool) -> some View {
        let open = expanded.contains(task.id)
        let list = style == .list
        let shape = RoundedRectangle(cornerRadius: list ? 0 : (open ? 14 : 22), style: .continuous)
        return VStack(spacing: 0) {
            Button { toggle(task.id) } label: {
                HStack(spacing: 10) {
                    badge(task.status)
                        .frame(width: 24, height: 24)
                    Text(task.title)
                        .buiFont(.label)
                        .foregroundStyle(theme.colors.ink)
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if let amount = task.amount {
                        Text(amount)
                            .buiFont(BUITextStyle(size: 12.5))
                            .monospacedDigit()
                            .foregroundStyle(theme.colors.ink2)
                            .lineLimit(1)
                            .fixedSize()
                    }
                    pill(task)
                    BUIIcon(.chevronDown, size: 15, lineWidth: 2.2)
                        .foregroundStyle(theme.colors.ink3)
                        .rotationEffect(.degrees(open ? 180 : 0))
                        .frame(width: 28, height: 28)
                        .padding(.leading, -8)
                }
                .padding(.horizontal, 10)
                .frame(height: 44)
                .contentShape(.rect)
            }
            .buttonStyle(RowPressStyle(fill: theme.colors.inset))
            .accessibilityValue(open ? "Expanded" : "Collapsed")

            if open {
                details(task)
            }
        }
        .clipShape(shape)
        .background {
            if !list {
                shape.fill(theme.colors.surface)
                    .shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.035, darkAlpha: 0.2), radius: 1, y: 1)
                    .shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.035, darkAlpha: 0.2), radius: 10, y: 5)
            }
        }
        .overlay {
            if !list {
                shape.strokeBorder(BUIElevation.card.ring(theme.colors) ?? .clear, lineWidth: 1)
            }
        }
        .overlay(alignment: .bottom) {
            if list && !isLast {
                Rectangle().fill(theme.colors.line).frame(height: 1)
            }
        }
    }

    private func details(_ task: TaskItem) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Rectangle()
                .fill(theme.colors.line)
                .frame(width: 1)
                .frame(width: 24)
            VStack(spacing: 6) {
                ForEach(Array(task.details.enumerated()), id: \.element) { index, detail in
                    HStack {
                        Text(detail.label)
                            .buiFont(.caption)
                            .foregroundStyle(theme.colors.ink2)
                        Spacer(minLength: 8)
                        Text(detail.meta)
                            .buiFont(BUITextStyle(size: 11.5, design: .monospaced))
                            .monospacedDigit()
                            .foregroundStyle(theme.colors.ink3)
                    }
                    .buiEntrance(.fadeUp, duration: 0.3, delay: 0.12 + Double(index) * 0.1)
                }
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .padding(.horizontal, 10)
        .padding(.bottom, 10)
        .transition(.opacity)
    }

    @ViewBuilder
    private func badge(_ status: TaskItem.Status) -> some View {
        switch status {
        case .pending(let step):
            ProgressRing(step: step, active: false)
        case .running(let step):
            ProgressRing(step: step, active: true)
        case .failed:
            StatusBadge(fill: theme.colors.red, glyph: .close, size: 12)
                .id("failed")
        case .completed:
            StatusBadge(fill: theme.colors.green, glyph: .check, size: 13)
                .id("completed")
        }
    }

    @ViewBuilder
    private func pill(_ task: TaskItem) -> some View {
        switch task.status {
        case .completed:
            StatusPill(text: completedLabel, tint: theme.colors.green, fill: theme.colors.greenTint)
        case .failed:
            Button {
                onRetry?(task)
            } label: {
                StatusPill(text: failedLabel, tint: theme.colors.red, fill: theme.colors.redTint, spinningRetry: true)
            }
            .buttonStyle(.plain)
            .accessibilityHint("Retry")
        case .pending, .running:
            EmptyView()
        }
    }
}

/// The 24 pt step ring: a `line` track with an `ink-3` arc (28 %) turning every 1.1 s.
struct ProgressRing: View {
    var step: Int
    var active: Bool
    @Environment(\.buiTheme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var turning = false

    var body: some View {
        ZStack {
            Circle().stroke(theme.colors.line, lineWidth: 2).padding(1)
            if active {
                Circle()
                    .trim(from: 0, to: 0.28)
                    .stroke(theme.colors.ink3, style: StrokeStyle(lineWidth: 2, lineCap: .round))
                    .padding(1)
                    .rotationEffect(.degrees(turning ? 360 : 0))
                    .onAppear {
                        guard !reduceMotion else { return }
                        withAnimation(.linear(duration: 1.1).repeatForever(autoreverses: false)) { turning = true }
                    }
            }
            Text("\(step)")
                .buiFont(BUITextStyle(size: 10.5, weight: .semibold))
                .monospacedDigit()
                .foregroundStyle(theme.colors.ink)
        }
        .frame(width: 24, height: 24)
    }
}

/// 22 pt result badge that pops in.
struct StatusBadge: View {
    var fill: Color
    var glyph: BUIGlyph
    var size: CGFloat

    var body: some View {
        Circle()
            .fill(fill)
            .overlay { BUIIcon(glyph, size: size, lineWidth: 3.5).foregroundStyle(.white) }
            .frame(width: 22, height: 22)
            .buiEntrance(.popIn, duration: 0.3)
    }
}

/// `h-5.5 rounded-full px-2 text-[11.5px] font-medium` status pill.
struct StatusPill: View {
    var text: String
    var tint: Color
    var fill: Color
    var spinningRetry = false
    @State private var turning = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        HStack(spacing: 6) {
            Text(text)
                .buiFont(BUITextStyle(size: 11.5, weight: .medium))
            if spinningRetry {
                BUIIcon(.retry, size: 12, lineWidth: 3)
                    .rotationEffect(.degrees(turning ? 360 : 0))
                    .onAppear {
                        guard !reduceMotion else { return }
                        withAnimation(.linear(duration: 1.2).repeatForever(autoreverses: false)) { turning = true }
                    }
            }
        }
        .foregroundStyle(tint)
        .padding(.horizontal, 8)
        .frame(height: 22)
        .background(fill, in: Capsule())
        .fixedSize()
        .buiEntrance(.fadeIn, duration: 0.2, curve: .easeOut)
    }
}

/// Fills the whole row while pressed — the touch stand-in for the original's `hover:bg-inset`.
struct RowPressStyle: ButtonStyle {
    var fill: Color

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(configuration.isPressed ? fill : .clear)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}

/// The list style's single card around all rows.
private struct ListCard: ViewModifier {
    var active: Bool
    @Environment(\.buiTheme) private var theme

    func body(content: Content) -> some View {
        if active {
            content.buiSurface(radius: theme.radii.card, elevation: .card)
        } else {
            content
        }
    }
}
