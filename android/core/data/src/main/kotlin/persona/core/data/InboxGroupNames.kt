package persona.core.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.serialization.json.*
import persona.core.contracts.api.InboxAssistantTransport
import java.util.logging.Logger

/** Conversation metadata is account-scoped; event titles and mute labels are not conversation names. */
internal class InboxGroupNames(private val now: () -> Long = System::currentTimeMillis) {
    private data class Account(val scope: String, val id: Int?)
    private data class Names(val values: Map<String, String>, val retryAt: Long)
    @Volatile private var cached = emptyMap<Account, Names>()

    fun name(item: JsonObject, scope: String): String? {
        val lark = group(item) ?: return null
        return cached[Account(scope, account(lark))]?.values?.get(lark.text("feed_id"))
    }

    suspend fun refresh(items: List<JsonObject>, transport: InboxAssistantTransport) {
        cached = cached.filterKeys { it.scope == transport.scope }
        for ((id, rows) in items.mapNotNull(::group).groupBy(::account)) {
            val key = Account(transport.scope, id)
            if ((cached[key]?.retryAt ?: 0) > now()) continue
            val found = mutableMapOf<String, String>()
            try {
                val names = withTimeout(10_000) {
                    val wanted = rows.mapNotNull { it.text("feed_id") }.toSet()
                    val seen = mutableSetOf<String>()
                    var cursor: String? = null
                    do {
                        val page = transport.larkConversationsForAccount(id, cursor)
                        (page["conversations"] as? JsonArray).orEmpty().filterIsInstance<JsonObject>().forEach { chat ->
                            val feed = chat.text("id")
                            val name = chat.text("name")?.trim()?.takeIf { it.isNotEmpty() }
                            if (feed in wanted && name != null && chat["is_group"]?.jsonPrimitive?.booleanOrNull == true) {
                                found[requireNotNull(feed)] = name
                            }
                        }
                        cursor = page.text("next_cursor")?.takeIf { it.isNotBlank() }
                        check(cursor == null || seen.add(cursor)) { "Conversation cursor did not advance" }
                    } while (cursor != null && !found.keys.containsAll(wanted))
                    found.toMap()
                }
                cached = cached + (key to Names(names, now() + CACHE_MILLIS))
            } catch (error: TimeoutCancellationException) {
                currentCoroutineContext().ensureActive()
                failed(key, error, found)
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                failed(key, error, found)
            }
        }
    }

    private fun failed(key: Account, error: Exception, found: Map<String, String>) {
        // Metadata failure must not prevent inbox messages from loading. Retain only previously verified names.
        Logger.getLogger("InboxGroupNames").warning("Conversation names unavailable: ${error.javaClass.simpleName}")
        cached = cached + (key to Names(cached[key]?.values.orEmpty() + found, now() + RETRY_MILLIS))
    }

    private fun group(item: JsonObject): JsonObject? {
        if (item.text("kind") !in setOf("group", "mention", "bot")) return null
        val lark = item["lark"] as? JsonObject ?: return null
        return lark.takeUnless { it["is_group"]?.jsonPrimitive?.booleanOrNull == false }
    }
    private fun account(lark: JsonObject) = lark["ol_account"]?.jsonPrimitive?.intOrNull?.takeIf { it > 0 }
    private fun JsonObject.text(key: String) = (this[key] as? JsonPrimitive)?.contentOrNull

    private companion object {
        const val CACHE_MILLIS = 5 * 60_000L
        const val RETRY_MILLIS = 30_000L
    }
}
