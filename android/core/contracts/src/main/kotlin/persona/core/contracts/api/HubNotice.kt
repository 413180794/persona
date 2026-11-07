package persona.core.contracts.api

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText

/**
 * Whether this app and the hub go together (docs/operations/hub-update.md "Compatibility", iOS `HubCompat`).
 */
enum class HubCompat {
    Ok,
    HubTooOld,
    AppTooOld;

    companion object {
        const val MinHubProtocol = 1

        /** What a hub's hello says; no hello yet (it went by before the page was there, or none came) says nothing. */
        fun of(hello: persona.core.contracts.Hello?, appBuild: Long, connected: Boolean = true): HubCompat =
            if (hello == null) Ok else of(hello.protocol, hello.minAppBuild, appBuild, connected)

        fun of(protocolVersion: Int?, minAppBuild: Int?, appBuild: Long, connected: Boolean = true): HubCompat {
            if (!connected) return Ok
            if (minAppBuild != null && minAppBuild > 0 && appBuild > 0 && appBuild < minAppBuild) {
                return AppTooOld
            }
            if ((protocolVersion ?: 0) < MinHubProtocol) {
                return HubTooOld
            }
            return Ok
        }
    }
}

/**
 * The one quiet line over the Agents list about the hub (docs/operations/hub-update.md "In the app", iOS `HubNotice`),
 * the most pressing first.
 */
sealed interface HubNotice {
    data object AppTooOld : HubNotice
    data object HubTooOld : HubNotice
    data class Updating(val what: CoreText) : HubNotice
    data class RolledBack(val why: String, val build: Long) : HubNotice
    data object AutoOn : HubNotice
    data class Available(val version: String, val build: Long) : HubNotice
    data class NewImage(val version: String) : HubNotice

    /**
     * What closing the line remembers, so this one does not come back (null: cannot be closed).
     */
    val dismissKey: String?
        get() = when (this) {
            HubTooOld -> "hubTooOld"
            is RolledBack -> "rolledBack:$build"
            is Available -> "build:$build"
            is NewImage -> "image:$version"
            AppTooOld, is Updating, AutoOn -> null
        }

    companion object {
        fun of(status: HubUpdateStatus?, compat: HubCompat = HubCompat.Ok, dismissed: Set<String> = emptySet()): HubNotice? {
            return candidates(status, compat).firstOrNull { notice ->
                val key = notice.dismissKey
                key == null || key !in dismissed
            }
        }

        fun candidates(status: HubUpdateStatus?, compat: HubCompat = HubCompat.Ok): List<HubNotice> {
            val out = mutableListOf<HubNotice>()
            when (compat) {
                HubCompat.AppTooOld -> return listOf(AppTooOld)
                HubCompat.HubTooOld -> out.add(HubTooOld)
                HubCompat.Ok -> Unit
            }
            if (status == null) return out

            if (status.working) {
                val what = when (status.state) {
                    "downloading" -> CoreText.of(Key.HubDownloading)
                    "waiting_idle" -> if (status.busy.isEmpty()) CoreText.of(Key.HubWaitingIdle) else CoreText.of(Key.HubWaitingBusy, status.busy.size)
                    "restarting" -> CoreText.of(Key.HubRestarting)
                    "confirming" -> CoreText.of(Key.HubConfirming)
                    else -> CoreText.plain(status.state)
                }
                out.add(Updating(what))
            }
            if (status.state == "rolled_back" && !status.error.isNullOrBlank() && status.latest?.build != null) {
                out.add(RolledBack(status.error, status.latest.build))
            }
            if (status.autoNotice) {
                out.add(AutoOn)
            }
            val latest = status.latest
            if (latest != null && latest.newer) {
                if (status.channel == "docker") {
                    out.add(NewImage(latest.version))
                } else if (status.canUpdate && (!status.auto || latest.held != null) && !latest.bad) {
                    out.add(Available(latest.version, latest.build))
                }
            }
            return out
        }

        fun text(notice: HubNotice): CoreText = when (notice) {
            AppTooOld -> CoreText.of(Key.HubAppTooOld)
            HubTooOld -> CoreText.of(Key.HubTooOld)
            is Updating -> CoreText.of(Key.HubUpdating, notice.what)
            is RolledBack -> CoreText.plain(notice.why)
            AutoOn -> CoreText.of(Key.HubAutoOn)
            is Available -> CoreText.of(Key.HubAvailable, notice.version)
            is NewImage -> CoreText.of(Key.HubNewImage, notice.version)
        }
    }
}
