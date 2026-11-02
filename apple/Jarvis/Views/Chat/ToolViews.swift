import BeautifulUI
import MarkdownUI
import SwiftUI

/// How each Claude Code tool is named and summarized in the chat.
enum ToolPresentation {
    nonisolated static func baseName(_ name: String) -> String {
        if name.hasPrefix("mcp__") { return name.split(separator: "__").last.map(String.init) ?? name }
        return name
    }

    static func symbol(_ name: String) -> String {
        let n = baseName(name)
        if n.hasPrefix("phone_") { return "iphone" }
        if n.hasPrefix("mail_") { return "envelope" }
        switch n {
        case "conversation_rename": return Theme.Icon.edit
        case "conversation_archive": return Theme.Icon.archive
        case "conversation_unarchive": return Theme.Icon.unarchive
        case "conversations_list", "conversation_read", "conversation_new", "conversation_send": return Theme.Icon.conversation
        case "Bash", "BashOutput", "KillShell", "KillBash": return "terminal"
        case "Read": return "doc.text"
        case "Edit", "MultiEdit", "NotebookEdit": return "pencil.line"
        case "Write": return "doc.badge.plus"
        case "Grep": return "text.magnifyingglass"
        case "Glob", "LS": return "folder.badge.questionmark"
        case "WebFetch": return "globe"
        case "WebSearch": return "magnifyingglass"
        case "Task", "Agent": return "person.2"
        case "TodoWrite", "TaskCreate", "TaskUpdate": return "checklist"
        case "AskUserQuestion": return "questionmark.bubble"
        case "ExitPlanMode", "EnterPlanMode": return "list.bullet.clipboard"
        case "ToolSearch": return "wrench.and.screwdriver"
        case "Skill": return "sparkles"
        case "Monitor": return "waveform.path.ecg"
        default: return name.hasPrefix("mcp__") ? "puzzlepiece.extension" : Theme.Icon.unknownTool
        }
    }

    static func verb(_ name: String) -> String {
        let n = baseName(name)
        if n.hasPrefix("phone_") { return "iPhone" }
        if n.hasPrefix("mail_") { return "邮件" }
        switch n {
        // Jarvis managing its own conversations (docs/assistant.md「管理自己的对话」).
        case "conversations_list": return "查看对话列表"
        case "conversation_read": return "读对话"
        case "conversation_rename": return "重命名对话"
        case "conversation_archive": return "归档对话"
        case "conversation_unarchive": return "取消归档"
        case "conversation_new": return "新开对话"
        case "conversation_send": return "发到对话"
        case "Bash": return "运行"
        case "Read": return "读取"
        case "Edit", "MultiEdit": return "编辑"
        case "Write": return "写入"
        case "Grep": return "搜索"
        case "Glob", "LS": return "查找"
        case "WebFetch": return "打开网页"
        case "WebSearch": return "网页搜索"
        case "Task", "Agent": return "子任务"
        case "TodoWrite": return "更新待办"
        case "AskUserQuestion": return "提问"
        case "ExitPlanMode": return "提交计划"
        case "EnterPlanMode": return "进入计划模式"
        case "ToolSearch": return "加载工具"
        case "Skill": return "使用技能"
        case "Monitor": return "监视"
        default: return n
        }
    }

    static func subject(_ call: ToolCall) -> String {
        let i = call.input
        let n = baseName(call.name)
        func s(_ k: String) -> String? { i[k]?.string.flatMap { $0.isEmpty ? nil : $0 } }
        switch n {
        case "Bash": return s("description") ?? firstLine(s("command") ?? "")
        case "Read", "Edit", "MultiEdit", "Write", "NotebookEdit":
            return ((s("file_path") ?? s("notebook_path") ?? "") as NSString).lastPathComponent
        case "Grep": return s("pattern") ?? ""
        case "Glob": return s("pattern") ?? ""
        case "WebFetch": return URL(string: s("url") ?? "")?.host() ?? s("url") ?? ""
        case "WebSearch": return s("query") ?? ""
        case "Task", "Agent": return s("description") ?? ""
        case "Monitor": return s("description") ?? firstLine(s("command") ?? "")
        case "Skill": return s("skill") ?? s("command") ?? ""
        case "conversations_list": return s("query") ?? ""
        case "conversation_rename": return [s("kind"), s("topic")].compactMap { $0 }.joined(separator: " | ")
        case "conversation_new", "conversation_send": return firstLine(s("text") ?? "")
        case "conversation_read", "conversation_archive", "conversation_unarchive": return String((s("id") ?? "").prefix(8))
        case "ToolSearch": return s("query") ?? ""
        case "AskUserQuestion": return i["questions"]?.array?.first?["question"]?.string ?? ""
        case "ExitPlanMode": return PlanText.title(s("plan") ?? "")
        case "TodoWrite":
            let todos = i["todos"]?.array ?? []
            let done = todos.filter { $0["status"]?.string == "completed" }.count
            return "\(done)/\(todos.count)"
        default:
            if n.hasPrefix("phone_") || n.hasPrefix("mail_") { return n.split(separator: "_").dropFirst().joined(separator: " ") }
            return i.object?.values.compactMap(\.string).first.map(firstLine) ?? ""
        }
    }

