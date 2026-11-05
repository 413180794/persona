import Foundation
import Testing
@testable import Jarvis

/// 按仓库 (`RepoPlace`, `RepoTree`): the repository and working tree a working directory names, and the machines →
/// repositories → working trees the Agents list draws. The path rules are the web's (`web/src/lib/agent-repos.test.ts`).
struct AgentRepoTreeTests {
    private func session(_ id: String, _ cwd: String, machine: String = "machine-a", group: String = "") -> Session {
        let hub = group.isEmpty ? "" : #","app_hub_group":"\#(group)""#
        let json = """
        {"machine_id":"\(machine)","session_id":"\(id)","title":"\(id)","cwd":"\(cwd)","status":"idle","state":"idle"\(hub)}
        """
        do { return try JSON.decoder.decode(Session.self, from: Data(json.utf8)) }
        catch { fatalError("bad fixture: \(error)") }
    }

    private let machines = [MachineRef(id: "machine-b"), MachineRef(id: "machine-a")]

    // MARK: Where a working directory sits

    @Test func aMainCheckoutIsTheRepositoryItself() {
        #expect(RepoPlace(cwd: "/code/app") == RepoPlace(repo: "/code/app", name: "app", worktree: "/code/app", main: true))
        #expect(RepoPlace(cwd: "/code/app/") == RepoPlace(cwd: "/code/app"), "a trailing slash is the same directory")
    }

    @Test func theUsualWorkingTreeFoldersBelongToTheRepositoryAboveThem() {
        for cwd in ["/code/app/.worktrees/fix-login", "/code/app/.claude/worktrees/fix-login", "/code/app/.herdr/worktrees/fix-login"] {
            #expect(RepoPlace(cwd: cwd) == RepoPlace(repo: "/code/app", name: "app", worktree: cwd, main: false), "\(cwd)")
        }
    }

    @Test func aHomeLevelCollectionNamesTheRepositoryAfterTheFolder() {
        #expect(RepoPlace(cwd: "/home/me/.worktrees/persona/fix-login")
            == RepoPlace(repo: "/home/me/.worktrees/persona", name: "persona", worktree: "/home/me/.worktrees/persona/fix-login", main: false))
    }

    @Test func withoutAWorkingTreeFolderTheDirectoryIsItsOwnRepository() {
        #expect(RepoPlace(cwd: "/code/app/server/internal")
            == RepoPlace(repo: "/code/app/server/internal", name: "internal", worktree: "/code/app/server/internal", main: true))
        #expect(RepoPlace(cwd: "") == RepoPlace(repo: "", name: "", worktree: "", main: true))
    }

    // MARK: The tree

    @Test func theMainCheckoutComesFirstThenTreesByName() throws {
        let tree = RepoTree.build([session("main", "/code/app"), session("wt-b", "/code/app/.worktrees/fix-b"),
                                   session("wt-a", "/code/app/.worktrees/fix-a"), session("wt-a2", "/code/app/.worktrees/fix-a")],
                                  machines: machines)
        let repo = try #require(tree.first?.repos.first)
        #expect(tree.count == 1 && tree[0].repos.count == 1)
        #expect(repo.name == "app" && !repo.flat)
        #expect(repo.worktrees.map { $0.name + ($0.main ? "*" : "") } == ["app*", "fix-a", "fix-b"])
        #expect(repo.worktrees[1].sessions.map(\.sessionID) == ["wt-a", "wt-a2"])
        #expect(repo.sessions.count == 4)
    }

    @Test func aRepositoryWithOnlyItsMainCheckoutIsFlat() throws {
        let repo = try #require(RepoTree.build([session("a", "/code/app"), session("b", "/code/app")], machines: machines).first?.repos.first)
        #expect(repo.flat)
        #expect(repo.worktrees[0].sessions.map(\.sessionID) == ["a", "b"])
    }

    @Test func repositoriesGoByNameAsFinderSortsThem() {
        let tree = RepoTree.build([session("z", "/code/zeta"), session("10", "/code/app10"), session("2", "/code/app2"),
                                   session("a", "/code/Alpha")], machines: machines)
        #expect(tree[0].repos.map(\.name) == ["Alpha", "app2", "app10", "zeta"])
    }

    @Test func machinesGoInTheHubsOrderAndOneItDoesNotListGoesLast() {
        let tree = RepoTree.build([session("a", "/code/app"), session("x", "/code/app", machine: "machine-x"),
                                   session("b", "/code/app", machine: "machine-b")], machines: machines)
        #expect(tree.map(\.id) == ["machine-b", "machine-a", "machine-x"])
    }

    @Test func sameNamedRepositoriesOnTwoMachinesStayApart() {
        let tree = RepoTree.build([session("a", "/code/app"), session("b", "/code/app", machine: "machine-b")], machines: machines)
        #expect(tree.map { $0.id + ":" + $0.repos.map(\.name).joined() } == ["machine-b:app", "machine-a:app"])
        #expect(Set(tree.flatMap(\.repos).map(\.id)).count == 2, "their fold ids differ")
    }

    @Test func theSameMachineIdInAnotherGroupOfHubsIsAnotherMachine() {
        let tree = RepoTree.build([session("a", "/code/app"), session("b", "/code/app", group: "work")], machines: machines)
        #expect(tree.map(\.id) == ["machine-a", "work|machine-a"])
    }

    @Test func anAgentWithoutAWorkingDirectoryGoesUnderOther() {
        let tree = RepoTree.build([session("a", "")], machines: machines)
        #expect(tree[0].repos.map(\.name) == [RepoTree.unnamed])
    }

    @Test func foldIdsAreStableAndApart() {
        let tree = RepoTree.build([session("a", "/code/app"), session("b", "/code/app/.worktrees/fix")], machines: machines)
        let repo = tree[0].repos[0]
        #expect(repo.id == "repo/machine-a//code/app")
        #expect(repo.worktrees.map(\.id) == ["tree/machine-a//code/app", "tree/machine-a//code/app/.worktrees/fix"])
    }

    /// An app that never chose a grouping shows 按仓库 (#2108), on a hub with todos or without.
    @Test @MainActor func byRepositoryIsTheDefault() {
        #expect(DevGrouping.standard == .repo)
        #expect(DevGrouping.resolved(.standard, supportsTodos: false) == .repo)
        #expect(DevGrouping.offered(supportsTodos: false).first == .repo)
    }
}
