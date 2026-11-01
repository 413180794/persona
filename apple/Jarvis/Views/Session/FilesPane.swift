import SwiftUI

/// The Files tab: the session's directory as a tree you walk into, with git change marks; tap a file to read it.
struct FilesPane: View {
    @Environment(AppModel.self) private var model
    let machineID: String
    let cwd: String
    var git: GitStatus?
    @State private var stack: [String] = [] // folders entered below cwd
    @State private var listing: FileListing?
    @State private var listed = "" // the path `listing` belongs to (the server returns it cleaned)
    @State private var error: String?
    @State private var file: FileRef?
    @AppStorage("files.showHidden") private var showHidden = false

    private var path: String { stack.reduce(cwd) { ($0 as NSString).appendingPathComponent($1) } }

    var body: some View {
        VStack(spacing: 0) {
            breadcrumb
            List {
                if let listing, listed == path {
                    let entries = listing.entries.filter { showHidden || !$0.name.hasPrefix(".") }
                    if entries.isEmpty {
                        Text(listing.entries.isEmpty ? "空文件夹" : "只有隐藏文件").foregroundStyle(.secondary)
                    }
                    ForEach(entries) { e in entryRow(e) }
                    if listing.truncated {
                        Text("文件太多，只列出了前 2000 个").font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                } else if let error {
                    ContentUnavailableView("打不开这个文件夹", systemImage: "folder.badge.questionmark", description: Text(error))
                        .listRowBackground(Color.clear)
                } else {
                    HStack { Spacer(); ProgressView(); Spacer() }.listRowBackground(Color.clear)
                }
            }
            .listStyle(.plain)
            .themedPage()
            .refreshable { await load() }
        }
        .task(id: path) { await load() }
        .sheet(item: $file) { ref in FileViewer(ref: ref) }
        #if os(macOS)
        .toolbar { MacRefreshItem { await load() } } // no pull to refresh on the Mac
        #endif
    }

    private var breadcrumb: some View {
        HStack(spacing: Theme.Space.s) {
            if !stack.isEmpty {
                Button {
                    Haptics.tap()
                    stack.removeLast()
                } label: {
                    Image(systemName: Theme.Icon.back).font(Theme.Font.captionStrong).scaledFrame(width: Theme.Size.wellS, height: Theme.Size.wellS)
                }
                .themedButton()
                .clipShape(.circle)
                .accessibilityLabel("上一级")
            }
            ScrollViewReader { proxy in
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: Theme.Space.xs) {
                        crumb((cwd as NSString).lastPathComponent, level: 0)
                        ForEach(Array(stack.enumerated()), id: \.offset) { i, name in
                            Image(systemName: "chevron.compact.right").font(Theme.Font.label).foregroundStyle(.tertiary)
                            crumb(name, level: i + 1)
                        }
                    }
                    .padding(.vertical, Theme.Space.xxs)
                }
                .onChange(of: stack.count) { _, n in withAnimation { proxy.scrollTo(n, anchor: .trailing) } }
            }
            Menu {
                Toggle(isOn: $showHidden) { Label("显示隐藏文件", systemImage: Theme.Icon.view) }
                Button { UIPasteboard.general.string = path } label: { Label("拷贝路径", systemImage: Theme.Icon.copy) }
            } label: {
                Image(systemName: Theme.Icon.more).font(Theme.Font.captionStrong).scaledFrame(width: Theme.Size.wellS, height: Theme.Size.wellS)
            }
            #if os(macOS)
            // A Mac menu draws as a pop-up button with its own arrow: the ⋯ was squeezed to two dots inside the circle.
            .menuStyle(.button)
            .menuIndicator(.hidden)
            #endif
            .themedButton()
            .clipShape(.circle)
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.sm)
    }

    private func crumb(_ name: String, level: Int) -> some View {
        let current = level == stack.count
        return Button {
            guard !current else { return }
            Haptics.tap()
            stack = Array(stack.prefix(level))
        } label: {
            Text(name.isEmpty ? "/" : name)
                .font(current ? Theme.Font.captionStrong : Theme.Font.caption)
                .lineLimit(1)
                .foregroundStyle(current ? Color.primary : Color.secondary)
                .padding(.horizontal, Theme.Space.s)
                .padding(.vertical, Theme.Space.xs)
                .background(current ? Theme.Fill.chip : .clear, in: .capsule)
        }
        .buttonStyle(.plain)
        .id(level)
    }

    private func entryRow(_ e: FileEntry) -> some View {
        let full = (path as NSString).appendingPathComponent(e.name)
        let mark = gitMark(full, dir: e.dir)
        return Button {
            if e.dir {
                Haptics.tap()
                stack.append(e.name)
            } else {
                file = FileRef(machineID: machineID, path: full, cwd: cwd)
            }
        } label: {
            HStack(spacing: Theme.Space.m) {
                Image(systemName: e.dir ? Theme.Icon.folderFilled : FileViewer.symbol(for: e.name))
                    .font(Theme.Font.body)
                    .foregroundStyle(e.dir ? Theme.accent : Color.secondary)
                    .scaledFrame(width: Theme.Size.icon)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    HStack(spacing: Theme.Space.xs) {
                        Text(e.name).font(Theme.Font.secondary).foregroundStyle(e.name.hasPrefix(".") ? Color.secondary : Color.primary).lineLimit(1)
                        if e.symlink == true { Image(systemName: "arrow.turn.up.right").font(Theme.Font.label).foregroundStyle(.tertiary) }
                    }
                    HStack(spacing: Theme.Space.sm) {
                        if !e.dir { Text(ByteCountFormatter.string(fromByteCount: e.size, countStyle: .file)) }
                        if let m = e.mtime { Text(Fmt.relative(m)) }
                    }
                    .font(Theme.Font.label)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                }
                Spacer(minLength: 6)
                if let mark {
                    if e.dir {
                        Circle().fill(GitBadge(letter: mark).color).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                    } else {
                        GitBadge(letter: mark)
                    }
                }
                if e.dir { Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary) }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .contextMenu {
            Button { UIPasteboard.general.string = full } label: { Label("拷贝路径", systemImage: Theme.Icon.copy) }
        }
    }

    /// The git letter of a file, or for a folder the letter of anything changed inside it.
    private func gitMark(_ full: String, dir: Bool) -> String? {
        guard let git, let root = git.root, !git.files.isEmpty, full.hasPrefix(root + "/") else { return nil }
        let rel = String(full.dropFirst(root.count + 1))
        if dir {
            return git.files.first { $0.path.hasPrefix(rel + "/") }?.letter
        }
        return git.files.first { $0.path == rel }?.letter
    }

    private func load() async {
        guard let api = model.api, !cwd.isEmpty else { return }
        let target = path
        do {
            let l = try await api.listDir(machine: machineID, path: target, cwd: cwd)
            if target == path { listing = l; listed = target; error = nil }
        } catch {
            if target == path { self.error = error.shown }
        }
    }
}
