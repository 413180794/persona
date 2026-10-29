import SwiftUI

/// The Git tab of a session: branch and sync state, working-tree changes with their diffs, and the history.
struct GitPane: View {
    @Environment(AppModel.self) private var model
    let machineID: String
    let cwd: String
    @Binding var status: GitStatus?
    @State private var error: String?
    @State private var older: [GitCommit] = []
    @State private var noMore = false
    @State private var loadingMore = false
    @State private var diff: GitDiffTarget?
    @State private var commit: GitCommit?

    var body: some View {
        List {
            if let s = status, s.isRepo {
                Section { header(s) }
                changes(s)
                history(s)
            } else if let error {
                ContentUnavailableView("读不到 Git 状态", systemImage: "exclamationmark.triangle", description: Text(error))
                    .listRowBackground(Color.clear)
            } else if status?.isRepo == false {
                ContentUnavailableView("不是 Git 仓库", systemImage: "arrow.triangle.branch", description: Text(cwd))
                    .listRowBackground(Color.clear)
            } else {
                HStack { Spacer(); ProgressView(); Spacer() }.listRowBackground(Color.clear)
            }
        }
        .listStyle(.insetGrouped)
        .themedPage()
        .refreshable { await load() }
        .task(id: cwd) { await load() }
        .sheet(item: $diff) { t in GitDiffSheet(machineID: machineID, cwd: cwd, target: t) }
        .sheet(item: $commit) { c in GitCommitSheet(machineID: machineID, cwd: cwd, commit: c) }
    }

    private func header(_ s: GitStatus) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: "arrow.triangle.branch").foregroundStyle(Theme.accent)
                Text(s.branch ?? "分离的 HEAD").font(.headline).lineLimit(1)
                Spacer(minLength: 4)
                if s.ahead > 0 { sync("arrow.up", s.ahead, .secondary) }
                if s.behind > 0 { sync("arrow.down", s.behind, Theme.cautionText) }
            }
            HStack(spacing: 6) {
                if let up = s.upstream {
                    Label(up, systemImage: "cloud").lineLimit(1)
                    if s.ahead == 0 && s.behind == 0 { Text("· 已同步") }
                } else {
                    Label("没有上游分支", systemImage: "cloud.slash").lineLimit(1)
                }
                if s.stashes > 0 { Text("· 暂存 \(s.stashes)") }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            if let root = s.root {
                Text(Fmt.path(root, home: nil)).font(.caption2.monospaced()).foregroundStyle(.tertiary).lineLimit(1).truncationMode(.head)
            }
        }
        .padding(.vertical, 2)
    }

    private func sync(_ symbol: String, _ n: Int, _ color: Color) -> some View {
        Chip(text: "\(n)", symbol: symbol, look: color == .secondary ? .neutral : .tint(color)).monospacedDigit()
    }

    @ViewBuilder private func changes(_ s: GitStatus) -> some View {
        Section {
            if s.files.isEmpty {
                Label("工作区是干净的", systemImage: "checkmark.circle").foregroundStyle(.secondary)
            }
            ForEach(s.files) { f in
                Button { diff = GitDiffTarget(file: f, staged: f.isStaged && !f.hasUnstaged) } label: { GitFileRow(file: f) }
                    .buttonStyle(.plain)
                    .contextMenu {
                        Button { UIPasteboard.general.string = f.path } label: { Label("拷贝路径", systemImage: "doc.on.doc") }
                    }
            }
        } header: {
            if !s.files.isEmpty {
                let added = s.files.reduce(0) { $0 + $1.added }, deleted = s.files.reduce(0) { $0 + $1.deleted }
                HStack {
                    Text("改动 \(s.files.count)")
                    Spacer()
                    Text("+\(added)").foregroundStyle(Theme.Diff.add)
                    Text("−\(deleted)").foregroundStyle(Theme.Diff.del)
                }
                .monospacedDigit()
            }
        }
    }

    @ViewBuilder private func history(_ s: GitStatus) -> some View {
        Section("提交") {
            ForEach(s.commits + older) { c in
                Button { commit = c } label: { GitCommitRow(commit: c, isHead: c.hash == s.head?.hash) }
                    .buttonStyle(.plain)
            }
            if !noMore && s.commits.count >= 30 {
                Button {
                    Task { await loadMore(after: s.commits.count + older.count) }
                } label: {
                    HStack(spacing: 6) {
                        if loadingMore { ProgressView().controlSize(.mini) }
                        Text("加载更早的提交").lineLimit(1)
                    }
                    .frame(maxWidth: .infinity)
                }
                .disabled(loadingMore)
            }
        }
    }

    private func load() async {
        guard let api = model.api, !cwd.isEmpty else { return }
        do {
            status = try await api.gitStatus(machine: machineID, cwd: cwd)
            older = []
            noMore = false
            error = nil
        } catch {
            if status == nil { self.error = error.shown }
        }
    }

    private func loadMore(after skip: Int) async {
        guard let api = model.api else { return }
        loadingMore = true
        defer { loadingMore = false }
        if let more = try? await api.gitLog(machine: machineID, cwd: cwd, skip: skip) {
            older += more
            noMore = more.count < 50
        }
    }
}

