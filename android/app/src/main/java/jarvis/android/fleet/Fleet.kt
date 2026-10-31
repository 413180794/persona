package jarvis.android.fleet

import jarvis.android.data.AgentState
import jarvis.android.data.Interaction
import jarvis.android.data.Machine
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.data.parseTime
import kotlinx.serialization.Serializable

/**
 * The 「Fleet」 summary (iOS `FleetState`, hub `liveactivity.go` `buildFleet`): every agent that needs you, is working
 * or finished unseen. On iOS the hub pushes it to the Live Activity; Android builds the same thing from the
 * connection's state ([build]) for the ongoing notification, the home-screen widget and the Quick Settings tile.
 * Times are Unix seconds, as in the hub's JSON.
 */
@Serializable
data class FleetState(
    val phase: String = "", // needsYou | working | done | "" (nothing)
    val needsYou: Int = 0,
    val working: Int = 0,
    val unread: Int = 0,
    val items: List<FleetItem> = emptyList(),
    val workingSince: Double? = null,
    /** The Jarvis tab's assistant is working (counted apart, as on iOS). */
    val assistant: Boolean = false,
) {
    val isEmpty: Boolean get() = phase.isEmpty()

    /** Agents left out when only the first [showing] items are drawn (「+2 个」). */
    fun hidden(showing: Int): Int = maxOf(0, needsYou + working + unread - minOf(showing, items.size))

    /** The first request a notification button can settle (a permission or a plan). */
    val firstActionable: FleetItem?
        get() = items.firstOrNull { (it.kind == "permission" || it.kind == "plan") && it.interaction != null }

    /** The one agent to open on a tap, when there is exactly one; otherwise the app opens where the user left it. */
    val single: SessionKey?
        get() = items.firstOrNull()?.key?.takeIf { needsYou + working + unread == 1 }

    companion object {
        const val MAX_ITEMS = 3

        /** hub `buildFleet` over the app's own snapshot; [muted] is the hub's mute rule ([jarvis.android.data.Hub.isMuted]). */
        fun build(
            sessions: List<Session>,
            interactions: List<Interaction>,
            machines: List<Machine>,
            muted: (Session) -> Boolean,
        ): FleetState {
            val names = machines.associate { it.id to it.displayName }
            val byKey = sessions.associateBy { it.key }
            val needs = mutableListOf<FleetItem>()
            val working = mutableListOf<FleetItem>()
            val unread = mutableListOf<FleetItem>()
            val asked = HashSet<SessionKey>()
            var assistant = false
            for (ix in interactions.sortedBy { parseTime(it.createdAt) }) {
                val s = byKey[ix.sessionKey] ?: continue
                if (muted(s) || ix.sessionKey in asked) continue
                asked += ix.sessionKey
                needs += FleetItem(
                    id = s.key.path, title = title(s), place = place(names[s.machineId] ?: s.machineId, s), kind = kindOf(ix.kind),
                    detail = clip(oneLine(detail(ix)), 60), interaction = ix.id, since = parseTime(ix.createdAt)?.epochSecond?.toDouble(),
                )
            }
            for (s in sessions) {
                if (muted(s) || s.key in asked) continue
                val place = place(names[s.machineId] ?: s.machineId, s)
                when (s.agentState) {
                    AgentState.NeedsYou -> if (!s.assistant) needs += FleetItem(s.key.path, title(s), place, "screen", "终端等你处理")
                    AgentState.Working -> if (s.assistant) assistant = true else {
                        working += FleetItem(s.key.path, title(s), place, "working", since = s.updated?.epochSecond?.toDouble())
                    }
                    AgentState.Unread -> if (!s.assistant && s.loop == null) {
                        unread += FleetItem(
                            s.key.path, title(s), place, "unread",
                            clip(oneLine(s.lastText.orEmpty().lineSequence().firstOrNull { it.isNotBlank() }.orEmpty()), 60),
                            since = s.updated?.epochSecond?.toDouble(),
                        )
                    }
                    else -> Unit
                }
            }
            working.sortBy { it.since ?: Double.MAX_VALUE }
            unread.sortByDescending { it.since ?: 0.0 }
            val phase = when {
                needs.isNotEmpty() -> "needsYou"
                working.isNotEmpty() -> "working"
                unread.isNotEmpty() -> "done"
                else -> ""
            }
            return FleetState(
                phase = phase, needsYou = needs.size, working = working.size, unread = unread.size,
                items = (needs + working + unread).take(MAX_ITEMS), workingSince = working.firstOrNull()?.since, assistant = assistant,
            )
        }

        private fun kindOf(k: String) = when (k) {
            "permission", "plan", "question" -> k
            else -> "screen"
        }

        /** hub `agentTitle`. */
        fun title(s: Session): String = clip(oneLine(s.displayTitle), 24)

        /** hub `agentPlace`: 「机器 · 空间 › 标签」. */
        fun place(machine: String, s: Session): String {
            var p = machine
            val space = s.spaceName
            if (!space.isNullOrEmpty()) {
                p += " · $space"
                val slot = s.slotName
                if (!slot.isNullOrEmpty() && slot != s.slotIndex?.toString()) p += " › $slot"
            }
            return clip(oneLine(p), 32)
        }

        /** hub `interactionDetail`. */
        fun detail(ix: Interaction): String = when (ix.kind) {
            "permission" -> ix.summary?.takeIf { it.isNotEmpty() } ?: ix.toolName.orEmpty()
            // The plan's first line is usually a Markdown heading: drop the marks (「## 计划」 → 「计划」).
            "plan" -> ix.plan.orEmpty().lineSequence().firstOrNull { it.isNotBlank() }.orEmpty().trimStart().trimStart('#', '>', '-', '*', ' ')
            "question" -> ix.questions?.firstOrNull()?.question ?: ix.summary.orEmpty()
            else -> ix.dialog?.question?.takeIf { it.isNotBlank() } ?: "终端等你处理"
        }

        fun oneLine(s: String): String = s.split(Regex("\\s+")).filter { it.isNotEmpty() }.joinToString(" ")

        fun clip(s: String, n: Int): String = if (s.codePointCount(0, s.length) <= n) s else {
            s.substring(0, s.offsetByCodePoints(0, n - 1)) + "…"
        }
    }
}

