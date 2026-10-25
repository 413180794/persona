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
    private static let pattern = try! NSRegularExpression(
        pattern: #"(?<![\w/:.\-~])(~(?:/[\w.@+\-]+)+/?|/[\w.@+\-]+(?:/[\w.@+\-]+)+/?|[\w.@\-]+(?:/[\w.@+\-]+)+\.[A-Za-z][A-Za-z0-9]{0,7})(?![\w/\-])"#)

    /// File-looking paths in text, in order, without duplicates or URL fragments.
    static func find(in text: String, limit: Int = 6) -> [String] {
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
                HStack(spacing: 6) {
                    ForEach(paths, id: \.self) { p in
                        Button { open(p) } label: {
                            HStack(spacing: 4) {
                                Image(systemName: FileViewer.symbol(for: p)).font(.caption2)
                                Text((p as NSString).lastPathComponent.isEmpty ? p : (p as NSString).lastPathComponent)
                                    .font(.caption.weight(.medium))
                                    .lineLimit(1)
                            }
                            .padding(.horizontal, 9)
                            .padding(.vertical, 5)
                            .background(Theme.accent.opacity(0.1), in: .capsule)
                            .foregroundStyle(Theme.accent)
                        }
                        .buttonStyle(.plain)
                        .contextMenu { Button { UIPasteboard.general.string = p } label: { Label("拷贝路径", systemImage: "doc.on.doc") } }
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
                            Button { history.removeLast(); ref = back } label: { Image(systemName: "chevron.left") }
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
                        Label(e, systemImage: e.hasSuffix("/") ? "folder" : FileViewer.symbol(for: e))
                            .foregroundStyle(Color.primary)
                    }
                }
            } else if let text = file.text {
                if isMarkdown(file) && !raw {
                    ScrollView {
                        VStack(alignment: .leading, spacing: 10) {
                            meta(file)
                            Markdown(text).markdownTheme(.jarvis).textSelection(.enabled)
                        }
                        .padding(16)
                    }
                } else {
                    // The code view scrolls both ways by itself (no outer vertical scroll stealing the gesture).
                    VStack(alignment: .leading, spacing: 10) {
                        meta(file).padding(.horizontal, 16)
                        CodeText(text: text).padding(.horizontal, 12)
                    }
                    .padding(.top, 12)
                }
            } else if let data = file.data, let bytes = Data(base64Encoded: data) {
                if (file.mime ?? "").hasPrefix("image/") {
                    ScrollView {
                        meta(file).padding(.horizontal, 16)
                        Base64Image(payload: ImagePayload(mime: file.mime ?? "image/png", data: data)).padding(16)
                    }
                } else if (file.mime ?? "") == "application/pdf", let doc = PDFDocument(data: bytes) {
                    PDFKitView(document: doc)
                } else {
                    EmptyStateView(symbol: "doc.questionmark", title: "无法预览",
                                   message: "\(file.mime ?? "二进制文件") · \(ByteCountFormatter.string(fromByteCount: file.size, countStyle: .file))")
                }
            }
        } else if let error {
            EmptyStateView(symbol: "exclamationmark.triangle", title: "打不开这个文件", message: error)
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private var menu: some View {
        Menu {
            if let file, isMarkdown(file) {
                Button { raw.toggle() } label: { Label(raw ? "渲染 Markdown" : "查看源文件", systemImage: raw ? "doc.richtext" : "chevron.left.forwardslash.chevron.right") }
            }
            Button { UIPasteboard.general.string = ref.path } label: { Label("拷贝路径", systemImage: "doc.on.doc") }
            if let text = file?.text {
                Button { UIPasteboard.general.string = text } label: { Label("拷贝内容", systemImage: "doc.on.clipboard") }
                ShareLink(item: text, subject: Text(file?.name ?? "")) { Label("分享", systemImage: "square.and.arrow.up") }
            }
        } label: { Image(systemName: "ellipsis.circle") }
    }

    private func meta(_ f: FileContent) -> some View {
        HStack(spacing: 6) {
            Text(Fmt.path(f.path, home: model.machine(ref.machineID)?.home)).lineLimit(1).truncationMode(.head)
            Text("·")
            Text(ByteCountFormatter.string(fromByteCount: f.size, countStyle: .file))
            if f.truncated { Text("· 只显示前 2 MB").foregroundStyle(Theme.attention) }
        }
        .font(.caption.monospaced())
        .foregroundStyle(.secondary)
    }

    private func isMarkdown(_ f: FileContent) -> Bool {
        ["md", "markdown", "mdx"].contains((f.name as NSString).pathExtension.lowercased())
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
        case "": path.hasSuffix("/") ? "folder" : "doc"
        default: "chevron.left.forwardslash.chevron.right"
        }
    }
}

/// Monospaced text with line numbers; no wrapping, scrolls sideways and down like an editor.
struct CodeText: View {
    var text: String

    var body: some View {
        MonoTextView(text: MonoText.numbered(text))
            .background(Theme.codeBackground, in: .rect(cornerRadius: 12, style: .continuous))
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
