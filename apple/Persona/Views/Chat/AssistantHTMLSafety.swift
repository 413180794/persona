import Foundation
import Observation

/// Runtime support is not a security verdict. Each platform is enabled only after its real acceptance passes.
nonisolated enum AssistantHTMLSafety {
    #if os(iOS)
    // Real WKWebView network/navigation probes and bounded literal-loop recovery passed in #2778.
    static let executionVerified = true
    #else
    static let executionVerified = false
    #endif
}

/// At most two visible card pages execute at once. This is a concurrency bound, not a CPU or memory quota.
@MainActor @Observable
final class HTMLExecutionBudget {
    static let shared = HTMLExecutionBudget()
    static let maximum = 2
    private(set) var owners: Set<UUID> = []
    func acquire(_ owner: UUID) -> Bool {
        if owners.contains(owner) { return true }
        guard owners.count < Self.maximum else { return false }
        owners.insert(owner)
        return true
    }
    func release(_ owner: UUID) { owners.remove(owner) }
}