    static func firstLine(_ s: String) -> String {
        s.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: true).first.map(String.init) ?? s
    }
}

/// A run of tool calls (and the thoughts between them), collapsed to the latest step while Claude works.
struct ToolGroupView: View {
    var steps: [ToolStep]
    var isLatest: Bool
    /// Every step listed, no "N 个步骤" fold (inside a line that was already tapped open).
    var flat = false
    @State private var expanded = false
    @Environment(\.chatPin) private var pin

    private var calls: [ToolCall] { steps.compactMap { if case .call(let c) = $0 { c } else { nil } } }
    private var pinKey: String { "group-" + (steps.first?.id ?? "") }
    @State private var unfold = Unfolding()

    var body: some View {
        let calls = calls
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            if calls.count > 2 && !expanded && !flat {
                Button {
                    toggleInPlace(pin, pinKey, animation: Theme.Motion.standard, unfold: $unfold) { expanded = true }
                } label: {
                    HStack(spacing: Theme.Space.sm) {
                        Image(systemName: Theme.Icon.space).font(Theme.Font.label)
                        Text("\(calls.count) 个步骤").font(Theme.Font.labelMedium)
                        Text(summary(calls)).font(Theme.Font.label).foregroundStyle(.tertiary).lineLimit(1)
                        Spacer(minLength: 0)
                        Image(systemName: Theme.Icon.expand).font(Theme.Font.labelStrong)
                    }
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, Theme.Space.m)
                    .padding(.vertical, Theme.Space.s)
                    .background(Theme.Fill.well, in: .capsule)
                }
                .buttonStyle(.plain)
                // Always show the last call: it is what Claude is doing right now.
                if let last = calls.last { ToolCallRow(call: last, autoExpand: isLatest && last.isRunning) }
            } else {
                ForEach(steps) { step in
                    switch step {
                    case .call(let c): ToolCallRow(call: c, autoExpand: false)
                    case .thought(let t):
                        if t.kind == .notice { NoticeRow(item: t) } else { ThinkingRow(item: t, compact: true) }
                    case .folded(let f): FoldedWorkRow(fold: f)
                    }
                }
                .unfolding(unfold)
                if calls.count > 2 && !flat {
                    Button("收起") { toggleInPlace(pin, pinKey, animation: Theme.Motion.standard, unfold: $unfold, closing: true) { expanded = false } }
                        .font(Theme.Font.labelMedium)
                        .padding(.leading, Theme.Space.m)
                }
            }
        }
        .chatPinned(pinKey) // its top: the "N 个步骤" line closed, the first step open
    }

    private func summary(_ calls: [ToolCall]) -> String {
        var counts: [String: Int] = [:]
        var order: [String] = []
        for c in calls {
            let v = ToolPresentation.verb(c.name)
            if counts[v] == nil { order.append(v) }
            counts[v, default: 0] += 1
        }
        return order.prefix(3).map { "\($0) \(counts[$0]!)" }.joined(separator: " · ")
    }
}

/// Two or more tool calls in a row of an open work line, as one line of chips (BeautifulUI's `ToolChipGroup`):
/// "搜索 hourly · 读取 3 个文件 · 编辑 Provider.swift +6 −3". Tapping opens a row per call under it (`ChatView.runLines`),
/// where each call opens to its input and output as before. One text layout per run, however long the run.
struct ToolRunRow: View, Equatable {
    nonisolated let calls: [ToolCall]
    nonisolated let open: Bool
    nonisolated let first: Bool
    var toggle: () -> Void

    nonisolated static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.calls == rhs.calls && lhs.open == rhs.open && lhs.first == rhs.first
    }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("ToolRunRow")
        #endif
        Button(action: toggle) {
            ToolChipGroup(ToolRunSummary.chips(calls), isOpen: open, isRunning: calls.contains(where: \.isRunning))
                .padding(.horizontal, WorkCardSlice.inset)
                .padding(.vertical, Theme.Space.s)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background { WorkCardSlice(top: first, bottom: false) }
        .overlay(alignment: .top) { if !first { WorkCardSlice.separator } }
        .accessibilityHint(open ? "收起这 \(calls.count) 步" : "展开这 \(calls.count) 步")
        .accessibilityIdentifier("tool-run")
    }
}

/// A run's chips: one per kind of tool, in the order first used. One call names its subject ("读取 WeatherStore.swift"),
/// several count ("读取 3 个文件"); edits to one file carry its +N −N; a failed call turns its chip red.
enum ToolRunSummary {
    private enum Kind: Hashable { case read, search, edit, run, todo, other(String) }

    private static func kind(_ name: String) -> Kind {
        switch ToolPresentation.baseName(name) {
        case "Read", "NotebookRead": .read
        case "Grep", "Glob", "LS", "WebSearch", "ToolSearch": .search
        case "Edit", "MultiEdit", "Write", "NotebookEdit": .edit
        case "Bash": .run
        case "TodoWrite": .todo
        default: .other(ToolPresentation.verb(name))
        }
    }

    static func chips(_ calls: [ToolCall]) -> [ToolChipGroup.Chip] {
        var order: [Kind] = []
        var groups: [Kind: [ToolCall]] = [:]
        for c in calls {
            let k = kind(c.name)
            if groups[k] == nil { order.append(k) }
            groups[k, default: []].append(c)
        }
        return order.map { k in chip(k, groups[k]!) }
    }