struct GitDiffTarget: Identifiable, Hashable {
    var file: GitFile
    var staged: Bool
    var id: String { file.path + (staged ? "#staged" : "") }
}

/// Status letter badge colored like the editors do.
struct GitBadge: View {
    var letter: String

    var body: some View {
        Text(letter)
            .font(.system(size: 11, weight: .bold, design: .monospaced))
            .foregroundStyle(color)
            .frame(width: 20, height: 20)
            .background(color.opacity(0.14), in: .rect(cornerRadius: 5, style: .continuous))
    }

    var color: Color {
        switch letter {
        case "A", "U": Theme.Diff.add
        case "D", "!": Theme.Diff.del
        case "R", "C": .secondary
        default: Theme.Diff.changed
        }
    }
}

struct GitFileRow: View {
    var file: GitFile
    /// The working tree's changes say staged / partly staged; a commit's files have no stage (their `index` is only
    /// the change kind, for the badge).
    var showsStage = true

    var body: some View {
        HStack(spacing: 10) {
            GitBadge(letter: file.letter)
            VStack(alignment: .leading, spacing: 2) {
                Text(file.name).font(.subheadline.weight(.medium)).lineLimit(1)
                HStack(spacing: 4) {
                    if showsStage, file.isStaged { Text(file.hasUnstaged ? "部分已暂存" : "已暂存").foregroundStyle(.primary) }
                    if let orig = file.origPath { Text("← " + (orig as NSString).lastPathComponent) }
                    if !file.folder.isEmpty { Text(file.folder).truncationMode(.head) }
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            }
            Spacer(minLength: 6)
            if file.binary == true {
                Text("二进制").font(.caption2).foregroundStyle(.secondary)
            } else if file.added + file.deleted > 0 {
                HStack(spacing: 4) {
                    Text("+\(file.added)").foregroundStyle(Theme.Diff.add)
                    Text("−\(file.deleted)").foregroundStyle(Theme.Diff.del)
                }
                .font(.caption.monospacedDigit())
            }
        }
        .contentShape(.rect)
    }
}

struct GitCommitRow: View {
    var commit: GitCommit
    var isHead = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(commit.subject).font(.subheadline.weight(.medium)).lineLimit(2)
            HStack(spacing: 6) {
                Text(commit.short).font(.caption.monospaced()).foregroundStyle(Theme.accent)
                Text(commit.author).lineLimit(1)
                if let d = commit.date { Text("· " + Fmt.relative(d)).lineLimit(1) }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            let refs = commit.refNames
            if !refs.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 4) {
                        // A local branch outlined, a remote one (origin/…) plain: refs are facts, not buttons.
                        ForEach(refs, id: \.self) { r in Chip(text: r, look: r.contains("/") ? .neutral : .outline) }
                    }
                }
            }
        }
        .contentShape(.rect)
    }
}