@Serializable
data class FleetItem(
    /** "machine/session". */
    val id: String,
    val title: String = "",
    val place: String = "",
    /** permission | plan | question | screen | working | unread. */
    val kind: String = "working",
    val detail: String = "",
    /** The interaction 允许 / 拒绝 / 批准 resolve. */
    val interaction: String? = null,
    val since: Double? = null,
) {
    val needsYou: Boolean get() = kind != "working" && kind != "unread"

    val key: SessionKey?
        get() {
            val i = id.indexOf('/')
            return if (i <= 0 || i == id.length - 1) null else SessionKey(id.substring(0, i), id.substring(i + 1))
        }
}

/** The wording in one place (iOS `FleetText`), so the notification, widget and tile say the same thing. */
object FleetText {
    fun headline(s: FleetState): String = when (s.phase) {
        "needsYou" -> "${s.needsYou} 个需要你"
        "working" -> "${s.working} 个在工作"
        "done" -> "${s.unread} 个做完了"
        else -> "都处理完了"
    }

    /** The counts after the headline: 「3 个在工作 · 1 个做完了」. */
    fun rest(s: FleetState): String {
        val parts = mutableListOf<String>()
        if (s.phase != "working" && s.working > 0) parts += "${s.working} 个在工作"
        if (s.phase != "done" && s.unread > 0) parts += "${s.unread} 个做完了"
        return parts.joinToString(" · ")
    }

    fun line(item: FleetItem): String = when (item.kind) {
        "permission" -> if (item.detail.isEmpty()) "等你授权" else "授权：" + item.detail
        "plan" -> if (item.detail.isEmpty()) "计划等你批准" else "计划：" + item.detail
        "question" -> item.detail.ifEmpty { "在问你" }
        "screen" -> item.detail.ifEmpty { "终端等你处理" }
        "working" -> "工作中"
        "unread" -> item.detail.ifEmpty { "做完了" }
        else -> item.detail
    }

    /** What a locked screen says instead of the agent's name and content. */
    fun anonymous(item: FleetItem): String = when (item.kind) {
        "permission" -> "有个授权等你"
        "plan" -> "有个计划等你批准"
        "question" -> "有个提问等你回答"
        "screen" -> "有个终端等你处理"
        "working" -> "工作中"
        else -> "做完了"
    }

    fun more(n: Int): String = "+$n 个"
}