    private static func chip(_ kind: Kind, _ calls: [ToolCall]) -> ToolChipGroup.Chip {
        let first = calls[0]
        let error = calls.contains(where: \.isError)
        let one = calls.count == 1
        let subject = ToolPresentation.subject(first)
        let ascii = subject.unicodeScalars.allSatisfy(\.isASCII)
        func named(_ label: String, many: String) -> ToolChipGroup.Chip {
            ToolChipGroup.Chip(symbol: ToolPresentation.symbol(first.name), label: label, detail: one ? subject : many,
                               monospacedDetail: one && ascii, isError: error)
        }
        switch kind {
        case .read: return named("读取", many: "\(calls.count) 个文件")
        case .search: return named(ToolPresentation.verb(first.name), many: "\(calls.count) 次")
        case .run: return named("运行", many: "\(calls.count) 条命令")
        case .todo:
            // The latest list is the state of the to-dos.
            return ToolChipGroup.Chip(symbol: Theme.Icon.todo, label: "待办", detail: ToolPresentation.subject(calls[calls.count - 1]),
                                      monospacedDetail: true, isError: error)
        case .other(let verb):
            return ToolChipGroup.Chip(symbol: ToolPresentation.symbol(first.name), label: verb,
                                      detail: one ? subject : "\(calls.count) 次", monospacedDetail: one && ascii, isError: error)
        case .edit:
            var adds = 0, dels = 0
            var files: [String] = []
            for c in calls {
                let d = diff(c)
                adds += d.adds
                dels += d.dels
                let f = c.input["file_path"]?.string ?? c.input["notebook_path"]?.string ?? ""
                if !files.contains(f) { files.append(f) }
            }
            return ToolChipGroup.Chip(
                symbol: ToolPresentation.symbol(first.name),
                label: files.count == 1 ? ToolPresentation.verb(first.name) : "编辑",
                detail: files.count == 1 ? (files[0] as NSString).lastPathComponent : "\(files.count) 个文件",
                monospacedDetail: files.count == 1,
                additions: adds, deletions: dels, isError: error
            )
        }
    }

    /// The same counts the open call's diff shows (`ToolDiffMemo`, warmed off the main thread with the rows).
    static func diff(_ c: ToolCall) -> (adds: Int, dels: Int) {
        let i = c.input
        switch ToolPresentation.baseName(c.name) {
        case "Edit":
            let d = i["diff"]?.string.map(ToolDiffMemo.unified)
                ?? ToolDiffMemo.edit(old: i["old_string"]?.string ?? "", new: i["new_string"]?.string ?? "")
            return (d.adds, d.dels)
        case "MultiEdit":
            return (i["edits"]?.array ?? []).reduce((0, 0)) { n, e in
                let d = ToolDiffMemo.edit(old: e["old_string"]?.string ?? "", new: e["new_string"]?.string ?? "")
                return (n.0 + d.adds, n.1 + d.dels)
            }
        case "Write":
            let d = ToolDiffMemo.edit(old: "", new: i["content"]?.string ?? "")
            return (d.adds, d.dels)
        default:
            return (0, 0)
        }
    }
}

struct ToolCallRow: View {
    @Environment(\.taskIndex) private var tasks
    var call: ToolCall
    var autoExpand: Bool
    /// A row of an open work line's card (`WorkCardSlice` draws the card): no gray tile of its own, 44 pt tall.
    var grouped = false
    @State private var expanded: Bool?
    @Environment(\.openTools) private var openTools

    private var isOpen: Bool {
        openTools?.isOpen(call.id) ?? expanded ?? (autoExpand || (ResultBlock.debugFull && call.result?.fullSize != nil)
            || Self.debugDetails)
    }

    /// Mac screenshots (docs/mac.md): `-showToolDetails YES` opens every tool row (diffs, Bash output) without a click.
    #if DEBUG && os(macOS)
    static let debugDetails = UserDefaults.standard.bool(forKey: "showToolDetails")
    #else
    static let debugDetails = false
    #endif

    private func setOpen(_ open: Bool) {
        if let openTools { openTools.set(call.id, open) } else { expanded = open }
    }

    @Environment(\.chatPin) private var pin
    private var pinKey: String { "call-" + call.id }
    @State private var unfold = Unfolding()

