import MarkdownUI
import SwiftUI

/// How each Claude Code tool is named and summarized in the chat.
enum ToolPresentation {
    static func baseName(_ name: String) -> String {
        if name.hasPrefix("mcp__") { return name.split(separator: "__").last.map(String.init) ?? name }
        return name
    }

    static func symbol(_ name: String) -> String {
        let n = baseName(name)
        if n.hasPrefix("phone_") { return "iphone" }
        if n.hasPrefix("mail_") { return "envelope" }
        switch n {
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
        default: return name.hasPrefix("mcp__") ? "puzzlepiece.extension" : "gearshape"
        }
    }

    static func verb(_ name: String) -> String {
        let n = baseName(name)
        if n.hasPrefix("phone_") { return "iPhone" }
        if n.hasPrefix("mail_") { return "邮件" }
        switch n {
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
        case "Skill": return s("skill") ?? s("command") ?? ""
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
        s.split(separator: "\n", omittingEmptySubsequences: true).first.map(String.init) ?? s
    }
}

/// A run of tool calls (and the thoughts between them), collapsed to the latest step while Claude works.
struct ToolGroupView: View {
    var steps: [ToolStep]
    var isLatest: Bool
    /// Every step listed, no "N 个步骤" fold (inside a line that was already tapped open).
    var flat = false
    @State private var expanded = false

    private var calls: [ToolCall] { steps.compactMap { if case .call(let c) = $0 { c } else { nil } } }

    var body: some View {
        let calls = calls
        VStack(alignment: .leading, spacing: 6) {
            if calls.count > 2 && !expanded && !flat {
                Button {
                    withAnimation(.snappy) { expanded = true }
                } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "square.stack.3d.up").font(.caption)
                        Text("\(calls.count) 个步骤").font(.caption.weight(.medium))
                        Text(summary(calls)).font(.caption).foregroundStyle(.tertiary).lineLimit(1)
                        Spacer(minLength: 0)
                        Image(systemName: "chevron.down").font(.caption2.weight(.semibold))
                    }
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
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
                    }
                }
                if calls.count > 2 && !flat {
                    Button("收起") { withAnimation(.snappy) { expanded = false } }
                        .font(.caption.weight(.medium))
                        .padding(.leading, 12)
                }
            }
        }
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

struct ToolCallRow: View {
    var call: ToolCall
    var autoExpand: Bool
    /// A row of an open work line's card (`WorkCardSlice` draws the card): no gray tile of its own, 44 pt tall.
    var grouped = false
    @State private var expanded: Bool?

    private var isOpen: Bool { expanded ?? (autoExpand || (ResultBlock.debugFull && call.result?.fullSize != nil)) }

