package persona.core.domain.views

import java.util.UUID
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.longOrNull
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.AssistantViewAnswer
import persona.core.contracts.api.TasksApi
import persona.core.contracts.generated.ExtensionAssistantHTMLActionInput
import persona.core.contracts.generated.ExtensionAssistantHTMLActionKind
import persona.core.contracts.generated.ExtensionViewAction
import persona.core.contracts.generated.ExtensionViewActionInput

// The cards of one hub's assistant conversations (docs/assistant/assistant-views.md "Live cards" "Persisted submissions"): the live cards, the
// answers and the registered reply blocks, from the feed's full snapshots and the hub's events. One store per hub
// (TasksStore is), and within it everything is kept per conversation: a card of one never shows in another.

/** A live card as the hub keeps it: [view] is the whole AssistantView JSON, [version] orders its replacements. */
data class LiveCard(val id: String, val version: Long, val view: JsonElement, val updatedAt: String? = null)

/** One conversation's cards: live ones by id, answers by card key, and the reply blocks the hub registered. */
data class ConversationCards(
    val views: Map<String, LiveCard> = emptyMap(),
    val answers: Map<String, ExtensionViewAction> = emptyMap(),
    val blocks: Set<String> = emptySet(),
)

class AssistantViewStore {
    private val _state = MutableStateFlow<Map<String, ConversationCards>>(emptyMap())
    val state: StateFlow<Map<String, ConversationCards>> = _state.asStateFlow()

    fun of(conversation: String): ConversationCards = _state.value[conversation] ?: ConversationCards()

    /**
     * The feed's full snapshot of [conversation]. A card or answer the snapshot has older than what an event already
     * brought stays as it is (the snapshot may have been read before the event); one it does not name stays too.
     */
    fun snapshot(conversation: String, views: Map<String, JsonElement>, answers: Map<String, JsonElement>, blocks: List<String>) {
        val cards = views.values.mapNotNull(::liveCard)
        val read = answers.values.mapNotNull(::answer)
        change(conversation) { c ->
            c.copy(views = cards.fold(c.views) { all, v -> putView(all, v) },
                answers = read.fold(c.answers) { all, a -> putAnswer(all, a) },
                blocks = c.blocks + blocks.filter { it.isNotEmpty() })
        }
    }

    /** `assistant_view`: a live card created or replaced; an older version than the one shown is dropped. */
    fun view(conversation: String, raw: JsonElement) {
        val card = liveCard(raw) ?: return
        change(conversation) { it.copy(views = putView(it.views, card)) }
    }

    /** `assistant_view_action` (or the hub's answer to this device): a card's answer, ordered by its revision. */
    fun answer(conversation: String, raw: JsonElement) {
        answer(raw)?.let { answer(conversation, it) }
    }

    fun answer(conversation: String, action: ExtensionViewAction) {
        if (action.key.isEmpty()) return
        change(conversation) { it.copy(answers = putAnswer(it.answers, action)) }
    }

    /** `assistant_view_block`: the hub registered a reply block from a trusted transcript. */
    fun block(conversation: String, key: String) {
        if (key.isNotEmpty()) change(conversation) { it.copy(blocks = it.blocks + key) }
    }

    private fun change(conversation: String, f: (ConversationCards) -> ConversationCards) {
        if (conversation.isEmpty()) return
        _state.update { all ->
            val old = all[conversation] ?: ConversationCards()
            val new = f(old)
            if (new == old) all else all + (conversation to new)
        }
    }

    companion object {
        fun liveCard(raw: JsonElement): LiveCard? {
            val o = raw as? JsonObject ?: return null
            val id = (o["id"] as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull?.takeIf { it.isNotEmpty() } ?: return null
            val version = (o["version"] as? JsonPrimitive)?.takeIf { !it.isString }?.longOrNull?.takeIf { it > 0 } ?: return null
            val view = o["view"] as? JsonObject ?: return null
            return LiveCard(id, version, view, (o["updated_at"] as? JsonPrimitive)?.contentOrNull)
        }

        fun answer(raw: JsonElement): ExtensionViewAction? =
            runCatching { JsonCodec.decodeFromJsonElement(ExtensionViewAction.serializer(), raw) }.getOrNull()?.takeIf { it.key.isNotEmpty() }

        private fun putView(all: Map<String, LiveCard>, card: LiveCard): Map<String, LiveCard> {
            val old = all[card.id]
            return if (old != null && old.version >= card.version) all else all + (card.id to card)
        }

        private fun putAnswer(all: Map<String, ExtensionViewAction>, a: ExtensionViewAction): Map<String, ExtensionViewAction> =
            if (newer(all[a.key], a)) all + (a.key to a) else all

        /**
         * Whether [new] replaces [old]: by the hub's revision (never by how far the status got); an older hub's answers
         * without one by when they changed.
         */
        fun newer(old: ExtensionViewAction?, new: ExtensionViewAction): Boolean {
            if (old == null) return true
            val a = old.revision
            val b = new.revision
            return when {
                a != null && b != null -> b > a
                b != null -> true
                a != null -> false
                else -> (new.updatedAt ?: new.at) > (old.updatedAt ?: old.at)
            }
        }
    }
}

/** What became of a card's action, for the card to say. */
sealed interface CardOutcome {
    /** The hub recorded it (or had already recorded the same request). */
    data object Recorded : CardOutcome

    /** Another answer was there first: the card shows that one. */
    data object AlreadyAnswered : CardOutcome

    /** The card changed since the user saw it: drawn again, nothing sent; the user looks and confirms again. */
    data object Changed : CardOutcome