    var body: some View {
        let base = ToolPresentation.baseName(call.name)
        VStack(alignment: .leading, spacing: 0) {
            Group { if AppStyle.current.usesBeautifulUI { chipHeader } else { header(base) } }
                .chatPinned(pinKey)

            if base == "TodoWrite" {
                if AppStyle.current.usesBeautifulUI {
                    TodoList(todos: call.input["todos"]?.array ?? []).padding(.horizontal, pad).padding(.bottom, Theme.Space.m)
                } else {
                    TodoList(todos: call.input["todos"]?.array ?? []).padding(.leading, detailLeading).padding(.trailing, pad).padding(.bottom, Theme.Space.s)
                }
            } else if isOpen {
                ToolDetail(call: call)
                    .padding(.leading, detailLeading)
                    .padding(.trailing, pad)
                    .padding(.bottom, Theme.Space.m)
                    .unfolding(unfold)
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .background(grouped ? Color.clear : Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
    }

    private var pad: CGFloat { grouped ? WorkCardSlice.inset : 10 }
    /// On the work timeline what a tool shows keeps right of the rail, under its words.
    private var detailLeading: CGFloat { grouped && WorkCardSlice.timeline ? WorkCardSlice.textColumn : pad }
    private var onTimeline: Bool { grouped && WorkCardSlice.timeline }

    /// Around a chip row (`ToolChipRow`): in a work card it lines up with the card's other rows; alone, a little
    /// tighter. Thoughts and background notices in a card use the same (`ThinkingRow`, `TaskNoteRow`).
    static func chipInsets(grouped: Bool) -> EdgeInsets {
        grouped ? EdgeInsets(top: 5, leading: 9, bottom: 5, trailing: 9) : EdgeInsets(top: 2, leading: 7, bottom: 2, trailing: 7)
    }

    private var tint: Color { call.isError ? Theme.danger : call.isRunning ? Theme.busy : .secondary }

    /// The row's head: a round kind icon, the verb, the subject, a background task's badge and the chevron.
    private func header(_ base: String) -> some View {
        Button {
            Haptics.tap()
            toggleInPlace(pin, pinKey, animation: Theme.Motion.state, unfold: $unfold, closing: isOpen) { setOpen(!isOpen) }
        } label: {
            HStack(spacing: Theme.Space.m) {
                ZStack {
                    // On the timeline the icon sits on the rail: the page under its tint hides the line behind it.
                    if onTimeline { Circle().fill(Theme.pageBackground).scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon) }
                    Circle().fill(tint.opacity(0.14)).scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
                    if call.isRunning {
                        ProgressView().controlSize(.mini).tint(tint)
                    } else {
                        Image(systemName: ToolPresentation.symbol(call.name))
                            .font(Theme.Font.labelStrong).dynamicTypeSize(...DynamicTypeSize.xLarge)
                            .foregroundStyle(tint)
                    }
                }
                Text(ToolPresentation.verb(call.name)).font(onTimeline ? Theme.Font.secondaryMedium : Theme.Font.secondaryStrong)
                Text(ToolPresentation.subject(call))
                    .font(Theme.Font.secondary)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 4)
                taskBadge
                if call.isError { Image(systemName: Theme.Icon.errorFilled).foregroundStyle(Theme.danger).font(Theme.Font.label) }
                Image(systemName: Theme.Icon.disclosure)
                    .font(Theme.Font.labelStrong)
                    .foregroundStyle(.tertiary)
                    .rotationEffect(.degrees(isOpen ? 90 : 0))
            }
            .padding(.vertical, onTimeline ? Theme.Space.s : grouped ? 10 : 7)
            .padding(.horizontal, pad)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }

    /// The row's head: BeautifulUI's tool row — kind icon (a chevron once open), the verb, the subject in a chip.
    private var chipHeader: some View {
        ToolChipRow(chip, isOpen: isOpen, isRunning: call.isRunning) {
            Haptics.tap()
            toggleInPlace(pin, pinKey, animation: Theme.Motion.state, unfold: $unfold, closing: isOpen) { setOpen(!isOpen) }
        } accessory: {
            taskBadge
            if call.isError { Image(systemName: Theme.Icon.errorFilled).foregroundStyle(Theme.danger).font(Theme.Font.label) }
        }
        .padding(Self.chipInsets(grouped: grouped))
    }

    /// A call that went to the background says how its task went (后台 · 已完成).
    @ViewBuilder private var taskBadge: some View {
        if let id = call.use.toolUseID, tasks.started.contains(id) || tasks.notes[id] != nil {
            let b = TaskNoteText.badge(tasks.notes[id] ?? [])
            if AppStyle.current.usesBeautifulUI {
                BUIPill(b.text, tone: b.failed ? .red : .neutral)
            } else {
                Text(b.text).font(Theme.Font.label).foregroundStyle(b.failed ? Theme.danger : Color.secondary)
                    .lineLimit(1).fixedSize()
                    .padding(.horizontal, Theme.Space.sm).padding(.vertical, Theme.Space.xxs)
                    .background(Theme.Fill.well, in: .capsule)
            }
        }
    }

    private var chip: BeautifulUI.ToolCall {
        let subject = ToolPresentation.subject(call)
        return BeautifulUI.ToolCall(
            id: call.id,
            .symbol(ToolPresentation.symbol(call.name)),
            label: ToolPresentation.verb(call.name),
            chip: subject,
            // Paths, patterns and commands in the code face; a description someone wrote (often Chinese) in the text face.
            monospacedChip: subject.unicodeScalars.allSatisfy(\.isASCII)
        )
    }
}

struct ToolDetail: View {
    @Environment(\.rowMemoScope) private var memoScope
    var call: ToolCall
    @Environment(\.openFile) private var openFile

    var body: some View {
        let base = ToolPresentation.baseName(call.name)
        let i = call.input
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            switch base {
            case "Bash":
                CodeBlock(text: "$ " + (i["command"]?.string ?? ""), maxLines: 12)
            case "Edit":
                // Codex: the patch as a unified diff; Claude: the old and new strings.
                let diff = i["diff"]?.string.map(DiffView.init(unified:))
                    ?? DiffView(old: i["old_string"]?.string ?? "", new: i["new_string"]?.string ?? "")
                diffHeader(i["file_path"]?.string, [diff])
                diff
            case "MultiEdit":
                let diffs = (i["edits"]?.array ?? []).map { DiffView(old: $0["old_string"]?.string ?? "", new: $0["new_string"]?.string ?? "") }
                diffHeader(i["file_path"]?.string, diffs)
                ForEach(Array(diffs.enumerated()), id: \.offset) { _, d in d }
            case "Write":
                let diff = DiffView(old: "", new: i["content"]?.string ?? "")
                diffHeader(i["file_path"]?.string, [diff])
                diff
            case "Read", "NotebookEdit":
                path(i["file_path"]?.string ?? i["notebook_path"]?.string)
            case "ExitPlanMode":
                Markdown(MarkdownMemo.content(i["plan"]?.string ?? "")).markdownTheme(.jarvis)
            case "AskUserQuestion":
                ForEach(Array((i["questions"]?.array ?? []).enumerated()), id: \.offset) { _, q in
                    Text(q["question"]?.string ?? "").font(Theme.Font.captionMedium)
                }
            case "Task", "Agent":
                Text(i["prompt"]?.string ?? "").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(8)
            default:
                if i != .object([:]) { CodeBlock(text: ToolTextMemo.input(call.use, scope: memoScope), maxLines: 14) }
            }
            if let result = call.result, base != "Edit" || result.isError == true {
                ResultBlock(text: result.cutText, isError: result.isError ?? false, cut: result.fullSize.map { (result.id, $0) })
                if base == "Bash" { PortChips(text: result.cutText) } // a dev server saying where it listens
            } else if call.isRunning {
                HStack(spacing: Theme.Space.sm) {
                    ProgressView().controlSize(.mini)
                    Text("运行中…").font(Theme.Font.label).foregroundStyle(.secondary)
                }
            }
            ForEach(Array(call.images.enumerated()), id: \.offset) { i, img in Base64Image(payload: img, gallery: call.images, index: i) }
        }
    }

    /// The file a diff is in: name, folder, +N −N; a tap opens it.
    @ViewBuilder private func diffHeader(_ p: String?, _ diffs: [DiffView]) -> some View {
        if let p, !p.isEmpty {
            let adds = diffs.reduce(0) { $0 + $1.adds }, dels = diffs.reduce(0) { $0 + $1.dels }
            let header = HStack(spacing: Theme.Space.sm) {
                Image(systemName: FileViewer.symbol(for: p)).font(Theme.Font.label).foregroundStyle(.secondary)
                Text((p as NSString).lastPathComponent).font(Theme.Font.captionStrong).foregroundStyle(.primary).lineLimit(1)
                Text((p as NSString).deletingLastPathComponent).font(Theme.Font.caption).foregroundStyle(.secondary)
                    .lineLimit(1).truncationMode(.head)
                Spacer(minLength: 4)
                if adds > 0 { Text("+\(adds)").foregroundStyle(Theme.Diff.add) }
                if dels > 0 { Text("−\(dels)").foregroundStyle(Theme.Diff.del) }
                if openFile != nil { Image(systemName: Theme.Icon.openDocument).font(Theme.Font.label).foregroundStyle(Theme.accent) }
            }
            .font(Theme.Font.captionStrong.monospacedDigit())
            if let openFile {
                Button { openFile(p) } label: { header.contentShape(.rect) }
                    .buttonStyle(.plain)
                    .accessibilityLabel("查看文件 \((p as NSString).lastPathComponent)，新增 \(adds) 行，删除 \(dels) 行")
            } else {
                header
            }
        }
    }

    @ViewBuilder private func path(_ p: String?) -> some View {
        if let p, !p.isEmpty {
            if let openFile {
                Button { openFile(p) } label: {
                    HStack(spacing: Theme.Space.xs) {
                        Image(systemName: FileViewer.symbol(for: p)).font(Theme.Font.label)
                        Text(p).lineLimit(2).truncationMode(.head)
                        Image(systemName: Theme.Icon.openDocument).font(Theme.Font.label)
                    }
                    .font(Theme.Font.code)
                    .foregroundStyle(Theme.accent)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("查看文件 \((p as NSString).lastPathComponent)")
            } else {
                Text(p).font(Theme.Font.code).foregroundStyle(.secondary).lineLimit(2).truncationMode(.head).textSelection(.enabled)
            }
        }
    }
}

nonisolated enum ToolTextMemo {
    private static let inputs = TextMemo<String>(limit: 512)
    private static let lines = TextMemo<[String]>(limit: 256)

    /// The call's input as shown, once per call — keyed by its chat and id, as `RowMemo` is (ids repeat across chats).
    static func input(_ item: Item, scope: String) -> String {
        inputs(RowMemo.key(scope, item.id)) { _ in (item.toolInput ?? .object([:])).pretty }
    }

    static func codeLines(_ text: String) -> [String] { lines(text) { $0.components(separatedBy: "\n") } }

    static func prewarm(_ item: Item, scope: String) {
        if item.kind == .toolResult { _ = codeLines(item.cutText); return }
        let input = item.toolInput ?? .object([:])
        switch ToolPresentation.baseName(item.toolName ?? "") {
        case "Bash": _ = codeLines("$ " + (input["command"]?.string ?? ""))
        case "ExitPlanMode": _ = MarkdownMemo.content(input["plan"]?.string ?? "")
        case "Edit", "MultiEdit", "Write", "Read", "NotebookEdit", "AskUserQuestion", "Task", "Agent": break
        default: _ = codeLines(self.input(item, scope: scope))
        }
    }
}

struct CodeBlock: View {
    var text: String
    var maxLines: Int = 20
    @State private var full = false

    var body: some View {
        let lines = ToolTextMemo.codeLines(text)
        let clipped = !full && lines.count > maxLines
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            ScrollView(.horizontal, showsIndicators: false) {
                Text(clipped ? lines.prefix(maxLines).joined(separator: "\n") : text)
                    .font(Theme.Font.code)
                    .textSelection(.enabled)
                    .padding(Theme.Space.m)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.codeBackground, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            if clipped {
                Button("展开全部 \(lines.count) 行") { withAnimation { full = true } }.font(Theme.Font.labelMedium)
            }
        }
    }
}

struct ResultBlock: View {
    var text: String
    var isError: Bool
    /// The page carries only the start of a long output: its item id and whole size.
    var cut: (id: String, size: Int)? = nil
    @Environment(\.loadFullText) private var loadFullText
    @State private var showFull = false

    /// Simulator automation: `-showFullOutput card` opens the cut tool outputs, `sheet` also the first one's whole text.
    #if DEBUG
    static let debugFull = UserDefaults.standard.string(forKey: "showFullOutput") != nil
    static let debugSheet = UserDefaults.standard.string(forKey: "showFullOutput") == "sheet"
    @MainActor static var debugFullShown = false
    #else
    static let debugFull = false
    #endif

    var body: some View {
        if text.isEmpty && cut == nil {
            Text(isError ? "失败" : "完成，无输出").font(Theme.Font.label).foregroundStyle(isError ? Theme.danger : .secondary)
        } else {
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                // Above the output: over it, the mark covered the end of the first line.
                if isError {
                    Label("出错了", systemImage: Theme.Icon.errorFilled).font(Theme.Font.labelStrong).foregroundStyle(Theme.danger)
                        .lineLimit(1)
                }
                CodeBlock(text: text, maxLines: 10)
                if let cut {
                    Button { showFull = true } label: {
                        Label("已截断，看全文（\(ByteCountFormatter.string(fromByteCount: Int64(cut.size), countStyle: .file))）",
                              systemImage: Theme.Icon.text)
                            .lineLimit(1)
                            .minimumScaleFactor(0.8)
                    }
                    .font(Theme.Font.labelMedium)
                    .sheet(isPresented: $showFull) { FullOutputSheet(itemID: cut.id, load: loadFullText) }
                    #if DEBUG
                    .task {
                        guard Self.debugSheet, !Self.debugFullShown else { return }
                        Self.debugFullShown = true
                        try? await Task.sleep(for: .seconds(1))
                        showFull = true
                    }
                    #endif
                }
            }
        }
    }
}

/// A tool's whole output, fetched when opened (pages carry its first 4 KB). Monospaced, scrolls both ways.
struct FullOutputSheet: View {
    var itemID: String
    var load: ((String) async throws -> String)?
    @Environment(\.dismiss) private var dismiss
    @State private var text: String?
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Group {
                if let text {
                    MonoTextView(text: MonoText.plain(text, color: .label, size: 12))
                } else if let error {
                    ContentUnavailableView("加载失败", systemImage: Theme.Icon.error, description: Text(error))
                } else {
                    ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .background(Theme.codeBackground)
            .navigationTitle("工具输出")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() }.lineLimit(1) }
                if let text {
                    ToolbarItem(placement: .topBarLeading) {
                        Button { UIPasteboard.general.string = text } label: { Label("拷贝全文", systemImage: Theme.Icon.copy) }
                    }
                }
            }
        }
        .task {
            guard text == nil, let load else { return }
            do { text = try await load(itemID) } catch { if let msg = error.shown { self.error = msg } }
            #if DEBUG
            if ResultBlock.debugFull, let dir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first {
                // What 看全文 got, for checking against the transcript (`-showFullOutput sheet`).
                try? (text ?? "ERROR: \(error ?? "")").write(to: dir.appending(path: "full-output.txt"), atomically: true, encoding: .utf8)
                try? itemID.write(to: dir.appending(path: "full-output-id.txt"), atomically: true, encoding: .utf8)
            }
            #endif
        }
    }
}

