package persona.feature.settings

import persona.core.contracts.api.*
import persona.core.designsystem.failureReason
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/** The purposes grouped by their hub group (empty: 其他), in the hub's order, matching [query] by name, group, id or model. */
internal fun modelPurposeGroups(purposes: List<ModelPurpose>, providers: List<ModelProvider>, query: String): List<Pair<String, List<ModelPurpose>>> {
    val q = query.trim()
    return purposes.filter { purpose ->
        q.isEmpty() || (listOf(purpose.name, purpose.group, purpose.id) + purpose.summaryParts(providers)).any { it.contains(q, ignoreCase = true) }
    }.groupBy { it.group.ifBlank { "其他" } }.toList()
}

/** A purpose's row: what is in effect, then the hub's status or where it inherits from. */
internal fun purposeSubtitle(purpose: ModelPurpose, catalog: ModelCatalog): String = listOfNotNull(
    purpose.summaryParts(catalog.providers).joinToString("，").ifEmpty { "未配置" },
    purpose.status?.takeIf { it.isNotBlank() } ?: purpose.inheritedFrom?.takeIf { it.isNotBlank() }?.let { from ->
        "跟随 " + (catalog.purposes.firstOrNull { it.id == from }?.displayName ?: from)
    },
).joinToString("\n")

internal data class ProviderOption(val id: String, val label: String)

/** 跟随默认, then the providers the purpose can use (hidden ones marked), and a reference the hub no longer offers. */
internal fun providerOptions(purpose: ModelPurpose, providers: List<ModelProvider>, chosen: String): List<ProviderOption> {
    val candidates = purpose.providersFrom(providers)
    return listOf(ProviderOption("", "跟随默认")) +
        candidates.map { ProviderOption(it.id, it.displayName + if (it.hidden) "（已隐藏）" else "") } +
        listOfNotNull(ProviderOption(chosen, "$chosen（当前引用不可用）").takeIf { chosen.isNotEmpty() && candidates.none { it.id == chosen } })
}

/** Effort is a field of launchers and of Responses APIs; a Messages API provider has none to set. */
internal fun showsEffort(purpose: ModelPurpose, provider: ModelProvider?): Boolean =
    purpose.supports("effort") && !(provider?.kind == "api" && provider.api != "responses")

internal data class ModelCatalogState(
    val catalog: ModelCatalog? = null,
    val loading: Boolean = false,
    val error: String? = null,
    val unsupported: Boolean = false,
)

internal class ModelCatalogModel(private val api: ModelConfiguration) {
    private val mutable = MutableStateFlow(ModelCatalogState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val catalog = api.catalog()
            mutable.update { it.copy(catalog = catalog, error = null, unsupported = false) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: ModelConfigurationUnsupported) { mutable.update { it.copy(unsupported = true, error = null) } }
        catch (error: Exception) { mutable.update { it.copy(error = "读取失败：${failureReason(error)}") } }
        finally { mutable.update { it.copy(loading = false) } }
    }
}

internal data class ModelPurposeState(
    val detail: ModelPurposeDetail? = null,
    val draft: ModelConfigurationDraft = ModelConfigurationDraft(),
    val busy: Boolean = false,
    val error: String? = null,
    /** Another device saved first: saving stays off until the page is reloaded (which drops the edit). */
    val conflict: Boolean = false,
) {
    val purpose: ModelPurpose? get() = detail?.purpose
    /** The provider the form talks about: the chosen one, else the one in effect. */
    val provider: ModelProvider? get() = detail?.providers?.firstOrNull { it.id == (draft.provider.ifEmpty { purpose?.effective?.provider }) }
    val changed: Boolean get() = purpose?.let { draft != ModelConfigurationDraft.from(it) } == true
}

/** One purpose's edit; a conflict never overwrites another device's change, and the draft survives it. */
internal class ModelPurposeModel(private val api: ModelConfiguration, private val id: String) {
    private val mutable = MutableStateFlow(ModelPurposeState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.busy) return
        mutable.update { it.copy(busy = true, error = null) }
        try {
            val detail = api.purpose(id)
            mutable.update { it.copy(detail = detail, draft = ModelConfigurationDraft.from(detail.purpose), conflict = false) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = "读取失败：${failureReason(error)}") } }
        finally { mutable.update { it.copy(busy = false) } }
    }

    fun edit(draft: ModelConfigurationDraft) = mutable.update { if (it.busy) it else it.copy(draft = draft) }

    /** The notice to show when the hub took it ([restore]: every override of this purpose cleared); null otherwise. */
    suspend fun save(restore: Boolean = false): String? {
        val current = state.value
        val purpose = current.purpose ?: return null
        if (current.busy || current.conflict) return null
        mutable.update { it.copy(busy = true, error = null) }
        return try {
            when (val result = api.save(purpose, if (restore) ModelConfigurationDraft() else current.draft)) {
                is ModelPurposeSave.Saved -> {
                    mutable.update { it.copy(detail = result.detail, draft = ModelConfigurationDraft.from(result.detail.purpose)) }
                    if (restore) "已恢复继承" else "已保存，下一次运行生效"
                }
                ModelPurposeSave.Conflict -> {
                    mutable.update { it.copy(conflict = true, error = "配置已在别处修改。你的编辑已保留；重新加载后再修改并保存。") }
                    null
                }
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = "未保存：${failureReason(error)}") }; null }
        finally { mutable.update { it.copy(busy = false) } }
    }
}
