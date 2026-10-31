import SwiftUI

/// A question the agent asks before acting.
public struct ApprovalQuestion: Identifiable, Sendable {
    public enum Kind: Sendable {
        /// Radio choices; picking one advances on its own.
        case single
        /// Checkboxes; waits for Continue.
        case multiple
    }

    public var id: String
    public var prompt: String
    public var kind: Kind
    public var options: [String]

    public init(id: String? = nil, _ prompt: String, kind: Kind = .single, options: [String]) {
        self.id = id ?? prompt
        self.prompt = prompt
        self.kind = kind
        self.options = options
    }
}

/// What the person answered: picked option indexes and free text, per question index.
public struct ApprovalAnswers: Sendable, Equatable {
    public var picks: [Int: [Int]] = [:]
    public var custom: [Int: String] = [:]

    public init(picks: [Int: [Int]] = [:], custom: [Int: String] = [:]) {
        self.picks = picks
        self.custom = custom
    }

    public func hasAnswer(_ question: Int) -> Bool {
        !(picks[question] ?? []).isEmpty || !(custom[question] ?? "").trimmingCharacters(in: .whitespaces).isEmpty
    }

    /// Applies a tap on `option`: single-choice questions replace the pick and clear free
    /// text; multiple-choice questions toggle it.
    public mutating func toggle(_ option: Int, in question: Int, kind: ApprovalQuestion.Kind) {
        let current = picks[question] ?? []
        switch kind {
        case .single:
            picks[question] = [option]
            custom[question] = ""
        case .multiple:
            picks[question] = current.contains(option) ? current.filter { $0 != option } : current + [option]
        }
    }

    /// Typing free text on a single-choice question drops its pick.
    public mutating func setCustom(_ text: String, in question: Int, kind: ApprovalQuestion.Kind) {
        custom[question] = text
        if kind == .single { picks[question] = [] }
    }
}

/// Human-in-the-loop questions, one at a time: the card slides between questions and
/// resizes to fit, the step counter rolls like an odometer, single-choice answers advance on
/// their own and multi-select waits for Continue.
public struct ApprovalCard: View {
    var questions: [ApprovalQuestion]
    var skipLabel: String
    var continueLabel: String
    var sendLabel: String
    var customPlaceholder: String
    var sentMessage: String
    var onSubmit: ((ApprovalAnswers) -> Void)?
    var onDismiss: (() -> Void)?

    @State private var index: Int
    @State private var answers: ApprovalAnswers
    @State private var sent: Bool
    @State private var open = true
    @State private var forward = true
    @State private var advanceTask: Task<Void, Never>?
    @FocusState private var customFocused: Bool
    @Environment(\.buiTheme) private var theme

    /// - Parameters:
    ///   - startAt / answers / submitted: resume a partly answered approval.
    public init(
        questions: [ApprovalQuestion],
        startAt: Int = 0,
        answers: ApprovalAnswers = ApprovalAnswers(),
        submitted: Bool = false,
        skipLabel: String = "Skip",
        continueLabel: String = "Continue",
        sendLabel: String = "Send",
        customPlaceholder: String = "Something else…",
        sentMessage: String = "Answers sent",
        onSubmit: ((ApprovalAnswers) -> Void)? = nil,
        onDismiss: (() -> Void)? = nil
    ) {
        self.questions = questions
        self.skipLabel = skipLabel
        self.continueLabel = continueLabel
        self.sendLabel = sendLabel
        self.customPlaceholder = customPlaceholder
        self.sentMessage = sentMessage
        self.onSubmit = onSubmit
        self.onDismiss = onDismiss
        _index = State(initialValue: min(max(0, startAt), max(0, questions.count - 1)))
        _answers = State(initialValue: answers)
        _sent = State(initialValue: submitted)
    }

    private var isLast: Bool { index == questions.count - 1 }
    private static let slide = BUICurve(0.22, 1, 0.36, 1).animation(duration: 0.36)

