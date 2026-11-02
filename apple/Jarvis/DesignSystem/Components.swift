import BeautifulUI
import SwiftUI

/// Agent state dot (docs/agent-states.md): needs you breathes — the one dot that moves, and not with Reduce Motion;
/// working has a still ring around it (its spinner is in the work and tool rows); finished-unseen solid green, seen
/// muted gray.
struct StatusDot: View {
    var state: AgentState
    var size: CGFloat = 9
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// The needs-you ring: one breath every `breath` seconds, out to `1 + breathScale` times the dot as it fades.
    static let breath: TimeInterval = 1.0
    static let breathScale: CGFloat = 1.2
    static let ringWidth = Theme.Size.lineMedium
    static let ringOpacity = 0.6

    var body: some View {
        Group {
            if state == .working {
                StrokeShapeView(shape: Circle().scale(1.7), style: state.color.opacity(0.45),
                                strokeStyle: StrokeStyle(lineWidth: 1.5 * 1.7), isAntialiased: true,
                                background: Circle().fill(state.color))
            } else if state == .needsYou, !reduceMotion {
                Circle().fill(state.color)
                    .overlay {
                        #if os(macOS)
                        // Core Animation runs it: the clock below relaid the whole window 30 times a second.
                        MacBreathingRing(color: state.color.opacity(Self.ringOpacity))
                            .allowsHitTesting(false)
                            .accessibilityHidden(true)
                        #else
                        // The clock drives the ring; a repeating state animation leaked into row layout changes.
                        TimelineView(.animation(minimumInterval: 1 / 30)) { context in
                            let p = context.date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: Self.breath) / Self.breath
                            let eased = 1 - (1 - p) * (1 - p)
                            Circle()
                                .stroke(state.color.opacity(Self.ringOpacity), lineWidth: Self.ringWidth)
                                .scaleEffect(1 + Self.breathScale * eased)
                                .opacity(1 - eased)
                        }
                        .allowsHitTesting(false)
                        #endif
                    }
            } else {
                Circle().fill(state.color)
            }
        }
        .frame(width: size, height: size)
        .accessibilityLabel(state.label)
    }
}

/// Machine glyph inside a soft tinted squircle with the state dot in the corner.
struct SessionAvatar: View {
    var machine: Machine?
    var state: AgentState
    var size: CGFloat = 44
    /// In place of the machine's symbol (the inbox shows what the agent asks: a question, a plan…).
    var symbol: String?
    /// The state dot on the corner; off where the square's color already says it (the inbox's 需要你 rows).
    var dot = true

    var body: some View {
        let tint: Color = state == .idle || state == .offline || state == .unknown ? .secondary : state.color
        ZStack(alignment: .bottomTrailing) {
            RoundedRectangle(cornerRadius: size * 0.3, style: .continuous)
                .fill(tint.opacity(state == .offline ? 0.06 : 0.12))
                .frame(width: size, height: size)
                .overlay {
                    Image(systemName: symbol ?? machine?.symbol ?? Theme.Icon.terminal)
                        .font(.system(size: size * 0.42, weight: .medium))
                        .foregroundStyle(tint)
                }
            if dot, state != .offline {
                StatusDot(state: state, size: size * 0.24)
                    .padding(2.5)
                    .background(Circle().fill(Color(.systemBackground)))
                    .offset(x: 3, y: 3)
            }
        }
    }
}

/// The BeautifulUI style's mark for an agent's state, its task badge (docs/agent-states.md): 需要你 a red badge with "!", working the
/// turning ring, 完成未看 the green check, seen an empty ring; offline and unknown a gray dot. Still, apart from the
/// working ring: the red badge says 需要你 without the breathing dot's 30 fps redraw.
struct AgentStateBadge: View {
    var state: AgentState
    private static let attention = BUIGlyph("M12 6v8M12 18.5v.01")

