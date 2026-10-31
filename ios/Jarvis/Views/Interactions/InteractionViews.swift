import MarkdownUI
import SwiftUI

extension InteractionKind {
    var title: String {
        switch self {
        case .question: "Claude 在问你"
        case .plan: "计划等你审批"
        case .permission: "需要你授权"
        case .screen: "终端等你处理"
        }
    }

    var symbol: String {
        switch self {
        case .question: "questionmark.bubble.fill"
        case .plan: "list.bullet.clipboard.fill"
        case .permission: "hand.raised.fill"
        case .screen: Theme.Icon.terminalFilled
        }
    }
}

extension Interaction {
    /// The assistant asks through its ask_user tool: it is Jarvis asking, not a Claude Code agent.
    var title: String { toolName == "ask_user" ? "Jarvis 在问你" : kind.title }
}

/// Compact card on the chat list: enough context plus one-tap answers for simple questions.
struct InteractionPreviewCard: View {
    @Environment(AppModel.self) private var model
    var interaction: Interaction
    var open: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            HStack(spacing: Theme.Space.s) {
                // Every interaction is 需要你: the same red as the agent's dot and the section's count.
                Image(systemName: interaction.kind.symbol)
                    .foregroundStyle(StatusPalette.needsYou.dot)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(interaction.title).font(Theme.Font.heading)
                    Text([interaction.sessionTitle, model.machine(interaction.machineID)?.name].compactMap { $0 }.joined(separator: " · "))
                        .font(Theme.Font.label)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
                Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
            }
            Text(summary)
                .font(Theme.Font.body).lineHeight(.body)
                .lineLimit(3)
                .frame(maxWidth: .infinity, alignment: .leading)
            quickActions
        }
        .card()
        .contentShape(.rect)
        .onTapGesture(perform: open)
    }

    private var summary: String {
        switch interaction.kind {
        case .question: interaction.questions?.first?.question ?? ""
        case .plan: PlanText.title(interaction.plan ?? "")
        case .permission: [interaction.toolName, interaction.summary].compactMap { $0 }.joined(separator: "：")
        case .screen: ScreenParser.activity(interaction.screen ?? "", max: 3).joined(separator: "\n")
        }
    }

    @ViewBuilder private var quickActions: some View {
        switch interaction.kind {
        case .question:
            if let qs = interaction.questions, qs.count == 1, let q = qs.first, !q.multiSelect, q.options.count <= 4 {
                FlowButtons(labels: q.options.map(\.label)) { label in
                    Task { await model.resolve(interaction, Resolution(answers: [q.question: label])) }
                }
            }
        case .permission:
            HStack {
                Button("允许") { Task { await model.resolve(interaction, Resolution(allow: true)) } }
                    .themedButton(prominent: true)
                Button("拒绝") { Task { await model.resolve(interaction, Resolution(allow: false)) } }
                    .themedButton()
            }
            .controlSize(.small)
        default:
            EmptyView()
        }
    }
}

struct FlowButtons: View {
    var labels: [String]
    var action: (String) -> Void

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Theme.Space.s) {
                ForEach(labels, id: \.self) { label in
                    let (text, recommended) = QuestionForm.display(label)
                    Button { action(label) } label: {
                        HStack(spacing: Theme.Space.sm) {
                            Text(text).lineLimit(1)
                            if recommended { RecommendedTag() }
                        }
                    }
                    .themedButton()
                    .controlSize(.small)
                    .accessibilityLabel(text + (recommended ? "，推荐" : ""))
                }
            }
        }
        .scrollClipDisabled()
    }
}

/// 「推荐」 beside the option Claude suggests ("… (Recommended)", `QuestionForm.display`): the question panel, the
/// terminal dialog and the quick answers on the Agents card draw the same tag, in the AI's color (it is Claude's
/// suggestion; the accent would read as something to tap).
struct RecommendedTag: View {
    var body: some View { Chip(text: "推荐", look: .tint(Theme.ai)) }
}

enum PlanText {
    static func title(_ plan: String) -> String {
        for line in plan.split(separator: "\n") {
            let t = line.trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "#* "))
            if !t.isEmpty { return t }
        }
        return "执行计划"
    }
}

