package persona.feature.settings

import androidx.navigation3.runtime.NavKey
import persona.core.contracts.Session
import persona.core.contracts.api.BindPrompt
import persona.core.contracts.api.ChannelCapabilities
import persona.core.contracts.api.ChannelChange
import persona.core.contracts.api.ChannelDetail
import persona.core.contracts.api.ChannelManagement
import persona.core.contracts.api.ChannelTarget
import persona.core.contracts.api.HubChannel
import persona.core.contracts.parseTime
import persona.core.designsystem.Status
import persona.core.designsystem.failureReason
import persona.core.navigation.ExtensionRoute
import persona.core.navigation.SettingsPage
import persona.core.navigation.SettingsPageRoute
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

// 设置 › 扩展 › 渠道 (docs/channels.md「界面」): the words, the list's sections, where messages go, and the models of the
// list, a channel's page and its binding a step at a time. The pages are in ChannelsScreen.kt.

/** What a channel is called and says in the settings (the same words as the web's lib/channels.ts). */
internal object ChannelText {
    fun state(c: HubChannel): String = when (c.state) {
        "connected" -> "已连接"
        "error" -> "出错"
        "disabled" -> "已停用"
        "not_installed" -> "可以安装"
        else -> "未连接"
    }

    /** Broken: drawn in the danger colour with its one-line reason. */
    fun isProblem(c: HubChannel): Boolean = c.state == "error"

    /** The dot beside the state: green connected, grey otherwise (darker grey when it is off). */
    fun status(c: HubChannel): Status = when (c.state) {
        "connected" -> Status.Unread
        "disabled", "not_installed" -> Status.Offline
        else -> Status.Idle
    }

    /** The row's second line: the error, the app's ends, the account, else what it is. */
    fun subtitle(c: HubChannel): String? = when {
        c.state == "error" && !c.error.isNullOrBlank() -> c.error
        c.kind == "app" && c.surfaces.isNotEmpty() -> c.surfaces.joinToString(" · ")
        c.source != "market" && !c.account?.name.isNullOrBlank() -> c.account?.name
        else -> c.description?.takeIf { it.isNotBlank() }
    }

    /** 设置's row: how many are connected (the apps are always), and how many went wrong; null before the list is in. */
    fun listSummary(list: List<HubChannel>?): String? {
        list ?: return null
        val on = list.count { it.kind != "app" && it.state == "connected" }
        val bad = list.count(::isProblem)
        return if (bad > 0) "$on 个已连接 · $bad 个出错" else "$on 个已连接"
    }

    fun hasProblem(list: List<HubChannel>?): Boolean = list.orEmpty().any(::isProblem)

    fun targetText(t: ChannelTarget?): String = when (t?.kind) {
        "new" -> "新对话（下一条消息开）"
        "conversation" -> t.title?.takeIf { it.isNotBlank() } ?: "一段对话"
        "agent" -> t.title?.takeIf { it.isNotBlank() }?.let { "Agent · $it" } ?: "一个 Agent"
        else -> "主对话"
    }

    /** What the channel can do, in a few words each for its page. */
    fun capabilities(c: ChannelCapabilities): List<String> = buildList {
        add("收" + listOfNotNull("文字", "图片".takeIf { c.imagesIn }, "文件".takeIf { c.filesIn }, "语音".takeIf { c.voiceIn }).joinToString("、"))
        add(if (c.format == "plain") "回答是纯文字" else "回答带格式（Markdown）")
        if (c.maxText > 0) add("一条最多 ${c.maxText} 字，长的拆开发")
        when (c.asks) {
            "text" -> add("提问和批准回复序号或文字")
            "buttons" -> add("提问和批准点按钮")
        }
        if (c.typing) add("处理时显示「正在输入」")
        if (c.progress) add("处理时显示进度")
    }

    fun bindTitle(c: HubChannel): String = when (c.bind) {
        "qr" -> "扫码绑定"
        "code" -> "配对"
        else -> "绑定"
    }

    /** Set up on this page: not hub.json's or an extension page's, not a market's. */
    fun editable(c: HubChannel): Boolean = !c.readOnly && c.source != "market"