    var body: some View {
        switch state {
        case .needsYou: badge(Theme.needsYou, Self.attention)
        case .working: BUISpinner(size: Theme.Size.badge, lineWidth: Theme.Size.rule)
        case .unread: badge(Theme.positive, .check)
        case .idle: Circle().strokeBorder(Theme.cardStroke, lineWidth: Theme.Size.rule).frame(width: Theme.Size.badge, height: Theme.Size.badge)
        case .offline, .unknown: StatusDot(state: state, size: 9)
        }
    }

    private func badge(_ fill: Color, _ glyph: BUIGlyph) -> some View {
        Circle().fill(fill)
            .overlay { BUIIcon(glyph, size: 13, lineWidth: 3.5).foregroundStyle(.white) }
            .frame(width: Theme.Size.badge, height: Theme.Size.badge)
            .accessibilityHidden(true)
    }
}

/// The app's one small label (not a control): a fact beside other text. Three looks —
/// `.neutral`: secondary words on a soft gray (a model, a version, a tab, 私聊);
/// `.tint`: a meaning's words on its soft fill (a state's tone, @我, 推荐 in the AI's color);
/// `.outline`: secondary words in a hairline (which agent: Claude / Codex).
/// The BeautifulUI style draws it as that library's pill (`BUIPill`).
struct Chip: View {
    enum Look {
        case neutral
        /// `text` defaults to the color itself; a state passes its tone (`.tone(_:)`).
        case tint(Color, text: Color? = nil)
        case outline

        static func tone(_ t: StatusTone) -> Look { .tint(t.tint, text: t.text) }
    }

    var text: String
    var symbol: String?
    var look: Look = .neutral
    /// Truncates instead of keeping its full width when offered less (in a FlowLayoutRow, a chip longer than the line).
    var shrinks = false

    var body: some View {
        if AppStyle.current.usesBeautifulUI {
            BUIPill(text, tone: pillTone, icon: symbol.map { .symbol($0) }, truncates: shrinks)
        } else {
            plain
        }
    }

    /// The BeautifulUI style: its pill in the look's colors.
    private var pillTone: BUIPill.Tone {
        switch look {
        case .neutral: .neutral
        // A tone's tint is already soft; a bare color gets its own.
        case .tint(let c, let text): .custom(ink: text ?? c, fill: text == nil ? c.opacity(0.12) : c)
        case .outline: .outline
        }
    }

    private var plain: some View {
        Group {
            if let symbol {
                HStack(spacing: Theme.Space.xs) {
                    Image(systemName: symbol).font(Theme.Font.labelStrong)
                    Text(text).lineLimit(1)
                }
            } else {
                Text(text).lineLimit(1)
            }
        }
        .font(Theme.Font.labelMedium)
        .foregroundStyle(words)
        .padding(.horizontal, Theme.Space.s)
        .padding(.vertical, 2.5)
        .background {
            if case .outline = look {
                Capsule().fill(fill).stroke(Color.secondary.opacity(0.4), lineWidth: 0.8)
            } else {
                Capsule().fill(fill)
            }
        }
        .fixedSize(horizontal: !shrinks, vertical: true)
    }

    private var words: Color {
        switch look {
        case .neutral, .outline: .secondary
        case .tint(let c, let text): text ?? c
        }
    }

    private var fill: Color {
        switch look {
        case .neutral: Theme.Fill.chip
        case .tint(let c, let text): text == nil ? c.opacity(0.12) : c // a tone's tint is already soft
        case .outline: .clear
        }
    }
}

/// Something a message names that a tap opens — a file (`FileChips`), a machine's port (`PortChips`) — as a badge
/// under it: its icon and name in the accent on a soft accent capsule, then a detail (a page's title) a step quieter.
/// `dimmed`: it is not there any more (a stopped server), in secondary words on the gray chip fill.
struct LinkBadge: View {
    var symbol: String
    var text: String
    var detail: String? = nil
    var dimmed = false

