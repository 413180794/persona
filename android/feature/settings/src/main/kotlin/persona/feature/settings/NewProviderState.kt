package persona.feature.settings

import persona.core.contracts.api.NewProvider
import persona.core.contracts.api.ProviderPreset
import persona.core.contracts.api.ProviderSetup
import persona.core.designsystem.failureReason
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/**
 * 新增模型供应商 (docs/launchers.md「从插件添加」): typed in, or — when the hub's plugins offer presets — taken from one,
 * where only the name, the key and maybe another of its models are filled in and the hub adds the rest.
 */
internal data class NewProviderDraft(
    val fromPreset: Boolean = false,
    val name: String = "",
    val apiKey: String = "",
    /** launcher | api */
    val kind: String = "launcher",
    /** messages | responses, for an API. */
    val api: String = "messages",
    val baseUrl: String = "",
    val model: String = "",
    val smallModel: String = "",
    val subagentModel: String = "",
    val args: String = "",
    /** The picked preset's id and the model picked from it (its default when untouched). */
    val preset: String = "",
    val presetModel: String = "",
)

internal data class NewProviderState(
    val presets: List<ProviderPreset> = emptyList(),
    val loaded: Boolean = false,
    val draft: NewProviderDraft = NewProviderDraft(),
    val busy: Boolean = false,
    val error: String? = null,
) {
    val usesPreset: Boolean get() = draft.fromPreset && selected != null
    val selected: ProviderPreset? get() = presets.firstOrNull { it.id == draft.preset }
    val nameProblem: String? get() = providerNameProblem(draft.name)

    /** What is sent; null while something required is missing. */
    val request: NewProvider? get() {
        val d = draft
        val name = d.name.trim()
        val key = d.apiKey.trim()
        if (name.isEmpty() || nameProblem != null || key.isEmpty()) return null
        selected?.takeIf { d.fromPreset }?.let { preset ->
            val model = d.presetModel.trim()
            return NewProvider(name, key, preset = preset.id, model = model.takeIf { it.isNotEmpty() && it != preset.defaultModel })
        }
        val api = d.kind == "api"
        if (d.baseUrl.isBlank() || d.model.isBlank()) return null
        return NewProvider(
            name, key, kind = d.kind, api = if (api) d.api else "messages", baseUrl = d.baseUrl.trim(), model = d.model.trim(),
            smallModel = d.smallModel.trim().takeIf { !api && it.isNotEmpty() },
            subagentModel = d.subagentModel.trim().takeIf { !api && it.isNotEmpty() },
            args = d.args.split(Regex("\\s+")).filter { it.isNotEmpty() }.takeIf { !api && it.isNotEmpty() },
        )
    }
    val canSave: Boolean get() = !busy && request != null
}

/** What is wrong with a provider's name, the way the hub would put it; null while empty or fine. */
internal fun providerNameProblem(name: String): String? {
    val n = name.trim()
    return when {
        n.isEmpty() -> null
        !Regex("^[A-Za-z_][A-Za-z0-9_-]*$").matches(n) -> "名字只能用字母、数字、_ 和 -，以字母或 _ 开头"
        n == "claude" || n == "presets" -> "不能叫 $n"
        else -> null
    }
}

/** "直接 API · api.example.com", "Claude Code 启动器 · gateway.example.com". */
internal fun presetSubtitle(preset: ProviderPreset): String {
    val kind = if (preset.kind == "api") "直接 API" else "Claude Code 启动器"
    val host = runCatching { java.net.URI(preset.baseUrl).host }.getOrNull()
    return if (host.isNullOrBlank()) kind else "$kind · $host"
}

/** 模型配置's id of a provider added under [name]. */
internal fun providerId(name: String) = "launcher.${name.trim()}"

internal class NewProviderModel(private val api: ProviderSetup) {
    private val mutable = MutableStateFlow(NewProviderState())
    val state = mutable.asStateFlow()

    /** The presets; the first is picked, and [preferPreset] opens on 从插件添加 when there are any. */
    suspend fun load(preferPreset: Boolean = false) {
        try {
            val presets = api.presets()
            mutable.update { s ->
                val first = presets.firstOrNull()
                val keep = presets.any { it.id == s.draft.preset }
                s.copy(presets = presets, loaded = true, draft = if (keep || first == null) s.draft
                    else s.draft.copy(preset = first.id, presetModel = first.defaultModel, fromPreset = preferPreset || s.draft.fromPreset))
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        // 自己填写 still works without them.
        catch (_: Exception) { mutable.update { it.copy(loaded = true) } }
    }

    fun edit(draft: NewProviderDraft) = mutable.update { if (it.busy) it else it.copy(draft = draft, error = null) }

    /** Picking another preset resets the model to its default. */
    fun pick(id: String) = mutable.update { s ->
        val preset = s.presets.firstOrNull { it.id == id } ?: return@update s
        if (s.busy) s else s.copy(draft = s.draft.copy(preset = id, presetModel = preset.defaultModel))
    }

    /** The added provider's 模型配置 id; null (with the reason in [NewProviderState.error]) when the hub refused it. */
    suspend fun save(): String? {
        val current = state.value
        val request = current.request ?: return null
        if (current.busy) return null
        mutable.update { it.copy(busy = true, error = null) }
        return try {
            api.add(request)
            providerId(request.name)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = "未保存：${failureReason(error)}") }; null }
        finally { mutable.update { it.copy(busy = false) } }
    }
}