/// The diff of one changed file; switches between the unstaged and staged halves when it has both.
struct GitDiffSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let machineID: String
    let cwd: String
    @State var target: GitDiffTarget
    @State private var diff: GitDiff?
    @State private var error: String?

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if target.file.isStaged && target.file.hasUnstaged {
                    Picker("", selection: $target.staged) {
                        Text("未暂存").tag(false)
                        Text("已暂存").tag(true)
                    }
                    .pickerStyle(.segmented)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)
                }
                if let diff {
                    DiffText(diff: diff.diff, truncated: diff.truncated)
                } else if let error {
                    ContentUnavailableView("读不到改动", systemImage: "exclamationmark.triangle", description: Text(error))
                } else {
                    ProgressView().frame(maxHeight: .infinity)
                }
            }
            .navigationTitle(target.file.name)
            .navigationSubtitle(target.file.folder.isEmpty ? "仓库根目录" : target.file.folder)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } }
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button { UIPasteboard.general.string = diff?.diff } label: { Label("拷贝 diff", systemImage: "doc.on.doc") }
                        Button { UIPasteboard.general.string = target.file.path } label: { Label("拷贝路径", systemImage: "link") }
                    } label: { Image(systemName: "ellipsis") }
                }
            }
            .task(id: target) {
                diff = nil
                do {
                    diff = try await model.api?.gitDiff(machine: machineID, cwd: cwd, path: target.file.path, staged: target.staged)
                } catch { self.error = error.shown }
            }
        }
    }
}

/// One commit: message, author, the files it touched, and each file's diff.
struct GitCommitSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let machineID: String
    let cwd: String
    let commit: GitCommit
    @State private var show: GitShow?
    @State private var error: String?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(commit.subject).font(.headline)
                        if let body = (show?.commit.body ?? commit.body), !body.isEmpty {
                            Text(body).font(.callout).foregroundStyle(.secondary).textSelection(.enabled)
                        }
                        HStack(spacing: 6) {
                            Text(commit.author)
                            if let d = commit.date { Text("· " + d.formatted(date: .abbreviated, time: .shortened)) }
                        }
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        Button {
                            UIPasteboard.general.string = commit.hash
                            Haptics.success()
                        } label: {
                            Label(commit.hash, systemImage: "number").font(.caption2.monospaced()).lineLimit(1).truncationMode(.middle)
                        }
                        .buttonStyle(.borderless)
                    }
                    .padding(.vertical, 2)
                }
                if let show {
                    let chunks = DiffText.split(show.diff)
                    Section("\(show.files.count) 个文件") {
                        ForEach(show.files) { f in
                            NavigationLink {
                                DiffText(diff: chunks[f.path] ?? "", truncated: show.truncated && chunks[f.path] == nil)
                                    .navigationTitle(f.name)
                                    .navigationBarTitleDisplayMode(.inline)
                            } label: {
                                GitFileRow(file: f, showsStage: false)
                            }
                        }
                    }
                } else if let error {
                    Text(error).foregroundStyle(Theme.danger)
                } else {
                    HStack { Spacer(); ProgressView(); Spacer() }
                }
            }
            .listStyle(.insetGrouped)
            .themedPage()
            .navigationTitle(commit.short)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } }
            .task {
                do { show = try await model.api?.gitShow(machine: machineID, cwd: cwd, hash: commit.hash) }
                catch { self.error = error.shown }
            }
        }
    }
}

/// A unified diff with old / new line numbers, colored lines, no wrapping (scroll sideways), rendered lazily.
struct DiffText: View {
    var diff: String
    var truncated = false

    struct Line: Identifiable {
        enum Kind { case add, del, context, hunk, info }
        var id: Int
        var kind: Kind
        var text: String
        var old: Int?
        var new: Int?
    }

    var body: some View {
        let lines = Self.parse(diff)
        if lines.isEmpty {
            ContentUnavailableView("没有文本改动", systemImage: "doc.text", description: Text("可能是二进制文件、只改了权限，或者改动已被撤销。"))
        } else {
            VStack(spacing: 0) {
                MonoTextView(text: Self.attributed(lines), inset: UIEdgeInsets(top: 6, left: 0, bottom: 6, right: 12))
                if truncated {
                    Text("改动太大，只显示了前 512 KB").font(.caption).foregroundStyle(.secondary).padding(8)
                }
            }
            .background(Theme.codeBackground)
        }
    }