    var body: some View {
        HStack(spacing: Theme.Space.xs) {
            Image(systemName: symbol).font(Theme.Font.label)
            Text(text).font(Theme.Font.labelMedium).lineLimit(1)
            if let detail, !detail.isEmpty {
                Text(detail).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
        }
        .padding(.horizontal, Theme.Space.s)
        .padding(.vertical, Theme.Space.sm)
        .background(dimmed ? Theme.Fill.chip : Theme.accent.opacity(0.1), in: .capsule)
        .foregroundStyle(dimmed ? AnyShapeStyle(.secondary) : AnyShapeStyle(Theme.accent))
    }
}

/// A count on a row or a section header: bold digits on a capsule, "99+" past 99. `.solid` is white on a color —
/// `Theme.countBadge` gray, the accent when it is about you (@我); `.tone` is a state's words on its tint.
struct CountBadge: View {
    enum Look {
        case solid(Color)
        case tone(StatusTone)
    }

    var count: Int
    var look: Look = .solid(Theme.countBadge)

    var body: some View {
        Text(count > 99 ? "99+" : "\(count)")
            .font(Theme.Font.labelHeavy.monospacedDigit())
            .contentTransition(.numericText(value: Double(count)))
            .foregroundStyle(words)
            .padding(.horizontal, Theme.Space.sm)
            .padding(.vertical, Theme.Space.xxs)
            .background(fill, in: .capsule)
            .fixedSize()
    }

    private var words: Color {
        switch look {
        case .solid: .white
        case .tone(let t): t.text
        }
    }

    private var fill: Color {
        switch look {
        case .solid(let c): c
        case .tone(let t): t.tint
        }
    }
}

struct StatusLabel: View {
    var state: AgentState

    var body: some View {
        HStack(spacing: Theme.Space.sm) {
            StatusDot(state: state, size: 7)
            Text(state.label)
                .font(Theme.Font.labelStrong)
                .foregroundStyle(state.tone.text)
        }
    }
}

/// A few options side by side, one chosen: the system's segmented control; in the BeautifulUI style its segmented switch.
struct SegmentedChoice<Value: Hashable>: View {
    var title: String
    var options: [(value: Value, label: String)]
    @Binding var selection: Value

    init(_ title: String, _ options: [(value: Value, label: String)], selection: Binding<Value>) {
        self.title = title
        self.options = options
        _selection = selection
    }

    var body: some View {
        if AppStyle.current.usesBeautifulUI {
            BUISegmented(options.map { .init($0.value, $0.label) }, selection: $selection, fills: true)
        } else {
            Picker(title, selection: $selection) {
                ForEach(options.indices, id: \.self) { i in Text(options[i].label).tag(options[i].value) }
            }
            .pickerStyle(.segmented)
        }
    }
}

struct EmptyStateView: View {
    var symbol: String
    var title: String
    var message: String
    var action: (title: String, run: () -> Void)?

    var body: some View {
        if AppStyle.current.usesBeautifulUI {
            // BeautifulUI's empty state: the mark on a raised tile, the title, a line of help and one action.
            BUIEmptyState(symbol: symbol, title: title, message: message, actionTitle: action?.title, action: action?.run)
                .frame(maxWidth: .infinity)
        } else {
            plain
        }
    }

    private var plain: some View {
        VStack(spacing: Theme.Space.l) {
            Image(systemName: symbol)
                .font(Theme.Font.display).imageScale(.large)
                .foregroundStyle(Theme.accent.gradient)
                .symbolEffect(.bounce, value: title)
            Text(title).font(Theme.Font.title)
            Text(message)
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            if let action {
                Button(action.title, action: action.run)
                    .themedButton(prominent: true)
                    .padding(.top, Theme.Space.xs)
            }
        }
        .padding(Theme.Space.xxl)
        .frame(maxWidth: .infinity)
    }
}

extension View {
    /// Rounded card surface used across lists; in the BeautifulUI style its surface (the style's card color, its ring and
    /// soft shadow).
    @ViewBuilder func card(padding: CGFloat = 14, radius: CGFloat = Theme.Radius.card) -> some View {
        if AppStyle.current.usesBeautifulUI {
            self.padding(padding)
                .buiSurface(radius: radius, elevation: .card)
        } else {
            self.padding(padding)
                .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: radius, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: radius, style: .continuous).stroke(Theme.cardStroke, lineWidth: 0.5))
        }
    }
}

