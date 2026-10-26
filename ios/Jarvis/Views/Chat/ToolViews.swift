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
                    .background(Color.primary.opacity(0.04), in: .capsule)
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
    @State private var expanded: Bool?

    private var isOpen: Bool { expanded ?? autoExpand }

    var body: some View {
        let base = ToolPresentation.baseName(call.name)
        VStack(alignment: .leading, spacing: 0) {
            Button {
                Haptics.tap()
                withAnimation(.snappy(duration: 0.25)) { expanded = !isOpen }
            } label: {
                HStack(spacing: 10) {
                    ZStack {
                        Circle().fill(tint.opacity(0.14)).frame(width: 26, height: 26)
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
                .padding(.vertical, 7)
                .padding(.horizontal, 10)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)

            if base == "TodoWrite" {
                TodoList(todos: call.input["todos"]?.array ?? []).padding(.horizontal, 10).padding(.bottom, 8)
            } else if isOpen {
                ToolDetail(call: call)
                    .padding(.horizontal, 10)
                    .padding(.bottom, 10)
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .background(Color.primary.opacity(0.035), in: .rect(cornerRadius: 14, style: .continuous))
    }

    private var tint: Color { call.isError ? Theme.danger : call.isRunning ? Theme.working : .secondary }
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
                path(i["file_path"]?.string)
                if let diff = i["diff"]?.string { // Codex: the patch as a unified diff
                    UnifiedDiffView(diff: diff)
                } else {
                    DiffView(old: i["old_string"]?.string ?? "", new: i["new_string"]?.string ?? "")
                }
            case "MultiEdit":
                path(i["file_path"]?.string)
                ForEach(Array((i["edits"]?.array ?? []).enumerated()), id: \.offset) { _, e in
                    DiffView(old: e["old_string"]?.string ?? "", new: e["new_string"]?.string ?? "")
                }
            case "Write":
                path(i["file_path"]?.string)
                CodeBlock(text: i["content"]?.string ?? "", maxLines: 16)
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
                ResultBlock(text: result.text ?? "", isError: result.isError ?? false)
            } else if call.isRunning {
                HStack(spacing: 6) {
                    ProgressView().controlSize(.mini)
                    Text("运行中…").font(.caption).foregroundStyle(.secondary)
                }
            }
            ForEach(Array(call.images.enumerated()), id: \.offset) { i, img in Base64Image(payload: img, gallery: call.images, index: i) }
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
            .background(Theme.codeBackground, in: .rect(cornerRadius: 10, style: .continuous))
            if clipped {
                Button("展开全部 \(lines.count) 行") { withAnimation { full = true } }.font(.caption.weight(.medium))
            }
        }
    }
}

struct ResultBlock: View {
    var text: String
    var isError: Bool

    var body: some View {
        if text.isEmpty {
            Text(isError ? "失败" : "完成，无输出").font(.caption).foregroundStyle(isError ? Theme.danger : .secondary)
        } else {
            CodeBlock(text: text, maxLines: 10)
                .overlay(alignment: .topTrailing) {
                    if isError { Image(systemName: "xmark.octagon.fill").foregroundStyle(Theme.danger).padding(6) }
                }
        }
    }
}

struct DiffView: View {
    var old: String
    var new: String

    var body: some View {
        let removed = old.isEmpty ? [] : old.components(separatedBy: "\n")
        let added = new.isEmpty ? [] : new.components(separatedBy: "\n")
        ScrollView(.horizontal, showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(removed.prefix(30).enumerated()), id: \.offset) { _, l in line("-", l, Theme.danger) }
                ForEach(Array(added.prefix(30).enumerated()), id: \.offset) { _, l in line("+", l, Theme.idle) }
                if removed.count > 30 || added.count > 30 {
                    Text("… 共 -\(removed.count) +\(added.count) 行").font(.caption2).foregroundStyle(.secondary).padding(6)
                }
            }
            .padding(.vertical, 6)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.codeBackground, in: .rect(cornerRadius: 10, style: .continuous))
    }

    private func line(_ sign: String, _ text: String, _ color: Color) -> some View {
        HStack(spacing: 6) {
            Text(sign).foregroundStyle(color).frame(width: 10)
            Text(text.isEmpty ? " " : text)
        }
        .font(.system(size: 11.5, design: .monospaced))
        .padding(.horizontal, 8)
        .padding(.vertical, 1)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(color.opacity(0.1))
    }
}

/// A unified diff as Codex writes it: hunk headers quiet, + green, - red, context plain.
struct UnifiedDiffView: View {
    var diff: String
    private static let maxLines = 60

    var body: some View {
        let lines = diff.components(separatedBy: "\n").filter { !$0.hasPrefix("---") && !$0.hasPrefix("+++") }
        let shown = lines.prefix(Self.maxLines)
        ScrollView(.horizontal, showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(shown.enumerated()), id: \.offset) { _, l in line(l) }
                if lines.count > Self.maxLines {
                    Text("… 还有 \(lines.count - Self.maxLines) 行").font(.caption2).foregroundStyle(.secondary).padding(6)
                }
            }
            .padding(.vertical, 6)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.codeBackground, in: .rect(cornerRadius: 10, style: .continuous))
    }

    private func line(_ l: String) -> some View {
        let color: Color? = l.hasPrefix("+") ? Theme.idle : l.hasPrefix("-") ? Theme.danger : nil
        return Text(l.isEmpty ? " " : l)
            .font(.system(size: 11.5, design: .monospaced))
            .foregroundStyle(l.hasPrefix("@@") ? Color.secondary : color ?? .primary)
            .padding(.horizontal, 8)
            .padding(.vertical, 1)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background((color ?? .clear).opacity(0.1))
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
                        .foregroundStyle(status == "completed" ? Theme.idle : status == "in_progress" ? Theme.working : .secondary)
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
    @Namespace private var zoom

    var body: some View {
        Group {
            if let image {
                Image(uiImage: image).resizable().scaledToFit()
                    .clipShape(.rect(cornerRadius: 12, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).stroke(Theme.cardStroke, lineWidth: 0.5))
                    .frame(maxHeight: 280)
                    .contentShape(.rect)
                    .onTapGesture { showViewer = true }
                    .matchedTransitionSource(id: "image", in: zoom)
                    .accessibilityAddTraits(.isButton)
                    .accessibilityLabel("图片，点按查看大图")
            } else {
                RoundedRectangle(cornerRadius: 12).fill(Color.primary.opacity(0.05)).frame(height: 120)
            }
        }
        .fullScreenCover(isPresented: $showViewer) {
            ImageViewer(images: gallery.isEmpty ? [payload] : gallery, index: gallery.isEmpty ? 0 : index,
                        conversation: chatImages, attach: attachImage)
                .navigationTransition(.zoom(sourceID: "image", in: zoom))
        }
        .task(id: payload.data.count) {
            let data = payload.data
            // Thumbnails only need ~2x their on-screen size.
            image = await Task.detached {
                guard let d = Data(base64Encoded: data), let full = UIImage(data: d) else { return nil }
                return full.preparingThumbnail(of: ZoomMath.fittedSize(image: full.size, in: CGSize(width: 900, height: 900))) ?? full
            }.value
        }
    }
}
