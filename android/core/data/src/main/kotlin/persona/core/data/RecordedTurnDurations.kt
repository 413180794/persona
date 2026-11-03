package persona.core.data

import persona.core.contracts.Item

internal val Item.startsRecordedTurn: Boolean
    get() = (role == "user" && kind == "text" && queued != true) || wake == true

internal data class RecordedDuration(val seconds: Long, val seq: Int)

/** Resolve native associations inside the loaded history; partial or stale associations stay hidden. */
internal fun recordedTurnDurations(items: List<Item>): Map<String, RecordedDuration> = buildMap {
    val byId = items.associateBy { it.id }
    val byUUID = items.filter { it.kind != "turn_duration" }.associateBy { it.id.substringBeforeLast('#') }
    for (marker in items) {
        if (marker.kind != "turn_duration") continue
        val duration = marker.turnDuration ?: continue
        val ms = duration.durationMs?.takeIf { it >= 0 } ?: continue
        val anchor = when (duration.source) {
            "claude_turn_duration" -> duration.parentUuid?.let(byUUID::get)
            "codex_task_complete" -> if (!duration.turnId.isNullOrBlank()) duration.userItemId?.let(byId::get) else null
            else -> null
        } ?: continue
        if (anchor.seq >= marker.seq) continue
        val turn = items.lastOrNull { it.seq <= anchor.seq && (it.startsRecordedTurn || it.kind == "recap") }
            ?.takeIf { it.startsRecordedTurn } ?: continue
        if (items.any { it.seq > anchor.seq && it.seq < marker.seq && (it.startsRecordedTurn || it.kind == "recap") }) continue
        put(turn.id, RecordedDuration(ms / 1_000, marker.seq))
    }
}
