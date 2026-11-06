import AppIntents
import Foundation

/// A machine as Siri sees it: id and name come from `/api/v1/machines`. The id is `MachineRef.path` (the machine id,
/// "group|id" for another group of hubs); the name says the group when there are several.
struct MachineEntity: AppEntity, Hashable, Sendable {
    nonisolated static let typeDisplayRepresentation = TypeDisplayRepresentation(name: "Machines")
    nonisolated static let defaultQuery = MachineEntityQuery()

    var id: String
    var name: String
    var online: Bool

    nonisolated init(id: String, name: String, online: Bool = true) {
        self.id = id
        self.name = name
        self.online = online
    }

    nonisolated init(_ machine: Machine) {
        self.init(id: machine.ref.path, name: machine.title, online: machine.online)
    }

    /// The machine id at its hub, and its group.
    var ref: MachineRef { MachineRef(path: id) }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(name)", subtitle: online ? "Online" : "Offline [Machine Entity be1b4f]")
    }
}

/// Resolves machines by asking the hub, so Shortcuts offers the same list the app shows.
struct MachineEntityQuery: EntityQuery, EntityStringQuery {
    nonisolated init() {}

    nonisolated func entities(for identifiers: [String]) async throws -> [MachineEntity] {
        let all = try await Self.all()
        // Keep unresolved ids working (they were picked earlier and may be offline now).
        return identifiers.map { id in
            all.first { $0.id == id } ?? MachineEntity(id: id, name: id, online: false)
        }
    }

    nonisolated func suggestedEntities() async throws -> [MachineEntity] {
        // Online machines first: that is what people pick in Siri.
        let all = try await Self.all()
        return all.filter(\.online) + all.filter { !$0.online }
    }

    nonisolated func entities(matching string: String) async throws -> [MachineEntity] {
        let needle = string.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !needle.isEmpty else { return try await suggestedEntities() }
        return try await Self.all().filter {
            $0.name.localizedCaseInsensitiveContains(needle) || $0.id.localizedCaseInsensitiveContains(needle)
        }
    }

    private static func all() async throws -> [MachineEntity] {
        do {
            return try await IntentSupport.machines().map(MachineEntity.init)
        } catch {
            throw IntentFailureSentence(IntentFailure.sentence(for: error, host: IntentSupport.host()))
        }
    }
}

/// Lets a query rethrow a ready-made sentence: Siri shows `errorDescription` as-is.
nonisolated struct IntentFailureSentence: LocalizedError, Sendable {
    var message: String
    nonisolated init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}
