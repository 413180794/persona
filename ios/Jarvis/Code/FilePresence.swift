import Foundation
import SwiftUI

/// Where the paths of a conversation are looked up: its machine and working directory (relative paths are in it).
struct FileScope: Hashable, Sendable {
    var machine: String
    var cwd: String
}

/// Which paths mentioned in conversations are there on their machine (docs/ios.md「文件查看器」), so only those are
/// linked: a message may name another machine's file (the hub's ~/.jarvis/…), or one since deleted, and tapping it
/// only said it could not be opened. Asked in batches (GET /api/v1/machines/{mid}/stat) a moment after rows appear,
/// remembered per machine + directory + path. Until the answer comes, and when none can come (a node from before
/// the endpoint, no network), a path is plain text: better unlinked than a link that fails.
@MainActor @Observable
final class FilePresence {
    enum Kind: String, Decodable, Sendable { case file, dir, denied, missing }

    private struct Key: Hashable {
        var scope: FileScope
        var path: String
    }

    private struct Entry {
        var kind: Kind?
        var at: Date
    }

    private var known: [Key: Entry] = [:]
    @ObservationIgnored private var waiting: [FileScope: Set<String>] = [:]
    @ObservationIgnored private var flushing: Task<Void, Never>?

    /// How long an answer holds: a file that is there stays linked a while; a missing one may be written soon (the
    /// agent says it will), so it is asked again sooner; no answer at all, sooner still.
    static func lifetime(_ kind: Kind?) -> TimeInterval {
        switch kind {
        case .file, .dir, .denied: 30 * 60
        case .missing: 2 * 60
        case nil: 60
        }
    }

    /// What is known of a path; nil while unknown.
    func kind(_ path: String, in scope: FileScope) -> Kind? {
        known[Key(scope: scope, path: path)]?.kind
    }

    /// Whether a path is one to link: there as a file or a directory, or in a folder macOS keeps from jarvisd (opening
    /// it says what to do).
    func linkable(_ path: String, in scope: FileScope) -> Bool {
        switch kind(path, in: scope) {
        case .file, .dir, .denied: true
        default: false
        }
    }

    /// Asks about the paths not known (or known too long ago), together with whatever else rows ask for in the
    /// next moment.
    func ask(_ paths: [String], in scope: FileScope, api: APIClient?) {
        guard let api, !api.config.isDemo else { return }
        let now = Date.now
        let stale = paths.filter { p in
            guard let e = known[Key(scope: scope, path: p)] else { return true }
            return now.timeIntervalSince(e.at) > Self.lifetime(e.kind)
        }
        guard !stale.isEmpty else { return }
        waiting[scope, default: []].formUnion(stale)
        guard flushing == nil else { return }
        flushing = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(150))
            await self?.flush(api)
        }
    }

    private func flush(_ api: APIClient) async {
        let batches = waiting
        waiting = [:]
        flushing = nil
        for (scope, paths) in batches {
            let list = Array(paths)
            for start in stride(from: 0, to: list.count, by: 50) {
                let chunk = Array(list[start..<min(list.count, start + 50)])
                let answers = try? await api.stat(machine: scope.machine, paths: chunk, cwd: scope.cwd)
                let now = Date.now
                for p in chunk {
                    known[Key(scope: scope, path: p)] = Entry(kind: answers?[p] ?? (answers == nil ? nil : .missing), at: now)
                }
            }
        }
    }

    #if DEBUG
    /// Tests: what the cache holds for a path.
    func remembered(_ path: String, in scope: FileScope) -> (kind: Kind?, at: Date)? {
        known[Key(scope: scope, path: path)].map { ($0.kind, $0.at) }
    }
    #endif
}

nonisolated extension APIClient {
    /// GET /api/v1/machines/{mid}/stat: path → file / dir / denied / missing on that machine.
    func stat(machine: String, paths: [String], cwd: String) async throws -> [String: FilePresence.Kind] {
        struct Answer: Decodable {
            struct Stat: Decodable { var path: String; var kind: String }
            var stats: [Stat]
        }
        // One `paths` value, a path a line (the client's query is a dictionary; the hub also takes path=…&path=…).
        let data = try await request("GET", "/api/v1/machines/\(machine)/stat", query: ["paths": paths.joined(separator: "\n"), "cwd": cwd])
        let answer = try JSONDecoder().decode(Answer.self, from: data)
        var out: [String: FilePresence.Kind] = [:]
        for s in answer.stats { out[s.path] = FilePresence.Kind(rawValue: s.kind) ?? .missing }
        return out
    }
}

extension EnvironmentValues {
    /// The conversation's machine and directory, for linking the paths it mentions (set by ChatView).
    @Entry var fileScope: FileScope? = nil
}
