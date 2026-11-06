import Foundation
import Observation

@MainActor @Observable final class PersonalAgentListModel {
    private(set) var page: SessionListModel<PersonalAgentSession>?
    private var configuration: ServerConfig?
    private var group: String?

    func connect(_ api: APIClient) {
        guard configuration != api.config || group != api.group else { return }
        page?.cancel()
        configuration = api.config
        group = api.group
        page = SessionListModel(id: { $0.id }, removalID: { $0 },
            fetch: { try await api.personalAgentListPage($0, cursor: $1, since: $2) },
            counts: { try await api.sessionListCounts($0, personal: true) })
        page?.changed = { PersonalAgentIndex.update($0) }
    }
}
