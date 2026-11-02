import Foundation

/// The app's last known state on disk (Caches/state/<name>.json): every page paints from it at once — offline, on a
/// cold start, when switching tabs — and the network only refreshes it in the background.
nonisolated enum StateCache {
    private static func url(_ name: String) -> URL? {
        guard let base = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first else { return nil }
        let safe = String(name.map { $0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" ? $0 : "_" })
        return base.appending(path: "state").appending(path: safe + ".json")
    }

    static func load<T: Decodable>(_ name: String, as type: T.Type = T.self) -> T? {
        guard let url = url(name), let data = try? Data(contentsOf: url) else { return nil }
        return try? JSON.decoder.decode(T.self, from: data)
    }

    static func remove(_ name: String) {
        Task.detached(priority: .utility) {
            guard let url = url(name) else { return }
            try? FileManager.default.removeItem(at: url)
        }
    }

    static func save<T: Encodable & Sendable>(_ name: String, _ value: T) {

        Task.detached(priority: .utility) {
            guard let url = url(name), let data = try? JSON.encoder.encode(value) else { return }
            try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try? data.write(to: url, options: .atomic)
        }
    }
}

/// Coalesces bursts of changes into one write a moment later.
@MainActor
final class Debouncer {
    private var task: Task<Void, Never>?
    private let delay: Duration
    init(_ delay: Duration = .seconds(1)) { self.delay = delay }

    func run(_ work: @escaping @MainActor () -> Void) {
        task?.cancel()
        task = Task { [delay] in
            try? await Task.sleep(for: delay)
            guard !Task.isCancelled else { return }
            work()
        }
    }
}
