package persona.feature.settings

import android.content.res.Resources
import persona.core.contracts.api.NewProvider
import persona.core.contracts.api.ProviderPreset
import persona.core.contracts.api.ProviderSetup
import persona.core.designsystem.failureReasonText
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/**
 * 新增模型供应商 (docs/launchers.md「从扩展添加」): typed in, or — when the hub's extensions offer presets — taken from one,
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
    val error: ResText? = null,
) {
    val usesPreset: Boolean get() = draft.fromPreset && selected != null
    val selected: ProviderPreset? get() = presets.firstOrNull { it.id == draft.preset }
    val nameProblem: ResText? get() = providerNameProblem(draft.name)

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
internal fun providerNameProblem(name: String): ResText? {
    val n = name.trim()
    return when {
        n.isEmpty() -> null
        !Regex("^[A-Za-z_][A-Za-z0-9_-]*$").matches(n) -> ResText.of(R.string.settings_src_provider_name_invalid)
        n == "claude" || n == "presets" -> ResText.of(R.string.settings_src_provider_name_reserved, n)
        else -> null
    }
}

/** "直接 API · api.example.com", "Claude Code 启动器 · gateway.example.com". */
internal fun presetSubtitle(res: Resources, preset: ProviderPreset): String {
    val kind = res.getString(if (preset.kind == "api") R.string.settings_src_provider_kind_api else R.string.settings_src_provider_kind_launcher)
    val host = runCatching { java.net.URI(preset.baseUrl).host }.getOrNull()
    return if (host.isNullOrBlank()) kind else res.getString(R.string.settings_src_provider_kind_host, kind, host)
}

/** 模型配置's id of a provider added under [name]. */
internal fun providerId(name: String) = "launcher.${name.trim()}"

internal class NewProviderModel(private val api: ProviderSetup) {
    private val mutable = MutableStateFlow(NewProviderState())
    val state = mutable.asStateFlow()

    /** The presets; the first is picked, and [preferPreset] opens on 从扩展添加 when there are any. */
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
        catch (error: Exception) { mutable.update { it.copy(error = ResText.of(R.string.settings_src_not_saved, failureReasonText(error))) }; null }
        finally { mutable.update { it.copy(busy = false) } }
    }
}