    var body: some View {
        let base = ToolPresentation.baseName(call.name)
        VStack(alignment: .leading, spacing: 0) {
            Button {
                Haptics.tap()
                withAnimation(.snappy(duration: 0.25)) { expanded = !isOpen }
            } label: {
                HStack(spacing: 10) {
                    ZStack {
                        Circle().fill(tint.opacity(0.14)).frame(width: 24, height: 24)
                        if call.isRunning {
                            ProgressView().controlSize(.mini).tint(tint)
                        } else {
                            Image(systemName: ToolPresentation.symbol(call.name))
                                .font(.system(size: 11, weight: .semibold))
                                .foregroundStyle(tint)
                        }
                    }
                    Text(ToolPresentation.verb(call.name)).font(.subheadline.weight(.semibold))
                    Text(ToolPresentation.subject(call))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                    Spacer(minLength: 4)
                    if call.isError { Image(systemName: "exclamationmark.circle.fill").foregroundStyle(Theme.danger).font(.caption) }
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(isOpen ? 90 : 0))
                }
                .padding(.vertical, grouped ? 10 : 7)
                .padding(.horizontal, pad)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)

            if base == "TodoWrite" {
                TodoList(todos: call.input["todos"]?.array ?? []).padding(.horizontal, pad).padding(.bottom, 8)
            } else if isOpen {
                ToolDetail(call: call)
                    .padding(.horizontal, pad)
                    .padding(.bottom, 10)
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .background(grouped ? Color.clear : Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
    }

    private var pad: CGFloat { grouped ? WorkCardSlice.inset : 10 }

    private var tint: Color { call.isError ? Theme.danger : call.isRunning ? Theme.busy : .secondary }
}

struct ToolDetail: View {
    var call: ToolCall
    @Environment(\.openFile) private var openFile

    var body: some View {
        let base = ToolPresentation.baseName(call.name)
        let i = call.input
        VStack(alignment: .leading, spacing: 8) {
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
                Markdown(i["plan"]?.string ?? "").markdownTheme(.jarvis)
            case "AskUserQuestion":
                ForEach(Array((i["questions"]?.array ?? []).enumerated()), id: \.offset) { _, q in
                    Text(q["question"]?.string ?? "").font(.footnote.weight(.medium))
                }
            case "Task", "Agent":
                Text(i["prompt"]?.string ?? "").font(.footnote).foregroundStyle(.secondary).lineLimit(8)
            default:
                if i != .object([:]) { CodeBlock(text: i.pretty, maxLines: 14) }
            }
            if let result = call.result, base != "Edit" || result.isError == true {
                ResultBlock(text: result.cutText, isError: result.isError ?? false, cut: result.fullSize.map { (result.id, $0) })
            } else if call.isRunning {
                HStack(spacing: 6) {
                    ProgressView().controlSize(.mini)
                    Text("运行中…").font(.caption).foregroundStyle(.secondary)
                }
            }
            ForEach(Array(call.images.enumerated()), id: \.offset) { i, img in Base64Image(payload: img, gallery: call.images, index: i) }
        }
    }

    /// The file a diff is in: name, folder, +N −N; a tap opens it.
    @ViewBuilder private func diffHeader(_ p: String?, _ diffs: [DiffView]) -> some View {
        if let p, !p.isEmpty {
            let adds = diffs.reduce(0) { $0 + $1.adds }, dels = diffs.reduce(0) { $0 + $1.dels }
            let header = HStack(spacing: 6) {
                Image(systemName: FileViewer.symbol(for: p)).font(.caption).foregroundStyle(.secondary)
                Text((p as NSString).lastPathComponent).font(.footnote.weight(.semibold)).foregroundStyle(.primary).lineLimit(1)
                Text((p as NSString).deletingLastPathComponent).font(.footnote).foregroundStyle(.secondary)
                    .lineLimit(1).truncationMode(.head)
                Spacer(minLength: 4)
                if adds > 0 { Text("+\(adds)").foregroundStyle(Theme.Diff.add) }
                if dels > 0 { Text("−\(dels)").foregroundStyle(Theme.Diff.del) }
                if openFile != nil { Image(systemName: "arrow.up.forward.square").font(.caption).foregroundStyle(Theme.accent) }
            }
            .font(.footnote.weight(.semibold).monospacedDigit())
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
                    HStack(spacing: 4) {
                        Image(systemName: FileViewer.symbol(for: p)).font(.caption2)
                        Text(p).lineLimit(2).truncationMode(.head)
                        Image(systemName: "arrow.up.forward.square").font(.caption2)
                    }
                    .font(.caption.monospaced())
                    .foregroundStyle(Theme.accent)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("查看文件 \((p as NSString).lastPathComponent)")
            } else {
                Text(p).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(2).truncationMode(.head).textSelection(.enabled)
            }
        }
    }
}

struct CodeBlock: View {
    var text: String
    var maxLines: Int = 20
    @State private var full = false

