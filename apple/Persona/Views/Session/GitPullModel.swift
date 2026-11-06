import Foundation
import Observation

@MainActor @Observable
final class GitPullModel {
    struct Deps {
        var pull: (String, String) async throws -> GitPullResult

        init(api: APIClient) {
            pull = { try await api.gitPull(machine: $0, cwd: $1) }
        }

        init(pull: @escaping (String, String) async throws -> GitPullResult) {
            self.pull = pull
        }
    }

    private(set) var isPulling = false
    private(set) var message: String?
    private(set) var failed = false

    func pull(machine: String, cwd: String, deps: Deps) async -> GitStatus? {
        guard !isPulling else { return nil }
        isPulling = true
        message = nil
        failed = false
        defer { isPulling = false }
        do {
            let result = try await deps.pull(machine, cwd)
            message = result.updated ? String(localized: "Pulled the latest code") : String(localized: "Already up to date")
            return result.status
        } catch {
            failed = true
            message = error.shown
            return nil
        }
    }
}
