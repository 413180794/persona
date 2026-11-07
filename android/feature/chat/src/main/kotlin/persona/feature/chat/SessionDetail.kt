package persona.feature.chat

import persona.core.contracts.Session
import persona.core.contracts.SessionInfo
import persona.core.contracts.SessionKey

/** A history seed is display-only and must never cross a conversation or enter the live metadata cache. */
internal fun readOnlySessionMetadata(key: SessionKey, historyReadOnly: Boolean, seed: Session?): Session? =
    seed?.takeIf { historyReadOnly && it.key == key }

/** A row summary updates live state, never erases detail already loaded by an open chat. */
internal fun sessionWithDetail(row: Session, previous: Session?, info: SessionInfo?): Session {
    if (row.waiting == null) return row
    val held = previous?.takeIf { it.key == row.key }
    return row.copy(
        launcher = info?.launcher ?: held?.launcher,
        permissionMode = info?.permissionMode ?: held?.permissionMode,
        effort = info?.effort ?: held?.effort,
        model = row.model ?: info?.model ?: held?.model,
        efforts = held?.efforts.orEmpty(), ports = held?.ports, queued = held?.queued,
        background = held?.background, subagents = held?.subagents, workflows = held?.workflows,
    )
}
