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
                    .buttonStyle(.glassProminent)
                Button("拒绝") { Task { await model.resolve(interaction, Resolution(allow: false)) } }
                    .buttonStyle(.glass)
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
                        .buttonStyle(.glass)
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

/// Full interaction UI pinned above the composer in a chat.
struct InteractionPanel: View {
    @Environment(AppModel.self) private var model
    var interaction: Interaction

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 8) {
                Image(systemName: interaction.kind.symbol).foregroundStyle(Theme.attention)
                Text(interaction.kind.title).font(.subheadline.weight(.semibold))
                Spacer()
                Menu {
                    Button { Task { await model.resolve(interaction, .dismissed) } } label: {
                        Label("交给电脑上的终端处理", systemImage: "desktopcomputer")
                    }
                } label: {
                    Image(systemName: "ellipsis.circle").foregroundStyle(.secondary)
                }
            }
            switch interaction.kind {
            case .question: QuestionForm(interaction: interaction)
            case .plan: PlanForm(interaction: interaction)
            case .permission: PermissionForm(interaction: interaction)
            case .screen: ScreenForm(interaction: interaction)
            }
        }
        .padding(16)
        .glassEffect(.regular, in: .rect(cornerRadius: 24, style: .continuous))
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }
}

struct QuestionForm: View {
    @Environment(AppModel.self) private var model
    var interaction: Interaction
    @State private var picks: [String: Set<String>] = [:]
    @State private var other: [String: String] = [:]
    @State private var page = 0
    @State private var sending = false

