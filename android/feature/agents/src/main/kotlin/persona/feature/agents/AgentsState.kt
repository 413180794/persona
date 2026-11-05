package persona.feature.agents

import persona.core.designsystem.PendingStatus
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.*
import persona.core.contracts.*
import persona.core.contracts.api.HubPreferences
import persona.core.domain.sort.*
import persona.core.domain.answer.requestSourceLabel
import persona.core.domain.answer.RequestExpiry
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.first
import kotlinx.serialization.json.*
import java.time.Duration
import java.time.Instant
import javax.inject.Inject
import javax.inject.Singleton

private fun AgentPreferences.hubJson() = buildJsonObject {
    put("spaces", JsonArray(spaces.map(::JsonPrimitive)))
    put("sessions", JsonArray(sessions.map(::JsonPrimitive)))
    put("allowed", JsonArray(allowed.sorted().map(::JsonPrimitive)))
}.toString()

private fun hubPreferences(raw: String): AgentPreferences {
    val json = JsonCodec.parseToJsonElement(raw).jsonObject
    fun strings(key: String) = (json[key] as? JsonArray).orEmpty().mapNotNull { (it as? JsonPrimitive)?.contentOrNull }
    return AgentPreferences(strings("spaces"), strings("sessions"), strings("allowed").toSet())
}

/** Display settings stay local; pins, mutes and read receipts belong to the paired hub. */
@Singleton
class AgentsPreferences @Inject constructor(private val store: DataStore<Preferences>, private val hub: HubPreferences) {
    val snapshots: Flow<AgentsPreferencesSnapshot> get() = snapshots("")
    fun snapshots(scope: String): Flow<AgentsPreferencesSnapshot> = combine(store.data, hub.preferences) { p, remote ->
        AgentsPreferencesSnapshot(
            range = AgentRange.entries.firstOrNull { it.name == p[agentRangeKey(scope)] } ?: AgentRange.Running,
            grouping = AgentGrouping.fromPreference(p[stringPreferencesKey("grouping")]),
            compact = p[booleanPreferencesKey("compact")] ?: false,
            pins = AgentPreferences(remote.pins.spaces, remote.pins.sessions, remote.pins.allowed),
            mutes = AgentPreferences(remote.mutes.spaces, remote.mutes.sessions, remote.mutes.allowed),
            seen = remote.seen,
        )
    }
    /** Hub pins and mutes as last read for each pairing scope. */
    internal val hubCache: AgentPreferencesCache = object : AgentPreferencesCache {
        override suspend fun read(scope: String): Pair<AgentPreferences, AgentPreferences>? {
            val p = store.data.first()
            val pins = p[stringPreferencesKey("hub-pins:$scope")] ?: return null
            val mutes = p[stringPreferencesKey("hub-mutes:$scope")] ?: return null
            return hubPreferences(pins) to hubPreferences(mutes)
        }
        override suspend fun write(scope: String, pins: AgentPreferences, mutes: AgentPreferences) {
            store.edit {
                it[stringPreferencesKey("hub-pins:$scope")] = pins.hubJson()
                it[stringPreferencesKey("hub-mutes:$scope")] = mutes.hubJson()
            }
        }
    }
    suspend fun grouping(value: AgentGrouping) { store.edit { it[stringPreferencesKey("grouping")] = value.name } }
    suspend fun range(scope: String, value: AgentRange) { store.edit { it[agentRangeKey(scope)] = value.name } }
    suspend fun compact(value: Boolean) { store.edit { it[booleanPreferencesKey("compact")] = value } }
    suspend fun togglePin(id: String) = hub.togglePin(id)
    suspend fun toggleMute(id: String) = hub.toggleMute(id)
    suspend fun markSeen(sessions: List<Session>) {
        sessions.forEach { hub.markSeen(it.key, it.readVersion) }
    }
}

data class AgentsPreferencesSnapshot(
    val grouping: AgentGrouping = AgentGrouping.Repo,
    val compact: Boolean = false,
    val pins: AgentPreferences = AgentPreferences(),
    val mutes: AgentPreferences = AgentPreferences(),
    val seen: Map<String, String> = emptyMap(),
    val range: AgentRange = AgentRange.Running,
)

/** Shared metadata supplied by the composition root for the active hub scope. */
data class AgentsMetadata(
    val rows: Map<SessionKey, AgentRowMetadata> = emptyMap(),
    val requestSources: Map<String, String> = emptyMap(),
)

