package jarvis.android.ui.settings

import androidx.compose.ui.Modifier
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.GroupFooter
import jarvis.android.ui.design.SearchField
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.ButtonKind
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import jarvis.android.data.Hub
import jarvis.android.data.HubException
import jarvis.android.settings.ModelConfigurationBody
import jarvis.android.settings.ModelConfigurationDraft
import jarvis.android.settings.ModelPurposeBody
import jarvis.android.settings.modelConfiguration
import jarvis.android.settings.modelPurpose
import jarvis.android.settings.modelPurposeGroups
import jarvis.android.settings.saveModelPurpose
import jarvis.android.voice.VoiceModels
import jarvis.android.voice.voiceModels
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** The hub owns the purpose catalog. A purpose is a real model consumer, never a locally invented skill. */
@Composable
fun ModelConfigurationScreen(
    hub: Hub, onBack: () -> Unit, onPurpose: (String) -> Unit,
    /** Loaded ahead (the screenshot tests); it is read again either way. */
    preloaded: ModelConfigurationBody? = null,
) {
    var data by remember { mutableStateOf(preloaded) }
    var error by remember { mutableStateOf<String?>(null) }
    var query by remember { mutableStateOf("") }
    var loading by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    suspend fun load() {
        if (loading) return
        loading = true
        try {
            data = hub.api.modelConfiguration()
            error = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = configurationError(e)
        } finally {
            loading = false
        }
    }
    LaunchedEffect(Unit) { load() }
    val groups = modelPurposeGroups(data?.purposes.orEmpty(), query)

    KitPage("模型配置", onBack, actions = {
        CircleButton(JIcons.retry, "刷新", { scope.launch { load() } }, enabled = !loading)
    }) {
        item(key = "search") {
            Column(Modifier.padding(top = JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                SearchField(query, { query = it }, "搜索用途或模型", Modifier.padding(horizontal = JSpace.page))
                GroupFooter("各功能下一次运行使用这里的配置，不打断正在工作的任务。连接与凭据在模型供应商中管理。")
            }
        }
        error?.let { message -> item(key = "error") { KitGroup { row { KitNoticeRow("读取失败", message, tint = kitColors.danger) } } } }
        if (loading && data == null) item(key = "loading") { KitLoadingRow() }
        // One group per kind of use (iOS: a section each), every purpose a row in it.
        groups.forEach { (group, purposes) ->
            item(key = "group.$group") {
                KitGroup(header = "$group · ${purposes.size}") {
                    purposes.forEach { p ->
                        row {
                            val source = data?.purposes?.firstOrNull { it.id == p.inheritedFrom }?.name ?: p.inheritedFrom
                            KitRow(p.name, onClick = { onPurpose(p.id) }, subtitle = listOfNotNull(
                                p.summary(data?.providers.orEmpty()),
                                p.status ?: source?.let { "跟随 $it" },
                            ).joinToString("\n"))
                        }
                    }
                }
            }
        }
        if (data != null && groups.isEmpty()) item(key = "empty") { KitGroup { row { KitNoticeRow("没有匹配的用途", "试试其他名称或模型。") } } }
    }
}

/** Single-purpose drafts are kept until saved; a conflict never silently overwrites another device's changes. */
@Composable
fun ModelPurposeScreen(hub: Hub, id: String, onBack: () -> Unit) {
    var data by remember(id) { mutableStateOf<ModelPurposeBody?>(null) }
    var draft by remember(id) { mutableStateOf(ModelConfigurationDraft()) }
    var error by remember(id) { mutableStateOf<String?>(null) }
    var conflict by remember(id) { mutableStateOf(false) }
    var busy by remember(id) { mutableStateOf(false) }
    var models by remember(id) { mutableStateOf<VoiceModels?>(null) }
    var modelsError by remember(id) { mutableStateOf<String?>(null) }
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    suspend fun load() {
        if (busy) return
        busy = true
        try {
            data = hub.api.modelPurpose(id)
            draft = ModelConfigurationDraft.from(data!!.purpose)
            conflict = false
            error = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = configurationError(e)
        } finally {
            busy = false
        }
    }
    LaunchedEffect(id) {
        load()
        if (id.startsWith("plugin.")) {
            try {
                models = hub.api.voiceModels(id.removePrefix("plugin."))
                modelsError = null
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                modelsError = e.kitMessage
            }
        }
    }
    fun save(restore: Boolean = false) {
        val p = data?.purpose ?: return
        if (busy || conflict) return
        busy = true
        error = null
        scope.launch {
            try {
                data = hub.api.saveModelPurpose(p, if (restore) ModelConfigurationDraft() else draft)
                draft = ModelConfigurationDraft.from(data!!.purpose)
                kitToast(ctx, if (restore) "已恢复继承" else "已保存，下一次运行生效")
                onBack()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                conflict = e is HubException && e.status == 409
                error = if (conflict) "配置已在别处修改。你的编辑已保留；重新加载后再修改并保存。" else e.kitMessage
            } finally {
                busy = false
            }
        }
    }
    val p = data?.purpose
    val provider = data?.providers?.firstOrNull { it.id == (draft.provider.takeIf { value -> value.isNotEmpty() } ?: p?.effective?.provider) }

    KitPage(p?.name ?: "模型配置", onBack, actions = {
        PillButton(if (busy) "保存中…" else "保存", { save() }, kind = ButtonKind.Prominent, enabled = p != null && !busy && !conflict)
    }) {
        if (p == null && busy) item(key = "loading") { KitLoadingRow() }
        if (p != null) {
            item(key = "effective") {
                KitGroup(header = "当前生效", footer = p.inheritedFrom?.let { "继承来源：$it" }) {
                    row { KitValueRow("供应商", data?.providers?.firstOrNull { it.id == p.effective.provider }?.name ?: p.effective.provider ?: "未配置") }
                    row { KitValueRow("模型", p.effective.model ?: "供应商默认", mono = true) }
                    p.status?.let { status -> row { KitValueRow("状态", status) } }
                }
            }
            item(key = "draft") {
                KitGroup(header = "用途配置", footer = "留空恢复继承。保存只修改配置，不发起模型请求；会话的临时选择仍可覆盖默认值。") {
                    if (p.supports("provider")) row {
                        val candidates = p.providersFrom(data?.providers.orEmpty())
                        val options = listOf(KitOption("", "跟随默认")) + candidates.map { KitOption(it.id, it.name + if (it.hidden) "（已隐藏）" else "") } +
                            if (draft.provider.isNotEmpty() && candidates.none { it.id == draft.provider }) listOf(KitOption(draft.provider, "${draft.provider}（当前引用不可用）")) else emptyList()
                        KitPickerRow("模型供应商", draft.provider, options, { draft = draft.choosingProvider(it) }, enabled = !busy)
                    }
                    if (p.supports("model")) row {
                        if (p.kind == "plugin") {
                            val available = models?.models.orEmpty()
                            val options = listOf(KitOption("", "跟随插件默认")) + available.map { KitOption(it.id, it.label) } +
                                if (draft.model.isNotEmpty() && available.none { it.id == draft.model }) listOf(KitOption(draft.model, "${draft.model}（当前引用）")) else emptyList()
                            KitPickerRow("具体模型", draft.model, options, { draft = draft.copy(model = it) }, enabled = !busy && models != null)
                        } else {
                            KitTextField("具体模型", draft.model, { draft = draft.copy(model = it) }, provider?.model ?: p.effective.model ?: "跟随默认", enabled = !busy)
                        }
                    }
                    if (p.supports("effort") && !(provider?.kind == "api" && provider.api != "responses")) row {
                        KitTextField("思考强度", draft.effort, { draft = draft.copy(effort = it) }, p.effective.effort ?: "跟随默认", enabled = !busy)
                    }
                    row { KitActionRow("恢复继承", { save(true) }, enabled = !busy && !conflict) }
                }
            }
            if (p.kind == "plugin") item(key = "plugin") {
                KitGroup {
                    row { KitNoticeRow("插件模型", modelsError?.let { "模型列表未读取：$it" } ?: "只选择这个插件提供的模型；不修改插件自己的供应商或凭据。") }
                }
            }
        }
        error?.let { message -> item(key = "error") {
            KitGroup {
                row { KitNoticeRow("未保存", message, tint = kitColors.danger) }
                if (conflict || p == null) row { KitActionRow(if (conflict) "重新加载（丢弃编辑）" else "重试", { scope.launch { load() } }, enabled = !busy) }
            }
        } }
    }
}

private fun configurationError(e: Exception): String =
    if (e is HubException && e.status == 404) "这个 hub 还不支持统一模型配置，请先更新服务器。" else e.kitMessage
