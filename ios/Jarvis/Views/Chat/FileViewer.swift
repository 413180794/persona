import MarkdownUI
import PDFKit
import SwiftUI

nonisolated struct FileContent: Decodable, Sendable {
    var path: String
    var name: String
    var size: Int64
    var mime: String?
    var text: String?
    var data: String?
    var truncated: Bool
    var isDir: Bool
    var entries: [String]?

    enum CodingKeys: String, CodingKey {
        case path, name, size, mime, text, data, truncated, entries
        case isDir = "is_dir"
    }
}

extension APIClient {
    func file(machine: String, path: String, cwd: String?) async throws -> FileContent {
        try await get("/api/v1/machines/\(machine)/file", query: ["path": path, "cwd": cwd ?? ""])
    }
}

/// A file mentioned in a conversation, opened on the machine the session runs on.
struct FileRef: Identifiable, Hashable {
    var machineID: String
    var path: String
    var cwd: String?
    var id: String { machineID + ":" + path }
}

nonisolated enum FilePaths {
    // Home-relative (~/x), absolute (/x/y) or relative with a directory and an extension (ios/Jarvis/Foo.swift).
    // The "~" must stay part of the match: dropping it turned ~/.config/… into /.config/….
    // Relative paths need a letter-led extension so model names like deepseek/v4.1-flash are not files.
    // ASCII only, like links (`Links`): CJK text or full-width punctuation right after a path ends it ("Foo.swift里的").
    private static let pattern = try! NSRegularExpression(
        pattern: #"(?<![A-Za-z0-9_/:.\-~])(~(?:/[A-Za-z0-9_.@+\-]+)+/?|/[A-Za-z0-9_.@+\-]+(?:/[A-Za-z0-9_.@+\-]+)+/?|[A-Za-z0-9_.@\-]+(?:/[A-Za-z0-9_.@+\-]+)+\.[A-Za-z][A-Za-z0-9]{0,7})(?![A-Za-z0-9_/\-])"#)

    private static let memo = TextMemo<[String]>()

    /// File-looking paths in text, in order, without duplicates or URL fragments (memoized: rows ask on every redraw).
    static func find(in text: String, limit: Int = 6) -> [String] {
        limit == 6 ? memo(text) { scan($0, limit: 6) } : scan(text, limit: limit)
    }

    private static func scan(_ text: String, limit: Int) -> [String] {
        let ns = text as NSString
        var out: [String] = []
        for m in pattern.matches(in: text, range: NSRange(location: 0, length: ns.length)) {
            var p = ns.substring(with: m.range(at: 1))
            while let last = p.last, ".,;:)".contains(last) { p.removeLast() }
            let before = m.range.location >= 3 ? ns.substring(with: NSRange(location: m.range.location - 3, length: 3)) : ""
            if before.hasSuffix("://") || p.hasPrefix("//") || p.count < 4 || out.contains(p) { continue }
            if p.hasPrefix("/") && p.split(separator: "/").count < 2 { continue }
            out.append(p)
            if out.count >= limit { break }
        }
        return out
    }
}

/// Tappable chips for paths found in a message.
struct FileChips: View {
    var paths: [String]
    var open: (String) -> Void

    var body: some View {
        if !paths.isEmpty {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: Theme.Space.sm) {
                    ForEach(paths, id: \.self) { p in
                        Button { open(p) } label: {
                            HStack(spacing: Theme.Space.xs) {
                                Image(systemName: FileViewer.symbol(for: p)).font(Theme.Font.label)
                                Text((p as NSString).lastPathComponent.isEmpty ? p : (p as NSString).lastPathComponent)
                                    .font(Theme.Font.labelMedium)
                                    .lineLimit(1)
                            }
                            .padding(.horizontal, Theme.Space.s)
                            .padding(.vertical, Theme.Space.sm)
                            .background(Theme.accent.opacity(0.1), in: .capsule)
                            .foregroundStyle(Theme.accent)
                        }
                        .buttonStyle(.plain)
                        .contextMenu { Button { UIPasteboard.general.string = p } label: { Label("拷贝路径", systemImage: Theme.Icon.copy) } }
                    }
                }
            }
            .scrollClipDisabled()
        }
    }
}

