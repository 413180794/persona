import Foundation
import Testing
@testable import Persona

/// The Files / Git panes' live refresh (#3089, `FsLive`): what the hub is told and when a pane refreshes.
@MainActor
struct FsLiveTests {
    /// A fake socket and a clock that only moves when the test says so.
    @MainActor final class Rig {
        var sent: [(group: String, msg: [String: Any])] = []
        var open: Set<String> = [""]
        var timers: [(id: Int, action: @MainActor () -> Void)] = []
        var nextTimer = 0
        lazy var live = FsLive(send: { [unowned self] g, m in sent.append((g, m)) }, isOpen: { [unowned self] g in open.contains(g) },
                               schedule: { [unowned self] _, action in
                                   nextTimer += 1
                                   let id = nextTimer
                                   timers.append((id, action))
                                   return { [unowned self] in timers.removeAll { $0.id == id } }
                               })

        /// The debounce runs out.
        func fire() {
            let due = timers
            timers = []
            due.forEach { $0.action() }
        }

        func last() -> [String: Any]? { sent.last?.msg }
    }

    private let scope = FsLive.Scope(group: "", machineID: "mac", cwd: "/w")

    @Test func mergesPanesOfOneFolderIntoOneMessage() {
        let rig = Rig()
        let files = rig.live.watch(scope, paths: ["/w/src"], git: false) { _ in }
        #expect(rig.sent.count == 1)
        #expect(rig.last()?["type"] as? String == "fs_watch")
        #expect(rig.last()?["paths"] as? [String] == ["/w/src"])
        #expect(rig.last()?["git"] as? Bool == false)

        let git = rig.live.watch(scope, paths: [], git: true) { _ in }
        #expect(rig.sent.count == 2)
        #expect(rig.last()?["paths"] as? [String] == ["/w/src"])
        #expect(rig.last()?["git"] as? Bool == true)

        // The same set again is not sent again.
        files.update(paths: ["/w/src"], git: false)
        #expect(rig.sent.count == 2)

        files.update(paths: ["/w"], git: false)
        #expect(rig.last()?["paths"] as? [String] == ["/w"])

        git.stop()
        #expect(rig.last()?["git"] as? Bool == false)
        files.stop()
        #expect(rig.last()?["paths"] as? [String] == [])
        #expect(rig.last()?["git"] as? Bool == false)
        #expect(rig.sent.count == 5)
    }

    @Test func debouncesChangesIntoOneRefresh() {
        let rig = Rig()
        var changes: [FsChange] = []
        _ = rig.live.watch(scope, paths: ["/w", "/w/src"], git: true) { changes.append($0) }
        rig.live.received(group: "", machineID: "mac", cwd: "/w", dirs: ["/w/src"], git: false)
        rig.live.received(group: "", machineID: "mac", cwd: "/w", dirs: ["/w", "/w/other"], git: true)
        #expect(changes.isEmpty)
        #expect(rig.timers.count == 1) // the second one restarted the timer
        rig.fire()
        #expect(changes == [FsChange(dirs: ["/w", "/w/src"], git: true)])
    }

    @Test func onlyTellsThePanesItConcerns() {
        let rig = Rig()
        var files: [FsChange] = [], git: [FsChange] = []
        _ = rig.live.watch(scope, paths: ["/w"], git: false) { files.append($0) }
        _ = rig.live.watch(scope, paths: [], git: true) { git.append($0) }
        rig.live.received(group: "", machineID: "mac", cwd: "/w", dirs: [], git: true)
        rig.live.received(group: "", machineID: "other", cwd: "/w", dirs: ["/w"], git: true)
        rig.live.received(group: "g2", machineID: "mac", cwd: "/w", dirs: ["/w"], git: true)
        rig.fire()
        #expect(files.isEmpty)
        #expect(git == [FsChange(dirs: [], git: true)])
    }

    @Test func aStoppedPaneIsNotRefreshed() {
        let rig = Rig()
        var changes: [FsChange] = []
        let h = rig.live.watch(scope, paths: ["/w"], git: false) { changes.append($0) }
        rig.live.received(group: "", machineID: "mac", cwd: "/w", dirs: ["/w"], git: false)
        h.stop()
        #expect(rig.timers.isEmpty)
        rig.fire()
        #expect(changes.isEmpty)
    }

    @Test func reconnectTellsTheHubAgainAndRefreshesEverything() {
        let rig = Rig()
        rig.open = [] // offline: nothing is sent, the hello will
        var changes: [FsChange] = []
        _ = rig.live.watch(scope, paths: ["/w"], git: true) { changes.append($0) }
        #expect(rig.sent.isEmpty)
        rig.open = [""]
        rig.live.reconnected(group: "")
        #expect(rig.sent.count == 1)
        #expect(rig.last()?["machine_id"] as? String == "mac")
        #expect(rig.last()?["cwd"] as? String == "/w")
        rig.fire()
        #expect(changes == [FsChange(dirs: ["/w"], git: true)])
        // Another reconnect is a new socket: told again.
        rig.live.reconnected(group: "")
        #expect(rig.sent.count == 2)
    }

    @Test func decodesFsChanged() {
        let data = Data(#"{"type":"fs_changed","machine_id":"mac","cwd":"/w","dirs":["/w/src"],"git":true}"#.utf8)
        guard case .fsChanged(let machine, let cwd, let dirs, let git)? = ServerEvent.decode(data) else {
            Issue.record("not decoded")
            return
        }
        #expect(machine == "mac")
        #expect(cwd == "/w")
        #expect(dirs == ["/w/src"])
        #expect(git)
        #expect(ServerEvent.decode(Data(#"{"type":"fs_changed","machine_id":"mac"}"#.utf8)) == nil)
    }
}
