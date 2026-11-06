package persona.core.contracts.api

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Settings › Managers (docs/managers.md, docs/protocol.md"Managers"): the selected hub's roster of managers, each a whole
 * assistant, and the one-time install codes that put a new one (or one moved elsewhere) on a computer. The hub answers
 * these itself (`/api/v1/managers`). Bound to the active pairing's selected hub.
 */
interface HubManagers {
    /** The hub's `managers` pushes (sent only to an app that asks for the `managers` feature): read the list again. */
    val changes: Flow<Unit> get() = emptyFlow()

    /** `GET /api/v1/managers`; a hub from before the roster is a [ManagersUnsupported]. */
    suspend fun roster(): ManagerRoster

    /** `POST /api/v1/managers {name}`: a new manager, still installing, and its install code. */
    suspend fun create(name: String): ManagerCode

    /** `POST /api/v1/managers/{id}/code`: a new install code (moving it elsewhere, or one whose code ran out). */
    suspend fun newCode(id: String): ManagerCode

    /** `PATCH /api/v1/managers/{id} {name}`. */
    suspend fun rename(id: String, name: String): HubManager

    /** `PATCH /api/v1/managers/{id} {default: true}`. */
    suspend fun makeDefault(id: String): HubManager

    /** `DELETE /api/v1/managers/{id}`: its token stops working at once (an installing one's code is void). */
    suspend fun delete(id: String)
}

/** The hub has no `/api/v1/managers` (404): it predates the roster. */
class ManagersUnsupported : Exception("This hub has no managers")

/** GET /api/v1/managers: the default first, the installing ones last. */
@Serializable
data class ManagerRoster(
    val managers: List<HubManager> = emptyList(),
    @SerialName("default") val defaultId: String = "",
)

/** One manager as the roster has it; times are RFC 3339. */
@Serializable
data class HubManager(
    val id: String,
    val name: String = "",
    val online: Boolean = false,
    @SerialName("default") val isDefault: Boolean = false,
    /** The assistant the hub starts beside itself: it cannot be deleted or moved. */
    val builtin: Boolean = false,
    val host: String? = null,
    val os: String? = null,
    val arch: String? = null,
    /** How it runs: child (the hub's own), service / systemd / launchd, docker / container, external, or other words. */
    val deploy: String? = null,
    val version: String? = null,
    /** Connected since (online only). */
    val since: String? = null,
    /** Last connected (offline: Last synced at). */
    @SerialName("last_seen") val lastSeen: String? = null,
    /** Made in the app, its install code not traded yet: when that code runs out. */
    val installing: String? = null,
) {
    val isInstalling: Boolean get() = !installing.isNullOrBlank()
}

/** A one-time install code (10 minutes, used once) and, from a hub that gives them, the commands that use it. */
@Serializable
data class ManagerCode(
    val code: String = "",
    @SerialName("expires_at") val expiresAt: String = "",
    /** POST /api/v1/managers also answers the new manager. */
    val manager: HubManager? = null,
    val commands: ManagerCommands? = null,
)

/** Ready-made install commands with the code in them; either may be missing. */
@Serializable
data class ManagerCommands(val service: String? = null, val docker: String? = null)
