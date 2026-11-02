package jarvis.platform.notify

import jarvis.core.contracts.FleetState
import jarvis.core.designsystem.Status
import jarvis.core.contracts.api.ConnectionState
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.conflate
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flow

/** Only visible content participates in notification deduplication. */
internal data class OngoingStatus(
    val title: String,
    val counts: String,
    val detail: String,
    val criticalText: String?,
    val needsYou: Int = 0,
    val working: Int = 0,
    val unread: Int = 0,
) {
    fun shouldPromote(sdk: Int, allowed: Boolean, enabled: Boolean = true): Boolean =
        sdk >= 36 && allowed && enabled && criticalText != null

    fun lockScreenText(showDetails: Boolean): String =
        listOf(counts, detail.takeIf { showDetails }.orEmpty()).filter(String::isNotBlank).joinToString("\n")

    companion object {
        fun from(state: ConnectionState, fleet: FleetState): OngoingStatus {
            val title = when (state) {
                ConnectionState.Online -> "已连接 hub"
                ConnectionState.Connecting -> "正在连接 hub"
                ConnectionState.Updating -> "正在更新"
                ConnectionState.WaitingForNetwork -> "等待网络"
                ConnectionState.NeedsLocalNetworkPermission -> "需要本地网络权限"
                ConnectionState.CertificateMismatch -> "hub 证书已变化，请重新配对"
                ConnectionState.Revoked -> "配对已撤销"
            }
            // A disconnected snapshot must not look like current work or expose an old request.
            if (state != ConnectionState.Online) return OngoingStatus(title, "", "", null)
            val needsYou = fleet.needsYou.coerceAtLeast(0)
            val working = fleet.working.coerceAtLeast(0)
            val unread = fleet.unread.coerceAtLeast(0)
            val counts = "${Status.NeedsYou.label} $needsYou · ${Status.Working.label} $working · ${Status.Unread.label} $unread"
            val detail = if (needsYou > 0) fleet.items.firstOrNull { it.interaction != null }?.let {
                listOf(it.title, it.detail).filter(String::isNotBlank).joinToString("：").take(2000)
            }.orEmpty() else ""
            val critical = when {
                needsYou > 0 -> "💬${compact(needsYou)}"
                working > 0 -> "⚙${compact(working)}"
                unread > 0 -> "●${compact(unread)}"
                else -> null
            }
            return OngoingStatus(title, counts, detail, critical, needsYou, working, unread)
        }

        private fun compact(count: Int) = if (count > 999) "999+" else count.toString()
    }
}

/** Keep the newest pending value; continuous input cannot postpone delivery indefinitely. */
internal fun <T> Flow<T>.throttleOngoing(intervalMillis: Long = 2_500): Flow<T> = flow {
    require(intervalMillis > 0)
    this@throttleOngoing.distinctUntilChanged().conflate().distinctUntilChanged().collect {
        emit(it)
        delay(intervalMillis)
    }
}