    var body: some View {
        let lines = text.components(separatedBy: "\n")
        let clipped = !full && lines.count > maxLines
        VStack(alignment: .leading, spacing: 4) {
            ScrollView(.horizontal, showsIndicators: false) {
                Text(clipped ? lines.prefix(maxLines).joined(separator: "\n") : text)
                    .font(.system(size: 12, design: .monospaced))
                    .textSelection(.enabled)
                    .padding(10)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.codeBackground, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
            if clipped {
                Button("展开全部 \(lines.count) 行") { withAnimation { full = true } }.font(.caption.weight(.medium))
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
            Text(isError ? "失败" : "完成，无输出").font(.caption).foregroundStyle(isError ? Theme.danger : .secondary)
        } else {
            VStack(alignment: .leading, spacing: 4) {
                // Above the output: over it, the mark covered the end of the first line.
                if isError {
                    Label("出错了", systemImage: "xmark.octagon.fill").font(.caption.weight(.semibold)).foregroundStyle(Theme.danger)
                        .lineLimit(1)
                }
                CodeBlock(text: text, maxLines: 10)
                if let cut {
                    Button { showFull = true } label: {
                        Label("已截断，看全文（\(ByteCountFormatter.string(fromByteCount: Int64(cut.size), countStyle: .file))）",
                              systemImage: "text.alignleft")
                            .lineLimit(1)
                            .minimumScaleFactor(0.8)
                    }
                    .font(.caption.weight(.medium))
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
                    ContentUnavailableView("加载失败", systemImage: "exclamationmark.triangle", description: Text(error))
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
                        Button { UIPasteboard.general.string = text } label: { Image(systemName: "doc.on.doc") }
                            .accessibilityLabel("拷贝全文")
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
struct DiffView: View {
    var lines: [DiffText.Line]
    var adds: Int
    var dels: Int
    @State private var full = false
    @State private var moreRight = false
    private static let maxLines = 40

    /// All of old removed, then all of new added (Claude's Edit has no line numbers or context).
    init(old: String, new: String) {
        let removed = old.isEmpty ? [] : old.components(separatedBy: "\n")
        let added = new.isEmpty ? [] : new.components(separatedBy: "\n")
        lines = removed.enumerated().map { DiffText.Line(id: $0.offset, kind: .del, text: $0.element) }
            + added.enumerated().map { DiffText.Line(id: removed.count + $0.offset, kind: .add, text: $0.element) }
        adds = added.count
        dels = removed.count
    }

    init(unified diff: String) {
        lines = DiffText.parse(diff)
        adds = lines.filter { $0.kind == .add }.count
        dels = lines.filter { $0.kind == .del }.count
    }

    var body: some View {
        let clipped = !full && lines.count > Self.maxLines
        VStack(alignment: .leading, spacing: 4) {
            ScrollView(.horizontal, showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(clipped ? Array(lines.prefix(Self.maxLines)) : lines) { line($0) }
                }
                .padding(.vertical, 6)
            }
            .onScrollGeometryChange(for: Bool.self) { g in
                g.contentSize.width - g.contentOffset.x > g.containerSize.width + 1
            } action: { _, more in moreRight = more }
            .overlay(alignment: .trailing) {
                if moreRight {
                    LinearGradient(colors: [Theme.codeBackground.opacity(0), Theme.codeBackground], startPoint: .leading, endPoint: .trailing)
                        .frame(width: 28)
                        .allowsHitTesting(false)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.codeBackground)
            .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
            if clipped {
                Button("展开全部 \(lines.count) 行") { withAnimation { full = true } }.font(.caption.weight(.medium)).lineLimit(1)
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
        return HStack(spacing: 6) {
            Text(sign).foregroundStyle(color).frame(width: 10)
            Text(l.text.isEmpty ? " " : l.text).foregroundStyle(quiet ? Color.secondary : .primary)
        }
        .font(.caption.monospaced())
        .padding(.horizontal, 8)
        .padding(.vertical, 1)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(fill)
    }
}

struct TodoList: View {
    var todos: [JSONValue]

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            ForEach(Array(todos.enumerated()), id: \.offset) { _, t in
                let status = t["status"]?.string ?? "pending"
                HStack(alignment: .top, spacing: 8) {
                    Image(systemName: status == "completed" ? "checkmark.circle.fill" : status == "in_progress" ? "circle.dotted.circle" : "circle")
                        .foregroundStyle(status == "completed" ? Theme.positive : status == "in_progress" ? Theme.busy : .secondary)
                        .symbolEffect(.rotate, options: .repeating, isActive: status == "in_progress")
                    Text(status == "in_progress" ? (t["activeForm"]?.string ?? t["content"]?.string ?? "") : (t["content"]?.string ?? ""))
                        .font(.footnote)
                        .strikethrough(status == "completed", color: .secondary)
                        .foregroundStyle(status == "completed" ? .secondary : .primary)
                }
            }
        }
    }
}

/// Thumbnail of an image in a conversation; tap opens the full-screen viewer (paging through `gallery`).
struct Base64Image: View {
    @Environment(\.chatImages) private var chatImages
    @Environment(\.attachImage) private var attachImage
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
        Group {
            if let image {
                Image(uiImage: image).resizable().scaledToFit()
                    .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous).stroke(Theme.cardStroke, lineWidth: 0.5))
                    .frame(maxHeight: 280)
                    .contentShape(.rect)
                    .onTapGesture { showViewer = true }
                    .matchedTransitionSource(id: "image", in: zoom)
                    .accessibilityAddTraits(.isButton)
                    .accessibilityLabel("图片，点按查看大图")
            } else if let aspect = payload.aspect { // lazy: hold the image's shape while it downloads
                RoundedRectangle(cornerRadius: Theme.Radius.control).fill(Theme.Fill.well)
                    .aspectRatio(aspect, contentMode: .fit)
                    .frame(maxHeight: 280)
                    .overlay { ProgressView().controlSize(.small) }
            } else {
                RoundedRectangle(cornerRadius: Theme.Radius.control).fill(Theme.Fill.well).frame(height: 120)
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
}
