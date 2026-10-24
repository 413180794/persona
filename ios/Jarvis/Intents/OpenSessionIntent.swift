import AppIntents
import Foundation

/// A session as Siri sees it, so `AskClaudeIntent` can hand the phone straight to that chat.
struct SessionEntity: AppEntity, Hashable, Sendable {
    nonisolated static let typeDisplayRepresentation = TypeDisplayRepresentation(name: "会话")
    nonisolated static let defaultQuery = SessionEntityQuery()

    /// `machine/session`: the same shape as the `-openSession` debug switch.
    var id: String
    var machineID: String
    var sessionID: String
    var title: String

    nonisolated init(machineID: String, sessionID: String, title: String) {
        self.id = "\(machineID)/\(sessionID)"
        self.machineID = machineID
        self.sessionID = sessionID
        self.title = title
    }

    nonisolated init(_ session: Session) {
        self.init(machineID: session.machineID, sessionID: session.sessionID, title: session.title)
    }

    var key: SessionKey { SessionKey(machineID: machineID, sessionID: sessionID) }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(title.isEmpty ? sessionID : title)", subtitle: "\(machineID)")
    }
}

struct SessionEntityQuery: EntityQuery {
    nonisolated init() {}

    nonisolated func entities(for identifiers: [String]) async throws -> [SessionEntity] {
        let all = try await Self.all()
        return identifiers.map { id in
            all.first { $0.id == id } ?? SessionEntity(machineID: id, sessionID: "", title: id)
        }
    }

    nonisolated func suggestedEntities() async throws -> [SessionEntity] {
        try await Self.all().filter { $0.sessionID != "" }
    }

    private static func all() async throws -> [SessionEntity] {
        do {
            return try await IntentSupport.client().sessions().map(SessionEntity.init)
        } catch {
            throw IntentFailureSentence(IntentFailure.sentence(for: error, host: IntentSupport.host()))
        }
    }
}

/// Opens the app on one chat. The app is in the foreground here, so touching `AppModel` is fine.
struct OpenSessionIntent: OpenIntent {
    nonisolated static let title: LocalizedStringResource = "打开会话"
    nonisolated static var supportedModes: IntentModes { .foreground(.immediate) }

    @Parameter(title: "会话")
    var target: SessionEntity

    nonisolated init() {}

    nonisolated init(target: SessionEntity) { self.target = target }

    nonisolated func perform() async throws -> some IntentResult {
        let key = target.key
        await MainActor.run {
            AppModel.shared.selectedTab = .chats
            AppModel.shared.openChat = key
        }
        return .result()
    }
}