    /** It has a binding step of its own (scan, pair, fill in). */
    fun binds(c: HubChannel): Boolean = editable(c) && c.bind !in setOf("none", "config", "")

    fun isBound(c: HubChannel): Boolean = c.state == "connected" || (c.state == "error" && !c.account?.name.isNullOrBlank())

    /** The page offers 绑定: it binds here, it is on, and it is not bound. */
    fun canBind(c: HubChannel): Boolean = binds(c) && c.enabled && !isBound(c)

    /**
     * The page the hub links to (`settings_url`), where Android has it: an extension's page, 模型配置. The Feishu bot's own
     * settings page is iOS's and the web's only; without a page here the row is left out.
     */
    fun settingsDestination(url: String?): NavKey? {
        val path = url?.trim()?.trim('/') ?: return null
        return when {
            path.startsWith("settings/plugins/") -> path.removePrefix("settings/plugins/").takeIf { it.matches(ExtensionId) }?.let(::ExtensionRoute)
            path == "settings/model-config" -> SettingsPageRoute(SettingsPage.ModelConfig)
            else -> null
        }
    }

    /**
     * The header's line: the account, the time of the last message; empty when it has neither. The state is on the
     * header's right and the description under it, so neither is repeated here.
     */
    fun headerLine(c: HubChannel, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String =
        listOfNotNull(
            c.account?.name?.takeIf { it.isNotBlank() },
            parseTime(c.lastEventAt)?.let { "最近一条 " + ago(it, now, zone) },
        ).joinToString(" · ")

    fun ago(at: Instant, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String {
        val gap = Duration.between(at, now)
        return when {
            gap.isNegative || gap < Duration.ofMinutes(1) -> "刚刚"
            gap < Duration.ofHours(1) -> "${gap.toMinutes()} 分钟前"
            gap < Duration.ofDays(1) -> "${gap.toHours()} 小时前"
            else -> DateTimeFormatter.ofPattern("M 月 d 日").withZone(zone).format(at)
        }
    }

    /** 最近's 在等 line: the answers and questions the channel waits on; null when it waits on nothing. */
    fun waiting(d: ChannelDetail): String? = listOfNotNull(
        d.waiting.answers.takeIf { it > 0 }?.let { "$it 个回答" },
        d.waiting.asks.takeIf { it > 0 }?.let { "$it 个提问" },
    ).joinToString(" · ").ifEmpty { null }

    /** How a failed change reads: the hub's words when it gave any. */
    fun failure(error: Throwable, verb: String = "没有改成"): String = "$verb：${failureReason(error)}"

    private val ExtensionId = Regex("[A-Za-z0-9_-]{1,64}")
}

/** A section of the list: the apps, the installed ones, those a market has. */
internal data class ChannelSection(val title: String, val footer: String, val channels: List<HubChannel>)

internal fun channelSections(list: List<HubChannel>): List<ChannelSection> = listOf(
    ChannelSection("Persona", "在 Persona 的哪一端说都进同一段对话，提问和批准在卡片上原地作答。", list.filter { it.kind == "app" }),
    ChannelSection("已接入", "这些地方发给机器人的消息会交给 Persona，回答和 Agent 的提问发回原处。只认绑定的那一个人。",
        list.filter { it.kind != "app" && it.source != "market" }),
    ChannelSection("可以安装", "扩展市场里提供渠道的扩展：装好后在这里绑定。", list.filter { it.source == "market" }),
).filter { it.channels.isNotEmpty() }

/** One choice of 「消息发到」. */
internal data class TargetOption(val target: ChannelTarget, val label: String, val selected: Boolean)

/**
 * 「消息发到」's choices: the main conversation, a new one, then the conversations this channel's messages went to
 * lately, then the assistant's other ones (newest first, [limit] in all). [assistant] is in the hub's own ids.
 */
internal fun targetOptions(detail: ChannelDetail, assistant: List<Session>, limit: Int = 20): List<TargetOption> {
    val current = detail.channel.target
    fun isCurrent(machine: String, session: String) =
        current?.kind == "conversation" && current.machineId == machine && current.sessionId == session
    val recent = detail.conversations.filter { it.machineId.isNotBlank() && it.sessionId.isNotBlank() }
        .map { Triple(it.machineId, it.sessionId, it.title?.takeIf(String::isNotBlank)) }
    val others = assistant.filter { it.assistant }.sortedByDescending { it.updated }
        .map { Triple(it.machineId, it.sessionId, it.title.takeIf(String::isNotBlank) ?: it.lastText?.takeIf(String::isNotBlank)?.take(40)) }
    // The current conversation is always a choice, even when it is neither recent nor listed (another machine's).
    val chosen = current?.takeIf { it.kind == "conversation" && !it.machineId.isNullOrBlank() && !it.sessionId.isNullOrBlank() }
        ?.let { listOf(Triple(it.machineId!!, it.sessionId!!, it.title?.takeIf(String::isNotBlank))) }.orEmpty()
    val conversations = (chosen + recent + others)
        .groupBy { it.first to it.second }.values.map { same -> same.first().copy(third = same.firstNotNullOfOrNull { it.third }) }
        .take(limit)
    return buildList {
        add(TargetOption(ChannelTarget.Main, "主对话", current == null || current.kind == "main"))
        add(TargetOption(ChannelTarget.New, "新对话（下一条消息开）", current?.kind == "new"))
        conversations.forEach { (machine, session, title) ->
            add(TargetOption(ChannelTarget.conversation(machine, session), title ?: "一段对话", isCurrent(machine, session)))
        }
    }
}

internal data class ChannelListState(
    val channels: List<HubChannel>? = null,
    val loading: Boolean = false,
    val error: String? = null,
) {
    val sections: List<ChannelSection> get() = channelSections(channels.orEmpty())
}

/** 设置 › 扩展 › 渠道: the list. */
internal class ChannelListModel(private val api: ChannelManagement) {
    private val mutable = MutableStateFlow(ChannelListState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val list = api.channels()
            mutable.update { it.copy(channels = list, error = null) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = ChannelText.failure(error, "读取失败")) } }
        finally { mutable.update { it.copy(loading = false) } }
    }
}

