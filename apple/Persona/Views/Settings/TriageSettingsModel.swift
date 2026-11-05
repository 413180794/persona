import Foundation
import Observation

/// 设置 › 会话分诊 (docs/agent-work.md「开关」, the web's TriageSettings, Android's TriageSettingsPage): when an agent
/// stops, the hub can read its last reply, say whether it is done or what it waits for, and act on it. All off until
/// turned on here: the triage calls a model on every stopped turn, and 出错自动继续 / 到点叫醒 run a turn of the agent on
/// its own model. Saved on the hub (`PUT /api/v1/agents/triage`), so it is the same on every device.
@Observable final class TriageSettingsModel {
    struct Deps {
        var load: () async throws -> TriageSwitches
        var save: (TriageSwitches) async throws -> TriageSwitches
    }

    /// Nil until the hub answered (and on a hub without the triage).
    private(set) var switches: TriageSwitches?
    private(set) var saving = false
    /// Why the switches could not be read or the last change did not save.
    private(set) var error: String?
    /// The hub has no `/api/v1/agents/triage` (404): it is older than the triage.
    private(set) var unsupported = false
    @ObservationIgnored private let deps: Deps

    init(deps: Deps) { self.deps = deps }

    func load() async {
        do {
            switches = try await deps.load()
            error = nil
            unsupported = false
        } catch let e as APIError where e.status == 404 {
            unsupported = true
        } catch {
            if switches == nil { self.error = error.shown.map { String(localized: "没读到：\($0)") } }
        }
    }

    /// The triage itself can always be flipped; what it may do only while it is on.
    func canChange(_ key: WritableKeyPath<TriageSwitches, Bool>) -> Bool {
        guard let switches, !saving else { return false }
        return key == \TriageSwitches.enabled || switches.enabled
    }

    /// Shows the new value at once; a failed save puts the old one back and says why.
    func set(_ key: WritableKeyPath<TriageSwitches, Bool>, _ on: Bool) async {
        guard canChange(key), let before = switches, before[keyPath: key] != on else { return }
        var next = before
        next[keyPath: key] = on
        switches = next
        saving = true
        error = nil
        defer { saving = false }
        do {
            switches = try await deps.save(next)
        } catch {
            switches = before
            let why = error.shown ?? String(localized: "连不上 hub")
            self.error = String(localized: "没改成：\(why)")
        }
    }
}
