package persona.feature.settings

import android.content.res.Resources
import persona.core.contracts.api.HubExtension
import persona.core.contracts.api.ExtensionConfigRejected
import persona.core.contracts.api.ExtensionDetail
import persona.core.contracts.api.ExtensionManagement
import persona.core.designsystem.Status
import persona.core.designsystem.failureReason
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

// 设置 › 扩展 (docs/extensions.md「App：设置 › 扩展」): the words, the draft of an extension's configuration, and the two
// pages' models. The pages are in ExtensionsScreen.kt.

/** What an extension is called and says in the settings. */
internal object ExtensionText {
    /** Running or idle until something needs it (stopped) is all one to the user: it is on. */
    fun state(res: Resources, p: HubExtension): String = when (p.status) {
        "running", "stopped" -> res.getString(R.string.settings_ext_state_on)
        "starting" -> res.getString(R.string.settings_ext_state_starting)
        "needs_setup" -> res.getString(R.string.settings_ext_state_needs_setup)
        "disabled" -> res.getString(R.string.settings_ext_state_off)
        "incompatible" -> res.getString(R.string.settings_ext_state_incompatible)
        "error" -> res.getString(R.string.settings_ext_state_error)
        else -> res.getString(R.string.settings_ext_state_unknown)
    }

    /** Whether an enabled extension's program runs right now (the detail page's small print); null for any other state. */
    fun runNote(res: Resources, p: HubExtension): String? = when {
        p.kind == "extension_pack" -> res.getString(R.string.settings_ext_run_pack)
        p.status == "running" -> res.getString(R.string.settings_ext_run_running)
        p.status == "stopped" -> res.getString(R.string.settings_ext_run_idle)
        else -> null
    }

    /** Broken: drawn in the danger colour, not as a status dot. */
    fun isProblem(p: HubExtension): Boolean = p.status == "error" || p.status == "incompatible"

    /** The dot beside the state: green on (running or idle), amber waiting on the user, blue starting, grey otherwise. */
    fun status(p: HubExtension): Status = when (p.status) {
        "running", "stopped" -> Status.Unread
        "needs_setup" -> Status.NeedsYou
        "starting" -> Status.Working
        else -> Status.Offline
    }

    fun source(res: Resources, s: String?): String? = when (s) {
        "builtin" -> res.getString(R.string.settings_ext_source_builtin)
        "hub.json" -> "hub.json"
        "installed" -> res.getString(R.string.settings_ext_source_installed)
        else -> null
    }

    fun point(res: Resources, name: String): String = when (name) {
        "connectors" -> res.getString(R.string.settings_ext_point_connectors)
        "channel" -> res.getString(R.string.settings_ext_point_channel)
        "voice" -> res.getString(R.string.settings_ext_point_voice)
        "ui" -> res.getString(R.string.settings_ext_point_ui)
        "quota" -> res.getString(R.string.settings_ext_point_quota)
        "providers" -> res.getString(R.string.settings_ext_point_providers)
        "events" -> res.getString(R.string.settings_ext_point_events)
        "assistant" -> res.getString(R.string.settings_ext_point_assistant)
        "api" -> res.getString(R.string.settings_ext_point_api)
        "skills" -> res.getString(R.string.settings_ext_point_skills)
        "prompts" -> res.getString(R.string.settings_ext_point_prompts)
        "themes" -> res.getString(R.string.settings_ext_point_themes)
        "triage_templates" -> res.getString(R.string.settings_ext_point_triage_templates)
        "agent_filters" -> res.getString(R.string.settings_ext_point_agent_filters)
        "computer" -> res.getString(R.string.settings_ext_point_computer)
        "browser" -> res.getString(R.string.settings_ext_point_browser)
        "widgets" -> res.getString(R.string.settings_ext_point_widgets)
        "shortcuts" -> res.getString(R.string.settings_ext_point_shortcuts)
        else -> name
    }

    /** The detail's state line: the state, the version, where it came from. */
    fun stateLine(res: Resources, p: HubExtension): String =
        listOfNotNull(state(res, p), p.version?.takeIf { it.isNotBlank() }, source(res, p.source)).joinToString(" · ")

    /** 设置's row: how many, and how many need the user; null before the list is in. */
    fun listSummary(res: Resources, list: List<HubExtension>?): String? {
        list ?: return null
        if (list.isEmpty()) return res.getString(R.string.settings_ext_summary_none)
        val attention = list.count { it.status == "needs_setup" || isProblem(it) }
        val count = res.getQuantityString(R.plurals.settings_ext_summary_count, list.size, list.size)
        return if (attention > 0) res.getString(R.string.settings_ext_summary_attention, count,
            res.getQuantityString(R.plurals.settings_ext_summary_to_handle, attention, attention)) else count
    }