/** What the page is doing right now; one change at a time. */
internal enum class ChannelBusy { Enabled, Feature, Target, Unbind, Test }

/** Binding a step at a time: the step on screen, what was typed into it, and the hub's word on it. */
internal data class BindState(
    val prompt: BindPrompt? = null,
    val values: Map<String, String> = emptyMap(),
    val sending: Boolean = false,
    /** Why the last step did not go (the network, the hub); the hub's word on the values is the prompt's own error. */
    val error: String? = null,
) {
    val active: Boolean get() = prompt != null
    val polled: Boolean get() = prompt?.polled == true
    /** Fields to fill in, at least one filled (the hub says which it still needs). */
    val canSubmit: Boolean get() {
        val p = prompt ?: return false
        return !sending && !p.polled && p.fields.isNotEmpty() && p.fields.any { values[it.name].orEmpty().isNotBlank() }
    }
    /** How long to wait before asking the hub again, at least a second. */
    val pollMillis: Long get() = (prompt?.interval ?: 0).coerceAtLeast(MinPollSeconds) * 1000L

    private companion object { const val MinPollSeconds = 1 }
}

internal data class ChannelState(
    val detail: ChannelDetail? = null,
    val loading: Boolean = false,
    val busy: ChannelBusy? = null,
    val error: String? = null,
    val bind: BindState = BindState(),
    /** Binding just finished, as whom (empty when the hub did not say): the page says so once. */
    val boundAs: String? = null,
) {
    val channel: HubChannel? get() = detail?.channel
    val editable: Boolean get() = channel?.let(ChannelText::editable) == true
}

