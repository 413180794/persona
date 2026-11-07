import Foundation
import Observation
import UniformTypeIdentifiers

// Pictures and files pasted or dropped into a terminal the app drives (docs/integrations/terminal.md, section 18, as the web's
// #2232): each is uploaded to the machine of the pane (`POST /machines/{mid}/files?dest=terminal`), and once all
// are there their paths are typed into the pane as one bracketed paste, escaped as shell words, without a return.
// Claude Code and Codex read a pasted image path as an attachment.

/// The pure parts: the limit, names, escaping, what may still be typed when the uploads end, the words.
nonisolated enum TerminalPaste {
    /// The hub's limit for a terminal upload (`proto.MaxTerminalUploadBytes`).
    static let limit: Int64 = 50 << 20

    static let notDriving = String(localized: "Take over the terminal to paste files")
    static let switched = String(localized: "The terminal changed during upload, so nothing was pasted")
    static let released = String(localized: "You stopped driving the terminal during upload, so nothing was pasted")
    static let folders = String(localized: "Folders can't be uploaded. Drag in the files inside instead.")
    static let hubTooOld = String(localized: "This hub version is too old to upload files. Update the hub and try again.")

    /// Characters a path keeps as they are; everything else ASCII gets a backslash (the web's `escapePastePath`).
    private static let plain = Set("@%+=:,./-_".unicodeScalars)

    /// A path as one shell word: letters, digits, `_@%+=:,./-` and anything non-ASCII stay, the rest is escaped.
    static func escape(_ path: String) -> String {
        var out = ""
        for u in path.unicodeScalars {
            let keep = !u.isASCII || plain.contains(u) || ("a"..."z").contains(u) || ("A"..."Z").contains(u) || ("0"..."9").contains(u)
            if !keep { out.append("\\") }
            out.unicodeScalars.append(u)
        }
        return out
    }

    /// What goes into the pane for uploaded files: escaped paths, space-separated, a space after the last, as one
    /// bracketed paste (a program that reads pastes, Claude Code, attaches an image path it is given that way).
    static func typed(_ paths: [String]) -> String {
        "\u{1B}[200~" + paths.map(escape).joined(separator: " ") + " " + "\u{1B}[201~"
    }

    /// The name a picture without one is uploaded under, from its type ("image.png"); the machine cleans names again.
    static func pictureName(_ type: UTType?) -> String {
        "image." + (type?.preferredFilenameExtension ?? "png")
    }

    /// A photo from the library has no name: "image.heic", "video.mov"… by its type.
    static func photoName(_ type: UTType?) -> String {
        let base = type?.conforms(to: .movie) == true ? "video" : "image"
        return base + "." + (type?.preferredFilenameExtension ?? (base == "video" ? "mov" : "jpg"))
    }

    /// A picture Claude Code and Codex cannot read as it is (HEIC, TIFF…): sent as JPEG instead. PNG, JPEG, GIF and
    /// WebP stay; videos and other files are not pictures.
    static func needsJPEG(_ type: UTType?) -> Bool {
        guard let type, type.conforms(to: .image) else { return false }
        return ![UTType.png, .jpeg, .gif, .webP].contains { type.conforms(to: $0) }
    }

    /// Why a file is not uploaded, for the toast; nil when it may go.
    static func refusal(name: String, size: Int64) -> String? {
        if size <= 0 { return String(localized: "“\(name)” is empty") }
        if size > limit { return String(localized: "“\(name)” is over \(limit >> 20) MB and wasn't uploaded") }
        return nil
    }

    /// Dropped or pasted URLs: the files among them, and how many were folders (an upload is one file).
    static func files(_ urls: [URL]) -> (files: [URL], folders: Int) {
        var files: [URL] = []
        var folders = 0
        for url in urls where url.isFileURL {
            if url.hasDirectoryPath || (try? url.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) == true {
                folders += 1
            } else {
                files.append(url)
            }
        }
        return (files, folders)
    }

    enum Verdict: Equatable { case ok, changed, watching }

    /// Whether the paths may still be typed when the uploads end: only into the pane they were meant for (changed: the
    /// panel left it meanwhile) and only while this app still drives it (watching: taken back, or View Only).
    static func verdict(left: Bool, driving: Bool) -> Verdict {
        if left { return .changed }
        return driving ? .ok : .watching
    }

    /// "Uploading shot.png…" / "Uploading shot.png and 3 files…".
    static func uploadingText(_ names: [String]) -> String {
        guard names.count > 1 else {
            let name = names.first ?? String(localized: "terminalUpload.fileFallback", defaultValue: "file", comment: "A single file of unknown name being uploaded")
            return String(localized: "Uploading \(name)…")
        }
        return String(localized: "Uploading \(names[0]) and \(names.count)…")
    }

    /// What to say when a file did not make it; nil when the upload was cancelled.
    static func failure(_ error: Error, name: String) -> String? {
        if error.isCancellation { return nil }
        if let e = error as? APIError {
            if e.code == "too_large" { return String(localized: "“\(name)” is over \(limit >> 20) MB and wasn't uploaded") }
            if e.code == "node_outdated" { return e.message }
            if e.status == 404 { return hubTooOld }
        }
        return String(localized: "Couldn't upload “\(name)”: \(error.localizedDescription)")
    }
}