/// Asks before closing an agent: quitting Claude and closing its herdr tab on a computer can't be undone.
/// Asks before closing an agent: a small sheet that says which one (name, where it runs) and what closing does, with
/// 关闭 and 取消. (The system confirmation dialog was a wall of text with no cancel button.)
struct CloseAgentConfirmation: ViewModifier {
    @Binding var session: Session?

    func body(content: Content) -> some View {
        content.sheet(item: $session) { s in CloseAgentSheet(session: s) }
    }
}

struct CloseAgentSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var session: Session
    @State private var closing = false
    @State private var height: CGFloat = 380

    private var name: String {
        let n = session.agentName ?? session.title
        return n.isEmpty ? session.folderName : n
    }

    private var place: String {
        var parts: [String] = []
        if let m = model.machine(session.machineID)?.name { parts.append(m) }
        if let space = session.spaceName, !space.isEmpty {
            // A tab named by its number reads "#1", as in the chat's header.
            let slot = session.slotName.map { $0.allSatisfy(\.isNumber) ? "#" + $0 : $0 }
            parts.append(slot.map { "\(space) › \($0)" } ?? space)
        } else if !session.folderName.isEmpty {
            parts.append(session.folderName)
        }
        return parts.joined(separator: " · ")
    }

    var body: some View {
        VStack(spacing: 0) {
            Image(systemName: Theme.Icon.close)
                .font(Theme.Font.title)
                .foregroundStyle(Theme.danger)
                .scaledFrame(width: Theme.Size.box56, height: Theme.Size.box56)
                .background(Theme.danger.opacity(0.12), in: .circle)
                .padding(.top, 28)
            Text("关闭这个 Agent？")
                .font(Theme.Font.title)
                .padding(.top, Theme.Space.l)
            VStack(spacing: Theme.Space.xs) {
                Text(name).font(Theme.Font.heading).lineLimit(2).multilineTextAlignment(.center)
                HStack(spacing: Theme.Space.sm) {
                    Text(session.agentLabel)
                        .font(Theme.Font.labelStrong)
                        .padding(.horizontal, Theme.Space.s)
                        .padding(.vertical, Theme.Space.xxs)
                        .overlay(Capsule().stroke(Color.secondary.opacity(0.4), lineWidth: 1))
                    if !place.isEmpty { Text(place).lineLimit(1) }
                }
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
            }
            .padding(.top, Theme.Space.m)
            .padding(.horizontal, Theme.Space.xl)

            VStack(alignment: .leading, spacing: Theme.Space.m) {
                fact("power", "退出 \(session.agentLabel)，关掉它在 herdr 里的标签页（分屏时只关这个窗格）")
                fact("clock.arrow.circlepath", "对话记录保留，之后可以用 \(session.resumeCommand) 接着聊")
            }
            .padding(Theme.Space.l)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            .padding(.horizontal, Theme.Space.xl)
            .padding(.top, Theme.Space.xl)

            VStack(spacing: Theme.Space.m) {
                Button {
                    closing = true
                    Haptics.tap()
                    Task {
                        await model.close(session.key)
                        dismiss()
                    }
                } label: {
                    Group {
                        if closing { ProgressView().tint(.white) } else { Text("关闭 Agent").lineLimit(1) }
                    }
                    .font(Theme.Font.heading)
                    .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.wellS)
                }
                .themedButton(prominent: true)
                .tint(Theme.danger)
                .disabled(closing)
                Button { dismiss() } label: {
                    Text("取消").lineLimit(1).font(Theme.Font.heading).scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.wellS)
                }
                .themedButton()
                .disabled(closing)
            }
            .controlSize(.large)
            .padding(.horizontal, Theme.Space.xl)
            .padding(.top, Theme.Space.xl)
            .padding(.bottom, Theme.Space.m)
        }
        .fixedSize(horizontal: false, vertical: true)
        .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { height = $0 }
        .presentationDetents([.height(height)])
        .presentationDragIndicator(.visible)
        .interactiveDismissDisabled(closing)
    }

    private func fact(_ symbol: String, _ text: String) -> some View {
        Label {
            Text(text).font(Theme.Font.secondary).fixedSize(horizontal: false, vertical: true)
        } icon: {
            Image(systemName: symbol).font(Theme.Font.secondary).foregroundStyle(.secondary).scaledFrame(width: Theme.Size.iconS)
        }
    }
}

