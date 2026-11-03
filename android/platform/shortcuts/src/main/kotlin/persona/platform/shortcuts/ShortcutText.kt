package persona.platform.shortcuts

import persona.core.contracts.FleetState
import persona.core.contracts.Interaction
import persona.core.contracts.Machine
import persona.core.contracts.Resolution
import persona.core.contracts.Session
import persona.core.contracts.api.InteractionGoneException
import persona.core.contracts.api.InteractionPromptChangedException
import persona.core.contracts.parseTime
import persona.core.domain.answer.AnswerAction
import persona.core.domain.answer.AnswerBuilder
import persona.core.domain.answer.AnswerContext
import persona.core.domain.answer.AnswerEntry
import persona.core.domain.fleet.FleetSummary
import persona.core.domain.fleet.FleetText
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import java.time.Instant

/**
 * What the launcher shortcuts and the tile say and do (iOS IntentText / IntentFailure / IntentPicker, the old Android
 * app's ShortcutText), pure so tests pin it.
 */
object ShortcutText {
    const val TITLE = "批准第一个待处理"
    const val LOADING = "正在看有什么等你…"
    const val NOT_PAIRED = "还没配对。先打开 Persona，扫 hub 的配对二维码。"
    const val NO_PENDING = "没有等你处理的事。"
    const val PLAN_NOTE = "批准后自动接受编辑，命令仍会先问你。"

    /** The one that has been waiting longest (docs/intents.md「选择规则」). */
    fun pending(interactions: List<Interaction>): Interaction? =
        interactions.minByOrNull { parseTime(it.createdAt) ?: Instant.MAX }

    /**
     * What saying yes sends, or null when the request needs the app (a question, a terminal dialog). The launcher entry
     * of the shared policy: a plan is approved with acceptEdits (the whole plan was not read here), and only on an
     * unlocked phone.
     */
    fun resolution(ix: Interaction, locked: Boolean = false): Resolution? {
        val action = when (ix.kind) {
            "permission" -> AnswerAction.Allow
            "plan" -> AnswerAction.ApproveEdits
            else -> return null
        }
        return AnswerBuilder.build(ix, action, context = AnswerContext(entry = AnswerEntry.SiriShortcut, locked = locked)).resolution
    }

    /** The question before saying yes. */
    fun ask(ix: Interaction): String = when (ix.kind) {
        "permission" -> "允许 " + (ix.summary?.takeIf { it.isNotBlank() }?.let { excerpt(it, 120) } ?: ix.toolName?.takeIf { it.isNotBlank() } ?: "这个工具") + "？"
        "plan" -> "批准这个计划？" + (ix.plan?.takeIf { it.isNotBlank() }?.let { "\n「${excerpt(plain(it), 120)}」" } ?: "")
        else -> needsApp(ix.kind)
    }

    fun confirmLabel(ix: Interaction): String = if (ix.kind == "plan") "批准" else "允许"

    fun done(ix: Interaction): String = when (ix.kind) {
        "permission" -> ix.toolName?.takeIf { it.isNotEmpty() }?.let { "已允许 $it" } ?: "已允许"
        "plan" -> "已批准计划，开始执行"
        else -> needsApp(ix.kind)
    }

    fun needsApp(kind: String): String = when (kind) {
        "question" -> "这是个提问，需要打开 Persona 回答。"
        "screen" -> "终端在等你输入，需要打开 Persona。"
        else -> "需要打开 Persona 处理。"
    }

    /** 「会话 · 机器」 under the question. */
    fun where(ix: Interaction, machines: List<Machine>): String? =
        listOfNotNull(ix.sessionTitle?.takeIf { it.isNotBlank() }, machines.firstOrNull { it.id == ix.machineId }?.displayName)
            .joinToString(" · ").ifEmpty { null }

    /** Reading the pending requests failed. */
    fun loadFailure(error: Throwable): String {
        val failure = HubFailure.from(error)
        return when (failure.kind) {
            HubError.PairingExpired, HubError.HubMismatch -> PAIRING_EXPIRED
            HubError.CertificateMismatch -> CERTIFICATE_CHANGED
            in UNREACHABLE, HubError.Timeout, HubError.Transport -> UNREACHABLE_TEXT
            else -> failure.message.takeIf { failure.status != null && it.isNotBlank() } ?: "出错了，稍后再试。"
        }
    }

    /** Answering failed. A request that may have reached the hub is never called undelivered: check it in the app. */
    fun sendFailure(error: Throwable): String {
        if (error is InteractionGoneException) return "这个已经处理过了。"
        if (error is InteractionPromptChangedException) return "终端上的对话框已经变了，打开 Persona 看看新的。"
        val failure = HubFailure.from(error)
        return when {
            failure.kind == HubError.PairingExpired || failure.kind == HubError.HubMismatch -> PAIRING_EXPIRED
            failure.kind == HubError.CertificateMismatch -> CERTIFICATE_CHANGED
            // Refused before anything was sent: safe to say it did not get there.
            failure.kind in UNREACHABLE -> "没有送达：$UNREACHABLE_TEXT"
            failure.delivered == "none" || failure.status in 400..499 -> failure.message.ifBlank { "hub 没有接受这次操作。" }
            else -> "结果待确认：打开 Persona 看看这条处理了没有。"
        }
    }

    private const val PAIRING_EXPIRED = "配对已失效，打开 Persona 重新配对。"
    private const val CERTIFICATE_CHANGED = "hub 的证书变了，打开 Persona 重新配对。"
    private const val UNREACHABLE_TEXT = "连不上 hub，确认手机能访问 hub（同一个网络或 VPN）。"
    private val UNREACHABLE = setOf(HubError.NoNetwork, HubError.AddressNotFound, HubError.ConnectionRefused,
        HubError.Unreachable, HubError.LocalNetworkPermission)

    /** Markdown heading and quote marks off each line, so an excerpt reads as prose. */
    fun plain(markdown: String): String =
        markdown.lineSequence().joinToString("\n") { it.trimStart().trimStart('#', '>').trimStart() }

    /** Collapses whitespace and clips. */
    fun excerpt(text: String, limit: Int = 60): String {
        val flat = text.split(Regex("\\s+")).filter { it.isNotEmpty() }.joinToString(" ")
        return if (flat.length <= limit) flat else flat.take(limit) + "…"
    }

    /** The tile's second line: 「2 个需要你」, 「都处理完了」, or 「还没配对」. */
    fun tileSubtitle(paired: Boolean, fleet: FleetState): String = when {
        !paired -> "还没配对"
        FleetSummary.isEmpty(fleet) -> "都处理完了"
        else -> FleetText.headline(fleet)
    }
}

/** The launcher's recent agents after 问 Persona / 批准待处理: the most recently active live ones, Persona itself left out. */
object RecentAgents {
    const val COUNT = 2

    fun pick(sessions: Collection<Session>): List<Session> = sessions.filter { !it.assistant && it.isLive }
        .sortedWith(compareByDescending<Session> { it.updated ?: Instant.EPOCH }.thenBy { it.key.path }).take(COUNT)
}
