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
    @ObservationIgnored private let source: VideoFileSource?
    @ObservationIgnored private var resource: VideoFileResource?
    @ObservationIgnored private var pendingSource: VideoFileSource?
    private(set) var player: AVPlayer?
    /// A sparse cache, not a complete video suitable for local playback.
    private(set) var cacheURL: URL?
    /// The whole file once every byte is cached (the ⋯ menu shares it, `FileViewerPage.localFile`); a partly cached file
    /// has holes, so nothing until then.
    var localURL: URL? { total > 0 && received >= total ? cacheURL : nil }
    private(set) var error: String?
    private(set) var received: Int64 = 0
    private(set) var total: Int64 = 0
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var statusObserver: NSKeyValueObservation?

    init(ref: FileRef, file: FileContent, api: APIClient?, source: VideoFileSource? = nil) {
        self.ref = ref
        self.file = file
        self.api = api
        self.source = source
        total = file.size
    }

    func load() async {
        stop()
        error = nil
        received = 0
        guard let api else {
            error = String(localized: "Not connected to the hub [App Model 93d955]")
            return
        }
        let mine = generation
        let source = source ?? VideoFileSource(api: api, ref: ref, file: file)
        pendingSource = source
        var opened: VideoFileResource?
        var kept = false
        defer {
            if !kept {
                opened?.close()
                source.close()
                if generation == mine {
                    pendingSource = nil
                    resource = nil
                    cacheURL = nil
                }
            }
        }
        do {
            // Discover the real length and revision with two bytes, independent of the legacy preview.
            let info = try await source.read(0..<2, nil)
            try Task.checkCancellation()
            guard generation == mine else { return }
            let cache = try VideoFileCache(source: source, info: info, name: file.name) { [weak self] received, total in
                Task { @MainActor in
                    guard let self, self.generation == mine else { return }
                    self.received = received
                    self.total = total
                }
            }
            let resource = VideoFileResource(cache: cache, name: file.name) { [weak self] message in
                Task { @MainActor in
                    guard let self, self.generation == mine else { return }
                    self.error = message
                    self.player?.pause()
                    self.resource?.close()
                }
            }
            opened = resource
            self.resource = resource
            pendingSource = nil
            cacheURL = cache.url
            total = info.total
            guard try await resource.asset.load(.isPlayable) else {
                throw APIError(status: 0, message: String(localized: "This video's format or codec can't play on this device. Export it as an H.264 or HEVC MP4."))
            }
            try Task.checkCancellation()
            guard generation == mine else { return }
            let item = AVPlayerItem(asset: resource.asset)
            item.preferredForwardBufferDuration = 3
            item.canUseNetworkResourcesForLiveStreamingWhilePaused = false
            statusObserver = item.observe(\.status, options: [.new]) { @Sendable [weak self] item, _ in
                guard item.status == .failed else { return }
                let message = item.error?.localizedDescription ?? String(localized: "The video can't play. Check the file or export it again.")
                Task { @MainActor in
                    guard let self, self.generation == mine else { return }
                    if self.error == nil { self.error = message }
                    self.player?.pause()
                }
            }
            kept = true
            player = AVPlayer(playerItem: item)
            player?.play()
        } catch {
            guard generation == mine, !Task.isCancelled else { return }
            if self.error == nil { self.error = error.shown }
        }
    }

    func stop() {
        generation += 1
        statusObserver?.invalidate()
        statusObserver = nil
        player?.pause()
        player?.replaceCurrentItem(with: nil)
        player = nil
        pendingSource?.close()
        pendingSource = nil
        resource?.close()
        resource = nil
        cacheURL = nil
    }
}

struct VideoFileView: View {
    let model: VideoFileModel

    var body: some View {
        Group {
            if let error = model.error {
                EmptyStateView(symbol: Theme.Icon.error, title: String(localized: "Can't Play Video"), message: error)
            } else if let player = model.player {
                VideoPlayer(player: player)
                    .accessibilityIdentifier("file-video-player")
            } else {
                VStack(spacing: Theme.Space.m) {
                    ProgressView(value: Double(model.received), total: Double(max(model.total, 1)))
                    Text("Buffering Video…")
                        .font(Theme.Font.body)
                    Text(model.received.storageText + " / " + model.total.storageText)
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