extension View {
    func confirmClose(_ session: Binding<Session?>) -> some View { modifier(CloseAgentConfirmation(session: session)) }
}

/// Someone without a picture (a mail sender): the first letter on a disc.
struct Monogram<Fill: ShapeStyle>: View {
    var name: String
    var size: CGFloat = 40
    var fill: Fill
    /// The letter's color; by default the hierarchical primary (`Color.primary` turned white in a Mac list's selected
    /// row, on the light gray disc).
    var ink: Color?

    var body: some View {
        Circle().fill(fill).frame(width: size, height: size)
            .overlay(Text(String((name.isEmpty ? "?" : name).prefix(1))).font(Theme.Font.heading)
                .foregroundStyle(ink.map { AnyShapeStyle($0) } ?? AnyShapeStyle(.primary)))
    }
}

/// What a page that loads shows: a spinner, the thing, or why it failed.
enum Loadable<Value> {
    case loading
    case loaded(Value)
    case failed(String)

    var value: Value? {
        if case .loaded(let v) = self { v } else { nil }
    }
}

extension Loadable: Equatable where Value: Equatable {}
extension Loadable: Sendable where Value: Sendable {}

/// A `Loadable` drawn the one way: a centered spinner, the content, or the error with 重试.
struct LoadableContent<Value, Content: View>: View {
    var state: Loadable<Value>
    var retry: (() -> Void)?
    @ViewBuilder var content: (Value) -> Content

    var body: some View {
        switch state {
        case .loading:
            if AppStyle.current.usesBeautifulUI {
                // A page loading, not a row: BeautifulUI's pixel loader is fine here.
                LoadingState("正在载入", pattern: .dots).frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        case .loaded(let value):
            content(value)
        case .failed(let message):
            if AppStyle.current.usesBeautifulUI {
                BUIEmptyState(symbol: Theme.Icon.error, title: message, actionTitle: retry == nil ? nil : "重试", action: retry)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ContentUnavailableView {
                    Label(message, systemImage: Theme.Icon.error)
                } actions: {
                    if let retry { Button("重试", action: retry).themedButton() }
                }
            }
        }
    }
}

/// Chips in a row that wrap when they do not fit (a row can have several status columns).
struct FlowLayoutRow: Layout {
    var spacing: CGFloat = Theme.Space.sm

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width.flatMap { $0.isFinite ? $0 : nil } ?? .greatestFiniteMagnitude
        var x: CGFloat = 0, y: CGFloat = 0, line: CGFloat = 0, widest: CGFloat = 0
        for s in subviews {
            let size = Self.size(s, within: width)
            if x > 0 && x + size.width > width { y += line + spacing; x = 0; line = 0 }
            x += size.width + spacing
            line = max(line, size.height)
            widest = max(widest, x - spacing)
        }
        return CGSize(width: min(widest, width), height: y + line)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, line: CGFloat = 0
        for s in subviews {
            let size = Self.size(s, within: bounds.width)
            if x > bounds.minX && x + size.width > bounds.maxX { y += line + spacing; x = bounds.minX; line = 0 }
            s.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            line = max(line, size.height)
        }
    }

    /// Its ideal size, or the line's width if that is narrower (a subview that can shrink, like `Chip(shrinks:)`).
    private static func size(_ s: LayoutSubview, within width: CGFloat) -> CGSize {
        let ideal = s.sizeThatFits(.unspecified)
        return ideal.width <= width ? ideal : s.sizeThatFits(ProposedViewSize(width: width, height: nil))
    }
}