/// Full interaction UI pinned above the composer in a chat. Capped in height (scrolls inside) and collapsible,
/// so a long question never hides the conversation it is about.
struct InteractionPanel: View {
    @Environment(AppModel.self) private var model
    var interaction: Interaction
    @State private var collapsed = false
    @State private var formHeight: CGFloat = 0
    /// The plan full screen. Its button sits in this header: floating over the plan it covered the last line.
    @State private var showFullPlan = false

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            HStack(spacing: Theme.Space.sm) {
                Image(systemName: interaction.kind.symbol)
                    .font(Theme.Font.labelStrong)
                    .foregroundStyle(StatusPalette.needsYou.dot)
                Text(panelTitle).font(Theme.Font.labelStrong).foregroundStyle(StatusPalette.needsYou.text).lineLimit(1)
                if collapsed {
                    Text(previewText).font(Theme.Font.secondary).foregroundStyle(.primary).lineLimit(1)
                }
                Spacer(minLength: 4)
                if interaction.kind == .plan && !collapsed {
                    Button { showFullPlan = true } label: {
                        Image(systemName: Theme.Icon.enlarge)
                            .font(Theme.Font.captionStrong)
                            .foregroundStyle(.secondary)
                            .frame(width: 28, height: 28)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("全屏查看计划")
                }
                Menu {
                    Button { Task { await model.resolve(interaction, .dismissed) } } label: {
                        Label("交给电脑上的终端处理", systemImage: Theme.Icon.machine)
                    }
                } label: {
                    Image(systemName: Theme.Icon.more)
                        .font(Theme.Font.captionStrong)
                        .foregroundStyle(.secondary)
                        .frame(width: 28, height: 28)
                        .contentShape(.rect)
                }
                Image(systemName: Theme.Icon.expand)
                    .font(Theme.Font.captionStrong)
                    .foregroundStyle(.secondary)
                    .rotationEffect(.degrees(collapsed ? 180 : 0))
                    .frame(width: 28, height: 28)
            }
            .contentShape(.rect)
            .onTapGesture { withAnimation(Theme.Motion.standard) { collapsed.toggle() } }
            if !collapsed {
                ScrollView {
                    form.onGeometryChange(for: CGFloat.self) { $0.size.height } action: { formHeight = $0 }
                }
                .scrollBounceBehavior(.basedOnSize)
                // At most a little over half of what the page has (not the screen: iPhone Duo's displays differ).
                .containerRelativeFrame(.vertical) { length, _ in min(formHeight == 0 ? length : formHeight, length * 0.55) }
                .transition(.opacity)
            }
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.top, Theme.Space.m)
        .padding(.bottom, Theme.Space.l)
        .surface(in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous))
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    @ViewBuilder private var form: some View {
        switch interaction.kind {
        case .question: QuestionForm(interaction: interaction)
        case .plan: PlanForm(interaction: interaction, showFull: $showFullPlan)
        case .permission: PermissionForm(interaction: interaction)
        case .screen: ScreenForm(interaction: interaction)
        }
    }

    private var previewText: String {
        switch interaction.kind {
        case .question: interaction.questions?.first?.question ?? ""
        case .plan: PlanText.title(interaction.plan ?? "")
        case .permission: interaction.summary ?? ""
        case .screen: interaction.screenDialog?.question ?? ""
        }
    }

    /// A terminal dialog that reads as a question is titled like one.
    private var panelTitle: String {
        if interaction.kind == .screen, interaction.screenDialog != nil {
            return "\(model.session(interaction.sessionKey)?.agentLabel ?? "Claude") 在问你"
        }
        return interaction.title
    }
}

/// AskUserQuestion, iOS-list style: the question, its options in one grouped box (numbered like the terminal, a
/// check when picked, 推荐 for the suggested one), "其他" as the box's last row. One question, one choice: a tap answers.
/// Several questions: "header · 1/2" on top, small 上一题 / 下一题 / 提交 below.
struct QuestionForm: View {
    @Environment(AppModel.self) private var model
    var interaction: Interaction
    @State private var picks: [String: Set<String>] = [:]
    @State private var other: [String: String] = [:]
    @State private var page = 0
    @State private var sending = false
    @State private var writing = false
    @FocusState private var otherFocused: Bool

