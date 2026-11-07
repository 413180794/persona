package persona.core.contracts.api

import persona.core.contracts.CoreTextException
import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Settings › Hub version (docs/operations/hub-update.md "In the app"): the selected hub's version and channel, checking for and installing a
 * release, what an update waits for, auto-update and the history. The same API the web's `lib/hub-update.ts` and
 * iOS's `HubUpdate.swift` use. Bound to the active pairing's selected hub.
 */
interface HubUpdates {
    /** The hub's `hub_update` pushes: the status after every change, while the page is open too. */
    val events: Flow<HubUpdateStatus> get() = emptyFlow()

    /** `GET /api/v1/hub/update`; a hub from before updates in the app is a [HubUpdatesUnsupported]. */
    suspend fun status(): HubUpdateStatus

    /** `POST /api/v1/hub/update/check`. */
    suspend fun check(): HubUpdateStatus

    /** `POST /api/v1/hub/update`: [now] skips waiting for the assistant and the agents to be idle. */
    suspend fun start(now: Boolean): HubUpdateStatus

    /** `PUT /api/v1/hub/update/settings` with only the values given. */
    suspend fun settings(auto: Boolean? = null, noticeSeen: Boolean? = null): HubUpdateStatus

    /** `GET /api/v1/hub/update/history`, newest first. */
    suspend fun history(): List<HubUpdateRecord>
}

/** The hub has no `/api/v1/hub/update` (404): it predates updates in the app. */
class HubUpdatesUnsupported : CoreTextException(CoreText.of(Key.HubUpdatesUnsupported))

/** GET /api/v1/hub/update and the `hub_update` event's `update`. */
@Serializable
data class HubUpdateStatus(
    val version: String = "",
    val build: Long = 0,
    /** release | docker | dev */
    val channel: String = "dev",
    @SerialName("can_update") val canUpdate: Boolean = false,
    @SerialName("why_not") val whyNot: String? = null,
    val latest: HubRelease? = null,
    /** idle | checking | available | downloading | waiting_idle | restarting | confirming | updated | rolled_back | failed */
    val state: String = "idle",
    val progress: String? = null,
    val busy: List<HubUpdateBusy> = emptyList(),
    val error: String? = null,
    val auto: Boolean = false,
    @SerialName("auto_notice") val autoNotice: Boolean = false,
    @SerialName("last_check") val lastCheck: String? = null,
    val confirmed: Boolean = true,
    /** A Docker hub's image. */
    val image: HubImage? = null,
) {
    /** Doing something the page shows progress for (no buttons meanwhile). */
    val working: Boolean get() = state in WorkingStates
}

private val WorkingStates = setOf("downloading", "waiting_idle", "restarting", "confirming")

/** The newest release (or a Docker hub's newest image tag). */
@Serializable
data class HubRelease(
    val version: String = "",
    val build: Long = 0,
    val released: String? = null,
    val notes: String? = null,
    val newer: Boolean = false,
    /** Went back before: not installed automatically. */
    val bad: Boolean = false,
    /** Why auto-update does not install this newer one by itself, for people. */
    val held: String? = null,
)

/** Something the restart would interrupt. */
@Serializable
data class HubUpdateBusy(
    @SerialName("machine_id") val machineId: String = "",
    val machine: String = "",
    @SerialName("session_id") val sessionId: String? = null,
    val title: String = "",
)

/** The image a Docker hub runs: a pinned tag has to be changed in .env to get a newer one. */
@Serializable
data class HubImage(val name: String = "", val tag: String = "", val pinned: Boolean = false)

/** One update in the hub's history. */
@Serializable
data class HubUpdateRecord(
    @SerialName("from_version") val fromVersion: String = "",
    @SerialName("to_version") val toVersion: String = "",
    @SerialName("to_build") val toBuild: Long = 0,
    val started: String? = null,
    val ended: String? = null,
    /** updated | rolled_back | failed | held */
    val result: String = "",
    val reason: String? = null,
    val auto: Boolean = false,
)
