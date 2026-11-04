package persona.feature.settings

import persona.core.contracts.api.HubPlugin
import persona.core.contracts.api.PluginConfigRejected
import persona.core.contracts.api.PluginDetail
import persona.core.contracts.api.PluginManagement
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

// 设置 › 插件 (docs/plugins.md「App：设置 › 插件」): the words, the draft of a plugin's configuration, and the two
// pages' models. The pages are in PluginsScreen.kt.

/** What a plugin is called and says in the settings. */
internal object PluginText {
    /** Running or idle until something needs it (stopped) is all one to the user: it is on. */
    fun state(p: HubPlugin): String = when (p.status) {
        "running", "stopped" -> "启动"
        "starting" -> "正在启动"
        "needs_setup" -> "需要设置"
        "disabled" -> "停用"
        "incompatible" -> "这台 hub 用不了"
        "error" -> "出错了"
        else -> "状态未知"
    }

    /** Whether an enabled plugin's program runs right now (the detail page's small print); null for any other state. */
    fun runNote(p: HubPlugin): String? = when (p.status) {
        "running" -> "正在运行"
        "stopped" -> "暂未运行，用到时自动启动"
        else -> null
    }

    /** Broken: drawn in the danger colour, not as a status dot. */
    fun isProblem(p: HubPlugin): Boolean = p.status == "error" || p.status == "incompatible"

    /** The dot beside the state: green on (running or idle), amber waiting on the user, blue starting, grey otherwise. */
    fun status(p: HubPlugin): Status = when (p.status) {
        "running", "stopped" -> Status.Unread
        "needs_setup" -> Status.NeedsYou
        "starting" -> Status.Working
        else -> Status.Offline
    }

    fun source(s: String?): String? = when (s) {
        "builtin" -> "内置"
        "hub.json" -> "hub.json"
        "installed" -> "已安装"
        else -> null
    }

    fun point(name: String): String = when (name) {
        "connectors" -> "连接器"
        "channel" -> "对话渠道"
        "voice" -> "语音对话"
        "ui" -> "界面"
        "quota" -> "额度"
        "providers" -> "模型供应商"
        "events" -> "事件"
        "assistant" -> "助手"
        "api" -> "App 接口"
        else -> name
    }

    /** The detail's state line: the state, the version, where it came from. */
    fun stateLine(p: HubPlugin): String =
        listOfNotNull(state(p), p.version?.takeIf { it.isNotBlank() }, source(p.source)).joinToString(" · ")

    /** 设置's row: how many, and how many need the user; null before the list is in. */
    fun listSummary(list: List<HubPlugin>?): String? {
        list ?: return null
        if (list.isEmpty()) return "没有"
        val attention = list.count { it.status == "needs_setup" || isProblem(it) }
        return if (attention > 0) "${list.size} 个 · $attention 个要处理" else "${list.size} 个"
    }

    /** A field's title, marked when it must be filled and has no default. */
    fun fieldTitle(field: persona.core.contracts.api.PluginField): String =
        field.label + if (field.required == true && field.fallback == null) "（必填）" else ""

    /** What an empty field shows: the default it falls back to, or 未设置. */
    fun placeholder(field: persona.core.contracts.api.PluginField): String {
        val fallback = field.fallback ?: return "未设置"
        val text = field.enum?.firstOrNull { it.value == fallback }?.label ?: (fallback as? JsonPrimitive)?.content
        return if (text.isNullOrEmpty()) "未设置" else "默认：$text"
    }

    fun secretState(secret: persona.core.contracts.api.PluginSecret): String = when {
        secret.set -> "已设置"
        secret.required == true -> "未设置（必填）"
        else -> "未设置"
    }
}

/** The configuration being edited, by dotted path ("proxy.host"); an empty group goes with its last value. */
internal object PluginDraft {
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
    fun paths(form: List<persona.core.contracts.api.PluginField>): Set<String> = form.flatMap { field ->
        if (field.type == "object") field.fields.orEmpty().map { "${field.name}.${it.name}" } else listOf(field.name)
    }.toSet()

    /** A number as typed: its value, or why it is not one (blank is "not set"). */
    fun number(text: String, integer: Boolean): Pair<JsonPrimitive?, String?> {
        val t = text.trim()
        if (t.isEmpty()) return null to null
        val n = t.toDoubleOrNull()?.takeIf { it.isFinite() } ?: return null to (if (integer) "要是一个整数" else "要是一个数")
        val whole = n == Math.floor(n) && kotlin.math.abs(n) < 1e15
        if (integer && !whole) return null to "要是一个整数"
        return (if (whole) JsonPrimitive(n.toLong()) else JsonPrimitive(n)) to null
    }
}

internal data class PluginListState(
    val plugins: List<HubPlugin>? = null,
    /** The hub has plugin markets (`markets: true`): the list opens 插件市场. */
    val markets: Boolean = false,
    val loading: Boolean = false,
    val error: String? = null,
)

/** 设置 › 插件: the list. */
internal class PluginListModel(private val api: PluginManagement) {
    private val mutable = MutableStateFlow(PluginListState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val listing = api.listing()
            mutable.update { it.copy(plugins = listing.plugins, markets = listing.markets, error = null) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = "读取失败：${failureReason(error)}") } }
        finally { mutable.update { it.copy(loading = false) } }
    }
}

internal data class PluginState(
    val detail: PluginDetail? = null,
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

    fun value(path: List<String>): JsonElement? = PluginDraft.value(draft, path)

    fun fieldError(path: List<String>): String? = path.joinToString(".").let { fieldErrors[it] ?: numberProblems[it] }
}

/** 设置 › 插件 › one: its switch, its form (a draft saved as a whole), its secrets. */
internal class PluginModel(private val api: PluginManagement, private val id: String) {
    private val mutable = MutableStateFlow(PluginState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try { run { api.plugin(id) } } finally { mutable.update { it.copy(loading = false) } }
    }

    suspend fun setEnabled(on: Boolean) = busy { run { api.setEnabled(id, on) } }

    fun edit(path: List<String>, value: JsonElement?) = mutable.update {
        val key = path.joinToString(".")
        it.copy(draft = PluginDraft.set(it.draft, path, value), fieldErrors = it.fieldErrors - key)
    }

    /** A number field as typed: kept as text, its value put in the draft once it reads as one. */
    fun editNumber(path: List<String>, text: String, integer: Boolean) = mutable.update {
        val key = path.joinToString(".")
        val (number, problem) = PluginDraft.number(text, integer)
        it.copy(
            draft = if (problem == null) PluginDraft.set(it.draft, path, number) else it.draft,
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
            run(saved = true) { api.setConfig(id, PluginDraft.compact(state.value.draft)) }
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
     * not saved survives switching the plugin or setting a secret.
     */
    private suspend fun run(saved: Boolean = false, call: suspend () -> PluginDetail): Boolean = try {
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
    catch (refused: PluginConfigRejected) {
        val reason = refused.message ?: "设置不对"
        // A field the form does not draw (an older form, a whole-object rule) has nowhere to show: it goes on top.
        val drawn = refused.field in PluginDraft.paths(state.value.detail?.form.orEmpty())
        mutable.update {
            if (drawn) it.copy(fieldErrors = mapOf(refused.field to reason), error = null)
            else it.copy(fieldErrors = emptyMap(), error = "没有保存：" + listOf(refused.field, reason).filter { f -> f.isNotBlank() }.joinToString("："))
        }
        false
    } catch (error: Exception) {
        mutable.update { it.copy(error = (if (it.detail == null) "读取失败：" else "没有改成：") + failureReason(error)) }
        false
    }
}