    private var questions: [Question] { interaction.questions ?? [] }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            if questions.indices.contains(page) {
                let q = questions[page]
                if questions.count > 1 || q.header != nil || q.multiSelect {
                    Text([q.header.flatMap { $0.isEmpty ? nil : $0 },
                          questions.count > 1 ? "\(page + 1)/\(questions.count)" : nil,
                          q.multiSelect ? "可多选" : nil].compactMap { $0 }.joined(separator: " · "))
                        .monospacedDigit()
                    .font(Theme.Font.labelStrong)
                    .foregroundStyle(.secondary)
                }
                Text(q.question)
                    .font(Theme.Font.heading)
                    .fixedSize(horizontal: false, vertical: true)
                options(q)
                    .id(page)
                    .transition(.asymmetric(insertion: .move(edge: .trailing).combined(with: .opacity), removal: .opacity))
            }
            if needsFooter { footer }
        }
        .onChange(of: page) { _, p in writing = questions.indices.contains(p) && !(other[questions[p].question] ?? "").isEmpty }
    }

    // MARK: Options

    private func options(_ q: Question) -> some View {
        VStack(spacing: 0) {
            ForEach(Array(q.options.enumerated()), id: \.element.label) { i, opt in
                if i > 0 { Divider().padding(.leading, 50) }
                option(q, opt, number: i + 1)
            }
            Divider().padding(.leading, 50)
            otherRow(q)
        }
        .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
    }

    private func option(_ q: Question, _ opt: QuestionOption, number: Int) -> some View {
        let selected = picks[q.question, default: []].contains(opt.label)
        let (label, recommended) = Self.display(opt.label)
        return Button { pick(q, opt) } label: {
            HStack(alignment: .top, spacing: Theme.Space.m) {
                ZStack {
                    Circle().fill(selected ? Theme.accent : Theme.Fill.chip)
                    if selected {
                        Image(systemName: Theme.Icon.done).font(Theme.Font.labelHeavy).dynamicTypeSize(...DynamicTypeSize.xxLarge).foregroundStyle(.white)
                    } else {
                        Text("\(number)").font(Theme.Font.labelStrong.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
                .frame(width: 24, height: 24)
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    HStack(spacing: Theme.Space.sm) {
                        Text(label).font(Theme.Font.heading).foregroundStyle(.primary)
                        if recommended { RecommendedTag() }
                    }
                    if let d = opt.description, !d.isEmpty {
                        Text(d).font(Theme.Font.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    }
                }
                Spacer(minLength: 0)
            }
            .multilineTextAlignment(.leading)
            .padding(.horizontal, Theme.Space.l)
            .padding(.vertical, Theme.Space.m)
            .background(selected ? Theme.accent.opacity(0.08) : .clear)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(sending)
        .accessibilityLabel(label + (recommended ? "，推荐" : "") + (selected ? "，已选" : ""))
    }

    /// "其他": a quiet row; tapping it turns it into a field with its own send arrow.
    @ViewBuilder private func otherRow(_ q: Question) -> some View {
        let text = Binding(get: { other[q.question] ?? "" }, set: { other[q.question] = $0 })
        HStack(alignment: .center, spacing: Theme.Space.m) {
            Image(systemName: Theme.Icon.edit)
                .font(Theme.Font.labelStrong).dynamicTypeSize(...DynamicTypeSize.xxLarge)
                .foregroundStyle(.secondary)
                .frame(width: 24, height: 24)
                .background(Theme.Fill.chip, in: .circle)
            if writing {
                TextField("写下你的回答", text: text, axis: .vertical)
                    .lineLimit(1...4)
                    .focused($otherFocused)
                    .onChange(of: text.wrappedValue) { _, v in
                        if !v.trimmingCharacters(in: .whitespaces).isEmpty, !q.multiSelect { picks[q.question] = [] }
                    }
                if !needsFooter, !text.wrappedValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    Button { submit() } label: {
                        Image(systemName: "arrow.up.circle.fill").font(Theme.Font.title).foregroundStyle(Theme.accent)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("发送回答")
                }
            } else {
                Text("其他回答…").foregroundStyle(.secondary)
                Spacer(minLength: 0)
            }
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.m)
        .contentShape(.rect)
        .onTapGesture {
            guard !writing else { return }
            withAnimation(Theme.Motion.standard) { writing = true }
            otherFocused = true
        }
    }

    // MARK: Footer (several questions, or several choices)

    private var needsFooter: Bool { questions.count > 1 || questions.first?.multiSelect == true }

    private var footer: some View {
        HStack(spacing: Theme.Space.m) {
            if page > 0 {
                Button { withAnimation(Theme.Motion.standard) { page -= 1 } } label: { Text("上一题").lineLimit(1) }
                    .themedButton()
            }
            Spacer(minLength: 0)
            Button { advance() } label: {
                HStack(spacing: Theme.Space.sm) {
                    if sending { ProgressView().controlSize(.small).tint(.white) }
                    Text(isLast ? "提交" : "下一题").lineLimit(1)
                }
                .padding(.horizontal, Theme.Space.s)
            }
            .themedButton(prominent: true)
            .disabled(!canAdvance || sending)
        }
        .controlSize(.regular)
    }

    // MARK: Logic

    /// "取最长的那段 (Recommended)" → ("取最长的那段", true). The answer sent back keeps the original label.
    static func display(_ label: String) -> (String, Bool) { QuestionOption.display(label) }

    private func pick(_ q: Question, _ opt: QuestionOption) {
        Haptics.tap()
        var set = picks[q.question, default: []]
        if q.multiSelect {
            if set.contains(opt.label) { set.remove(opt.label) } else { set.insert(opt.label) }
        } else {
            set = [opt.label]
            other[q.question] = ""
            writing = false
        }
        withAnimation(.snappy(duration: 0.2)) { picks[q.question] = set }
        guard !q.multiSelect else { return }
        if questions.count == 1 {
            submit() // one question, one choice: the tap is the answer
        } else if page < questions.count - 1 {
            Task { try? await Task.sleep(for: .milliseconds(250)); withAnimation(Theme.Motion.standard) { page += 1 } }
        }
    }

    private var isLast: Bool { page >= questions.count - 1 || allAnswered }

    private func advance() {
        if isLast { submit() } else { withAnimation(Theme.Motion.standard) { page += 1 } }
    }

    private func answer(_ q: Question) -> String? {
        let custom = (other[q.question] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if !custom.isEmpty { return custom }
        let set = picks[q.question, default: []]
        guard !set.isEmpty else { return nil }
        return q.options.map(\.label).filter(set.contains).joined(separator: ", ")
    }

    private var allAnswered: Bool { questions.allSatisfy { answer($0) != nil } }
    private var canAdvance: Bool { questions.indices.contains(page) && answer(questions[page]) != nil }

    private func submit() {
        if !allAnswered {
            if let next = questions.firstIndex(where: { answer($0) == nil }) { withAnimation(Theme.Motion.standard) { page = next } }
            return
        }
        sending = true
        otherFocused = false
        var answers: [String: String] = [:]
        for q in questions { answers[q.question] = answer(q) }
        Task {
            await model.resolve(interaction, Resolution(answers: answers))
            sending = false
        }
    }
}

struct PlanForm: View {
    @Environment(AppModel.self) private var model
    var interaction: Interaction
    @Binding var showFull: Bool
    @State private var feedback = ""
    @State private var rejecting = false

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            ScrollView {
                Markdown(MarkdownMemo.content(interaction.plan ?? ""))
                    .markdownTheme(.jarvis)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(maxHeight: 200)
            .scrollBounceBehavior(.basedOnSize)
            if rejecting {
                TextField("告诉 Claude 要怎么改…", text: $feedback, axis: .vertical)
                    .lineLimit(2...6)
                    .padding(Theme.Space.m)
                    .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
                HStack {
                    Button("取消") { withAnimation { rejecting = false } }.themedButton()
                    Button {
                        Task { await model.resolve(interaction, Resolution(approve: false, feedback: feedback)) }
                    } label: { Text("打回修改").lineLimit(1).minimumScaleFactor(0.8).frame(maxWidth: .infinity) }
                        .themedButton(prominent: true)
                }
            } else {
                Button {
                    Task { await model.resolve(interaction, Resolution(approve: true, mode: "bypassPermissions")) }
                } label: {
                    Label("批准并自动执行", systemImage: Theme.Icon.run).lineLimit(1).minimumScaleFactor(0.8).frame(maxWidth: .infinity)
                }
                .themedButton(prominent: true)
                .controlSize(.large)
                HStack {
                    Button {
                        Task { await model.resolve(interaction, Resolution(approve: true, mode: "acceptEdits")) }
                    } label: { Text("自动接受编辑").lineLimit(1).minimumScaleFactor(0.8).frame(maxWidth: .infinity) }
                    Button { withAnimation { rejecting = true } } label: { Text("打回").lineLimit(1).minimumScaleFactor(0.8).frame(maxWidth: .infinity) }
                }
                .themedButton()
            }
        }
        .sheet(isPresented: $showFull) {
            NavigationStack {
                ScrollView {
                    Markdown(MarkdownMemo.content(interaction.plan ?? "")).markdownTheme(.jarvis).padding()
                }
                .navigationTitle(PlanText.title(interaction.plan ?? ""))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { showFull = false } } }
            }
        }
    }
}

struct PermissionForm: View {
    @Environment(AppModel.self) private var model
    var interaction: Interaction

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            HStack(spacing: Theme.Space.s) {
                Image(systemName: ToolPresentation.symbol(interaction.toolName ?? "")).foregroundStyle(Theme.accent)
                Text(interaction.toolName ?? "工具").font(Theme.Font.secondaryStrong)
            }
            Text(interaction.summary ?? interaction.toolInput?.pretty ?? "")
                .font(Theme.Font.code)
                .padding(Theme.Space.m)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.codeBackground, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
                .textSelection(.enabled)
            HStack {
                Button { Task { await model.resolve(interaction, Resolution(allow: false)) } } label: {
                    Text("拒绝").lineLimit(1).minimumScaleFactor(0.8).frame(maxWidth: .infinity)
                }
                .themedButton()
                Button { Task { await model.resolve(interaction, Resolution(allow: true)) } } label: {
                    Text("允许").lineLimit(1).minimumScaleFactor(0.8).frame(maxWidth: .infinity)
                }
                .themedButton(prominent: true)
            }
            .controlSize(.large)
            if interaction.sessionID != "" {
                Button("本会话总是允许") {
                    Task { await model.resolve(interaction, Resolution(allow: true, always: true)) }
                }
                .font(Theme.Font.caption)
                .frame(maxWidth: .infinity)
            }
        }
    }
}