    private var questions: [Question] { interaction.questions ?? [] }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if questions.count > 1 {
                Picker("问题", selection: $page) {
                    ForEach(Array(questions.enumerated()), id: \.offset) { i, q in
                        Text(q.header ?? "问题 \(i + 1)").tag(i)
                    }
                }
                .pickerStyle(.segmented)
            }
            if questions.indices.contains(page) {
                let q = questions[page]
                Text(q.question).font(.body.weight(.medium)).fixedSize(horizontal: false, vertical: true)
                if q.multiSelect { Text("可多选").font(.caption).foregroundStyle(.secondary) }
                VStack(spacing: 8) {
                    ForEach(q.options, id: \.label) { opt in option(q, opt) }
                    TextField("其他（自己写）", text: Binding(get: { other[q.question] ?? "" }, set: { other[q.question] = $0 }), axis: .vertical)
                        .lineLimit(1...4)
                        .padding(12)
                        .background(Color.primary.opacity(0.05), in: .rect(cornerRadius: 14, style: .continuous))
                }
            }
            Button {
                submit()
            } label: {
                HStack {
                    if sending { ProgressView().tint(.white) }
                    Text(questions.count > 1 && page < questions.count - 1 && !allAnswered ? "下一题" : "提交回答")
                        .frame(maxWidth: .infinity)
                }
            }
            .buttonStyle(.glassProminent)
            .controlSize(.large)
            .disabled(!canAdvance || sending)
        }
    }

    private func option(_ q: Question, _ opt: QuestionOption) -> some View {
        let selected = picks[q.question, default: []].contains(opt.label)
        return Button {
            Haptics.tap()
            var set = picks[q.question, default: []]
            if q.multiSelect {
                if selected { set.remove(opt.label) } else { set.insert(opt.label) }
            } else {
                set = [opt.label]
                other[q.question] = ""
            }
            picks[q.question] = set
            // Single question, single choice: answer right away.
            if questions.count == 1 && !q.multiSelect { submit() }
        } label: {
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: q.multiSelect ? (selected ? "checkmark.square.fill" : "square") : (selected ? "largecircle.fill.circle" : "circle"))
                    .foregroundStyle(selected ? Theme.accent : .secondary)
                    .font(.title3)
                VStack(alignment: .leading, spacing: 2) {
                    Text(opt.label).font(.body.weight(.medium)).foregroundStyle(.primary)
                    if let d = opt.description, !d.isEmpty {
                        Text(d).font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    }
                }
                Spacer(minLength: 0)
            }
            .multilineTextAlignment(.leading)
            .padding(12)
            .background(selected ? Theme.accent.opacity(0.12) : Color.primary.opacity(0.05), in: .rect(cornerRadius: 14, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).stroke(selected ? Theme.accent.opacity(0.5) : .clear, lineWidth: 1))
        }
        .buttonStyle(.plain)
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
            if let next = questions.firstIndex(where: { answer($0) == nil }) { withAnimation { page = next } }
            return
        }
        sending = true
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
            .frame(maxHeight: 260)
            .scrollBounceBehavior(.basedOnSize)
            .overlay(alignment: .bottomTrailing) {
                Button { showFull = true } label: { Image(systemName: "arrow.up.left.and.arrow.down.right") }
                    .buttonStyle(.glass)
                    .controlSize(.small)
                    .padding(4)
            }
            if rejecting {
                TextField("告诉 Claude 要怎么改…", text: $feedback, axis: .vertical)
                    .lineLimit(2...6)
                    .padding(12)
                    .background(Color.primary.opacity(0.05), in: .rect(cornerRadius: 14, style: .continuous))
                HStack {
                    Button("取消") { withAnimation { rejecting = false } }.buttonStyle(.glass)
                    Button {
                        Task { await model.resolve(interaction, Resolution(approve: false, feedback: feedback)) }
                    } label: { Text("打回修改").frame(maxWidth: .infinity) }
                        .buttonStyle(.glassProminent)
                }
            } else {
                Button {
                    Task { await model.resolve(interaction, Resolution(approve: true, mode: "bypassPermissions")) }
                } label: {
                    Label("批准并自动执行", systemImage: "play.fill").frame(maxWidth: .infinity)
                }
                .buttonStyle(.glassProminent)
                .controlSize(.large)
                HStack {
                    Button {
                        Task { await model.resolve(interaction, Resolution(approve: true, mode: "acceptEdits")) }
                    } label: { Text("批准，自动接受编辑").frame(maxWidth: .infinity) }
                    Button { withAnimation { rejecting = true } } label: { Text("打回").frame(maxWidth: .infinity) }
                }
                .buttonStyle(.glass)
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
                    Text("拒绝").frame(maxWidth: .infinity)
                }
                .buttonStyle(.glass)
                Button { Task { await model.resolve(interaction, Resolution(allow: true)) } } label: {
                    Text("允许").frame(maxWidth: .infinity)
                }
                .buttonStyle(.glassProminent)
            }
            .controlSize(.large)
            if interaction.sessionID != "" {
                Button("本会话内总是允许 \(interaction.toolName ?? "")") {
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
            KeyPad { keys in Task { await model.resolve(interaction, Resolution(keys: keys)) } }
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
                    .buttonStyle(.glassProminent)
                    .disabled(text.isEmpty)
            }
        }
    }
}

/// Common terminal keys for TUI dialogs.
struct KeyPad: View {
    var send: ([String]) -> Void
    private let keys: [(label: String, symbol: String?, keys: [String])] = [
        ("Esc", nil, ["esc"]), ("", "arrow.up", ["up"]), ("", "arrow.down", ["down"]),
        ("Tab", nil, ["tab"]), ("⇧Tab", nil, ["shift+tab"]), ("^C", nil, ["ctrl+c"]), ("", "return", ["enter"]),
    ]

    var body: some View {
        HStack(spacing: 6) {
            ForEach(Array(keys.enumerated()), id: \.offset) { _, k in
                Button {
                    Haptics.tap()
                    send(k.keys)
                } label: {
                    Group {
                        if let s = k.symbol { Image(systemName: s) } else { Text(k.label) }
                    }
                    .font(.footnote.weight(.semibold).monospaced())
                    .frame(maxWidth: .infinity, minHeight: 34)
                }
                .buttonStyle(.glass)
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
