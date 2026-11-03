import Foundation

nonisolated enum AgentCloseRules {
    static func needsConfirmation(_ session: Session) -> Bool {
        session.status.isActive || session.state == .working || session.state == .needsYou
            || session.state == .unknown || session.pendingInteractions > 0
            || !(session.background ?? []).isEmpty || !(session.queued ?? []).isEmpty || session.loop != nil
    }

    static func next(after closing: SessionKey, in order: [SessionKey], available: [SessionKey]) -> SessionKey? {
        let live = Set(available).subtracting([closing])
        guard let index = order.firstIndex(of: closing) else { return available.first { live.contains($0) } }
        return (Array(order.dropFirst(index + 1)) + Array(order.prefix(index).reversed())).first { live.contains($0) }
            ?? available.first { live.contains($0) }
    }
}