/// The terminal waiting on a choice (AskUserQuestion that bypassed the hook, a permission prompt…). The dialog is read
/// off the screen and laid out like a question: header, question, numbered options with their descriptions, "其他回答…"
/// for "Type something." The raw terminal and its keys stay one tap away; a screen with no dialog shows them directly.
struct ScreenForm: View {
    @Environment(AppModel.self) private var model
    var interaction: Interaction
    @State private var text = ""
    @State private var writing = false
    @State private var sending: String?
    @State private var showTerminal = false
    @FocusState private var focused: Bool

    var body: some View {
        let screen = interaction.screen ?? ""
        if let d = interaction.screenDialog {
            dialog(d, screen: screen)
        } else {
            VStack(alignment: .leading, spacing: Theme.Space.m) { terminal(screen) }
        }
    }

    private func dialog(_ d: ScreenParser.Dialog, screen: String) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            if !d.steps.isEmpty {
                ScreenSteps(steps: d.steps)
            } else if let h = d.header, !h.isEmpty {
                Text(h).font(Theme.Font.labelStrong).foregroundStyle(.secondary)
            }
            if !d.question.isEmpty {
                Text(d.question).font(Theme.Font.heading).fixedSize(horizontal: false, vertical: true)
            }
            if let detail = d.detail {
                Text(detail)
                    .font(Theme.Font.code)
                    .foregroundStyle(.secondary)
                    .padding(Theme.Space.m)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            }
            let choices = d.options.filter { $0.kind != .chat }
            VStack(spacing: 0) {
                ForEach(Array(choices.enumerated()), id: \.element.key) { i, opt in
                    if i > 0 { Divider().padding(.leading, 50) }
                    if opt.kind == .typeSomething { typeRow(opt) } else { choice(opt) }
                }
            }
            .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            HStack(spacing: Theme.Space.l) {
                if let chat = d.options.first(where: { $0.kind == .chat }) {
                    Button { send(chat.key, answer(chat)) } label: {
                        Label("改在对话里说", systemImage: "bubble.left").lineLimit(1)
                    }
                }
                Spacer(minLength: 0)
                Button { withAnimation(Theme.Motion.standard) { showTerminal.toggle() } } label: {
                    Label(showTerminal ? "收起终端" : "查看终端", systemImage: Theme.Icon.terminal).lineLimit(1)
                }
            }
            .font(Theme.Font.captionMedium)
            .buttonStyle(.plain)
            .foregroundStyle(Theme.accent)
            if showTerminal { terminal(screen) }
        }
    }

    private func choice(_ opt: ScreenParser.Dialog.Option) -> some View {
        let (label, recommended) = QuestionForm.display(opt.label)
        return Button { send(opt.key, answer(opt)) } label: {
            HStack(alignment: .top, spacing: Theme.Space.m) {
                ZStack {
                    Circle().fill(sending == opt.key ? Theme.accent : Theme.Fill.chip)
                    if sending == opt.key {
                        ProgressView().controlSize(.mini).tint(.white)
                    } else {
                        Text(opt.key).font(Theme.Font.labelStrong.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
                .frame(width: 24, height: 24)
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    HStack(spacing: Theme.Space.sm) {
                        Text(label).font(Theme.Font.heading).foregroundStyle(.primary)
                        if recommended { RecommendedTag() }
                    }
                    if let d = opt.detail {
                        Text(d).font(Theme.Font.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    }
                }
                Spacer(minLength: 0)
            }
            .multilineTextAlignment(.leading)
            .padding(.horizontal, Theme.Space.l)
            .padding(.vertical, Theme.Space.m)
            .background(sending == opt.key ? Theme.accent.opacity(0.08) : .clear)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(sending != nil)
        .accessibilityLabel(label + (recommended ? "，推荐" : ""))
    }

    /// "Type something.": a quiet row that turns into a field; sending picks the option, types, presses enter.
    private func typeRow(_ opt: ScreenParser.Dialog.Option) -> some View {
        HStack(alignment: .center, spacing: Theme.Space.m) {
            Image(systemName: Theme.Icon.edit)
                .font(Theme.Font.labelStrong).dynamicTypeSize(...DynamicTypeSize.xxLarge)
                .foregroundStyle(.secondary)
                .frame(width: 24, height: 24)
                .background(Theme.Fill.chip, in: .circle)
            if writing {
                TextField("写下你的回答", text: $text, axis: .vertical)
                    .lineLimit(1...4)
                    .focused($focused)
                    .submitLabel(.send)
                if !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    Button {
                        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
                        focused = false
                        send(opt.key, answer(opt, text: t))
                    } label: {
                        if sending == opt.key { ProgressView().controlSize(.small) }
                        else { Image(systemName: "arrow.up.circle.fill").font(Theme.Font.title).foregroundStyle(Theme.accent) }
                    }
                    .buttonStyle(.plain)
                    .disabled(sending != nil)
                    .accessibilityLabel("发送回答")
                }
            } else {
                Text("其他回答…").foregroundStyle(.secondary)
                Spacer(minLength: 0)
            }
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.m)
        .contentShape(.rect)
        .onTapGesture {
            guard !writing else { return }
            withAnimation(Theme.Motion.standard) { writing = true }
            focused = true
        }
    }

    /// The raw screen, its keys and a line of text: for anything the dialog reading did not catch.
    @ViewBuilder private func terminal(_ screen: String) -> some View {
        TerminalText(text: screen, maxLines: 18)
            .frame(maxHeight: 220)
        TerminalKeyBar { keys in Task { await model.resolve(interaction, Resolution(keys: keys)) } }
        HStack {
            Group {
                if interaction.secret == true {
                    SecureField("输入密码（不会保存）", text: $text)
                } else {
                    TextField("输入文字发送到终端", text: $text)
                }
            }
            .textFieldStyle(.plain)
            .padding(Theme.Space.m)
            .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            Button {
                let t = text
                text = ""
                Task { await model.resolve(interaction, Resolution(keys: ["enter"], text: t)) }
            } label: { Image(systemName: Theme.Icon.enter) }
                .themedButton(prominent: true)
                .disabled(text.isEmpty)
        }
    }

    /// The answer for an option: by its key when the hub read the dialog (it checks the screen first), else the keys.
    private func answer(_ opt: ScreenParser.Dialog.Option, text: String? = nil) -> Resolution {
        if interaction.dialog != nil { return Resolution(text: text, option: opt.key) }
        if let text { return Resolution(keys: ["enter"], text: text, preKeys: [opt.key]) }
        return Resolution(keys: [opt.key])
    }

    private func send(_ key: String, _ r: Resolution) {
        Haptics.tap()
        sending = key
        Task {
            await model.resolve(interaction, r)
            sending = nil
        }
    }
}

/// A multi-question dialog's questions, from the terminal's tab bar: done ones checked, the rest numbered.
struct ScreenSteps: View {
    var steps: [ScreenParser.Dialog.Step]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Theme.Space.sm) {
                ForEach(Array(steps.enumerated()), id: \.offset) { i, s in
                    HStack(spacing: Theme.Space.xs) {
                        Group {
                            if s.done { Image(systemName: Theme.Icon.done).font(.system(size: 8, weight: .bold)) }
                            else { Text("\(i + 1)").font(.system(size: 9, weight: .bold).monospacedDigit()) } // type-scale: glyph (a step number in a 16 pt circle)
                        }
                        .foregroundStyle(s.done ? .white : .secondary)
                        .frame(width: 15, height: 15)
                        .background(s.done ? Theme.positive : Color.primary.opacity(0.1), in: .circle)
                        Text(s.label).lineLimit(1)
                    }
                    .font(Theme.Font.labelMedium)
                    .foregroundStyle(s.done ? .secondary : .primary)
                    .padding(.leading, Theme.Space.xs).padding(.trailing, Theme.Space.s).padding(.vertical, Theme.Space.xs)
                    .background(Color.primary.opacity(0.05), in: .capsule)
                }
            }
        }
        .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("共 \(steps.count) 个问题，已答 \(steps.filter(\.done).count) 个")
    }
}

