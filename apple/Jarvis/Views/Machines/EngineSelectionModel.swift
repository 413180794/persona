import Foundation
import Observation

/// One engine per machine. Editing stays local until the hub accepts the PUT.
@Observable final class EngineSelectionModel {
    struct Deps {
        var api: () -> APIClient?
        var updated: (Machine) -> Void
    }

    var selected: MachineEngine
    private(set) var saving = false
    private(set) var error: String?
    private var edited = false
    @ObservationIgnored private let deps: Deps

    init(machine: Machine, pairing: Bool, deps: Deps) {
        selected = Self.initialChoice(machine, pairing: pairing)
        self.deps = deps
    }

    static func pairingMachine(_ machines: [Machine]) -> Machine? {
        machines.first { $0.kind == "local" } ?? machines.first
    }

    static func initialChoice(_ machine: Machine, pairing: Bool) -> MachineEngine {
        let available = Set(machine.engines.filter(\.available).compactMap { MachineEngine(rawValue: $0.id) })
        return pairing && available.count == 1 ? available.first! : machine.engine
    }

    func choose(_ engine: MachineEngine) {
        selected = engine
        edited = true
        error = nil
    }

    /// Live detection and current-engine changes refresh the picker until the user starts editing it.
    func synchronize(_ machine: Machine, pairing: Bool) {
        guard !edited && !saving else { return }
        selected = Self.initialChoice(machine, pairing: pairing)
    }

    func save(machineID: String) async -> Bool {
        guard !saving else { return false }
        saving = true
        error = nil
        defer { saving = false }
        do {
            guard let api = deps.api() else { throw APIError(status: 0, message: "还没连接 hub") }
            let saved = try await api.setEngine(machine: machineID, engine: selected)
            deps.updated(saved)
            selected = saved.engine
            edited = false
            return true
        } catch {
            self.error = "保存失败：\(error.localizedDescription)"
            return false
        }
    }
}