/// The chat's one diff: Edit / MultiEdit / Write (old and new text) and Codex's patches (a unified diff), on the Git
/// page's line model (`DiffText.Line`, `DiffText.parse`; the Git page draws whole files with MonoTextView). Only the
/// sign and the line's background are colored, the code stays label; long lines scroll sideways and a fade on the
/// right says there is more. Long diffs show their start and 展开全部.
nonisolated enum ToolDiffMemo {
    struct Content: Sendable {
        var lines: [DiffText.Line]
        var adds: Int
        var dels: Int
    }

    private enum Input: Hashable { case edit(String, String), unified(String) }
    nonisolated(unsafe) private static var values: [Input: Content] = [:]
    private static let lock = NSLock()

    private static func cached(_ input: Input, make: () -> Content) -> Content {
        lock.lock()
        if let value = values[input] { lock.unlock(); return value }
        lock.unlock()
        let value = make()
        lock.lock()
        if values.count >= 64 { values.removeAll(keepingCapacity: true) }
        values[input] = value
        lock.unlock()
        return value
    }

    static func edit(old: String, new: String) -> Content {
        cached(.edit(old, new)) {
            let removed = old.isEmpty ? [] : old.components(separatedBy: "\n")
            let added = new.isEmpty ? [] : new.components(separatedBy: "\n")
            let lines = removed.enumerated().map { DiffText.Line(id: $0.offset, kind: .del, text: $0.element) }
                + added.enumerated().map { DiffText.Line(id: removed.count + $0.offset, kind: .add, text: $0.element) }
            return Content(lines: lines, adds: added.count, dels: removed.count)
        }
    }

    static func unified(_ text: String) -> Content {
        cached(.unified(text)) {
            let lines = DiffText.parse(text)
            var adds = 0, dels = 0
            for line in lines {
                if line.kind == .add { adds += 1 }
                if line.kind == .del { dels += 1 }
            }
            return Content(lines: lines, adds: adds, dels: dels)
        }
    }

    static func prewarm(_ item: Item) {
        let input = item.toolInput ?? .object([:])
        switch ToolPresentation.baseName(item.toolName ?? "") {
        case "Edit":
            if let diff = input["diff"]?.string { _ = unified(diff) }
            else { _ = edit(old: input["old_string"]?.string ?? "", new: input["new_string"]?.string ?? "") }
        case "MultiEdit":
            for edit in input["edits"]?.array ?? [] {
                _ = self.edit(old: edit["old_string"]?.string ?? "", new: edit["new_string"]?.string ?? "")
            }
        case "Write": _ = edit(old: "", new: input["content"]?.string ?? "")
        default: break
        }
    }
}