    /** A field's title, marked when it must be filled and has no default. */
    fun fieldTitle(res: Resources, field: persona.core.contracts.api.ExtensionField): String =
        if (field.required == true && field.fallback == null) res.getString(R.string.settings_ext_field_required, field.label) else field.label

    /** What an empty field shows: the default it falls back to, or 未设置. */
    fun placeholder(res: Resources, field: persona.core.contracts.api.ExtensionField): String {
        val fallback = field.fallback ?: return res.getString(R.string.settings_ext_not_set)
        val text = field.enum?.firstOrNull { it.value == fallback }?.label ?: (fallback as? JsonPrimitive)?.content
        return if (text.isNullOrEmpty()) res.getString(R.string.settings_ext_not_set) else res.getString(R.string.settings_ext_default_value, text)
    }

    fun secretState(res: Resources, secret: persona.core.contracts.api.ExtensionSecret): String = when {
        secret.set -> res.getString(R.string.settings_ext_secret_set)
        secret.required == true -> res.getString(R.string.settings_ext_secret_required)
        else -> res.getString(R.string.settings_ext_not_set)
    }
}

/** The configuration being edited, by dotted path ("proxy.host"); an empty group goes with its last value. */
internal object ExtensionDraft {
    fun value(draft: JsonObject, path: List<String>): JsonElement? {
        var current: JsonElement = draft
        for (key in path) current = (current as? JsonObject)?.get(key) ?: return null
        return current.takeIf { it != JsonNull }
    }

    fun set(draft: JsonObject, path: List<String>, value: JsonElement?): JsonObject {
        val key = path.firstOrNull() ?: return draft
        val out = draft.toMutableMap()
        val next = if (path.size == 1) value else set(draft[key] as? JsonObject ?: JsonObject(emptyMap()), path.drop(1), value).takeIf { it.isNotEmpty() }
        if (next == null) out.remove(key) else out[key] = next
        return JsonObject(out)
    }

    /** Without empty strings and lists the user cleared: "not set" (the schema's default applies). */
    fun compact(obj: JsonObject): JsonObject = JsonObject(obj.mapNotNull { (key, value) ->
        when {
            value == JsonNull -> null
            value is JsonPrimitive && value.isString && value.content.isEmpty() -> null
            value is JsonArray && value.isEmpty() -> null
            value is JsonObject -> compact(value).takeIf { it.isNotEmpty() }?.let { key to it }
            else -> key to value
        }
    }.toMap())

    /** The dotted path of every field the form draws (an object's fields under its name). */
    fun paths(form: List<persona.core.contracts.api.ExtensionField>): Set<String> = form.flatMap { field ->
        if (field.type == "object") field.fields.orEmpty().map { "${field.name}.${it.name}" } else listOf(field.name)
    }.toSet()

    /** A number as typed: its value, or why it is not one (blank is "not set"). */
    fun number(res: Resources, text: String, integer: Boolean): Pair<JsonPrimitive?, String?> {
        val t = text.trim()
        if (t.isEmpty()) return null to null
        val n = t.toDoubleOrNull()?.takeIf { it.isFinite() } ?: return null to res.getString(if (integer) R.string.settings_ext_number_integer else R.string.settings_ext_number_any)
        val whole = n == Math.floor(n) && kotlin.math.abs(n) < 1e15
        if (integer && !whole) return null to res.getString(R.string.settings_ext_number_integer)
        return (if (whole) JsonPrimitive(n.toLong()) else JsonPrimitive(n)) to null
    }
}

internal data class ExtensionListState(
    val extensions: List<HubExtension>? = null,
    /** The hub has extension markets (`markets: true`): the list opens 扩展市场. */
    val markets: Boolean = false,
    val loading: Boolean = false,
    val error: String? = null,
)

/** 设置 › 扩展: the list. */
internal class ExtensionListModel(private val api: ExtensionManagement, private val res: Resources) {
    private val mutable = MutableStateFlow(ExtensionListState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val listing = api.listing()
            mutable.update { it.copy(extensions = listing.extensions, markets = listing.markets, error = null) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = res.getString(R.string.settings_ext_load_failed, failureReason(error))) } }
        finally { mutable.update { it.copy(loading = false) } }
    }
}

