import Foundation

/// Where an agent works, from its working directory alone (the protocol carries no repository): the repository and the
/// working tree in it. The web's 按仓库 reads paths by the same rules (`web/src/lib/agent-repos.ts`): a path under one
/// of the usual working-tree folders belongs to the directory above that folder; any other path is its own
/// repository's main checkout.
nonisolated struct RepoPlace: Equatable, Sendable {
    /// The repository's path; "" without a working directory.
    var repo: String
    var name: String
    /// The working tree's path.
    var worktree: String
    /// The repository's main checkout, not a tree under it.
    var main: Bool

    /// Where herdr, Claude Code and a home-level collection keep working trees.
    static let markers = ["/.worktrees/", "/.claude/worktrees/", "/.herdr/worktrees/"]

    init(repo: String, name: String, worktree: String, main: Bool) {
        self.repo = repo
        self.name = name
        self.worktree = worktree
        self.main = main
    }

    init(cwd: String) {
        var path = Substring(cwd)
        while path.hasSuffix("/") { path = path.dropLast() }
        guard !path.isEmpty else {
            self.init(repo: "", name: "", worktree: "", main: true)
            return
        }
        for marker in Self.markers {
            guard let at = path.range(of: marker), at.lowerBound > path.startIndex else { continue }
            let rest = path[at.upperBound...].split(separator: "/" as Character)
            let repo = String(path[..<at.lowerBound])
            // ~/.worktrees/<repo>/<tree>: the folder sits at the home level, so the repository is the first part after
            // it and the tree is what follows.
            if rest.count >= 2 {
                self.init(repo: repo + marker + rest[0], name: String(rest[0]), worktree: String(path), main: false)
            } else {
                self.init(repo: repo, name: Self.baseName(repo), worktree: String(path), main: false)
            }
            return
        }
        self.init(repo: String(path), name: Self.baseName(String(path)), worktree: String(path), main: true)
    }

    private static func baseName(_ path: String) -> String {
        path.split(separator: "/" as Character).last.map(String.init) ?? ""
    }
}

/// 按仓库 (docs/ios.md「Agents 的分组」): every agent under its machine, the repository its working directory names and
/// the working tree in it, as Orca's projects with the machine above them. Plain data, unit-tested apart from the list
/// (`AgentRepoTreeTests`); the list orders the agents inside a tree (`AgentListOrder`).
nonisolated struct RepoTree {
    struct Worktree: Identifiable {
        /// Its fold id.
        var id: String
        var name: String
        var path: String
        var main: Bool
        var sessions: [Session]
    }

    /// One repository on one machine.
    struct Repo: Identifiable {
        /// Its fold id.
        var id: String
        var machine: MachineRef
        var name: String
        var path: String
        /// The main checkout first, then by name.
        var worktrees: [Worktree]
        var sessions: [Session] { worktrees.flatMap(\.sessions) }
        /// Only its main checkout: its agents sit right under it, with no working-tree level.
        var flat: Bool { worktrees.count == 1 && worktrees[0].main }
    }

    /// One machine's repositories, by name.
    struct Host: Identifiable {
        var ref: MachineRef
        var repos: [Repo]
        var id: String { ref.path }
        var sessions: [Session] { repos.flatMap(\.sessions) }
    }

    /// The name a repository goes by when its agents have no working directory.
    static let unnamed = "其他"

    /// `sessions` (the live agents the list shows) under their machines in `machines`' order (the hub's); a machine it
    /// does not list goes last.
    static func build(_ sessions: [Session], machines: [MachineRef]) -> [Host] {
        var repos: [String: Repo] = [:]
        for s in sessions {
            let place = RepoPlace(cwd: s.cwd)
            let key = s.machineRef.path + "/" + place.repo
            var repo = repos[key] ?? Repo(id: "repo/" + key, machine: s.machineRef,
                                           name: place.name.isEmpty ? unnamed : place.name, path: place.repo, worktrees: [])
            if let i = repo.worktrees.firstIndex(where: { $0.path == place.worktree }) {
                repo.worktrees[i].sessions.append(s)
            } else {
                let name = place.worktree == place.repo ? repo.name : (place.worktree.split(separator: "/" as Character).last.map(String.init) ?? "")
                repo.worktrees.append(Worktree(id: "tree/" + s.machineRef.path + "/" + place.worktree, name: name,
                                               path: place.worktree, main: place.main, sessions: [s]))
            }
            repos[key] = repo
        }
        var hosts: [MachineRef: [Repo]] = [:]
        for var repo in repos.values {
            repo.worktrees.sort { a, b in a.main != b.main ? a.main : before(a.name, a.path, b.name, b.path) }
            hosts[repo.machine, default: []].append(repo)
        }
        return hostOrder(Set(hosts.keys), machines: machines).map { ref in
            Host(ref: ref, repos: hosts[ref, default: []].sorted { before($0.name, $0.path, $1.name, $1.path) })
        }
    }

    /// `refs` in `machines`' order (the hub's); one it does not list goes last, by its path.
    static func hostOrder(_ refs: Set<MachineRef>, machines: [MachineRef]) -> [MachineRef] {
        let index = Dictionary(machines.enumerated().map { ($1, $0) }, uniquingKeysWith: { a, _ in a })
        return refs.sorted { a, b in
            let ia = index[a] ?? Int.max, ib = index[b] ?? Int.max
            return ia != ib ? ia < ib : a.path < b.path
        }
    }

    /// By name as Finder sorts them ("app2" before "app10"), then by path: one order on every render.
    private static func before(_ a: String, _ pathA: String, _ b: String, _ pathB: String) -> Bool {
        switch a.localizedStandardCompare(b) {
        case .orderedAscending: true
        case .orderedDescending: false
        case .orderedSame: pathA < pathB
        }
    }
}
