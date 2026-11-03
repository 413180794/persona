import AVKit
import SwiftUI

nonisolated enum VideoFileType {
    static let extensions: Set<String> = ["mp4", "mov", "m4v", "mpeg", "mpg", "webm", "mkv", "avi"]

    static func matches(name: String, mime: String?) -> Bool {
        (mime ?? "").lowercased().hasPrefix("video/") || extensions.contains((name as NSString).pathExtension.lowercased())
    }
}

@MainActor @Observable
final class VideoFileModel {
    let ref: FileRef
    let file: FileContent
    @ObservationIgnored private let api: APIClient?
    private(set) var player: AVPlayer?
    private(set) var localURL: URL?
    private(set) var error: String?
    private(set) var received: Int64 = 0
    private(set) var total: Int64 = 0
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var statusObserver: NSKeyValueObservation?

    init(ref: FileRef, file: FileContent, api: APIClient?) {
        self.ref = ref
        self.file = file
        self.api = api
        total = file.size
    }

    func load() async {
        stop()
        error = nil
        received = 0
        guard let api else {
            error = "尚未连接 hub"
            return
        }
        let mine = generation
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("jarvis-video-" + UUID().uuidString, isDirectory: true)
        let url = directory.appendingPathComponent((file.name as NSString).lastPathComponent)
        var kept = false
        defer { if !kept { try? FileManager.default.removeItem(at: directory) } }
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            try await api.download("/api/v1/machines/\(ref.machineID)/file/download",
                                   query: ["path": file.path, "cwd": ref.cwd ?? ""], to: url) { [weak self] received, total in
                Task { @MainActor in
                    guard let self, self.generation == mine else { return }
                    self.received = received
                    if total > 0 { self.total = total }
                }
            }
            try Task.checkCancellation()
            guard generation == mine else { return }
            let asset = AVURLAsset(url: url)
            guard try await asset.load(.isPlayable) else {
                throw APIError(status: 0, message: "这个视频的格式或编码无法在当前设备播放，请导出为 H.264 或 HEVC 的 MP4 视频")
            }
            try Task.checkCancellation()
            guard generation == mine else { return }
            let item = AVPlayerItem(asset: asset)
            statusObserver = item.observe(\.status, options: [.new]) { @Sendable [weak self] item, _ in
                guard item.status == .failed else { return }
                let message = item.error?.localizedDescription ?? "视频无法播放，请检查文件或重新导出"
                Task { @MainActor in
                    guard let self, self.generation == mine else { return }
                    self.error = message
                    self.player?.pause()
                }
            }
            localURL = url
            kept = true
            player = AVPlayer(playerItem: item)
            player?.play()
        } catch {
            guard generation == mine, !Task.isCancelled else { return }
            self.error = error.shown
        }
    }

    func stop() {
        generation += 1
        statusObserver?.invalidate()
        statusObserver = nil
        player?.pause()
        player?.replaceCurrentItem(with: nil)
        player = nil
        if let localURL { try? FileManager.default.removeItem(at: localURL.deletingLastPathComponent()) }
        localURL = nil
    }
}

struct VideoFileView: View {
    let model: VideoFileModel

    var body: some View {
        Group {
            if let error = model.error {
                EmptyStateView(symbol: Theme.Icon.error, title: "视频无法播放", message: error)
            } else if let player = model.player {
                VideoPlayer(player: player)
                    .accessibilityIdentifier("file-video-player")
            } else {
                VStack(spacing: Theme.Space.m) {
                    ProgressView(value: Double(model.received), total: Double(max(model.total, 1)))
                    Text("正在下载视频…")
                        .font(Theme.Font.body)
                    Text(ByteCountFormatter.string(fromByteCount: model.received, countStyle: .file) + " / " +
                         ByteCountFormatter.string(fromByteCount: model.total, countStyle: .file))
                        .font(Theme.Font.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(Theme.Space.xl)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .task(id: ObjectIdentifier(model)) { await model.load() }
        .onDisappear { model.stop() }
    }
}
