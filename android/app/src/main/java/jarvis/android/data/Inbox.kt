package jarvis.android.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.time.Instant

/**
 * The inbox (docs/inbox.md): Feishu messages and mail as one list, one row per thing to deal with, triaged by the
 * hub's AI into 需要你 (action) / 知悉即可 (fyi) / 已折叠 (noise). Wire types mirror hub/inbox.go `InboxItem`.
 */
@Serializable
data class InboxItem(
    val id: String,
    val source: String = "",          // lark | mail
    val kind: String = "",            // dm | mention | group | bot | mail
    val account: String = "",
    @SerialName("account_name") val accountName: String = "",
    val title: String = "",
    val sender: String? = null,
    val avatar: String? = null,
    val preview: String = "",
    val count: Int = 0,
    @SerialName("started_at") val startedAt: String? = null,
    @SerialName("updated_at") val updatedAt: String? = null,
    val tier: String? = null,         // action | fyi | noise; empty until triaged
    val summary: String? = null,
    val reason: String? = null,
    val read: Boolean = false,
    @SerialName("done_at") val doneAt: String? = null,
    @SerialName("done_by") val doneBy: String? = null,
    val lark: InboxLark? = null,
    val mail: InboxMail? = null,
    val agent: InboxAgent? = null,
    /** Only in GET /inbox/{id}: this thing as plain text (the conversation, the mail), for 交给 AI. */
    val context: String? = null,
) {
    val updated: Instant? get() = parseTime(updatedAt)
    val isDone: Boolean get() = parseTime(doneAt) != null
    /** Not triaged yet counts as 需要你, as on iOS: better shown than missed. */
    val bucket: InboxBucket
        get() = when {
            isDone -> InboxBucket.Done
            tier == "fyi" -> InboxBucket.Fyi
            tier == "noise" -> InboxBucket.Noise
            else -> InboxBucket.Action
        }
    val kindLabel: String
        get() = when (kind) {
            "dm" -> "私聊"
            "mention" -> when {
                read -> "群聊" // read elsewhere: the @ is no longer unread
                lark?.atAll == true -> "@所有人"
                else -> "@我"
            }
            "group" -> "群聊"
            "bot" -> "机器人"
            "mail" -> if (mail?.direct == true) "邮件" else "抄送"
            else -> kind
        }
    val sourceLabel: String get() = if (source == "lark") "飞书" else accountName.ifBlank { "邮件" }
}

@Serializable
data class InboxLark(
    @SerialName("feed_id") val feedId: String = "",
    @SerialName("is_group") val isGroup: Boolean = false,
    @SerialName("start_position") val startPosition: String? = null,
    @SerialName("at_all") val atAll: Boolean = false,
)

@Serializable
data class InboxMail(
    @SerialName("account_id") val accountId: String = "",
    val mailbox: String = "INBOX",
    val uids: List<Long> = emptyList(),
    val direct: Boolean = false,
)

/** The AI 分身: model 2 decided to hand it to an agent, which drafts a reply (never sends it). */
@Serializable
data class InboxAgent(
    val state: String = "",           // skipped | queued | running | drafted | failed
    val task: String? = null,
    val draft: String? = null,
    val note: String? = null,
    val error: String? = null,
    @SerialName("session_id") val sessionId: String? = null,
)

enum class InboxBucket(val title: String) { Action("需要你"), Fyi("知悉即可"), Noise("已折叠"), Done("已处理") }

/** The source / kind filters at the top of the list (iOS InboxView). */
sealed interface InboxFilter {
    val title: String
    fun matches(it: InboxItem): Boolean

    data object All : InboxFilter { override val title = "全部"; override fun matches(it: InboxItem) = true }
    data object Feishu : InboxFilter { override val title = "飞书"; override fun matches(it: InboxItem) = it.source == "lark" }
    data class Account(val id: String, override val title: String) : InboxFilter {
        override fun matches(it: InboxItem) = it.source == "mail" && it.account == id
    }
    data object Mention : InboxFilter { override val title = "@我"; override fun matches(it: InboxItem) = it.kind == "mention" }
    data object Dm : InboxFilter { override val title = "私聊"; override fun matches(it: InboxItem) = it.kind == "dm" }
    data object Group : InboxFilter { override val title = "群聊"; override fun matches(it: InboxItem) = it.kind == "group" }
    data object Mail : InboxFilter { override val title = "邮件"; override fun matches(it: InboxItem) = it.source == "mail" }

    companion object {
        /** All / 飞书 / one per mail account | @我 / 私聊 / 群聊 / 邮件 — only the ones that have something. */
        fun available(items: List<InboxItem>): List<InboxFilter> {
            val accounts = items.filter { it.source == "mail" }.distinctBy { it.account }.map { Account(it.account, it.accountName.ifBlank { "邮箱" }) }
            return (listOf(All, Feishu) + accounts + listOf(Mention, Dm, Group, Mail)).filter { f -> f == All || items.any(f::matches) }
        }
    }
}

/** Buckets in display order with their items, newest first; search over title, sender, preview and summary. */
fun List<InboxItem>.bucketed(filter: InboxFilter, query: String): List<Pair<InboxBucket, List<InboxItem>>> {
    val q = query.trim()
    val shown = filter { filter.matches(it) }.filter { q.isEmpty() || listOfNotNull(it.title, it.sender, it.preview, it.summary).any { s -> s.contains(q, true) } }
    return InboxBucket.entries.mapNotNull { b ->
        shown.filter { it.bucket == b }.sortedByDescending { it.updated ?: Instant.EPOCH }.takeIf { it.isNotEmpty() }?.let { b to it }
    }
}