struct DiffView: View {
    var lines: [DiffText.Line]
    var adds: Int
    var dels: Int
    @State private var full = false
    @State private var moreRight = false
    private static let maxLines = 40

    /// All of old removed, then all of new added (Claude's Edit has no line numbers or context).
    init(old: String, new: String) {
        let content = ToolDiffMemo.edit(old: old, new: new)
        lines = content.lines
        adds = content.adds
        dels = content.dels
    }

    init(unified diff: String) {
        let content = ToolDiffMemo.unified(diff)
        lines = content.lines
        adds = content.adds
        dels = content.dels
    }

    var body: some View {
        let clipped = !full && lines.count > Self.maxLines
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            ScrollView(.horizontal, showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(clipped ? Array(lines.prefix(Self.maxLines)) : lines) { line($0) }
                }
                .padding(.vertical, Theme.Space.sm)
            }
            .onScrollGeometryChange(for: Bool.self) { g in
                g.contentSize.width - g.contentOffset.x > g.containerSize.width + 1
            } action: { _, more in moreRight = more }
            .overlay(alignment: .trailing) {
                if moreRight {
                    LinearGradient(colors: [Theme.codeBackground.opacity(0), Theme.codeBackground], startPoint: .leading, endPoint: .trailing)
                        .scaledFrame(width: Theme.Size.wellXS)
                        .allowsHitTesting(false)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.codeBackground)
            .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
            if clipped {
                Button("展开全部 \(lines.count) 行") { withAnimation { full = true } }.font(Theme.Font.labelMedium).lineLimit(1)
            }
        }
    }

    private func line(_ l: DiffText.Line) -> some View {
        let (sign, color, fill): (String, Color, Color) = switch l.kind {
        case .add: ("+", Theme.Diff.add, Theme.Diff.addFill)
        case .del: ("−", Theme.Diff.del, Theme.Diff.delFill)
        default: (" ", .secondary, .clear)
        }
        let quiet = l.kind == .hunk || l.kind == .info
        return HStack(spacing: Theme.Space.sm) {
            Text(sign).foregroundStyle(color).scaledFrame(width: Theme.Size.dotXL)
            Text(l.text.isEmpty ? " " : l.text).foregroundStyle(quiet ? Color.secondary : .primary)
        }
        .font(Theme.Font.code)
        .padding(.horizontal, Theme.Space.s)
        .padding(.vertical, 1)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(fill)
    }
}