internal data class ExtensionState(
    val detail: ExtensionDetail? = null,
    /** The configuration being edited; saved as a whole (PUT replaces it). */
    val draft: JsonObject = JsonObject(emptyMap()),
    /** What number fields hold as typed, by path, until saved. */
    val numberText: Map<String, String> = emptyMap(),
    /** Number fields typed into that do not read as one, by path. */
    val numberProblems: Map<String, String> = emptyMap(),
    /** The hub's word on a field it refused, by its dotted path. */
    val fieldErrors: Map<String, String> = emptyMap(),
    val loading: Boolean = false,
    val saving: Boolean = false,
    /** A switch or secret call is on its way. */
    val busy: Boolean = false,
    val error: String? = null,
) {
    val editable: Boolean get() = detail?.readOnly == false
    val dirty: Boolean get() = detail != null && (draft != detail.config || numberProblems.isNotEmpty())
    val canSave: Boolean get() = dirty && numberProblems.isEmpty() && !saving && !busy

    fun value(path: List<String>): JsonElement? = ExtensionDraft.value(draft, path)

    fun fieldError(path: List<String>): String? = path.joinToString(".").let { fieldErrors[it] ?: numberProblems[it] }
}

/** 设置 › 扩展 › one: its switch, its form (a draft saved as a whole), its secrets. */
internal class ExtensionModel(private val api: ExtensionManagement, private val id: String, private val res: Resources) {
    private val mutable = MutableStateFlow(ExtensionState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try { run { api.extension(id) } } finally { mutable.update { it.copy(loading = false) } }
    }

    suspend fun setEnabled(on: Boolean) = busy { run { api.setEnabled(id, on) } }
    suspend fun setContribution(item: persona.core.contracts.api.ExtensionContribution, on: Boolean) = busy {
        run { api.setContribution(id, item.point, item.id, on) }
    }

    fun edit(path: List<String>, value: JsonElement?) = mutable.update {
        val key = path.joinToString(".")
        it.copy(draft = ExtensionDraft.set(it.draft, path, value), fieldErrors = it.fieldErrors - key)
    }

    /** A number field as typed: kept as text, its value put in the draft once it reads as one. */
    fun editNumber(path: List<String>, text: String, integer: Boolean) = mutable.update {
        val key = path.joinToString(".")
        val (number, problem) = ExtensionDraft.number(res, text, integer)
        it.copy(
            draft = if (problem == null) ExtensionDraft.set(it.draft, path, number) else it.draft,
            numberText = it.numberText + (key to text),
            numberProblems = if (problem == null) it.numberProblems - key else it.numberProblems + (key to problem),
            fieldErrors = it.fieldErrors - key,
        )
    }

    /** Saves the whole draft; a field the hub refuses keeps the draft and shows beside that field. */
    suspend fun save(): Boolean {
        if (!state.value.canSave) return false
        mutable.update { it.copy(saving = true, fieldErrors = emptyMap(), error = null) }
        return try {
            run(saved = true) { api.setConfig(id, ExtensionDraft.compact(state.value.draft)) }
        } finally { mutable.update { it.copy(saving = false) } }
    }

    suspend fun setSecret(name: String, value: String): Boolean {
        val trimmed = value.trim()
        if (trimmed.isEmpty()) return false
        return busy { run { api.setSecret(id, name, trimmed) } }
    }

    suspend fun clearSecret(name: String): Boolean = busy { run { api.clearSecret(id, name) } }

    private suspend fun busy(block: suspend () -> Boolean): Boolean {
        if (state.value.busy || state.value.saving) return false
        mutable.update { it.copy(busy = true) }
        return try { block() } finally { mutable.update { it.copy(busy = false) } }
    }

    /**
     * Puts the hub's answer in. The draft follows it after a save, or while it holds no edit; an edit the user has
     * not saved survives switching the extension or setting a secret.
     */
    private suspend fun run(saved: Boolean = false, call: suspend () -> ExtensionDetail): Boolean = try {
        val detail = call()
        mutable.update {
            val follow = saved || !it.dirty
            it.copy(
                detail = detail, error = null,
                draft = if (follow) detail.config else it.draft,
                numberText = if (follow) emptyMap() else it.numberText,
                numberProblems = if (follow) emptyMap() else it.numberProblems,
            )
        }
        true
    } catch (cancelled: CancellationException) { throw cancelled }
    catch (refused: ExtensionConfigRejected) {
        val reason = refused.message ?: res.getString(R.string.settings_ext_config_wrong)
        // A field the form does not draw (an older form, a whole-object rule) has nowhere to show: it goes on top.
        val drawn = refused.field in ExtensionDraft.paths(state.value.detail?.form.orEmpty())
        mutable.update {
            if (drawn) it.copy(fieldErrors = mapOf(refused.field to reason), error = null)
            else it.copy(fieldErrors = emptyMap(), error = res.getString(R.string.settings_ext_not_saved,
                listOf(refused.field, reason).filter { f -> f.isNotBlank() }.joinToString(res.getString(R.string.settings_ext_colon))))
        }
        false
    } catch (error: Exception) {
        mutable.update { it.copy(error = res.getString(if (it.detail == null) R.string.settings_ext_load_failed else R.string.settings_ext_not_changed, failureReason(error))) }
        false
    }
}
