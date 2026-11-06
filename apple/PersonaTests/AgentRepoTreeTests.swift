import Foundation
import Testing
@testable import Jarvis

/// By Repository (`RepoPlace`, `RepoTree`): the repository and working tree a working directory names, and the machines →
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

    // MARK: Spaces nested under another (Orca's child worktrees, #3075)

    private func orca(_ id: String, _ cwd: String, space: String, parent: String? = nil, index: Int = 0,
                      kind: String = "orca", machine: String = "machine-a") -> Session {
        let parentField = parent.map { #","parent_space_id":"\#($0)""# } ?? ""
        let json = """
        {"machine_id":"\(machine)","session_id":"\(id)","title":"\(id)","cwd":"\(cwd)","status":"idle","state":"idle",
         "space_kind":"\(kind)","space_id":"\(space)","space_name":"\(space)","space_index":\(index)\(parentField)}
        """
        do { return try JSON.decoder.decode(Session.self, from: Data(json.utf8)) }
        catch { fatalError("bad fixture: \(error)") }
    }

    @Test func childSpacesGoUnderTheirParentsWorkingTreeDepthFirst() throws {
        // Orca keeps worktrees outside the repository: by the path alone each would be a repository of its own.
        let tree = RepoTree.build([
            orca("p", "/code/app", space: "w-main", index: 1),
            orca("c2", "/orca/app/metrics", space: "w-metrics", parent: "w-main", index: 5),
            orca("c1", "/orca/app/refresh", space: "w-refresh", parent: "w-main", index: 3),
            orca("g1", "/orca/app/refresh-docs", space: "w-docs", parent: "w-refresh", index: 4),
        ], machines: machines)
        let repos = try #require(tree.first?.repos)
        #expect(repos.map(\.name) == ["app"], "the children are not repositories of their own")
        let repo = repos[0]
        #expect(!repo.flat, "a main checkout with children shows its working-tree level")
        #expect(repo.worktrees.count == 1)
        let main = repo.worktrees[0]
        #expect(main.sessions.map(\.sessionID) == ["p"])
        #expect(main.children.map(\.name) == ["w-refresh", "w-docs", "w-metrics"], "depth first, by the host's order")
        #expect(main.children.map(\.id) == ["tree/space/machine-a/orca:w-refresh", "tree/space/machine-a/orca:w-docs",
                                            "tree/space/machine-a/orca:w-metrics"])
        #expect(main.childrenFoldID == "tree/machine-a//code/app" + RepoTree.childFoldSuffix)
        #expect(repo.sessions.map(\.sessionID).sorted() == ["c1", "c2", "g1", "p"])
        #expect(main.treeSessions.count == 4)
    }

    @Test func aChildWhoseParentIsNotListedStaysAtTheTop() {
        let tree = RepoTree.build([orca("c", "/orca/app/login", space: "w-login", parent: "w-gone")], machines: machines)
        #expect(tree[0].repos.map(\.name) == ["login"])
        #expect(tree[0].repos[0].worktrees[0].children.isEmpty)
    }

    @Test func loopsSelfParentsAndOtherHostsDoNotNest() {
        let tree = RepoTree.build([
            orca("a", "/code/a", space: "w2", parent: "w3"),
            orca("b", "/code/b", space: "w3", parent: "w2"),
            orca("self", "/code/self", space: "w6", parent: "w6"),
            orca("h", "/code/h", space: "w4", parent: "w5", kind: "herdr"),
            orca("o", "/code/o", space: "w5"),
            orca("m", "/code/m", space: "w7", parent: "w5", machine: "machine-b"),
        ], machines: machines)
        #expect(tree.flatMap(\.repos).flatMap(\.worktrees).allSatisfy { $0.children.isEmpty })
        #expect(tree.flatMap(\.repos).count == 6)
        #expect(RepoTree.parents(tree.flatMap(\.repos).flatMap(\.sessions)).isEmpty)
    }

    @Test func parentSpaceIdIsReadAndOptional() throws {
        #expect(orca("c", "/x", space: "w1", parent: "w0").parentSpaceID == "w0")
        #expect(orca("p", "/x", space: "w0").parentSpaceID == nil)
    }

    /// An app that never chose a grouping shows By Repository (#2108), on a hub with todos or without.
    @Test @MainActor func byRepositoryIsTheDefault() {
        #expect(DevGrouping.standard == .repo)
        #expect(DevGrouping.resolved(.standard, supportsTodos: false) == .repo)
        #expect(DevGrouping.offered(supportsTodos: false).first == .repo)
    }
}