/// TodoWrite's list as BeautifulUI's task rows in one card: a numbered ring for what is left, the ring turning on the
/// one in progress (its activeForm), a green check for what is done.
struct TodoList: View {
    var todos: [JSONValue]

    var body: some View {
        if AppStyle.current.usesBeautifulUI {
            TaskRows(Self.items(todos), style: .list, showsPills: false, maxWidth: nil)
        } else {
            checklist
        }
    }

    /// Jarvis' own: a check, a dotted ring or an empty one, the finished ones struck through.
    private var checklist: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            ForEach(Array(todos.enumerated()), id: \.offset) { _, t in
                let status = t["status"]?.string ?? "pending"
                HStack(alignment: .top, spacing: Theme.Space.s) {
                    Image(systemName: status == "completed" ? Theme.Icon.successFilled : status == "in_progress" ? "circle.dotted.circle" : Theme.Icon.unchecked)
                        .foregroundStyle(status == "completed" ? Theme.positive : status == "in_progress" ? Theme.busy : .secondary)
                    Text(status == "in_progress" ? (t["activeForm"]?.string ?? t["content"]?.string ?? "") : (t["content"]?.string ?? ""))
                        .font(Theme.Font.caption)
                        .strikethrough(status == "completed", color: .secondary)
                        .foregroundStyle(status == "completed" ? .secondary : .primary)
                }
            }
        }
    }

    static func items(_ todos: [JSONValue]) -> [TaskItem] {
        todos.enumerated().map { n, t in
            let status = t["status"]?.string ?? "pending"
            let content = t["content"]?.string ?? ""
            return switch status {
            case "completed": TaskItem(id: "\(n)", title: content, status: .completed)
            case "in_progress": TaskItem(id: "\(n)", title: t["activeForm"]?.string ?? content, status: .running(step: n + 1))
            default: TaskItem(id: "\(n)", title: content, status: .pending(step: n + 1))
            }
        }
    }
}

