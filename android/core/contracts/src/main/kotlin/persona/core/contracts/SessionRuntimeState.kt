package persona.core.contracts

import persona.core.contracts.api.DeliveryState

enum class DeliveryOutcome {
    NotDelivered, Delivered, Unknown;

    companion object {
        fun of(delivered: String?): DeliveryOutcome = when (delivered) {
            "none" -> NotDelivered
            "yes" -> Delivered
            else -> Unknown
        }
    }
}

val ErrorBody.deliveryOutcome: DeliveryOutcome get() = DeliveryOutcome.of(delivered)

/** Verification still means the operation's result is unknown. */
val DeliveryState.resultUnknown: Boolean
    get() = this == DeliveryState.NeedsConfirmation || this == DeliveryState.Unknown || this == DeliveryState.Verifying

/** Local projection of existing hub fields and confirmed client operations; never a new wire schema. */
data class SessionRuntimeState(
    val ended: Boolean = false,
    val replaced: Boolean = false,
    val stopping: Boolean = false,
    val turnDurationSeconds: Long? = null,
    val resultUnknown: Boolean = false,
) {
    companion object {
        /**
         * currentSessions must be the complete list for the machine, or null before it is verified.
         * Offline nodes and transcript history resets do not prove closure or replacement.
         * The hub has no turn-duration field: only pass a recorded duration, never timestamp estimates.
         */
        fun map(
            previous: Session,
            currentSessions: List<Session>?,
            machineOnline: Boolean?,
            closeConfirmed: Boolean = false,
            stopRequested: Boolean = false,
            recordedTurnDurationSeconds: Long? = null,
            deliveryStates: Iterable<DeliveryState> = emptyList(),
        ): SessionRuntimeState {
            val current = currentSessions?.firstOrNull { it.key == previous.key }
            val replacement = if (machineOnline == true) currentSessions?.firstOrNull { candidate ->
                candidate.machineId == previous.machineId && candidate.sessionId != previous.sessionId &&
                    candidate.status != "offline" && sameSlot(previous, candidate)
            } else null
            val replaced = replacement != null && (current == null || current.status == "offline")
            val ended = closeConfirmed || replaced || (machineOnline == true && currentSessions != null && current == null)
            return SessionRuntimeState(
                ended = ended,
                replaced = replaced,
                stopping = !ended && stopRequested && (current ?: previous).status == "working",
                turnDurationSeconds = recordedTurnDurationSeconds?.takeIf { it >= 0 },
                resultUnknown = deliveryStates.any { it.resultUnknown },
            )
        }

        /**
         * Whether [b] took [a]'s place: the same tab and, when both name their pane, the same pane. A tab can hold several
         * agents (#2320): another live pane of the tab is a sibling, not the gone session's replacement.
         */
        internal fun sameSlot(a: Session, b: Session): Boolean = when {
            !a.slotId.isNullOrEmpty() && !b.slotId.isNullOrEmpty() ->
                !a.spaceId.isNullOrEmpty() && a.spaceId == b.spaceId && a.slotId == b.slotId && a.spaceKind == b.spaceKind &&
                    (a.paneId.isNullOrEmpty() || b.paneId.isNullOrEmpty() || a.paneId == b.paneId)
            !a.paneId.isNullOrEmpty() -> a.paneId == b.paneId
            else -> false
        }
    }
}
