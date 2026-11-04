package persona.core.contracts.api

/**
 * Whether this app and the hub go together (docs/hub-update.md「兼容」, iOS `HubCompat`).
 */
enum class HubCompat {
    Ok,
    HubTooOld,
    AppTooOld;

    companion object {
        const val MinHubProtocol = 1

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
 * The one quiet line over the Agents list about the hub (docs/hub-update.md「App 里」, iOS `HubNotice`),
 * the most pressing first.
 */
sealed interface HubNotice {
    data object AppTooOld : HubNotice
    data object HubTooOld : HubNotice
    data class Updating(val what: String) : HubNotice
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
                    "downloading" -> "正在下载"
                    "waiting_idle" -> if (status.busy.isEmpty()) "等助手和 Agent 空闲" else "等 ${status.busy.size} 个在忙的结束"
                    "restarting" -> "正在重启，会断开一会儿"
                    "confirming" -> "新版本正在确认"
                    else -> status.state
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

        fun text(notice: HubNotice): String = when (notice) {
            AppTooOld -> "App 版本太旧，配不上这个 hub：请到 TestFlight 更新"
            HubTooOld -> "hub 版本太旧，部分功能用不了"
            is Updating -> "hub 正在更新：${notice.what}"
            is RolledBack -> notice.why
            AutoOn -> "hub 会在空闲时自动更新"
            is Available -> "hub 有新版本 ${notice.version}"
            is NewImage -> "hub 有新镜像 ${notice.version}"
        }
    }
}