    /** An HTML page's `open` the hub checked: open [url] in the app. */
    data class Open(val url: String) : CardOutcome

    /** Another action of the card is still on its way: nothing was sent. */
    data object Busy : CardOutcome

    /** The hub did not say it takes this (no `assistant_views` / `assistant_html_bridge`): nothing was sent. */
    data object Unsupported : CardOutcome

    /** It did not go: why, in the hub's words when it said (else null); the same request can be tried again. */
    data class Failed(val message: String?) : CardOutcome
}

/**
 * Sends a card's actions for one hub: the version the user saw goes with each, a request tried again keeps its
 * `client_msg_id` (so the hub records it once), and a refusal tells the card what to draw. [reload] pulls the
 * conversation's feed again (the cards' current snapshot).
 */
class AssistantCardActions(
    private val api: TasksApi,
    private val store: AssistantViewStore,
    private val reload: (conversation: String) -> Unit,
    private val newId: () -> String = { "view-" + UUID.randomUUID() },
) {
    private data class Attempt(val fingerprint: String, val id: String)

    /**
     * What the hub these actions go to said it takes, from its hello. A card answers ([answers]) only on a hub that
     * says `assistant_views` — an older hub or one with cards switched off is never sent an action it cannot read —
     * and an HTML page's bridge only with `assistant_html_bridge` too.
     */
    val features: StateFlow<Set<String>> = api.features ?: MutableStateFlow(emptySet())

    fun answers(features: Set<String> = this.features.value) = Views in features
    fun bridges(features: Set<String> = this.features.value) = Views in features && HtmlBridge in features

    companion object {
        const val Views = "assistant_views"
        const val HtmlBridge = "assistant_html_bridge"
    }

    private val attempts = HashMap<String, Attempt>()
    private val _busy = MutableStateFlow<Set<String>>(emptySet())
    /** `conversation|key` of the cards with an action on its way. */
    val busy: StateFlow<Set<String>> = _busy.asStateFlow()

    fun busyKey(conversation: String, key: String) = "$conversation|$key"

    /** The id for this exact request: the same as last time while nothing about it changed. */
    @Synchronized
    private fun clientId(card: String, fingerprint: String): String {
        val old = attempts[card]
        if (old != null && old.fingerprint == fingerprint) return old.id
        return Attempt(fingerprint, newId()).also { attempts[card] = it }.id
    }

    suspend fun submit(conversation: String, key: String, action: AssistantAction, values: Map<String, String>,
                       confirmed: Boolean, version: Long): CardOutcome {
        if (!answers()) return CardOutcome.Unsupported
        val card = busyKey(conversation, key)
        val sent = values.toSortedMap()
        val id = clientId("$card|action", listOf(action.id, action.page.orEmpty(), sent.toString(), confirmed, version).joinToString("\u0000"))
        return send(conversation, card) {
            api.viewAction(conversation, key, ExtensionViewActionInput(action = action.id, page = action.page, values = sent.takeIf { it.isNotEmpty() },
                clientMsgId = id, confirmed = confirmed.takeIf { it }, expectedVersion = version))
        }
    }

    /** An HTML page's reply ([text]) or link ([url]) through its bridge, after the user confirmed it in the app. */
    suspend fun html(conversation: String, key: String, componentKey: String, page: String?, version: Long,
                     text: String? = null, url: String? = null): CardOutcome {
        if (!bridges()) return CardOutcome.Unsupported
        val kind = if (text != null) ExtensionAssistantHTMLActionKind.REPLY else ExtensionAssistantHTMLActionKind.OPEN
        val card = busyKey(conversation, key)
        // A reply and a link each keep their own id: opening a link between two tries of a reply leaves its id as it was.
        val id = clientId("$card|html-$kind", listOf(componentKey, page.orEmpty(), text ?: url.orEmpty(), version).joinToString("\u0000"))
        return send(conversation, card) {
            api.viewHtmlAction(conversation, key, ExtensionAssistantHTMLActionInput(kind = kind, componentKey = componentKey, page = page,
                expectedVersion = version, text = text, url = url, clientMsgId = id, confirmed = true))
        }
    }

    private suspend fun send(conversation: String, card: String, call: suspend () -> AssistantViewAnswer): CardOutcome {
        if (card in _busy.value) return CardOutcome.Busy
        _busy.update { it + card }
        return try {
            when (val answer = call()) {
                is AssistantViewAnswer.Recorded -> { store.answer(conversation, answer.action); CardOutcome.Recorded }
                is AssistantViewAnswer.AlreadyAnswered -> {
                    answer.action?.let { store.answer(conversation, it) }
                    reload(conversation)
                    CardOutcome.AlreadyAnswered
                }
                is AssistantViewAnswer.ViewChanged -> {
                    answer.view?.let { v ->
                        store.view(conversation, JsonCodec.encodeToJsonElement(persona.core.contracts.generated.ExtensionAssistantView.serializer(), v))
                    }
                    reload(conversation)
                    CardOutcome.Changed
                }
                is AssistantViewAnswer.Open -> CardOutcome.Open(answer.url)
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            CardOutcome.Failed(e.message?.takeIf { it.isNotBlank() })
        } finally {
            _busy.update { it - card }
        }
    }
}

/** What a conversation's cards need on screen: its id, the hub's store and actions, and whether it may answer. */
class AssistantCardsHost(
    val conversation: String,
    val store: AssistantViewStore,
    val actions: AssistantCardActions,
    /** False for a read-only device or conversation: cards draw, nothing answers. */
    val interactive: Boolean,
)
