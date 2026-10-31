import SwiftUI

/// An AI action offered for a selection.
public struct SelectionAction: Identifiable, Sendable {
    public var id: String
    /// An SF Symbol name.
    public var symbol: String
    /// The edit to run; `nil` for actions handled elsewhere (e.g. Explain).
    public var action: String?
    /// The gerund shown while it runs, e.g. "Improving".
    public var busyLabel: String?

    public init(_ id: String, symbol: String, action: String? = nil, busyLabel: String? = nil) {
        self.id = id
        self.symbol = symbol
        self.action = action
        self.busyLabel = busyLabel
    }
}

struct SelectionHighlightAttribute: TextAttribute {}

/// A contextual AI bar attached beneath highlighted text. Pick an action or describe an
/// edit; the bar shows progress, the rewrite streams into the selection, then Keep, Discard
/// or retry. On a phone the prompt field narrows and extra actions scroll inside the bar.
public struct SelectionActions: View {
    public enum Phase: Sendable { case idle, thinking, streaming, result }

    var lead: String
    var original: String
    var rewrite: String
    var primaryActions: [SelectionAction]
    var moreActions: [SelectionAction]
    var placeholder: String
    var onAction: ((String) -> Void)?
    var onKeep: ((String) -> Void)?

    @State private var phase: Phase
    @State private var action = "Improve"
    @State private var prompt = ""
    @State private var expanded: Bool
    @State private var streamed = 0
    @State private var shown = false
    @State private var kept: String?
    @Environment(\.buiTheme) private var theme
    @Environment(\.self) private var environment

    public init(
        lead: String,
        original: String,
        rewrite: String,
        primaryActions: [SelectionAction],
        moreActions: [SelectionAction] = [],
        placeholder: String = "Describe edits",
        phase: Phase = .idle,
        expanded: Bool = false,
        onAction: ((String) -> Void)? = nil,
        onKeep: ((String) -> Void)? = nil
    ) {
        self.lead = lead
        self.original = original
        self.rewrite = rewrite
        self.primaryActions = primaryActions
        self.moreActions = moreActions
        self.placeholder = placeholder
        self.onAction = onAction
        self.onKeep = onKeep
        _phase = State(initialValue: phase)
        _expanded = State(initialValue: expanded)
        _streamed = State(initialValue: phase == .result ? Self.words(rewrite).count : 0)
    }

    nonisolated static func words(_ s: String) -> [String] {
        s.split(separator: " ", omittingEmptySubsequences: false).map(String.init)
    }

    private var selectionText: String {
        switch phase {
        case .idle, .thinking: kept ?? original
        case .streaming: Self.words(rewrite).prefix(streamed).joined(separator: " ")
        case .result: rewrite
        }
    }

    private var busyLabel: String {
        (primaryActions + moreActions).first { $0.action == action }?.busyLabel ?? "Editing"
    }

    public var body: some View {
        VStack(spacing: 8) {
            paragraph
            bar
                .opacity(shown ? 1 : 0)
                .scaleEffect(shown ? 1 : 0.95)
        }
        .frame(maxWidth: 460)
        .padding(.bottom, 12)
        .task {
            try? await Task.sleep(for: .milliseconds(280))
            withAnimation(BUICurve.outStrong.animation(duration: 0.22)) { shown = true }
        }
        .task(id: phase) { await advance() }
        .animation(BUICurve.outStrong.animation(duration: 0.32), value: phase)
        .animation(BUICurve.outStrong.animation(duration: 0.4), value: expanded)
    }

    private var paragraph: some View {
        let highlight = Color((environment.colorScheme == .dark ? theme.colors.accentTint : theme.colors.accent.mix(with: theme.colors.surface, by: 0.86)).resolve(in: environment))
        let selection = Text(selectionText).foregroundStyle(theme.colors.ink).customAttribute(SelectionHighlightAttribute())
        return Text("\(Text(lead))\(selection)")
            .buiFont(BUITextStyle(size: 13, lineHeight: 1.625))
            .foregroundStyle(theme.colors.ink)
            .textRenderer(SelectionHighlightRenderer(fill: highlight))
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var bar: some View {
        HStack(spacing: 2) {
            switch phase {
            case .thinking, .streaming:
                HStack(spacing: 6) {
                    BUISpinner()
                    if phase == .thinking {
                        Text("\(busyLabel)…").buiShimmer()
                    } else {
                        Text("\(busyLabel)…").foregroundStyle(theme.colors.ink2)
                    }
                }
                .buiFont(BUITextStyle(size: 12.5, lineHeight: 1.19))
                .padding(.horizontal, 10)
                .frame(height: 28)
                .transition(.opacity)
            case .result:
                Button {
                    kept = rewrite
                    onKeep?(rewrite)
                    reset()
                } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "checkmark").font(.system(size: 11, weight: .semibold))
                        Text("Keep")
                    }
                    .buiFont(BUITextStyle(size: 12.5, lineHeight: 1.19))
                    .foregroundStyle(theme.colors.canvas)
                    .padding(.horizontal, 10)
                    .frame(height: 28)
                    .background(theme.colors.ink, in: Capsule())
                }
                .buttonStyle(.plain)
                actionButton(SelectionAction("Discard", symbol: "xmark")) { reset() }
                divider(theme.colors.line)
                Button { run(action) } label: {
                    Image(systemName: "arrow.clockwise").font(.system(size: 12, weight: .medium))
                        .foregroundStyle(theme.colors.ink3)
                        .frame(width: 28, height: 28)
                        .contentShape(Circle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Try again")
            case .idle:
                idleContent
            }
        }
        .padding(4)
        .frame(height: 36)
        .background(theme.colors.surface, in: Capsule())
        .overlay(Capsule().strokeBorder(BUIElevation.overlay.ring(theme.colors) ?? .clear, lineWidth: 1))
        .shadow(color: .black.opacity(0.08), radius: 10, y: 6)
        .fixedSize(horizontal: phase != .idle, vertical: false)
    }