/// Reads a file (or lists a directory) on a machine: markdown rendered, code and text monospaced with line
/// numbers, images and PDFs shown inline.
struct FileViewer: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State var ref: FileRef
    @State private var file: FileContent?
    @State private var error: String?
    @State private var raw = false
    @State private var history: [FileRef] = []

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(file?.name ?? (ref.path as NSString).lastPathComponent)
                .navigationSubtitle(model.machine(ref.machineID)?.name ?? ref.machineID)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        if let back = history.last {
                            Button { history.removeLast(); ref = back } label: { Label("返回", systemImage: Theme.Icon.back) }
                        } else {
                            Button("完成") { dismiss() }
                        }
                    }
                    ToolbarItem(placement: .primaryAction) { menu }
                }
                .task(id: ref) { await load() }
        }
    }

    @ViewBuilder private var content: some View {
        if let file {
            if file.isDir {
                List(file.entries ?? [], id: \.self) { e in
                    Button {
                        history.append(ref)
                        ref = FileRef(machineID: ref.machineID, path: (file.path as NSString).appendingPathComponent(e), cwd: ref.cwd)
                    } label: {
                        Label(e, systemImage: e.hasSuffix("/") ? Theme.Icon.folder : FileViewer.symbol(for: e))
                            .foregroundStyle(Color.primary)
                    }
                }
                .themedPage()
            } else if let text = file.text {
                if isMarkdown(file) && !raw {
                    ScrollView {
                        VStack(alignment: .leading, spacing: Theme.Space.m) {
                            meta(file)
                            Markdown(Links.markdown(MarkdownHTML.simplify(text)), imageBaseURL: URL(fileURLWithPath: (file.path as NSString).deletingLastPathComponent, isDirectory: true))
                                .markdownTheme(.jarvis)
                                .markdownImageProvider(MachineImageProvider(load: loadImageFile))
                                .textSelection(.enabled)
                        }
                        .padding(Theme.Space.l)
                    }
                } else {
                    // The code view scrolls both ways by itself (no outer vertical scroll stealing the gesture).
                    VStack(alignment: .leading, spacing: Theme.Space.m) {
                        meta(file).padding(.horizontal, Theme.Space.l)
                        CodeText(text: text).padding(.horizontal, Theme.Space.m)
                    }
                    .padding(.top, Theme.Space.m)
                }
            } else if let data = file.data, let bytes = Data(base64Encoded: data) {
                if (file.mime ?? "").hasPrefix("image/") {
                    ScrollView {
                        meta(file).padding(.horizontal, Theme.Space.l)
                        Base64Image(payload: ImagePayload(mime: file.mime ?? "image/png", data: data)).padding(Theme.Space.l)
                    }
                } else if (file.mime ?? "") == "application/pdf", let doc = PDFDocument(data: bytes) {
                    PDFKitView(document: doc)
                } else {
                    EmptyStateView(symbol: Theme.Icon.missingFile, title: "无法预览",
                                   message: "\(file.mime ?? "二进制文件") · \(ByteCountFormatter.string(fromByteCount: file.size, countStyle: .file))")
                }
            }
        } else if let error {
            EmptyStateView(symbol: Theme.Icon.error, title: "打不开这个文件", message: error)
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private var menu: some View {
        Menu {
            if let file, isMarkdown(file) {
                Button { raw.toggle() } label: { Label(raw ? "渲染 Markdown" : "查看源文件", systemImage: raw ? "doc.richtext" : Theme.Icon.code) }
            }
            Button { UIPasteboard.general.string = ref.path } label: { Label("拷贝路径", systemImage: Theme.Icon.copy) }
            if let text = file?.text {
                Button { UIPasteboard.general.string = text } label: { Label("拷贝内容", systemImage: Theme.Icon.copy) }
                ShareLink(item: text, subject: Text(file?.name ?? "")) { Label("分享", systemImage: Theme.Icon.share) }
            }
        } label: { Image(systemName: Theme.Icon.moreMenu) }
    }

    private func meta(_ f: FileContent) -> some View {
        HStack(spacing: Theme.Space.sm) {
            Text(Fmt.path(f.path, home: model.machine(ref.machineID)?.home)).lineLimit(1).truncationMode(.head)
            Text("·")
            Text(ByteCountFormatter.string(fromByteCount: f.size, countStyle: .file))
            if f.truncated { Text("· 只显示前 2 MB").foregroundStyle(Theme.cautionText) }
        }
        .font(Theme.Font.code)
        .foregroundStyle(.secondary)
    }

    private func isMarkdown(_ f: FileContent) -> Bool {
        ["md", "markdown", "mdx"].contains((f.name as NSString).pathExtension.lowercased())
    }

    /// An image the markdown file refers to, read from the same machine.
    private func loadImageFile(_ path: String) async -> FileContent? {
        try? await model.api?.file(machine: ref.machineID, path: path, cwd: ref.cwd)
    }

    private func load() async {
        guard let api = model.api else { return }
        file = nil
        error = nil
        do {
            file = try await api.file(machine: ref.machineID, path: ref.path, cwd: ref.cwd)
        } catch {
            self.error = error.shown
        }
    }

    static func symbol(for path: String) -> String {
        switch (path as NSString).pathExtension.lowercased() {
        case "md", "markdown", "txt": "doc.text"
        case "png", "jpg", "jpeg", "gif", "webp", "heic": "photo"
        case "pdf": "doc.richtext"
        case "json", "yml", "yaml", "toml", "plist": "curlybraces"
        case "": path.hasSuffix("/") ? Theme.Icon.folder : Theme.Icon.file
        default: Theme.Icon.code
        }
    }
}

/// Monospaced text with line numbers; no wrapping, scrolls sideways and down like an editor.
struct CodeText: View {
    var text: String

    var body: some View {
        MonoTextView(text: MonoText.numbered(text))
            .background(Theme.codeBackground, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}

struct PDFKitView: UIViewRepresentable {
    var document: PDFDocument
    func makeUIView(context: Context) -> PDFView {
        let v = PDFView()
        v.autoScales = true
        v.document = document
        return v
    }
    func updateUIView(_ v: PDFView, context: Context) {}
}

/// Images in a markdown file on a computer: relative paths load from that machine, web URLs as usual.
struct MachineImageProvider: ImageProvider {
    var load: (String) async -> FileContent?

    func makeImage(url: URL?) -> some View {
        if let url, url.isFileURL {
            MachineMarkdownImage(path: url.path, load: load)
        } else {
            DefaultImageProvider.default.makeImage(url: url)
        }
    }
}

struct MachineMarkdownImage: View {
    var path: String
    var load: (String) async -> FileContent?
    @State private var payload: ImagePayload?
    @State private var failed = false

    var body: some View {
        Group {
            if let payload {
                Base64Image(payload: payload)
            } else if failed {
                // SVG and anything else that is not a bitmap: say what is there instead of a blank.
                Label((path as NSString).lastPathComponent, systemImage: Theme.Icon.photo)
                    .font(Theme.Font.label)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .padding(.horizontal, Theme.Space.m)
                    .padding(.vertical, Theme.Space.sm)
                    .background(Color.primary.opacity(0.05), in: .capsule)
            } else {
                RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).fill(Color.primary.opacity(0.05))
                    .scaledFrame(height: Theme.Size.box120)
                    .overlay { ProgressView() }
            }
        }
        .task(id: path) {
            guard payload == nil else { return }
            if let f = await load(path), let data = f.data, let mime = f.mime, mime.hasPrefix("image/"), mime != "image/svg+xml" {
                payload = ImagePayload(mime: mime, data: data)
            } else {
                failed = true
            }
        }
    }
}

/// MarkdownUI shows raw HTML as source text. READMEs use a little of it (image grids in `<table>`, centered `<p>`,
/// `<img width=…>`), so turn the common tags into markdown and drop the rest, leaving their text.
nonisolated enum MarkdownHTML {
    static func simplify(_ md: String) -> String {
        guard md.contains("<") else { return md }
        var out: [String] = []
        var fence: String?
        var table: [String]?
        for line in md.components(separatedBy: "\n") {
            let t = line.trimmingCharacters(in: .whitespaces)
            if let f = fence {
                if t.hasPrefix(f) { fence = nil }
                out.append(line)
                continue
            }
            if t.hasPrefix("```") || t.hasPrefix("~~~") {
                fence = String(t.prefix(3))
                out.append(line)
                continue
            }
            if table != nil {
                table!.append(line)
                if t.range(of: "</table>", options: .caseInsensitive) != nil {
                    out.append(contentsOf: Self.table(table!.joined(separator: "\n")))
                    table = nil
                }
                continue
            }
            if t.range(of: "^<table\\b", options: [.regularExpression, .caseInsensitive]) != nil {
                if t.range(of: "</table>", options: .caseInsensitive) != nil {
                    out.append(contentsOf: Self.table(t))
                } else {
                    table = [line]
                }
                continue
            }
            if t.hasPrefix("<") && !t.hasPrefix("<http") {
                let s = inline(t)
                if !s.isEmpty { out.append(s) }
                continue
            }
            out.append(line)
        }
        if let table { out.append(contentsOf: table) }
        return out.joined(separator: "\n")
    }

    /// `<table>` → a markdown table, the first row as the header.
    private static func table(_ html: String) -> [String] {
        let rows = matches("<tr\\b[^>]*>(.*?)</tr>", in: html).map { row in
            matches("<t[dh]\\b[^>]*>(.*?)</t[dh]>", in: row).map { inline($0).replacingOccurrences(of: "|", with: "\\|") }
        }.filter { !$0.isEmpty }
        guard let width = rows.map(\.count).max() else { return [] }
        func line(_ cells: [String]) -> String {
            "| " + (cells + Array(repeating: "", count: width - cells.count)).joined(separator: " | ") + " |"
        }
        return [""] + [line(rows[0]), line(Array(repeating: "---", count: width))] + rows.dropFirst().map(line) + [""]
    }

    /// Images and links into markdown, line breaks into spaces, other tags dropped.
    static func inline(_ html: String) -> String {
        var s = html
        s = replace("<img\\b[^>]*>", in: s) { tag in
            guard let src = attr("src", in: tag) else { return "" }
            return "![\(attr("alt", in: tag) ?? "")](\(src))"
        }
        s = replace("<a\\b[^>]*>(.*?)</a>", in: s) { tag in
            let text = replace("<[^>]+>", in: matches("<a\\b[^>]*>(.*?)</a>", in: tag).first ?? "") { _ in "" }
            guard let href = attr("href", in: tag) else { return text }
            return "[\(text)](\(href))"
        }
        s = replace("<br\\s*/?>", in: s) { _ in " " }
        s = replace("<[^>]+>", in: s) { _ in "" }
        for (k, v) in [("&nbsp;", " "), ("&lt;", "<"), ("&gt;", ">"), ("&quot;", "\""), ("&amp;", "&")] {
            s = s.replacingOccurrences(of: k, with: v)
        }
        return s.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    private static func attr(_ name: String, in tag: String) -> String? {
        matches("\\b\(name)\\s*=\\s*[\"']([^\"']*)[\"']", in: tag).first
    }

    /// The first capture group of each match (dot matches newlines, case-insensitive).
    private static func matches(_ pattern: String, in s: String) -> [String] {
        guard let re = try? NSRegularExpression(pattern: pattern, options: [.caseInsensitive, .dotMatchesLineSeparators]) else { return [] }
        let ns = s as NSString
        return re.matches(in: s, range: NSRange(location: 0, length: ns.length)).map { ns.substring(with: $0.range(at: 1)) }
    }

    private static func replace(_ pattern: String, in s: String, with f: (String) -> String) -> String {
        guard let re = try? NSRegularExpression(pattern: pattern, options: [.caseInsensitive, .dotMatchesLineSeparators]) else { return s }
        let ns = s as NSString
        var out = ""
        var last = 0
        for m in re.matches(in: s, range: NSRange(location: 0, length: ns.length)) {
            out += ns.substring(with: NSRange(location: last, length: m.range.location - last))
            out += f(ns.substring(with: m.range))
            last = m.range.location + m.range.length
        }
        return out + ns.substring(from: last)
    }
}