data class AgentRowMetadata(
    val draft: String = "",
    val hasAttachments: Boolean = false,
    val needsConfirmation: Boolean = false,
    val pendingCount: Int = 0,
    val sending: Boolean = false,
    val verifying: Boolean = false,
) {
    val deliveryLabels: List<String> get() = buildList {
        if (needsConfirmation) add(if (verifying) PendingStatus.Verifying.label else PendingStatus.Unconfirmed.label)
        if (pendingCount > 0) add("${PendingStatus.Waiting.label} · $pendingCount")
        if (sending) add(PendingStatus.Sending.label)
    }
    val draftLabel: String? get() = when {
        draft.isNotBlank() -> "草稿 · ${if (hasAttachments) "[附件] " else ""}${draft.replace('\n', ' ').take(20)}"
        hasAttachments -> "草稿 · [附件]"
        else -> null
    }
}

private val Session.readVersion: String get() = "${updatedAt.orEmpty()}|${rev ?: ""}"
fun Session.listSession(preferences: AgentsPreferencesSnapshot): AgentSession {
    val visible = if (agentState == AgentState.Unread && preferences.seen[key.path] == readVersion) copy(state = "idle") else this
    return AgentSession(visible, stateSince, subagents.orEmpty().any { it.running == true })
}

/** Keep opened rows in their old group and slot while the quick filter remains selected. */
internal fun retainOpenedRows(
    current: AgentListResult,
    previous: AgentListResult?,
    kept: Set<String>,
): AgentListResult {
    if (previous == null || kept.isEmpty()) return current
    val currentRows = current.groups.flatMap { it.rows }.associateBy { it.id }
    // 按仓库: a repository with working trees takes no rows of its own, and a repository or tree group that is gone is
    // not brought back as a stray section outside its computer; such a row stays where the list put it now.
    val currentIds = current.groups.map { it.id }.toSet()
    val parents = current.groups.mapNotNull { it.under }.toSet()
    val homes = previous.groups.filter { it.id !in parents && (it.id in currentIds || it.repo == null) }.map { it.id }.toSet()
    val retained = kept.intersect(currentRows.keys).filter { id -> previous.groups.any { it.id in homes && it.rows.any { row -> row.id == id } } }.toSet()
    val groups = current.groups.map { group -> group.copy(
        rows = group.rows.filterNot { it.id in retained },
        visibleRows = group.visibleRows.filterNot { it.id in retained },
    ) }.toMutableList()
    previous.groups.forEachIndexed { groupIndex, old ->
        if (old.id !in homes) return@forEachIndexed
        val rowsToKeep = old.rows.filter { it.id in retained }
        if (rowsToKeep.isEmpty()) return@forEachIndexed
        val index = groups.indexOfFirst { it.id == old.id }
        val group = if (index >= 0) groups[index] else old.copy(rows = emptyList(), visibleRows = emptyList())
        fun insert(existing: List<AgentSession>, oldRows: List<AgentSession>): List<AgentSession> {
            val result = existing.toMutableList()
            oldRows.forEachIndexed { position, row ->
                if (row.id in retained) result.add(position.coerceAtMost(result.size), currentRows.getValue(row.id))
            }
            return result
        }
        val restored = group.copy(rows = insert(group.rows, old.rows), visibleRows =
            if (group.folded || current.machines.any { it.folded && group.id in it.groups }) emptyList()
            else insert(group.visibleRows, if (old.folded) old.rows else old.visibleRows))
        if (index >= 0) groups[index] = restored else groups.add(groupIndex.coerceAtMost(groups.size), restored)
    }
    // A repository with working trees holds no rows itself; it stays while a tree under it does.
    return current.copy(groups = groups.filter { group -> group.rows.isNotEmpty() || groups.any { it.under == group.id && it.rows.isNotEmpty() } })
}

internal fun Interaction.quickQuestion(): Question? = questions?.singleOrNull()?.takeIf {
    kind == "question" && !it.multiSelect && it.options.size in 1..4 && secret != true
}

internal fun Interaction.sourceLabel(metadata: AgentsMetadata): String? =
    (if (kind == "screen") "终端" else requestSourceLabel()) ?: metadata.requestSources[id]?.trim()?.takeIf(String::isNotEmpty)

internal fun countdown(expiresAt: String?, now: Instant): String? = RequestExpiry.label(expiresAt, now)
internal fun Interaction.expired(now: Instant): Boolean = RequestExpiry.expired(expiresAt, now)
internal fun relativeTime(time: Instant?, now: Instant): String {
    time ?: return ""
    val elapsed = Duration.between(time, now)
    return when {
        elapsed.seconds < 60 -> "刚刚"
        elapsed.toMinutes() < 60 -> "${elapsed.toMinutes()} 分钟前"
        elapsed.toHours() < 24 -> "${elapsed.toHours()} 小时前"
        else -> "${elapsed.toDays()} 天前"
    }
}
