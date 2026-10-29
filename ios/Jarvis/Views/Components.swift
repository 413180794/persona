import SwiftUI

/// Agent state dot (docs/agent-states.md): needs you breathes — the one dot that moves, and not with Reduce Motion;
/// working has a still ring around it (its spinner is in the work and tool rows); finished-unseen solid green, seen
/// muted gray.
struct StatusDot: View {
    var state: AgentState
    var size: CGFloat = 9
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Circle()
            .fill(state.color)
            .frame(width: size, height: size)
            .overlay {
                if state == .working {
                    Circle().stroke(state.color.opacity(0.45), lineWidth: 1.5).scaleEffect(1.7)
                } else if state == .needsYou, !reduceMotion {
                    // The ring's size comes from the clock, not from a repeatForever animation on a @State flag:
                    // that animation's transaction leaked into other changes of the row (re-layout, a state change,
                    // the row coming back in a lazy list) and the ring sometimes grew far past 2.2×.
                    TimelineView(.animation(minimumInterval: 1 / 30)) { context in
                        let p = context.date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 1.0)
                        let eased = 1 - (1 - p) * (1 - p) // ease out
                        Circle()
                            .stroke(state.color.opacity(0.6), lineWidth: 1.5)
                            .scaleEffect(1 + 1.2 * eased)
                            .opacity(1 - eased)
                    }
                    .allowsHitTesting(false)
                }
            }
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
                    Image(systemName: symbol ?? machine?.symbol ?? "terminal")
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

/// The app's one small label (not a control): a fact beside other text. Three looks —
/// `.neutral`: secondary words on a soft gray (a model, a version, a tab, 私聊);
/// `.tint`: a meaning's words on its soft fill (a state's tone, @我, 推荐 in the AI's color);
/// `.outline`: secondary words in a hairline (which agent: Claude / Codex).
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

    var body: some View {
        Group {
            if let symbol {
                HStack(spacing: 3) {
                    Image(systemName: symbol).font(.caption2.weight(.semibold))
                    Text(text).lineLimit(1)
                }
            } else {
                Text(text).lineLimit(1)
            }
        }
        .font(.caption.weight(.medium))
        .foregroundStyle(words)
        .padding(.horizontal, 7)
        .padding(.vertical, 2.5)
        .background {
            if case .outline = look {
                Capsule().stroke(Color.secondary.opacity(0.4), lineWidth: 0.8)
            } else {
                Capsule().fill(fill)
            }
        }
        .fixedSize()
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
            .font(.caption2.weight(.bold).monospacedDigit())
            .contentTransition(.numericText(value: Double(count)))
            .foregroundStyle(words)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
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
        HStack(spacing: 5) {
            StatusDot(state: state, size: 7)
            Text(state.label)
                .font(.caption.weight(.semibold))
                .foregroundStyle(state.tone.text)
        }
    }
}

struct EmptyStateView: View {
    var symbol: String
    var title: String
    var message: String
    var action: (title: String, run: () -> Void)?

    var body: some View {
        VStack(spacing: 14) {
            Image(systemName: symbol)
                .font(.largeTitle.weight(.light)).imageScale(.large)
                .foregroundStyle(Theme.accent.gradient)
                .symbolEffect(.bounce, value: title)
            Text(title).font(.title3.weight(.semibold))
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            if let action {
                Button(action.title, action: action.run)
                    .themedButton(prominent: true)
                    .padding(.top, 4)
            }
        }
        .padding(32)
        .frame(maxWidth: .infinity)
    }
}

extension View {
    /// Rounded card surface used across lists.
    func card(padding: CGFloat = 14, radius: CGFloat = Theme.Radius.card) -> some View {
        self.padding(padding)
            .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: radius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: radius, style: .continuous).stroke(Theme.cardStroke, lineWidth: 0.5))
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
            Image(systemName: "xmark")
                .font(.title2.weight(.bold))
                .foregroundStyle(Theme.danger)
                .frame(width: 56, height: 56)
                .background(Theme.danger.opacity(0.12), in: .circle)
                .padding(.top, 28)
            Text("关闭这个 Agent？")
                .font(.title3.weight(.bold))
                .padding(.top, 14)
            VStack(spacing: 4) {
                Text(name).font(.headline).lineLimit(2).multilineTextAlignment(.center)
                HStack(spacing: 6) {
                    Text(session.agentLabel)
                        .font(.caption.weight(.semibold))
                        .padding(.horizontal, 7)
                        .padding(.vertical, 2)
                        .overlay(Capsule().stroke(Color.secondary.opacity(0.4), lineWidth: 1))
                    if !place.isEmpty { Text(place).lineLimit(1) }
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
            }
            .padding(.top, 10)
            .padding(.horizontal, 24)

            VStack(alignment: .leading, spacing: 12) {
                fact("power", "退出 \(session.agentLabel)，关掉它在 herdr 里的标签页（分屏时只关这个窗格）")
                fact("clock.arrow.circlepath", "对话记录保留，之后可以用 \(session.resumeCommand) 接着聊")
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            .padding(.horizontal, 20)
            .padding(.top, 20)

            VStack(spacing: 10) {
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
                    .font(.body.weight(.semibold))
                    .frame(maxWidth: .infinity, minHeight: 30)
                }
                .themedButton(prominent: true)
                .tint(Theme.danger)
                .disabled(closing)
                Button { dismiss() } label: {
                    Text("取消").lineLimit(1).font(.body.weight(.semibold)).frame(maxWidth: .infinity, minHeight: 30)
                }
                .themedButton()
                .disabled(closing)
            }
            .controlSize(.large)
            .padding(.horizontal, 20)
            .padding(.top, 20)
            .padding(.bottom, 12)
        }
        .fixedSize(horizontal: false, vertical: true)
        .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { height = $0 }
        .presentationDetents([.height(height)])
        .presentationDragIndicator(.visible)
        .interactiveDismissDisabled(closing)
    }

    private func fact(_ symbol: String, _ text: String) -> some View {
        Label {
            Text(text).font(.subheadline).fixedSize(horizontal: false, vertical: true)
        } icon: {
            Image(systemName: symbol).font(.subheadline).foregroundStyle(.secondary).frame(width: 22)
        }
    }
}

extension View {
    func confirmClose(_ session: Binding<Session?>) -> some View { modifier(CloseAgentConfirmation(session: session)) }
}
