/// Keeps explicit expansion and pull-to-expand from sharing a finger release. Offsets are normalized to zero
/// at the scroll view's top; a negative offset is the rubber-band pull distance.
nonisolated struct LarkPinnedInteraction: Equatable, Sendable {
    enum PullAction: Equatable, Sendable { case expand }

    static let pullThreshold: Double = 72
    static let topTolerance: Double = 4

    private(set) var isExpanded = false
    private(set) var pullAction: PullAction?
    private(set) var pullProgress: Double = 0
    private var isDragging = false

    var isPullArmed: Bool { pullAction != nil && pullProgress >= 1 }

    mutating func toggle() {
        cancelDrag()
        isExpanded.toggle()
    }

    /// Call at touch tracking's start, before the first overscroll measurement.
    /// A drag may begin anywhere before the shelf leaves the viewport, but returning
    /// from below a shelf cannot become expansion in the same gesture.
    mutating func beginDrag(scrollOffset: Double, shelfPassed: Bool, canExpand: Bool) {
        cancelDrag()
        guard scrollOffset.isFinite else { return }
        guard !isExpanded, canExpand, !shelfPassed else { return }
        isDragging = true
        pullAction = .expand
        updatePullProgress(scrollOffset)
    }

    mutating func update(scrollOffset: Double, shelfPassed: Bool) {
        guard scrollOffset.isFinite else { cancelDrag(); return }
        if isExpanded && shelfPassed && scrollOffset > Self.topTolerance {
            isExpanded = false
            cancelDrag()
        }
        guard isDragging else { return }
        guard !shelfPassed else {
            cancelDrag()
            return
        }
        // Track the current distance, not its maximum. Pulling past the mark
        // and then moving back above it is an intentional cancellation.
        updatePullProgress(scrollOffset)
    }

    /// Call once when finger tracking ends, before deceleration/bounce geometry.
    /// A cancelled gesture, disappearing shortcuts or a repeated callback cannot
    /// issue an expansion the user did not request.
    @discardableResult
    mutating func endDrag(canExpand: Bool, cancelled: Bool = false) -> PullAction? {
        let action = isDragging && isPullArmed && !cancelled ? pullAction : nil
        cancelDrag()
        guard let action else { return nil }
        if action == .expand {
            guard canExpand else { return nil }
            isExpanded = true
        }
        return action
    }

    mutating func cancelDrag() {
        isDragging = false
        pullAction = nil
        pullProgress = 0
    }

    private mutating func updatePullProgress(_ scrollOffset: Double) {
        pullProgress = min(1, max(0, -scrollOffset / Self.pullThreshold))
    }
}