    public var body: some View {
        Group {
            if !open {
                Button("Open approval") {
                    withAnimation(BUIMotion.popIn) { open = true }
                }
                .buttonStyle(.bui(.secondary, fontSize: 12.5))
            } else if sent {
                sentView
            } else {
                card
            }
        }
        .frame(maxWidth: 320)
    }

    private var sentView: some View {
        HStack(spacing: 12) {
            HStack(spacing: 6) {
                Circle()
                    .fill(theme.colors.green)
                    .overlay { BUIIcon(.check, size: 11, lineWidth: 3).foregroundStyle(.white) }
                    .frame(width: 18, height: 18)
                Text(sentMessage)
                    .buiFont(BUITextStyle(size: 12.5, weight: .medium))
                    .foregroundStyle(theme.colors.green)
            }
            .padding(.leading, 4)
            .padding(.trailing, 10)
            .padding(.vertical, 4)
            .background(theme.colors.greenTint, in: Capsule())
            Button {
                reset()
            } label: {
                Text("Start over")
                    .buiFont(BUITextStyle(size: 12, weight: .medium))
                    .foregroundStyle(theme.colors.ink3)
            }
            .buttonStyle(.plain)
            Spacer(minLength: 0)
        }
        .buiEntrance(.popIn, duration: 0.26)
    }

    private var card: some View {
        VStack(spacing: 0) {
            ZStack(alignment: .topLeading) {
                question(questions[index], at: index)
                    .id(index)
                    .transition(.asymmetric(
                        insertion: .move(edge: forward ? .bottom : .top).combined(with: .opacity),
                        removal: .move(edge: forward ? .top : .bottom).combined(with: .opacity)
                    ))
            }
            .frame(maxWidth: .infinity, alignment: .topLeading)
            .clipped()
            .padding(12)
            footer
        }
        .buiSurface(radius: theme.radii.card, elevation: .card)
        .overlay(alignment: .topTrailing) {
            Button {
                withAnimation(BUIMotion.standard) { open = false }
                onDismiss?()
            } label: {
                BUIIcon(.close, size: 14, lineWidth: 2.2)
            }
            .buttonStyle(IconButtonStyle())
            .padding(10)
            .accessibilityLabel("Dismiss")
        }
        .buiEntrance(.fadeUp, duration: 0.38)
    }

