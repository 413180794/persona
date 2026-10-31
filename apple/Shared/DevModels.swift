import Foundation

// Read-only views of a session's project for the Git and Files tabs (hub → node `git.*` / `fs.list`).

nonisolated struct GitCommit: Codable, Identifiable, Hashable, Sendable {
    var hash: String
    var short: String
    var subject: String
    var body: String?
    var author: String
    var date: Date?
    var refs: String?
    var id: String { hash }

    /// Branch / tag names pointing here ("HEAD -> master, origin/master" → ["master", "origin/master"]).
    var refNames: [String] {
        (refs ?? "").split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
            .map { $0.hasPrefix("HEAD -> ") ? String($0.dropFirst(8)) : $0 }
            .filter { !$0.isEmpty && $0 != "HEAD" }
    }
}

nonisolated struct GitFile: Codable, Identifiable, Hashable, Sendable {
    var path: String
    var origPath: String?
    var index: String
    var worktree: String
    var added: Int
    var deleted: Int
    var binary: Bool?
    var id: String { path }

    var isUntracked: Bool { index == "?" }
    var isConflicted: Bool { index == "U" || worktree == "U" }
    var isStaged: Bool { !isUntracked && index != "." }
    var hasUnstaged: Bool { !isUntracked && worktree != "." }
    /// The one letter shown in the change list: staged change wins, then the working-tree change.
    var letter: String {
        if isUntracked { return "U" }
        if isConflicted { return "!" }
        return index != "." ? index : worktree
    }
    var name: String { (path as NSString).lastPathComponent }
    var folder: String { (path as NSString).deletingLastPathComponent }

    enum CodingKeys: String, CodingKey {
        case path, index, worktree, added, deleted, binary
        case origPath = "orig_path"
    }
}

nonisolated struct GitStatus: Codable, Hashable, Sendable {
    var isRepo: Bool
    var root: String?
    var branch: String?
    var head: GitCommit?
    var upstream: String?
    var ahead: Int
    var behind: Int
    var files: [GitFile]
    var stashes: Int
    var commits: [GitCommit]

    enum CodingKeys: String, CodingKey {
        case root, branch, head, upstream, ahead, behind, files, stashes, commits
        case isRepo = "is_repo"
    }
}

nonisolated struct GitDiff: Codable, Hashable, Sendable {
    var path: String
    var diff: String
    var truncated: Bool
}

nonisolated struct GitShow: Codable, Hashable, Sendable {
    var commit: GitCommit
    var files: [GitFile]
    var diff: String
    var truncated: Bool
}

nonisolated struct FileEntry: Codable, Identifiable, Hashable, Sendable {
    var name: String
    var dir: Bool
    var symlink: Bool?
    var size: Int64
    var mtime: Date?
    var id: String { name }
}

nonisolated struct FileListing: Codable, Hashable, Sendable {
    var path: String
    var entries: [FileEntry]
    var truncated: Bool
}

extension APIClient {
    private func mp(_ machine: String) -> String { "/api/v1/machines/\(machine)" }

    func gitStatus(machine: String, cwd: String) async throws -> GitStatus {
        try await get(mp(machine) + "/git/status", query: ["cwd": cwd])
    }
    func gitDiff(machine: String, cwd: String, path: String, staged: Bool) async throws -> GitDiff {
        try await get(mp(machine) + "/git/diff", query: ["cwd": cwd, "path": path, "staged": staged ? "1" : "0"])
    }
    func gitShow(machine: String, cwd: String, hash: String) async throws -> GitShow {
        try await get(mp(machine) + "/git/show", query: ["cwd": cwd, "hash": hash])
    }
    private struct CommitsResponse: Decodable { var commits: [GitCommit] }
    func gitLog(machine: String, cwd: String, skip: Int, limit: Int = 50) async throws -> [GitCommit] {
        try await get(mp(machine) + "/git/log", query: ["cwd": cwd, "skip": String(skip), "limit": String(limit)],
                      as: CommitsResponse.self).commits
    }
    func listDir(machine: String, path: String, cwd: String) async throws -> FileListing {
        try await get(mp(machine) + "/fs", query: ["path": path, "cwd": cwd])
    }
}
