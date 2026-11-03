import AVFoundation
import CryptoKit
import PhotosUI
import SwiftUI
import UniformTypeIdentifiers

// Videos from 照片 (docs/protocol.md「发送视频」) go as files, never as pictures: read out of the library, compressed
// to H.264 MP4 only when they would not fit the hub's upload cap, and uploaded to the conversation's machine the way
// 文件 is. The composer shows each as a card until the message goes; the message carries it as a `<jarvis-file>` tag
// after what was typed: the agent reads the path, the app draws the card from the rest.

/// A file sent with a message: `<jarvis-file kind="video" path="…" name="…" size="…" duration="…"/>`.
nonisolated struct AttachedFile: Hashable, Sendable {
    var kind = "video"
    /// Where it is on the conversation's machine.
    var path: String
    /// What it was called on the phone (the machine's copy has a timestamp added).
    var name: String
    var size: Int64
    /// Seconds; nil when not known.
    var duration: Double?

    var xml: String {
        func esc(_ s: String) -> String {
            s.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "\"", with: "&quot;")
                .replacingOccurrences(of: "<", with: "&lt;").replacingOccurrences(of: ">", with: "&gt;")
        }
        let d = duration.map { String(format: " duration=\"%.1f\"", $0) } ?? ""
        return "<jarvis-file kind=\"\(esc(kind))\" path=\"\(esc(path))\" name=\"\(esc(name))\" size=\"\(size)\"\(d)/>"
    }

    /// "24.1 MB · 0:12".
    var detail: String {
        let size = ByteCountFormatter.string(fromByteCount: size, countStyle: .file)
        guard let duration else { return size }
        return size + " · " + RecordingFormat.clock(duration.rounded())
    }

    /// The message to send: what was typed, then a tag per file.
    static func message(_ text: String, files: [AttachedFile]) -> String {
        files.isEmpty ? text : text + "\n\n" + files.map(\.xml).joined(separator: "\n")
    }

    private static let tagRe = try! NSRegularExpression(pattern: #"\s*<jarvis-file\b([^>]*?)/?>(?:\s*</jarvis-file>)?\s*"#)
    private static let attrRe = try! NSRegularExpression(pattern: #"(\w+)="([^"]*)""#)
    private static let memo = TextMemo<(files: [AttachedFile], rest: String)>()

    /// The files in a message and the message without their tags. A tag without a path is dropped, not drawn.
    static func parse(_ text: String) -> (files: [AttachedFile], rest: String) {
        guard text.contains("<jarvis-file") else { return ([], text) }
        return memo(text, scan)
    }

    private static func scan(_ text: String) -> (files: [AttachedFile], rest: String) {
        let ns = text as NSString
        var files: [AttachedFile] = []
        for m in tagRe.matches(in: text, range: NSRange(location: 0, length: ns.length)) {
            let attrs = ns.substring(with: m.range(at: 1)) as NSString
            var a: [String: String] = [:]
            for am in attrRe.matches(in: attrs as String, range: NSRange(location: 0, length: attrs.length)) {
                a[attrs.substring(with: am.range(at: 1))] = attrs.substring(with: am.range(at: 2))
                    .replacingOccurrences(of: "&quot;", with: "\"").replacingOccurrences(of: "&lt;", with: "<")
                    .replacingOccurrences(of: "&gt;", with: ">").replacingOccurrences(of: "&amp;", with: "&")
            }
            guard let path = a["path"], !path.isEmpty else { continue }
            files.append(AttachedFile(kind: a["kind"] ?? "video", path: path, name: a["name"].flatMap { $0.isEmpty ? nil : $0 } ?? (path as NSString).lastPathComponent,
                                      size: Int64(a["size"] ?? "") ?? 0, duration: Double(a["duration"] ?? "")))
        }
        let rest = tagRe.stringByReplacingMatches(in: text, range: NSRange(location: 0, length: ns.length), withTemplate: "\n")
        return (files, rest.trimmingCharacters(in: .whitespacesAndNewlines))
    }
}

/// How a picked video is made to fit the upload (docs/protocol.md「发送视频」): as it is up to the hub's cap, else
/// H.264 MP4 at the largest of 1080p / 720p / 540p that fits; a video too long for even 540p is refused.
nonisolated enum VideoPrep {
    /// What the hub takes in one upload (`proto.MaxUploadBytes`).
    static let limit: Int64 = 200 << 20
    /// The H.264 MP4 sizes tried for a video over the limit, largest first.
    static let presets = [AVAssetExportPreset1920x1080, AVAssetExportPreset1280x720, AVAssetExportPreset960x540]
    /// How much of the limit an estimate may use: an export comes out a little off what was estimated.
    static let headroom = 0.9

    enum Failure: Error, Equatable {
        /// Even the smallest size is over the limit.
        case tooLong
    }

    /// The presets worth an export, largest first: those estimated to fit, and those with no estimate (0).
    static func candidates(_ estimates: [(preset: String, bytes: Int64)], limit: Int64 = limit) -> [String] {
        estimates.filter { $0.bytes <= 0 || Double($0.bytes) <= Double(limit) * headroom }.map(\.preset)
    }

    /// What the picker called the file, or 视频.<ext> for the names it makes up ("version=1&uuid=….mov").
    static func name(_ picked: String) -> String {
        let ext = (picked as NSString).pathExtension
        guard picked.contains("="), picked.contains("&") || picked.contains("uuid") else { return picked }
        return ext.isEmpty ? "视频" : "视频." + ext
    }

    static func size(_ url: URL) -> Int64 {
        Int64((try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0)
    }

    /// A file of the video that fits limit: url itself when it does, else an H.264 MP4 written next to it.
    /// progress gets how far the current export is (0…1).
    static func fit(_ url: URL, limit: Int64 = limit, progress: @escaping @Sendable (Double) -> Void) async throws -> URL {
        guard size(url) > limit else { return url }
        let asset = AVURLAsset(url: url)
        var estimates: [(preset: String, bytes: Int64)] = []
        for preset in presets {
            guard let session = AVAssetExportSession(asset: asset, presetName: preset) else { continue }
            session.outputFileType = .mp4
            estimates.append((preset, (try? await session.estimatedOutputFileLengthInBytes) ?? 0))
        }
        let dir = url.deletingLastPathComponent().appending(path: "compressed")
        let out = dir.appending(path: ((url.lastPathComponent as NSString).deletingPathExtension) + ".mp4")
        for preset in candidates(estimates, limit: limit) {
            try Task.checkCancellation()
            try? FileManager.default.removeItem(at: dir)
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try await export(asset, preset: preset, to: out, progress: progress)
            if size(out) <= limit { return out }
        }
        try? FileManager.default.removeItem(at: dir)
        throw Failure.tooLong
    }

    /// asset as H.264 MP4 at preset's size, its index up front (the machine may stream it).
    static func export(_ asset: AVAsset, preset: String, to out: URL, progress: @escaping @Sendable (Double) -> Void) async throws {
        guard let session = AVAssetExportSession(asset: asset, presetName: preset) else {
            throw APIError(status: 0, message: "这个视频没法压缩")
        }
        session.shouldOptimizeForNetworkUse = true
        let states = session.states(updateInterval: 0.25)
        let watch = Task {
            for await state in states {
                if case .exporting(let p) = state { progress(p.fractionCompleted) }
            }
        }
        defer { watch.cancel() }
        try await session.export(to: out, as: .mp4)
    }
}

/// A video picked in 照片, as a file (the picker's copy, which is gone after the import returns).
nonisolated struct PickedMovie: Transferable {
    let url: URL

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(importedContentType: .movie) { received in
            let dir = FileManager.default.temporaryDirectory.appending(path: "uploads/" + UUID().uuidString)
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            let dest = dir.appending(path: VideoPrep.name(received.file.lastPathComponent))
            try FileManager.default.copyItem(at: received.file, to: dest)
            return PickedMovie(url: dest)
        }
    }

    /// Whether a picked item is a video (else it is a picture).
    static func isVideo(_ item: PhotosPickerItem) -> Bool {
        item.supportedContentTypes.contains { $0.conforms(to: .movie) }
    }
}

/// A composer's videos: each read, fitted and uploaded to the conversation's machine one after another, then held as
/// a card until the message goes (`ready`). While one is on its way the message waits (`busy`).
@MainActor @Observable
final class VideoAttachments {
    static let most = 4

    struct Item: Identifiable {
        enum Step: Equatable { case waiting, reading, compressing(Double), uploading, ready }
        let id = UUID()
        var name: String
        var size: Int64 = 0
        var sent: Int64 = 0
        var duration: Double?
        var step = Step.waiting
        /// Set once it is on the machine.
        var file: AttachedFile?
    }

    private(set) var items: [Item] = []
    @ObservationIgnored private var tasks: [UUID: Task<Void, Never>] = [:]
    @ObservationIgnored private var last: Task<Void, Never>?

    var ready: [AttachedFile] { items.compactMap(\.file) }
    var busy: Bool { items.contains { $0.file == nil } }

    private enum Source { case pick(PhotosPickerItem), file(URL) }

    /// Videos picked in 照片, for machine.
    func start(_ picks: [PhotosPickerItem], machine: MachineRef, model: AppModel) {
        for p in picks { add(.pick(p), name: "视频", machine: machine, model: model) }
    }

    /// A video file of this app's (DEBUG screenshots and tests): moved in like a picked one.
    func start(file url: URL, machine: MachineRef, model: AppModel) {
        add(.file(url), name: url.lastPathComponent, machine: machine, model: model)
    }

    /// Cancels one on its way, or takes a ready one off the message.
    func remove(_ id: UUID) {
        tasks.removeValue(forKey: id)?.cancel()
        items.removeAll { $0.id == id }
    }

    /// The message went with the ready ones.
    func sent() { items.removeAll { $0.file != nil } }

    private func add(_ source: Source, name: String, machine: MachineRef, model: AppModel) {
        let features = model.features(machine.group)
        if !features.isEmpty, !features.contains("file_upload") {
            model.toast = Toast("hub 版本太旧，还不能发送视频：更新 hub 后再试", style: .error)
            return
        }
        guard items.count < Self.most else {
            model.toast = Toast("一次最多 \(Self.most) 个视频", style: .error)
            return
        }
        let item = Item(name: name)
        items.append(item)
        let prev = last
        let task = Task { [weak self] in
            await prev?.value
            guard let self, !Task.isCancelled else { return }
            await self.run(item.id, source: source, machine: machine, model: model)
        }
        tasks[item.id] = task
        last = task
    }

    private func run(_ id: UUID, source: Source, machine: MachineRef, model: AppModel) async {
        defer { tasks[id] = nil }
        var name = items.first { $0.id == id }?.name ?? "视频"
        var staged: URL?
        defer { if let staged { try? FileManager.default.removeItem(at: staged.deletingLastPathComponent()) } }
        do {
            update(id) { $0.step = .reading }
            let url: URL
            switch source {
            case .pick(let p):
                guard let movie = try await p.loadTransferable(type: PickedMovie.self) else { throw CocoaError(.fileReadUnknown) }
                url = movie.url
            case .file(let f):
                url = try await FileUploads.stage(f)
            }
            staged = url
            try Task.checkCancellation()
            name = url.lastPathComponent
            let duration = try? await AVURLAsset(url: url).load(.duration).seconds
            update(id) { $0.name = name; $0.size = VideoPrep.size(url); $0.duration = duration.flatMap { $0.isFinite ? $0 : nil } }
            let fitted = try await VideoPrep.fit(url) { p in
                Task { @MainActor in self.update(id) { if $0.step != .uploading, $0.step != .ready { $0.step = .compressing(p) } } } // a late one
            }
            try Task.checkCancellation()
            let size = VideoPrep.size(fitted)
            name = fitted.lastPathComponent
            update(id) { $0.name = name; $0.size = size; $0.step = .uploading }
            guard let api = model.api(machine.group) else { throw APIError(status: 0, message: "尚未连接 hub") }
            let out = try await api.upload(file: fitted, name: name, machine: machine.id) { sent in
                Task { @MainActor in self.update(id) { $0.sent = max($0.sent, sent) } }
            }
            try Task.checkCancellation()
            AttachedVideos.keep(fitted, machine, out.path) // plays at once from the sent message
            Haptics.tap()
            update(id) {
                $0.step = .ready
                $0.file = AttachedFile(path: out.path, name: name, size: out.size > 0 ? out.size : size, duration: $0.duration)
            }
        } catch {
            let duration = items.first { $0.id == id }?.duration
            items.removeAll { $0.id == id }
            if let msg = Self.message(error, name: name, duration: duration) { model.toast = Toast(msg, style: .error) }
        }
    }

    private func update(_ id: UUID, _ change: (inout Item) -> Void) {
        if let i = items.firstIndex(where: { $0.id == id }) { change(&items[i]) }
    }

    /// What to tell the user when a video did not make it; nil when they cancelled it.
    nonisolated static func message(_ error: Error, name: String, duration: Double?) -> String? {
        if (error as? VideoPrep.Failure) == .tooLong {
            let long = duration.map { "有 " + RecordingFormat.clock($0.rounded()) + "，" } ?? ""
            return "「\(name)」\(long)压缩到 540p 也超过 200 MB，发不了：先在「照片」里剪短再发"
        }
        if error.isCancellation { return nil }
        if let e = error as? CocoaError, e.code == .fileReadUnknown || e.code == .fileReadNoSuchFile {
            return "有视频读不出来（可能还在 iCloud 里没下载完）"
        }
        return FileUploads.message(error, name: name)
    }
}

/// The videos above the input box: on their way (reading, compressing, uploading — cancel), or ready to go (remove).
struct VideoAttachmentsBar: View {
    var videos: VideoAttachments

    var body: some View {
        VStack(spacing: Theme.Space.sm) {
            ForEach(videos.items) { item in
                AttachmentRow(symbol: Theme.Icon.video, name: item.name, status: Self.status(item), progress: Self.progress(item),
                              removeLabel: (item.file == nil ? "取消 " : "移除 ") + item.name) {
                    videos.remove(item.id)
                }
                .accessibilityElement(children: .contain)
                .accessibilityIdentifier(item.file == nil ? "video-attachment-busy" : "video-attachment-ready")
            }
        }
    }

    static func status(_ item: VideoAttachments.Item) -> String {
        switch item.step {
        case .waiting: "等待中"
        case .reading: "正在读取"
        case .compressing(let p): "正在压缩 \(Int(p * 100))%"
        case .uploading: "\(item.sent.storageText) / \(item.size.storageText)"
        case .ready: item.file?.detail ?? ""
        }
    }

    /// The bar's 0…1, nil once it is ready (no bar).
    static func progress(_ item: VideoAttachments.Item) -> Double? {
        switch item.step {
        case .waiting, .reading: 0
        case .compressing(let p): p
        case .uploading: item.size > 0 ? Double(min(item.sent, item.size)) / Double(item.size) : 0
        case .ready: nil
        }
    }
}

/// Videos this phone sent or played, kept so a card plays at once: Caches/attached-videos, one folder per machine and
/// path, the least recently played going past `budget`.
nonisolated enum AttachedVideos {
    static let budget: Int64 = 1 << 30
    static var folder: URL { URL.cachesDirectory.appending(path: "attached-videos") }

    static func key(_ machine: MachineRef, _ path: String) -> String {
        SHA256.hash(data: Data("\(machine.group)\n\(machine.id)\n\(path)".utf8)).prefix(16).map { String(format: "%02x", $0) }.joined()
    }

    /// The copy kept of machine's path, marked as just used.
    static func local(_ machine: MachineRef, _ path: String, in folder: URL = folder) -> URL? {
        let dir = folder.appending(path: key(machine, path))
        guard let name = (try? FileManager.default.contentsOfDirectory(atPath: dir.path))?.first(where: { !$0.hasPrefix(".") }) else { return nil }
        try? FileManager.default.setAttributes([.modificationDate: Date()], ofItemAtPath: dir.path)
        return dir.appending(path: name)
    }

    /// Moves file in as the copy of machine's path; returns where it is now.
    @discardableResult static func keep(_ file: URL, _ machine: MachineRef, _ path: String, in folder: URL = folder,
                                        budget: Int64 = budget) -> URL? {
        let dir = folder.appending(path: key(machine, path))
        let dest = dir.appending(path: file.lastPathComponent)
        do {
            try? FileManager.default.removeItem(at: dir)
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try FileManager.default.moveItem(at: file, to: dest)
        } catch {
            return nil
        }
        prune(folder, budget: budget, keeping: dir)
        return dest
    }

    /// Removes the least recently used copies until the rest fit budget; the one just kept stays whatever its size.
    static func prune(_ folder: URL, budget: Int64, keeping: URL) {
        let keys: [URLResourceKey] = [.contentModificationDateKey]
        let dirs = ((try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: keys)) ?? [])
            .map { ($0, (try? $0.resourceValues(forKeys: Set(keys)).contentModificationDate) ?? .distantPast) }
            .sorted { a, b in // the one just kept first, then the most recently used
                let ka = a.0.lastPathComponent == keeping.lastPathComponent, kb = b.0.lastPathComponent == keeping.lastPathComponent
                return ka != kb ? ka : a.1 > b.1
            }
        var total: Int64 = 0
        for (n, (dir, _)) in dirs.enumerated() {
            total += StorageUsage.size(of: dir)
            if total > budget, n > 0 { try? FileManager.default.removeItem(at: dir) }
        }
    }
}

extension View {
    /// A composer's 照片 picker: pictures and videos, four at most, in the library's own encoding (a video is
    /// compressed here, and only when it has to be: `VideoPrep`).
    func mediaPicker(isPresented: Binding<Bool>, selection: Binding<[PhotosPickerItem]>) -> some View {
        photosPicker(isPresented: isPresented, selection: selection, maxSelectionCount: 4, matching: .any(of: [.images, .videos]),
                     preferredItemEncoding: .current)
    }
}
