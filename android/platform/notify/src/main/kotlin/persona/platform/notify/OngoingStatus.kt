package persona.platform.notify

import android.os.Build
import persona.core.contracts.FleetState
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ConnectionState
import persona.core.domain.answer.AnswerEntry
import persona.core.domain.answer.ApprovalAction
import persona.core.domain.answer.ApprovalPolicy
import persona.core.domain.answer.Authentication
import persona.core.domain.fleet.FleetSummary
import persona.core.domain.fleet.FleetText
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.conflate
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flow

/**
 * The ongoing notification (the iPhone Live Activity's counterpart, docs/android.md「常驻汇总通知」): the hub's fleet
 * summed up when there is something to show, otherwise the connection line. Only visible content takes part in
 * equality, so the throttled updates skip anything the user would not see change.
 */
internal data class OngoingStatus(
    val title: String,
    /** The first agent: 「名字 · 在做什么」; without lock screen details only 「有个授权等你」. */
    val text: String? = null,
    /** The other counts: 「3 个在工作 · 1 个做完了」. */
    val subText: String? = null,
    /** Expanded: up to three agents with where they run, then 「+N 个」; without lock screen details, kinds and counts. */
    val lines: String? = null,
    /** The lock screen's version: counts and 「有个授权等你」, never names or content. */
    val publicText: String? = null,
    val criticalText: String? = null,
    val needsYou: Int = 0,
    val working: Int = 0,
    val unread: Int = 0,
    /** Only working agents left: the clock runs from when the longest turn started (epoch milliseconds). */
    val workingSince: Long? = null,
    /** The one agent a tap opens; null opens Agents. */
    val open: SessionKey? = null,
    /** The first permission or plan, settled from the notification's buttons. */
    val action: OngoingAction? = null,
) {
    /** The fleet summed up, not the connection line. */
    val summary: Boolean get() = criticalText != null

    /** [sdkFull]: Build.VERSION.SDK_INT_FULL; promoted notifications came in Android 16 QPR1 (36.1), not 36.0. */
    fun shouldPromote(sdkFull: Int, allowed: Boolean, enabled: Boolean = true): Boolean =
        sdkFull >= Build.VERSION_CODES_FULL.BAKLAVA_1 && allowed && enabled && criticalText != null

    companion object {
        /**
         * [quiet]: only finished agents have been left for a while (hub fleetDoneLinger): back to the connection line.
         * [details]: the device's 锁屏显示详情. Off (the default), the body and the expanded list say only what kinds of
         * requests wait and how many, never an agent's name, place or what it asks: a phone set to show all notification
         * content on its lock screen shows the private version there too.
         */
        fun from(state: ConnectionState, fleet: FleetState, quiet: Boolean = false, details: Boolean = false): OngoingStatus {
            // A disconnected snapshot must not look like current work or expose an old request.
            if (state != ConnectionState.Online || FleetSummary.isEmpty(fleet) || quiet) return OngoingStatus(ongoingTitle(state))
            val needsYou = fleet.needsYou.coerceAtLeast(0)
            val working = fleet.working.coerceAtLeast(0)
            val unread = fleet.unread.coerceAtLeast(0)
            val shown = fleet.items.take(MAX_ITEMS)
            val first = shown.firstOrNull()
            val firstRequest = shown.firstOrNull(FleetSummary::needsYou)?.let(FleetText::anonymous)
            val text: String?
            val lines: String?
            if (details) {
                val hidden = FleetSummary.hidden(fleet, shown.size)
                text = first?.let(FleetText::row) ?: FleetText.headline(fleet)
                lines = (shown.map(FleetText::rowWithPlace) + listOfNotNull(hidden.takeIf { it > 0 }?.let(FleetText::more)))
                    .joinToString("\n").ifEmpty { null }
            } else {
                // The counts of working and finished agents are the subtext already; only requests get lines of their own.
                val requests = shown.filter(FleetSummary::needsYou)
                text = firstRequest
                lines = (FleetText.anonymousRequests(requests) +
                    listOfNotNull((needsYou - requests.size).takeIf { it > 0 }?.let(FleetText::more)))
                    .joinToString("\n").ifEmpty { null }?.takeIf { it != text }
            }
            val publicText = listOf(firstRequest.orEmpty(), FleetText.rest(fleet))
                .filter(String::isNotEmpty).joinToString(" · ").ifEmpty { null }
            val critical = when {
                needsYou > 0 -> "💬${compact(needsYou)}"
                working > 0 -> "⚙${compact(working)}"
                unread > 0 -> "●${compact(unread)}"
                else -> null
            }
            return OngoingStatus(
                title = FleetText.headline(fleet), text = text,
                subText = FleetText.rest(fleet).ifEmpty { null }, lines = lines, publicText = publicText,
                criticalText = critical, needsYou = needsYou, working = working, unread = unread,
                workingSince = fleet.workingSince?.takeIf { FleetSummary.phase(fleet) == FleetSummary.WORKING && it > 0 }
                    ?.let { (it * 1000).toLong() },
                open = FleetSummary.single(fleet),
                action = FleetSummary.firstActionable(fleet)?.let { item ->
                    OngoingAction.of(item.kind, item.interaction.orEmpty(), FleetSummary.key(item))
                },
            )
        }

        const val MAX_ITEMS = 3

        private fun compact(count: Int) = if (count > 999) "999+" else count.toString()
    }
}

/** One button on the ongoing notification: answers a request through ActionReceiver, or opens its conversation. */
internal data class OngoingButton(val label: String, val key: String?, val authenticationRequired: Boolean)

/**
 * The first permission or plan and its buttons, from the lock screen entry of the shared policy: 拒绝 / 允许 for a
 * permission, 查看 / 批准 for a plan (acceptEdits). Saying yes needs an unlocked phone; saying no does not.
 */
internal data class OngoingAction(val kind: String, val interaction: String, val session: SessionKey?, val buttons: List<OngoingButton>) {
    companion object {
        fun of(kind: String, interaction: String, session: SessionKey?): OngoingAction? {
            if (interaction.isEmpty()) return null
            val policy = ApprovalPolicy.actions(AnswerEntry.LockScreen, kind, locked = true).associateBy { it.id }
            fun answer(id: ApprovalAction, label: String) = policy[id]?.let {
                OngoingButton(label, id.wire, it.authentication != Authentication.None)
            }
            val buttons = when (kind) {
                "permission" -> listOfNotNull(answer(ApprovalAction.Deny, "拒绝"), answer(ApprovalAction.Allow, "允许"))
                "plan" -> listOfNotNull(policy[ApprovalAction.OpenApp]?.let { OngoingButton("查看", null, false) },
                    answer(ApprovalAction.ApproveEdits, "批准"))
                else -> emptyList()
            }
            return OngoingAction(kind, interaction, session, buttons).takeIf { buttons.isNotEmpty() }
        }
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
