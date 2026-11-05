import Foundation

/// When a change reaches a listener that wants at most one per `gap`: at once after a quiet spell, else once the gap
/// since the last one is up, the changes meanwhile folded into that one. A value; `LarkChangeGateTests`.
public nonisolated struct LarkChangeGate {
    public enum When: Equatable { case now, later(ContinuousClock.Instant), pending }
    public let gap: Duration
    private var last: ContinuousClock.Instant?
    private var waiting = false

    public init(gap: Duration) { self.gap = gap }

    public mutating func arrive(at now: ContinuousClock.Instant) -> When {
        if waiting { return .pending }
        if let last, now < last + gap {
            waiting = true
            return .later(last + gap)
        }
        last = now
        return .now
    }

    /// The delayed one went out.
    public mutating func fired(at now: ContinuousClock.Instant) {
        waiting = false
        last = now
    }
}
