package persona.feature.voice

import persona.core.contracts.api.VoiceModelChoice
import persona.core.contracts.api.VoiceModelOptions
import persona.core.contracts.api.VoiceOption
import persona.core.designsystem.operationFeedback
import persona.core.domain.FailureAction

/** The voice page's words (docs/android.md「语音对话」). */
internal object VoiceText {
    const val Title = "语音"
    const val Connecting = "正在连接…"
    const val Listening = "在听"
    const val Speaking = "正在说，直接开口就能打断"
    const val Muted = "已静音"
    const val Ended = "对话已结束"
    /** A tool the plugin did not name. */
    const val Working = "处理中"
    const val Restarted = "连接断了（语音服务可能刚重启，或网络不稳），点下面的按钮重新开始"
    const val NoPlugin = "hub 上没有语音插件"
    const val Microphone = "麦克风打不开"
    const val ModelMissing = "配置的语音模型不在插件目录中，请在模型配置里重新选择。"
    const val ModelUnavailable = "语音模型不可用"
    const val ConfigUnreadable = "读不到语音模型配置"
    const val AskTitle = "语音对话要用麦克风"
    const val AskText = "只在这一页打开时听，离开就结束，不在后台录音。"
    const val DeniedTitle = "没有麦克风权限"
    const val DeniedText = "语音对话用不了。可以在系统设置里给 Persona 打开麦克风。"
    const val Allow = "允许使用麦克风"
    const val Voices = "音色"
    const val NoVoices = "这个模型没有可选的音色。"

    fun unreachable(reason: String) = "连不上语音服务：$reason"
    fun tool(label: String) = "$label…"
    fun voicesFailed(reason: String) = "音色列表没拿到：$reason"
}

/** What one conversation is doing, as the page shows it. */
internal data class VoiceState(
    val phase: Phase = Phase.Connecting,
    val lines: List<Line> = emptyList(),
    /** The tool being used right now (查知识库), said in place of the status. */
    val tool: String? = null,
    /** Persona's voice is playing. */
    val speaking: Boolean = false,
    val muted: Boolean = false,
    /** What went wrong, under the status; a fatal one also ended the conversation. */
    val error: String? = null,
) {
    enum class Phase { Connecting, Live, Ended }
}

/** The status line under the orb. A tool in use is said over speaking: it is why Persona is quiet or still going. */
internal fun statusText(state: VoiceState): String = when {
    state.phase == VoiceState.Phase.Connecting -> VoiceText.Connecting
    state.phase == VoiceState.Phase.Ended -> VoiceText.Ended
    state.tool != null -> VoiceText.tool(state.tool)
    state.speaking -> VoiceText.Speaking
    state.muted -> VoiceText.Muted
    else -> VoiceText.Listening
}

/** How a voice reads in the 音色 sheet: 女声 · 中文、英文 · 默认. */
internal fun voiceDetail(voice: VoiceOption, isDefault: Boolean): String? = listOfNotNull(
    when (voice.gender) { "female" -> "女声"; "male" -> "男声"; else -> null },
    voice.languages.mapNotNull { Languages[it] }.joinToString("、").ifEmpty { null },
    "默认".takeIf { isDefault },
).joinToString(" · ").ifEmpty { null }

private val Languages = mapOf(
    "zh" to "中文", "en" to "英文", "ja" to "日语", "ko" to "韩语", "es" to "西班牙语", "fr" to "法语", "de" to "德语", "yue" to "粤语",
)

/** Why the socket could not be used. [live]: it dropped in the middle of a conversation. */
internal fun connectFailure(error: Throwable, status: Int?, live: Boolean): String = when {
    status == 404 -> VoiceText.NoPlugin
    // Mid-conversation it is nearly always the plugin or the hub restarting, or the network; say that, not the error.
    live -> VoiceText.Restarted
    else -> VoiceText.unreachable(reason(error, status))
}

/** The plugin or the hub closed the socket: 1001 is a plugin restarting (a deploy); 1000 is the end of it. */
internal fun closeMessage(code: Int, reason: String, live: Boolean): String? = when {
    code == 1001 -> VoiceText.Restarted
    code == 1000 -> null
    reason.isNotBlank() -> reason.trim()
    live -> VoiceText.Restarted
    else -> null
}

/** Why the model configuration or the plugin's models could not be read; the page offers to try again. */
internal fun loadFailure(error: Throwable): String {
    val feedback = operationFeedback(error)
    return when {
        feedback.offline -> "连不上 hub，连上后点重试。"
        feedback.action == FailureAction.PairAgain || feedback.action == FailureAction.UpdateHub -> feedback.message
        else -> "hub 没有给出结果" + (hubMessage(error)?.let { "：$it" } ?: "。")
    }
}

/** Why the voices could not be read, to follow 音色列表没拿到：. */
internal fun voicesFailure(error: Throwable): String {
    val feedback = operationFeedback(error)
    return when {
        feedback.offline -> "连不上 hub"
        feedback.action == FailureAction.PairAgain || feedback.action == FailureAction.UpdateHub -> feedback.message
        else -> hubMessage(error) ?: "hub 没有给出结果"
    }
}

private fun reason(error: Throwable, status: Int?): String {
    val feedback = operationFeedback(error)
    return when {
        // The hub refuses plugins to a device that may only read and answer (docs/plugins.md); not an expired pairing.
        status == 403 -> "这台设备不能用语音对话"
        status == 401 || feedback.action == FailureAction.PairAgain -> feedback.message
        status != null && status >= 500 -> "语音服务没有响应"
        status != null -> "HTTP $status"
        feedback.action == FailureAction.UpdateHub -> feedback.message
        feedback.offline -> "连不上 hub"
        else -> hubMessage(error) ?: "连接失败"
    }
}

// A transport failure without the hub's words carries only its kind ("Protocol"), which says nothing to a person.
private val KindName = Regex("[A-Z][A-Za-z]+")

private fun hubMessage(error: Throwable): String? = error.message?.trim()?.takeIf { it.isNotEmpty() && !it.matches(KindName) }

/** Where setting up a conversation stands. */
internal sealed interface VoiceSetup {
    data object Loading : VoiceSetup

    /** The model configuration says the voice model cannot be used as it stands: [reason] says why. */
    data class Unavailable(val reason: String) : VoiceSetup

    /** The configuration could not be read: not the same as having none (docs/model-config.md「生效边界」). */
    data class Failed(val message: String) : VoiceSetup

    /** Ready to talk with [model]; null: the plugin's own default. */
    data class Ready(val model: String?) : VoiceSetup

    /** Stopped before it started (the app went to the background while it was being set up). */
    data object Stopped : VoiceSetup
}

/**
 * The model of the next conversation, from the model configuration of `plugin.<id>` ([choice]) and the plugin's
 * models: an older hub without model configuration (null) gets the plugin's default; a purpose whose status says it
 * cannot be used, or a model the plugin no longer offers, is not quietly swapped for another.
 */
internal fun resolveVoiceModel(choice: VoiceModelChoice?, models: VoiceModelOptions): VoiceSetup {
    val default = models.default?.takeIf { it.isNotBlank() }
    if (choice == null) return VoiceSetup.Ready(default)
    choice.status?.trim()?.takeIf { it.isNotEmpty() && it != "未验证" }?.let { return VoiceSetup.Unavailable(it) }
    val model = choice.model?.takeIf { it.isNotBlank() } ?: return VoiceSetup.Ready(default)
    if (models.models.none { it.id == model }) return VoiceSetup.Unavailable(VoiceText.ModelMissing)
    return VoiceSetup.Ready(model)
}