    private func question(_ question: ApprovalQuestion, at qIndex: Int) -> some View {
        let picked = answers.picks[qIndex] ?? []
        return VStack(alignment: .leading, spacing: 10) {
            Text(question.prompt)
                .buiFont(BUITextStyle(size: 14, weight: .medium))
                .foregroundStyle(theme.colors.ink)
                .padding(.trailing, 28)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
            VStack(alignment: .leading, spacing: 4) {
                ForEach(Array(question.options.enumerated()), id: \.offset) { option, label in
                    let on = picked.contains(option)
                    Button {
                        pick(option, question: question)
                    } label: {
                        HStack(spacing: 6) {
                            Indicator(kind: question.kind, on: on)
                            Text(label)
                                .buiFont(BUITextStyle(size: 13, lineHeight: 1))
                                .foregroundStyle(on ? theme.colors.ink : theme.colors.ink2)
                            Spacer(minLength: 0)
                        }
                        .padding(.leading, 4)
                        .padding(.trailing, 8)
                        .padding(.vertical, 4)
                        .contentShape(.rect(cornerRadius: theme.radii.control))
                    }
                    .buttonStyle(PressHighlightStyle(radius: theme.radii.control, color: theme.colors.hover))
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
                TextField(customPlaceholder, text: Binding(
                    get: { answers.custom[qIndex] ?? "" },
                    set: { text in answers.setCustom(text, in: qIndex, kind: question.kind) }
                ))
                .buiFont(BUITextStyle(size: 13, lineHeight: 1))
                .foregroundStyle(theme.colors.ink)
                .focused($customFocused)
                .submitLabel(isLast ? .send : .next)
                .onSubmit { if answers.hasAnswer(index) { advance() } }
                .padding(.leading, 10)
                .padding(.trailing, 8)
                .padding(.vertical, 4)
                .accessibilityLabel("Custom answer")
            }
        }
    }

    private var footer: some View {
        HStack(spacing: 12) {
            HStack(spacing: 4) {
                stepButton(.up, disabled: index <= 0) { go(to: index - 1) }
                    .accessibilityLabel("Previous question")
                Text("\(index + 1) / \(questions.count)")
                    .buiFont(BUITextStyle(size: 12, weight: .medium, lineHeight: 1, tracking: -0.008))
                    .monospacedDigit()
                    .foregroundStyle(theme.colors.ink3)
                    .contentTransition(.numericText(countsDown: !forward))
                stepButton(.down, disabled: isLast) { go(to: index + 1) }
                    .accessibilityLabel("Next question")
            }
            Spacer(minLength: 0)
            HStack(spacing: 6) {
                Button(skipLabel) {
                    if isLast {
                        withAnimation(BUIMotion.standard) { open = false }
                        onDismiss?()
                    } else {
                        go(to: index + 1)
                    }
                }
                .buttonStyle(.bui(.ghost))
                Button(isLast ? sendLabel : continueLabel) { advance() }
                    .buttonStyle(.bui(.accent))
                    .disabled(!answers.hasAnswer(index))
            }
            .padding(.trailing, -2)
        }
        .padding(10)
    }

    private enum StepDirection { case up, down }

    private func stepButton(_ direction: StepDirection, disabled: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            BUIIcon(direction == .up ? BUIGlyph("M18 15l-6-6-6 6") : .chevronDown, size: 14)
                .foregroundStyle(theme.colors.ink3)
                .frame(width: 18, height: 18)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .opacity(disabled ? 0.3 : 1)
    }

    // MARK: Actions

    private func pick(_ option: Int, question: ApprovalQuestion) {
        withAnimation(.easeInOut(duration: 0.2)) {
            answers.toggle(option, in: index, kind: question.kind)
        }
        guard question.kind == .single else { return }
        advanceTask?.cancel()
        advanceTask = Task {
            try? await Task.sleep(for: .milliseconds(480))
            guard !Task.isCancelled else { return }
            advance()
        }
    }

    private func go(to next: Int) {
        advanceTask?.cancel()
        let target = min(max(next, 0), questions.count - 1)
        guard target != index else { return }
        forward = target > index
        customFocused = false
        withAnimation(Self.slide) { index = target }
    }

    private func advance() {
        if isLast {
            advanceTask?.cancel()
            customFocused = false
            withAnimation(BUIMotion.popIn) { sent = true }
            onSubmit?(answers)
        } else {
            go(to: index + 1)
        }
    }

    private func reset() {
        withAnimation(BUIMotion.popIn) {
            index = 0
            answers = ApprovalAnswers()
            sent = false
            open = true
        }
    }
}

/// 16 pt radio or checkbox: `bg-ink` with a canvas dot or check when on, a 1.5 pt
/// `line-strong` ring when off.
private struct Indicator: View {
    var kind: ApprovalQuestion.Kind
    var on: Bool
    @Environment(\.buiTheme) private var theme

    var body: some View {
        let shape = kind == .single ? AnyShape(Circle()) : AnyShape(RoundedRectangle(cornerRadius: 5, style: .continuous))
        ZStack {
            shape.fill(on ? theme.colors.ink : .clear)
            if !on {
                shape.stroke(theme.colors.lineStrong, lineWidth: 1.5).padding(0.75)
            }
            if kind == .single {
                Circle()
                    .fill(theme.colors.canvas)
                    .frame(width: 6, height: 6)
                    .scaleEffect(on ? 1 : 0)
            } else {
                BUIIcon(.check, size: 12, lineWidth: 3)
                    .foregroundStyle(on ? theme.colors.canvas : .clear)
            }
        }
        .frame(width: 16, height: 16)
        .animation(.easeInOut(duration: 0.2), value: on)
    }
}
