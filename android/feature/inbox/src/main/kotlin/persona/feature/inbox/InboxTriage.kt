package persona.feature.inbox

import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import persona.core.contracts.JsonCodec
import persona.core.designsystem.JNotice
import persona.core.designsystem.Status
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PhIcons

// New items waiting for triage (#1773): since #1644 a plugin's new items enter the inbox only once the model has
// sorted them, and the hub says in GET /api/v1/inbox `triage` how many wait and why (server/internal/hub/
// inbox_admit.go). While some wait, a banner over the list says so: the same words, place and tone on the web, iOS
// and Android.

/** How triage stands; null from an older hub, which sends none. */
@Serializable
data class InboxTriage(
    /** ok | unconfigured | paused | error | timeout | behind. */
    val state: String = "",
    /** The state for people, the hub's own words; may be empty. */
    val reason: String? = null,
    val waiting: Int = 0,
    val sources: List<InboxTriageSource> = emptyList(),
) {
    companion object {
        /** The model use 去配置 opens: 信息筛选, the one that sorts the inbox (设置 › 模型). */
        const val Purpose = "inbox.triage"

        /** The response's `triage`; anything else (missing, not an object, unreadable) is none. */
        fun decode(json: JsonElement?): InboxTriage? = (json as? JsonObject)?.let {
            runCatching { JsonCodec.decodeFromJsonElement(serializer(), it) }.getOrNull()
        }
    }
}

/** One source's share of the waiting items. */
@Serializable
data class InboxTriageSource(
    val source: String = "",
    val name: String? = null,
    val state: String = "",
    val reason: String? = null,
    val waiting: Int = 0,
)

/** The banner's words, and whether it offers 去配置 (the model is not set up). */
data class InboxTriageBanner(val text: String, val configure: Boolean)

internal object InboxTriageText {
    /** What a state says when the hub gives no reason (the same words on the web and iOS). */
    val reasons = mapOf(
        "unconfigured" to "还没配分拣模型",
        "paused" to "分拣模型暂停了",
        "error" to "分拣模型出错了",
        "timeout" to "分拣模型太慢了",
        "behind" to "新消息太多，正在排队",
    )

    /**
     * The banner while new items wait; null when none wait, triage is ok, or the hub is older. One source waiting
     * names it (「有 3 条飞书消息在等分拣：…」), several do not; the reason is the hub's, else the state's words (an
     * unknown state without a reason says only how many wait).
     */
    fun banner(triage: InboxTriage?): InboxTriageBanner? {
        if (triage == null || triage.state.isEmpty() || triage.state == "ok" || triage.waiting <= 0) return null
        val waiting = triage.sources.filter { it.waiting > 0 }
        val name = waiting.singleOrNull()?.name?.trim().orEmpty()
        val reason = triage.reason?.trim()?.takeIf { it.isNotEmpty() } ?: reasons[triage.state].orEmpty()
        return InboxTriageBanner("有 ${triage.waiting} 条${name}消息在等分拣" + (if (reason.isNotEmpty()) "：$reason" else ""),
            configure = triage.state == "unconfigured")
    }
}

/**
 * Where 去配置 goes (设置 › 模型 › 信息筛选) and whether this device may change it ([mayConfigure]: a device that may
 * only read and answer may not, nor a hub without that model use). The shell provides it; none: text only.
 */
class InboxTriageSetup(val mayConfigure: suspend () -> Boolean, val configure: () -> Unit)

val LocalInboxTriageSetup = staticCompositionLocalOf<InboxTriageSetup?> { null }

/**
 * The banner over the list while new items wait for triage: the design system's notice in the warning tone, not
 * dismissable, gone once triage is ok. 去配置 only when the model is not set up and this device may set it up.
 */
@Composable
internal fun InboxTriageNotice(banner: InboxTriageBanner, modifier: Modifier = Modifier.padding(top = Tokens.Space.s)) {
    val setup = LocalInboxTriageSetup.current
    val allowed by produceState(false, setup, banner.configure) {
        value = banner.configure && setup != null && runCatching { setup.mayConfigure() }.getOrDefault(false)
    }
    JNotice(banner.text, modifier, status = Status.NeedsYou, icon = PhIcons.Clock,
        actionLabel = "去配置".takeIf { allowed }, onAction = setup?.configure?.takeIf { allowed })
}