    @ViewBuilder
    private var idleContent: some View {
        let typing = !prompt.trimmingCharacters(in: .whitespaces).isEmpty
        if !expanded {
            TextField(placeholder, text: $prompt)
                .buiFont(BUITextStyle(size: 12.5, lineHeight: 1.19))
                .foregroundStyle(theme.colors.ink)
                .submitLabel(.send)
                .onSubmit { run(prompt.trimmingCharacters(in: .whitespaces).isEmpty ? "Improve" : prompt) }
                .padding(.leading, 12)
                .padding(.trailing, 10)
                .frame(minWidth: 90, idealWidth: 145, maxWidth: typing ? .infinity : 145, minHeight: 28)
                .transition(.opacity.combined(with: .offset(x: -8)))
        }
        if typing {
            Button { run(prompt) } label: {
                BUIIcon(.arrowUp, size: 16, lineWidth: 2.4)
                    .foregroundStyle(theme.colors.surface)
                    .frame(width: 28, height: 28)
                    .background(theme.colors.ink, in: Circle())
            }
            .buttonStyle(ShrinkOnPress())
            .transition(.scale(scale: 0.88).combined(with: .opacity))
            .accessibilityLabel("Send edit instruction")
        } else {
            if !expanded { divider(theme.colors.lineStrong).padding(.horizontal, 2) }
            ScrollView(.horizontal) {
                HStack(spacing: 2) {
                    ForEach(primaryActions + (expanded ? moreActions : [])) { item in
                        actionButton(item) { if let a = item.action { run(a) } }
                    }
                }
            }
            .scrollIndicators(.hidden)
            .scrollBounceBehavior(.basedOnSize)
            .fixedSize(horizontal: !expanded, vertical: false)
            divider(theme.colors.line)
            Button {
                expanded.toggle()
            } label: {
                Image(systemName: "chevron.right")
                    .font(.system(size: 12, weight: .medium))
                    .rotationEffect(.degrees(expanded ? 180 : 0))
                    .foregroundStyle(theme.colors.ink)
                    .frame(width: 28, height: 28)
                    .contentShape(Circle())
            }
            .buttonStyle(ShrinkOnPress())
            .accessibilityLabel(expanded ? "Show fewer actions" : "Show more actions")
        }
    }

    private func actionButton(_ item: SelectionAction, perform: @escaping () -> Void) -> some View {
        Button(action: perform) {
            HStack(spacing: 4) {
                Image(systemName: item.symbol).font(.system(size: 12))
                Text(item.id)
            }
        }
        .buttonStyle(QuietPillStyle())
    }

    private func divider(_ color: Color) -> some View {
        Rectangle().fill(color).frame(width: 1, height: 16).padding(.horizontal, 2)
    }

    private func run(_ next: String) {
        action = next
        expanded = false
        prompt = ""
        streamed = 0
        phase = .thinking
        onAction?(next)
    }

    private func reset() {
        expanded = false
        prompt = ""
        action = "Improve"
        phase = .idle
    }

    /// thinking → streaming after 0.7 s; streaming reveals a word every 45 ms, then result.
    private func advance() async {
        switch phase {
        case .thinking:
            try? await Task.sleep(for: .milliseconds(700))
            guard !Task.isCancelled else { return }
            phase = .streaming
        case .streaming:
            let total = Self.words(rewrite).count
            while streamed < total {
                try? await Task.sleep(for: .milliseconds(45))
                guard !Task.isCancelled else { return }
                streamed += 1
            }
            phase = .result
        case .idle, .result:
            break
        }
    }
}

/// Rounded `accent` 14 % behind each line of the selection.
struct SelectionHighlightRenderer: TextRenderer {
    var fill: Color

    func draw(layout: Text.Layout, in ctx: inout GraphicsContext) {
        for line in layout {
            var rect: CGRect?
            for run in line where run[SelectionHighlightAttribute.self] != nil {
                rect = rect.map { $0.union(run.typographicBounds.rect) } ?? run.typographicBounds.rect
            }
            if let rect {
                ctx.fill(Path(roundedRect: rect.insetBy(dx: 0, dy: -1), cornerRadius: 3), with: .color(fill))
            }
            for run in line { ctx.draw(run) }
        }
    }
}

/// The Button atom's `quiet` `xs` size: 28 pt, no fill until pressed.
private struct QuietPillStyle: ButtonStyle {
    @Environment(\.buiTheme) private var theme

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .buiFont(BUITextStyle(size: 12, lineHeight: 1.19))
            .foregroundStyle(theme.colors.ink)
            .padding(.horizontal, 10)
            .frame(height: 28)
            .background(configuration.isPressed ? theme.colors.hover : .clear, in: Capsule())
            .scaleEffect(configuration.isPressed ? 0.96 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
            .fixedSize()
    }
}

private struct ShrinkOnPress: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.94 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}