    /// Gutter (old / new line numbers, +/−) and the line, colored; lines are padded to the longest so the
    /// added / removed backgrounds run the full width.
    static func attributed(_ lines: [Line]) -> NSAttributedString {
        let font = MonoText.font(11)
        let width = (lines.map(\.text.count).max() ?? 0) + 2
        let out = NSMutableAttributedString()
        func num(_ n: Int?) -> String { let s = n.map(String.init) ?? ""; return String(repeating: " ", count: max(0, 5 - s.count)) + s }
        for (i, l) in lines.enumerated() {
            let (mark, fg, bg): (String, UIColor, UIColor) = switch l.kind {
            case .add: ("+", UIColor(Theme.Diff.add), UIColor(Theme.Diff.add).withAlphaComponent(0.13))
            case .del: ("−", UIColor(Theme.Diff.del), UIColor(Theme.Diff.del).withAlphaComponent(0.12))
            case .hunk: (" ", .secondaryLabel, .quaternarySystemFill)
            case .info: (" ", .secondaryLabel, .clear)
            case .context: (" ", .label, .clear)
            }
            let gutter = num(l.old) + num(l.new) + " " + mark + " "
            out.append(NSAttributedString(string: gutter, attributes: [.font: font, .foregroundColor: UIColor.tertiaryLabel, .backgroundColor: bg]))
            let body = l.text.padding(toLength: max(width, l.text.count), withPad: " ", startingAt: 0)
            let color: UIColor = l.kind == .add || l.kind == .del || l.kind == .context ? .label : fg
            out.append(NSAttributedString(string: body + (i < lines.count - 1 ? "\n" : ""), attributes: [.font: font, .foregroundColor: color, .backgroundColor: bg]))
        }
        return out
    }

    static func parse(_ diff: String) -> [Line] {
        var out: [Line] = []
        var old = 0, new = 0
        for raw in diff.components(separatedBy: "\n") {
            let id = out.count
            if raw.hasPrefix("diff --git") || raw.hasPrefix("index ") || raw.hasPrefix("--- ") || raw.hasPrefix("+++ ") {
                continue
            }
            if raw.hasPrefix("@@") {
                // @@ -12,7 +12,9 @@ context
                let parts = raw.split(separator: " ")
                if parts.count > 2 {
                    old = Int(parts[1].dropFirst().split(separator: ",").first ?? "") ?? 0
                    new = Int(parts[2].dropFirst().split(separator: ",").first ?? "") ?? 0
                }
                out.append(Line(id: id, kind: .hunk, text: raw))
            } else if raw.hasPrefix("+") {
                out.append(Line(id: id, kind: .add, text: String(raw.dropFirst()), new: new)); new += 1
            } else if raw.hasPrefix("-") {
                out.append(Line(id: id, kind: .del, text: String(raw.dropFirst()), old: old)); old += 1
            } else if raw.hasPrefix(" ") {
                out.append(Line(id: id, kind: .context, text: String(raw.dropFirst()), old: old, new: new)); old += 1; new += 1
            } else if raw.hasPrefix("\\") || raw.hasPrefix("Binary") || raw.hasPrefix("new file") || raw.hasPrefix("deleted file")
                        || raw.hasPrefix("similarity") || raw.hasPrefix("rename ") || raw.hasPrefix("old mode") || raw.hasPrefix("new mode") {
                out.append(Line(id: id, kind: .info, text: raw))
            }
        }
        return out
    }

    /// Splits a multi-file diff (git show) into per-file diffs keyed by the new path.
    static func split(_ diff: String) -> [String: String] {
        var res: [String: String] = [:]
        var path: String?
        var buf: [String] = []
        func flush() { if let path { res[path] = buf.joined(separator: "\n") } }
        for line in diff.components(separatedBy: "\n") {
            if line.hasPrefix("diff --git ") {
                flush()
                buf = []
                // diff --git a/x b/x → x (paths with spaces keep working since b/ is taken from the end)
                if let r = line.range(of: " b/", options: .backwards) { path = String(line[r.upperBound...]) } else { path = nil }
            }
            buf.append(line)
        }
        flush()
        return res
    }
}