/// Thumbnail of an image in a conversation; tap opens the full-screen viewer (paging through `gallery`).
struct Base64Image: View {
    @Environment(\.chatImages) private var chatImages
    @Environment(\.attachImage) private var attachImage
    @Environment(\.viewImages) private var viewImages
    @Environment(\.imageZoom) private var pageZoom
    var payload: ImagePayload
    var gallery: [ImagePayload] = []
    var index = 0
    @State private var image: UIImage?
    @State private var showViewer = false
    init(payload: ImagePayload, gallery: [ImagePayload] = [], index: Int = 0) {
        self.payload = payload
        self.gallery = gallery
        self.index = index
        _image = State(initialValue: ChatImageLoader.cached(payload, side: 900)) // back on screen: no placeholder flash
    }
    @Namespace private var zoom

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("Base64Image")
        #endif
        Group {
            if let image {
                Image(uiImage: image).resizable().scaledToFit()
                    .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).stroke(Theme.cardStroke, lineWidth: 0.5))
                    .scaledFrame(maxHeight: Theme.Size.box280)
                    .contentShape(.rect)
                    .onTapGesture { open() }
                    .matchedTransitionSource(id: pageZoom == nil ? "image" : payload.identity, in: pageZoom ?? zoom)
                    .accessibilityAddTraits(.isButton)
                    .accessibilityLabel("图片，点按查看大图")
            } else if let aspect = payload.aspect { // lazy: hold the image's shape while it downloads
                RoundedRectangle(cornerRadius: Theme.Radius.control).fill(Theme.Fill.well)
                    .aspectRatio(aspect, contentMode: .fit)
                    .scaledFrame(maxHeight: Theme.Size.box280)
                    .overlay { ProgressView().controlSize(.small) }
            } else {
                RoundedRectangle(cornerRadius: Theme.Radius.control).fill(Theme.Fill.well).scaledFrame(height: Theme.Size.box120)
            }
        }
        .fullScreenCover(isPresented: $showViewer) {
            ImageViewer(images: gallery.isEmpty ? [payload] : gallery, index: gallery.isEmpty ? 0 : index,
                        conversation: chatImages, attach: attachImage)
                .navigationTransition(.zoom(sourceID: "image", in: zoom))
        }
        .task(id: payload.identity) {
            // Thumbnails only need ~2x their on-screen size.
            image = await ChatImageLoader.image(payload, side: 900)
        }
    }

    /// In a chat the page shows the viewer (`viewImages`); elsewhere this view does.
    private func open() {
        guard let viewImages else { return showViewer = true }
        viewImages(ImageViewing(images: gallery.isEmpty ? [payload] : gallery, index: gallery.isEmpty ? 0 : index,
                                conversation: chatImages, source: payload.identity))
    }
}

extension EnvironmentValues {
    /// Fetches the work a fold stands for into the chat (`AppModel.unfold`); nil where nothing can.
    @Entry var openFold: ((Item) async throws -> Void)? = nil
}

/// Work the page left out (`Item.fold`, docs/protocol.md「折叠的工作」). It shows only once its work line is open, and
/// then fetches the steps, which take its place.
struct FoldedWorkRow: View {
    @Environment(\.openFold) private var openFold
    var fold: Item
    @State private var failed: String?
    @State private var attempt = 0

    private var steps: Int { fold.fold.map { $0.tools + $0.thoughts } ?? 0 }

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            if let failed {
                Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution)
                Text("\(steps) 步没载入：\(failed)").lineLimit(2)
                Spacer(minLength: 0)
                Button("重试") {
                    self.failed = nil
                    attempt += 1
                }
                .lineLimit(1)
                .fixedSize()
            } else {
                ProgressView().controlSize(.small)
                Text("正在载入 \(steps) 步…").lineLimit(1)
                Spacer(minLength: 0)
            }
        }
        .font(Theme.Font.caption)
        .foregroundStyle(.secondary)
        .scaledFrame(minHeight: Theme.Size.wellXS)
        .task(id: attempt) {
            guard let openFold else { return }
            do {
                try await openFold(fold)
            } catch {
                failed = error.shown
            }
        }
    }
}
