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
        case .screen: "terminal.fill"
        }
    }
}

/// Compact card on the chat list: enough context plus one-tap answers for simple questions.
struct InteractionPreviewCard: View {
    @Environment(AppModel.self) private var model
    var interaction: Interaction
    var open: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: interaction.kind.symbol)
                    .foregroundStyle(Theme.attention)
                    .symbolEffect(.wiggle, options: .repeat(.periodic(delay: 3)))
                VStack(alignment: .leading, spacing: 1) {
                    Text(interaction.kind.title).font(.subheadline.weight(.semibold))
                    Text([interaction.sessionTitle, model.machine(interaction.machineID)?.name].compactMap { $0 }.joined(separator: " · "))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
                Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
            }
            Text(summary)
                .font(.callout)
                .lineLimit(3)
                .frame(maxWidth: .infinity, alignment: .leading)
            quickActions
        }
        .card()
        .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous).stroke(Theme.attention.opacity(0.35), lineWidth: 1))
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
            HStack(spacing: 8) {
                ForEach(labels, id: \.self) { label in
                    Button(label) { action(label) }
                        .themedButton()
                        .controlSize(.small)
                }
            }
        }
        .scrollClipDisabled()
    }
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

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Image(systemName: interaction.kind.symbol).foregroundStyle(Theme.attention)
                Text(interaction.kind.title).font(.subheadline.weight(.semibold))
                if collapsed {
                    Text(previewText).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer(minLength: 4)
                Image(systemName: "chevron.down")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .rotationEffect(.degrees(collapsed ? 180 : 0))
                Menu {
                    Button { Task { await model.resolve(interaction, .dismissed) } } label: {
                        Label("交给电脑上的终端处理", systemImage: "desktopcomputer")
                    }
                } label: {
                    Image(systemName: "ellipsis.circle").foregroundStyle(.secondary)
                }
            }
            .contentShape(.rect)
            .onTapGesture { withAnimation(.snappy) { collapsed.toggle() } }
            if !collapsed {
                ScrollView {
                    form.onGeometryChange(for: CGFloat.self) { $0.size.height } action: { formHeight = $0 }
                }
                .scrollBounceBehavior(.basedOnSize)
                .frame(height: formHeight == 0 ? nil : min(formHeight, UIScreen.main.bounds.height * 0.45))
                .transition(.opacity)
            }
        }
        .padding(16)
        .surface(in: .rect(cornerRadius: 24, style: .continuous))
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    @ViewBuilder private var form: some View {
        switch interaction.kind {
        case .question: QuestionForm(interaction: interaction)
        case .plan: PlanForm(interaction: interaction)
        case .permission: PermissionForm(interaction: interaction)
        case .screen: ScreenForm(interaction: interaction)
        }
    }

    private var previewText: String {
        switch interaction.kind {
        case .question: interaction.questions?.first?.question ?? ""
        case .plan: PlanText.title(interaction.plan ?? "")
        case .permission: interaction.summary ?? ""
        case .screen: ""
        }
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
        VStack(alignment: .leading, spacing: 12) {
            if questions.indices.contains(page) {
                let q = questions[page]
                if questions.count > 1 || q.header != nil || q.multiSelect {
                    Text([q.header.flatMap { $0.isEmpty ? nil : $0 },
                          questions.count > 1 ? "\(page + 1)/\(questions.count)" : nil,
                          q.multiSelect ? "可多选" : nil].compactMap { $0 }.joined(separator: " · "))
                        .monospacedDigit()
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                }
                Text(q.question)
                    .font(.headline)
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
        .background(Color.primary.opacity(0.045), in: .rect(cornerRadius: 16, style: .continuous))
    }

    private func option(_ q: Question, _ opt: QuestionOption, number: Int) -> some View {
        let selected = picks[q.question, default: []].contains(opt.label)
        let (label, recommended) = Self.display(opt.label)
        return Button { pick(q, opt) } label: {
            HStack(alignment: .top, spacing: 12) {
                ZStack {
                    Circle().fill(selected ? Theme.accent : Color.primary.opacity(0.08))
                    if selected {
                        Image(systemName: "checkmark").font(.system(size: 11, weight: .bold)).foregroundStyle(.white)
                    } else {
                        Text("\(number)").font(.caption.weight(.semibold).monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
                .frame(width: 24, height: 24)
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        Text(label).font(.body.weight(.medium)).foregroundStyle(.primary)
                        if recommended {
                            Text("推荐")
                                .font(.caption2.weight(.semibold))
                                .foregroundStyle(Theme.accent)
                                .padding(.horizontal, 6)
                                .padding(.vertical, 2)
                                .background(Theme.accent.opacity(0.12), in: .capsule)
                                .fixedSize()
                        }
                    }
                    if let d = opt.description, !d.isEmpty {
                        Text(d).font(.footnote).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    }
                }
                Spacer(minLength: 0)
            }
            .multilineTextAlignment(.leading)
            .padding(.horizontal, 14)
            .padding(.vertical, 11)
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
        HStack(alignment: .center, spacing: 12) {
            Image(systemName: "pencil")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(.secondary)
                .frame(width: 24, height: 24)
                .background(Color.primary.opacity(0.08), in: .circle)
            if writing {
                TextField("写下你的回答", text: text, axis: .vertical)
                    .lineLimit(1...4)
                    .focused($otherFocused)
                    .onChange(of: text.wrappedValue) { _, v in
                        if !v.trimmingCharacters(in: .whitespaces).isEmpty, !q.multiSelect { picks[q.question] = [] }
                    }
                if !needsFooter, !text.wrappedValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    Button { submit() } label: {
                        Image(systemName: "arrow.up.circle.fill").font(.title2).foregroundStyle(Theme.accent)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("发送回答")
                }
            } else {
                Text("其他回答…").foregroundStyle(.secondary)
                Spacer(minLength: 0)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .contentShape(.rect)
        .onTapGesture {
            guard !writing else { return }
            withAnimation(.snappy) { writing = true }
            otherFocused = true
        }
    }

    // MARK: Footer (several questions, or several choices)

    private var needsFooter: Bool { questions.count > 1 || questions.first?.multiSelect == true }

    private var footer: some View {
        HStack(spacing: 10) {
            if page > 0 {
                Button { withAnimation(.snappy) { page -= 1 } } label: { Text("上一题").lineLimit(1) }
                    .themedButton()
            }
            Spacer(minLength: 0)
            Button { advance() } label: {
                HStack(spacing: 6) {
                    if sending { ProgressView().controlSize(.small).tint(.white) }
                    Text(isLast ? "提交" : "下一题").lineLimit(1)
                }
                .padding(.horizontal, 8)
            }
            .themedButton(prominent: true)
            .disabled(!canAdvance || sending)
        }
        .controlSize(.regular)
    }

    // MARK: Logic

    /// "取最长的那段 (Recommended)" → ("取最长的那段", true). The answer sent back keeps the original label.
    static func display(_ label: String) -> (String, Bool) {
        for suffix in [" (Recommended)", "(Recommended)", "（推荐）", "(推荐)"] where label.hasSuffix(suffix) {
            return (String(label.dropLast(suffix.count)).trimmingCharacters(in: .whitespaces), true)
        }
        return (label, false)
    }

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
            Task { try? await Task.sleep(for: .milliseconds(250)); withAnimation(.snappy) { page += 1 } }
        }
    }

    private var isLast: Bool { page >= questions.count - 1 || allAnswered }

    private func advance() {
        if isLast { submit() } else { withAnimation(.snappy) { page += 1 } }
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
            if let next = questions.firstIndex(where: { answer($0) == nil }) { withAnimation(.snappy) { page = next } }
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
    @State private var feedback = ""
    @State private var rejecting = false
    @State private var showFull = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ScrollView {
                Markdown(interaction.plan ?? "")
                    .markdownTheme(.jarvis)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(maxHeight: 200)
            .scrollBounceBehavior(.basedOnSize)
            .overlay(alignment: .bottomTrailing) {
                Button { showFull = true } label: { Image(systemName: "arrow.up.left.and.arrow.down.right") }
                    .themedButton()
                    .controlSize(.small)
                    .padding(4)
            }
            if rejecting {
                TextField("告诉 Claude 要怎么改…", text: $feedback, axis: .vertical)
                    .lineLimit(2...6)
                    .padding(12)
                    .background(Color.primary.opacity(0.05), in: .rect(cornerRadius: 14, style: .continuous))
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
                    Label("批准并自动执行", systemImage: "play.fill").lineLimit(1).minimumScaleFactor(0.8).frame(maxWidth: .infinity)
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
                    Markdown(interaction.plan ?? "").markdownTheme(.jarvis).padding()
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
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Image(systemName: ToolPresentation.symbol(interaction.toolName ?? "")).foregroundStyle(Theme.accent)
                Text(interaction.toolName ?? "工具").font(.subheadline.weight(.semibold))
            }
            Text(interaction.summary ?? interaction.toolInput?.pretty ?? "")
                .font(.system(.footnote, design: .monospaced))
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.codeBackground, in: .rect(cornerRadius: 12, style: .continuous))
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
                .font(.footnote)
                .frame(maxWidth: .infinity)
            }
        }
    }
}

struct ScreenForm: View {
    @Environment(AppModel.self) private var model
    var interaction: Interaction
    @State private var text = ""

    var body: some View {
        let options = ScreenParser.options(interaction.screen ?? "")
        VStack(alignment: .leading, spacing: 12) {
            TerminalText(text: interaction.screen ?? "", maxLines: 18)
                .frame(maxHeight: 220)
            if !options.isEmpty {
                VStack(spacing: 6) {
                    ForEach(options, id: \.key) { opt in
                        Button {
                            Task { await model.resolve(interaction, Resolution(keys: [opt.key])) }
                        } label: {
                            HStack {
                                Text(opt.key).font(.caption.weight(.bold).monospacedDigit())
                                    .frame(width: 22, height: 22)
                                    .background(Theme.accent.opacity(0.15), in: .circle)
                                Text(opt.label).font(.subheadline).multilineTextAlignment(.leading)
                                Spacer(minLength: 0)
                                if opt.selected { Image(systemName: "chevron.left").font(.caption).foregroundStyle(.secondary) }
                            }
                            .padding(10)
                            .background(Color.primary.opacity(0.05), in: .rect(cornerRadius: 12, style: .continuous))
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            TerminalKeyBar { keys in Task { await model.resolve(interaction, Resolution(keys: keys)) } }
            HStack {
                TextField("输入文字发送到终端", text: $text)
                    .textFieldStyle(.plain)
                    .padding(10)
                    .background(Color.primary.opacity(0.05), in: .rect(cornerRadius: 12, style: .continuous))
                Button {
                    let t = text
                    text = ""
                    Task { await model.resolve(interaction, Resolution(keys: ["enter"], text: t)) }
                } label: { Image(systemName: "return") }
                    .themedButton(prominent: true)
                    .disabled(text.isEmpty)
            }
        }
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
                .font(.system(size: 10.5, design: .monospaced))
                .foregroundStyle(Theme.terminalText)
                .textSelection(.enabled)
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .defaultScrollAnchor(.bottomLeading)
        .background(Theme.terminalBackground, in: .rect(cornerRadius: 12, style: .continuous))
    }
}

#if DEBUG
extension Interaction {
    /// A two-question AskUserQuestion (descriptions, a recommended option, a multi-select) for screenshots.
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
