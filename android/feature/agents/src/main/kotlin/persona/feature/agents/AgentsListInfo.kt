package persona.feature.agents

import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.domain.sort.*
import persona.core.domain.answer.permissionSummary
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.*
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** Implement with the shared DraftStore for the active hub scope; never create a second draft store. */
fun interface AgentsDraftSource {
    fun observe(session: SessionKey): Flow<AgentDraft>
}

data class AgentDraft(val text: String = "", val hasAttachments: Boolean = false)

@OptIn(ExperimentalCoroutinesApi::class)
internal fun observeAgentDrafts(sessions: Flow<List<Session>>, source: AgentsDraftSource): Flow<Map<SessionKey, AgentDraft>> =
    sessions.map { rows -> rows.filterNot { it.assistant }.map { it.key }.distinct().sortedBy { it.path } }
        .distinctUntilChanged().flatMapLatest { keys ->
            if (keys.isEmpty()) flowOf(emptyMap())
            else combine(keys.map { key -> source.observe(key).map { key to it } }) { it.toMap() }
                .onStart { emit(emptyMap()) }
        }

internal fun AgentsMetadata.withOutbox(messages: List<OutboxMessage>): AgentsMetadata = copy(
    rows = rows + messages.filter { it.session.machineId.isNotBlank() && it.session.sessionId.isNotBlank() }
        .groupBy { it.session }.mapValues { (key, messages) ->
            (rows[key] ?: AgentRowMetadata()).withDelivery(messages, false)
        },
)

internal fun AgentRowMetadata.withDelivery(messages: List<OutboxMessage>, unknownRequest: Boolean): AgentRowMetadata = copy(
    needsConfirmation = needsConfirmation || unknownRequest || messages.any {
        it.state in setOf(DeliveryState.Unknown, DeliveryState.NeedsConfirmation, DeliveryState.Verifying)
    },
    pendingCount = maxOf(pendingCount, messages.count { it.state in setOf(DeliveryState.Queued, DeliveryState.WaitingForNetwork) }),
    sending = sending || messages.any { it.state == DeliveryState.Sending },
    verifying = verifying || messages.any { it.state == DeliveryState.Verifying },
)

internal fun visibleAgentRequests(
    pending: List<Interaction>, sessions: List<Session>, mutes: AgentPreferences, settings: AgentListSettings,
): List<Interaction> {
    val rows = sessions.associateBy { it.key }
    return pending.filter { request ->
        val session = rows[request.sessionKey]
        val row = session?.let { AgentSession(it) }
        session?.assistant != true && !mutes.mutes(request.sessionKey.path, row?.spacePreferenceKey) &&
            (settings.machineFilter.isEmpty() || request.machineId == settings.machineFilter) &&
            (settings.quick == AgentQuickFilter.All || (row?.let { settings.quick.matches(it) || it.id in settings.kept }
                ?: (settings.quick == AgentQuickFilter.NeedsYou))) &&
            (settings.query.isEmpty() || (session?.let { listOf(it.title, it.cwd, it.lastText, it.spaceName, it.slotName) }
                ?: listOf(request.sessionTitle, request.cwd, request.summary)).any { it?.contains(settings.query, ignoreCase = true) == true })
    }.sortedWith(compareBy<Interaction> { parseTime(it.expiresAt) ?: Instant.MAX }.thenBy { it.id })
}

internal val Interaction.cardTitle: String get() = when (kind) {
    "question" -> "有问题等你回答"
    "permission" -> "请求使用 ${toolName ?: "工具"}"
    "plan" -> "计划等你批准"
    "screen" -> "终端里有对话框等你处理"
    else -> "需要你处理"
}

/** Preserve the full supplied summary; use type-specific content when older hubs omit it. */
internal val Interaction.cardSummary: String get() = summary?.takeIf { it.isNotBlank() } ?: when (kind) {
    "question" -> questions.orEmpty().joinToString("\n\n") { it.question }
    "permission" -> permissionSummary().let { listOfNotNull(it.text, it.description).joinToString("\n") }
    "plan" -> plan.orEmpty()
    "screen" -> listOfNotNull(dialog?.question?.takeIf { it.isNotBlank() }, dialog?.detail?.takeIf { it.isNotBlank() })
        .joinToString("\n").ifBlank { screen.orEmpty() }
    else -> "打开会话查看请求"
}

/** Plain text for a short preview: drop Markdown heading, emphasis and code markers and blank lines. */
internal fun markdownPreview(text: String): String = text.lineSequence()
    .map { it.trim().replace(Regex("^#{1,6}\\s+"), "").replace("**", "").replace("`", "") }
    .filter { it.isNotBlank() }
    .joinToString("\n")

internal fun Session.spaceLocation(machine: String?): String {
    val space = spaceName?.takeIf { it.isNotBlank() } ?: spaceIndex?.let { "空间 $it" }
        ?: spaceId?.takeIf { it.isNotBlank() } ?: folderName.ifBlank { "未提供空间" }
    val slot = slotName?.takeIf { it.isNotBlank() } ?: slotIndex?.let { "标签页 $it" }
        ?: tabId?.takeIf { it.isNotBlank() } ?: slotId?.takeIf { it.isNotBlank() }
    return listOfNotNull(listOfNotNull(space, slot).joinToString(" › "), machine ?: machineId).joinToString(" · ")
}

internal fun LoopInfo.nextRunLabel(now: Instant, zone: ZoneId = ZoneId.systemDefault()): String {
    val next = parseTime(nextAt) ?: return "循环 · 下次时间待更新"
    val local = next.atZone(zone)
    val today = now.atZone(zone).toLocalDate()
    val format = when {
        local.toLocalDate() == today -> "HH:mm"
        local.year == today.year -> "M月d日 HH:mm"
        else -> "yyyy年M月d日 HH:mm"
    }
    return "循环 · 下次 ${local.format(DateTimeFormatter.ofPattern(format))}"
}