/// Something to upload: a file on this device, or bytes with a name (a pasted picture, a photo from the library).
enum TerminalFileSource {
    case file(URL)
    case data(Data, name: String)

    var name: String {
        switch self {
        case .file(let url): url.lastPathComponent
        case .data(_, let name): name
        }
    }

    var size: Int64 {
        switch self {
        case .file(let url): Int64((try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0)
        case .data(let data, _): Int64(data.count)
        }
    }
}

/// The uploads of one terminal panel: what is on its way (the bar over the terminal), then the paths typed in.
@MainActor @Observable
final class TerminalUploads {
    /// The files on their way, in the order given.
    private(set) var names: [String] = []
    /// Bumped when the panel leaves its pane (another session, closed): uploads that end later paste nothing.
    @ObservationIgnored private var generation = 0

    var status: String? { names.isEmpty ? nil : TerminalPaste.uploadingText(names) }

    init() {
        #if DEBUG
        // `-showTerminalUpload YES`: the bar as if two files were on their way, for screenshots.
        if UserDefaults.standard.bool(forKey: "showTerminalUpload") { names = ["截图.png", "build.log"] } // i18n-ignore
        #endif
    }

    func leave() { generation += 1 }

    func start(_ sources: [TerminalFileSource], feed: TerminalFeed, model: AppModel) {
        guard !sources.isEmpty else { return }
        guard feed.drives else {
            model.toast = Toast(TerminalPaste.notDriving, style: .info)
            return
        }
        let features = model.features(feed.key.group)
        if !features.isEmpty, !features.contains("file_upload") {
            model.toast = Toast(TerminalPaste.hubTooOld, style: .error)
            return
        }
        let sending = sources.filter { s in
            guard let why = TerminalPaste.refusal(name: s.name, size: s.size) else { return true }
            model.toast = Toast(why, style: .error)
            return false
        }
        guard !sending.isEmpty else { return }
        let names = sending.map(\.name)
        self.names += names
        let gen = generation, machine = feed.key.machineRef
        Task {
            var paths: [String] = []
            for s in sending {
                do {
                    paths.append(try await Self.upload(s, machine: machine, model: model))
                } catch {
                    if gen == generation, let msg = TerminalPaste.failure(error, name: s.name) { model.toast = Toast(msg, style: .error) }
                }
            }
            for n in names { if let i = self.names.firstIndex(of: n) { self.names.remove(at: i) } }
            guard !paths.isEmpty else { return }
            switch TerminalPaste.verdict(left: gen != generation, driving: feed.drives) {
            case .changed: model.toast = Toast(TerminalPaste.switched, style: .error)
            case .watching: model.toast = Toast(TerminalPaste.released, style: .error)
            case .ok:
                Haptics.tap()
                feed.type(TerminalPaste.typed(paths), model)
            }
        }
    }

    /// One file to the machine, from a private copy (a picked file may be an iCloud placeholder or lose its access).
    private static func upload(_ s: TerminalFileSource, machine: MachineRef, model: AppModel) async throws -> String {
        guard let api = model.api(machine.group) else { throw URLError(.notConnectedToInternet) }
        let staged: URL
        switch s {
        case .file(let url):
            let scoped = url.startAccessingSecurityScopedResource()
            defer { if scoped { url.stopAccessingSecurityScopedResource() } }
            staged = try await FileUploads.stage(url)
        case .data(let data, let name):
            let dir = FileManager.default.temporaryDirectory.appending(path: "uploads/" + UUID().uuidString)
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            staged = dir.appending(path: name)
            try data.write(to: staged)
        }
        defer { try? FileManager.default.removeItem(at: staged.deletingLastPathComponent()) }
        return try await api.upload(file: staged, name: s.name, machine: machine.id, dest: "terminal") { _ in }.path
    }
}