struct TerminalText: View {
    var text: String
    var maxLines: Int = 40

    var body: some View {
        let lines = text.components(separatedBy: "\n")
        let shown = lines.suffix(maxLines).joined(separator: "\n")
        ScrollView([.vertical, .horizontal]) {
            Text(shown)
                .font(Theme.Font.label.monospaced())
                .foregroundStyle(Theme.terminalText)
                .textSelection(.enabled)
                .padding(Theme.Space.m)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .defaultScrollAnchor(.bottomLeading)
        .background(Theme.terminalBackground, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}

#if DEBUG
extension Interaction {
    /// A two-question AskUserQuestion (descriptions, a recommended option, a multi-select) for screenshots.
    /// The terminal waiting on an AskUserQuestion dialog (the hook did not catch it), for screenshots.
    static func sampleScreen(_ key: SessionKey) -> Interaction? {
        let screen = [
            " Ran 1 shell command",
            "────────────────────────────────────────────────",
            " ←  ☐ 取消发布单  ☐ 推送代码  ✔ Submit  →",
            "",
            " │沙箱镜像第一次构建失败：SES 用 garble 混淆编译，而我选的 hertz-cli 版本",
            " │不支持，换版本后编译通过，修复也已推送（a7553e20）。但失败的那张发布单 37997",
            " │卡在构建阶段，没法重新构建。要取消它吗？",
            "",
            "❯ 1. 同意取消 37997 (Recommended)",
            "     用 example-cli release delivery cancel 37997 取消，然后用修复后的 commit 重新发一次。",
            "  2. 先不取消",
            "     我停在这里；镜像、Nacos 和 e2e 都先不动，release 7807 也暂时保留。",
            "  3. Type something.",
            "────────────────────────────────────────────────",
            "  4. Chat about this",
            "",
            " Enter to select · ↑/↓ to navigate · Esc to cancel",
        ].joined(separator: "\n")
        let payload: [String: Any] = ["id": "sample-s", "machine_id": key.machineID, "session_id": key.sessionID, "kind": "screen", "screen": screen]
        guard let data = try? JSONSerialization.data(withJSONObject: payload) else { return nil }
        return try? JSON.decoder.decode(Interaction.self, from: data)
    }

    static func sampleQuestion(_ key: SessionKey) -> Interaction? {
        let json = """
        {"id":"sample-q","machine_id":"\(key.machineID)","session_id":"\(key.sessionID)","kind":"question","questions":[
         {"question":"一轮对话里有后台任务把 Claude 唤醒时，哪一段算「真正的回复」？","header":"真正的回复","multi_select":false,"options":[
          {"label":"取最长的那段 (Recommended)","description":"唤醒前后各有一段收尾的话，取最长的作为回复；其余收进工作行和抽屉。"},
          {"label":"永远取最后一段","description":"所有工具合成一行，回复永远是最后一段话。"},
          {"label":"永远取唤醒前那段","description":"唤醒后的工具和回复全部收进「后台任务完成」。"}]},
         {"question":"抽屉里要显示哪些内容？","header":"抽屉内容","multi_select":true,"options":[
          {"label":"工具调用","description":"唤醒后跑的每个工具"},
          {"label":"思考","description":null},
          {"label":"回复","description":"唤醒后 Claude 说的话"}]}]}
        """
        return try? JSON.decoder.decode(Interaction.self, from: Data(json.utf8))
    }
}
#endif