/** 设置 › 扩展 › 渠道 › one: its switch, its binding, where its messages go, its features, a test message. */
internal class ChannelModel(private val api: ChannelManagement, private val id: String) {
    private val mutable = MutableStateFlow(ChannelState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val detail = api.channel(id)
            mutable.update { it.copy(detail = detail, error = null) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = ChannelText.failure(error, if (it.detail == null) "读取失败" else "没有刷新")) } }
        finally { mutable.update { it.copy(loading = false) } }
    }

    suspend fun setEnabled(on: Boolean) = change(ChannelBusy.Enabled, ChannelChange(enabled = on))

    suspend fun setFeature(feature: String, on: Boolean) = change(ChannelBusy.Feature, ChannelChange(features = mapOf(feature to on)))

    suspend fun setTarget(target: ChannelTarget): Boolean {
        if (target == state.value.channel?.target?.copy(title = null)) return true
        return change(ChannelBusy.Target, ChannelChange(target = target.copy(title = null)))
    }

    suspend fun unbind(): Boolean = busy(ChannelBusy.Unbind) {
        val detail = api.unbind(id)
        mutable.update { it.copy(detail = detail, bind = BindState(), error = null) }
    }

    /** A line to the bound owner: the caller says it went. */
    suspend fun test(): Boolean = busy(ChannelBusy.Test, verb = "没有发出") { api.test(id) }

    private suspend fun change(what: ChannelBusy, change: ChannelChange): Boolean = busy(what) {
        val detail = api.update(id, change)
        mutable.update { it.copy(detail = detail, error = null) }
    }

    private suspend fun busy(what: ChannelBusy, verb: String = "没有改成", block: suspend () -> Unit): Boolean {
        if (state.value.busy != null) return false
        mutable.update { it.copy(busy = what, error = null) }
        return try { block(); true }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = ChannelText.failure(error, verb)) }; false }
        finally { mutable.update { it.copy(busy = null) } }
    }

    // ---- binding (docs/channels.md「接口」: POST …/bind, a connector.Prompt a step at a time)

    /** 绑定: the first step. */
    suspend fun startBind(): Boolean = step(null, null)

    fun setValue(name: String, value: String) = mutable.update { it.copy(bind = it.bind.copy(values = it.bind.values + (name to value))) }

    /** 下一步: the step's fields as typed (trimmed; empty ones left out). */
    suspend fun submit(): Boolean {
        val bind = state.value.bind
        if (!bind.canSubmit) return false
        val prompt = bind.prompt ?: return false
        val values = prompt.fields.mapNotNull { f -> bind.values[f.name]?.trim()?.takeIf { it.isNotEmpty() }?.let { f.name to it } }.toMap()
        return step(prompt.step.orEmpty(), values)
    }

    /** The page has said that binding finished. */
    fun boundShown() = mutable.update { it.copy(boundAs = null) }

    /** 取消: the step goes away (the hub forgets a pairing code by itself when it expires). */
    fun cancelBind() = mutable.update { it.copy(bind = BindState()) }

    /**
     * While a code is on screen (a QR code to scan, a code to send to the bot), asks the hub again every interval until
     * the answer is another step or done; a failed ask is tried again next time. Returns when there is nothing to
     * wait on; cancelled with the page.
     */
    suspend fun poll(wait: suspend (Long) -> Unit = { delay(it) }) {
        while (true) {
            val bind = state.value.bind
            val prompt = bind.prompt?.takeIf { it.polled } ?: return
            wait(bind.pollMillis)
            // Cancelled or replaced while waiting: whatever is on screen now is the one to ask about.
            if (state.value.bind.prompt !== prompt) continue
            step(prompt.step.orEmpty(), null, quiet = true)
        }
    }

    /** True once a step went (the next one is on screen, or binding is done). */
    private suspend fun step(step: String?, values: Map<String, String>?, quiet: Boolean = false): Boolean {
        if (!quiet) {
            if (state.value.bind.sending) return false
            mutable.update { it.copy(bind = it.bind.copy(sending = true, error = null)) }
        }
        return try {
            val prompt = api.bind(id, step, values)
            if (prompt.done) {
                mutable.update { it.copy(bind = BindState(), boundAs = prompt.account.orEmpty()) }
                load()
            } else mutable.update {
                // A new step starts with nothing typed; the same step asked again (a mistake) keeps what was typed.
                val same = it.bind.prompt?.step == prompt.step
                it.copy(bind = BindState(prompt, if (same) it.bind.values else emptyMap()))
            }
            true
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) {
            if (!quiet) mutable.update { it.copy(bind = it.bind.copy(sending = false, error = ChannelText.failure(error, "没有绑定上"))) }
            false
        }
    }
}
