package persona.core.designsystem

import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import persona.core.domain.*
import persona.core.hub.HubError
import persona.core.hub.HubFailure

/**
 * What to tell the user after an operation failed. Pages show [localizedMessage] / [localizedActionLabel], and code
 * with `Resources` resolves [text] and [actionLabelRes]: that is what every screen shows. [message] is a fallback for
 * code without resources: Simplified Chinese, except for the main conversation's coded refusals (the hub's own words
 * for the ones it words, else a plain English stand-in; never the hub's raw error, see [operationFeedback]).
 */
data class OperationFeedback(val message: String, val action: FailureAction, val offline: Boolean = false, val text: UiText = UiText.Plain(message)) {
    @get:StringRes val actionLabelRes: Int? get() = when (action) {
        FailureAction.None -> null
        FailureAction.Retry -> R.string.designsystem_retry
        FailureAction.Verify -> R.string.designsystem_feedback_verify
        FailureAction.PairAgain -> R.string.designsystem_feedback_pair_again
        FailureAction.OpenTerminal -> R.string.designsystem_feedback_open_terminal
        FailureAction.ReviewInput -> R.string.designsystem_feedback_review_input
        FailureAction.UpdateHub -> R.string.designsystem_feedback_update_hub
        FailureAction.GrantPermission -> R.string.designsystem_feedback_grant_permission
        FailureAction.AddComputer -> R.string.designsystem_readiness_action_add_computer
        FailureAction.ConfirmComputer -> R.string.designsystem_readiness_action_confirm_computer
        FailureAction.WakeComputer -> R.string.designsystem_readiness_action_wake_computer
        FailureAction.InstallClaude -> R.string.designsystem_readiness_action_install_claude
        FailureAction.LoginClaude -> R.string.designsystem_readiness_action_login_claude
        FailureAction.ConnectModel -> R.string.designsystem_readiness_action_connect_model
        FailureAction.EditProvider -> R.string.designsystem_readiness_action_edit_provider
    }

    val localizedMessage: String @Composable get() = text.text()
    val localizedActionLabel: String? @Composable get() = actionLabelRes?.let { stringResource(it) }
}

fun operationFeedback(error: Throwable): OperationFeedback {
    if (error is persona.core.contracts.api.InteractionGoneException)
        return operationFeedback(mapOperationFailure("already_handled", "none", false))
    if (error is persona.core.contracts.api.InteractionPromptChangedException)
        return operationFeedback(mapOperationFailure("prompt_changed", "none", false))
    val failure = HubFailure.from(error)
    val category = when (failure.kind) {
        HubError.PairingExpired, HubError.HubMismatch -> FailureCategory.Pairing
        HubError.CertificateMismatch -> FailureCategory.Certificate
        HubError.Incompatible, HubError.TlsRequired -> FailureCategory.Incompatible
        HubError.LocalNetworkPermission -> FailureCategory.LocalPermission
        HubError.NoNetwork, HubError.AddressNotFound, HubError.ConnectionRefused, HubError.Unreachable,
        HubError.Timeout, HubError.Transport -> FailureCategory.Network
        else -> FailureCategory.Other
    }
    val code = failure.code ?: when (failure.kind) {
        HubError.NoNetwork -> "no_network"
        HubError.RateLimited -> "rate_limited"
        else -> null
    }
    val delivered = if ((error as? persona.core.contracts.api.AgentOperationFailure)?.uncertain == true && failure.delivered == null)
        "unknown" else failure.delivered
    // The hub words these refusals in the request's language (docs/onboarding.md 4.6, apierr.codeActions); for the
    // other codes its `error` is raw detail (node_unreachable: "rpc: connection closed"), never shown.
    val words = failure.message.takeIf { code in HubWordedCodes && failure.status != null && it.isNotBlank() && !it.startsWith("HTTP ") }
    return operationFeedback(mapOperationFailure(code, delivered, failure.retryable, category), words)
}

/**
 * [hubWords]: the hub's own sentence for one of [HubWordedCodes], which [OperationFeedback.message] carries (the app's
 * wording of the coded refusals is only in the string resources, [OperationFeedback.text]).
 */
fun operationFeedback(failure: OperationFailure, hubWords: String? = null): OperationFeedback {
    codedFeedback(failure.reason)?.let { (fallback, id) ->
        return OperationFeedback(hubWords ?: fallback, failure.action, failure.offline, UiText.Res(id))
    }
    val retry = failure.action == FailureAction.Retry
    val (text, id) = when (failure.reason) {
        FailureReason.Demo -> "体验模式下不会真的发送" to R.string.designsystem_feedback_demo
        FailureReason.PairingExpired -> "配对已失效，请重新扫码" to R.string.designsystem_feedback_pairing_expired
        FailureReason.CertificateChanged -> "hub 的证书变了，请重新扫码" to R.string.designsystem_feedback_certificate_changed
        FailureReason.Incompatible -> "hub 版本或安全配置不兼容，请更新 hub 后重新连接" to R.string.designsystem_feedback_incompatible
        FailureReason.LocalPermission -> "请允许访问本地网络后重新连接" to R.string.designsystem_feedback_local_permission
        FailureReason.InputDraft -> "电脑上有草稿，请先到终端处理；手机输入已保留" to R.string.designsystem_feedback_input_draft
        FailureReason.DialogOpen -> "电脑上有对话框，请先处理交互或打开终端" to R.string.designsystem_feedback_dialog_open
        FailureReason.StaleSession -> "会话已变化，请刷新并核对目标会话" to R.string.designsystem_feedback_stale_session
        FailureReason.Invalid -> "操作内容不符合要求，请检查输入" to R.string.designsystem_feedback_invalid
        FailureReason.IdReused -> "操作标识已被使用，请先核对原操作结果" to R.string.designsystem_feedback_id_reused
        FailureReason.PromptChanged -> "问题已变化，请刷新后重新选择" to R.string.designsystem_feedback_prompt_changed
        FailureReason.Expired -> "请求已过期或已处理，请回到终端核对" to R.string.designsystem_feedback_expired
        FailureReason.RateLimited -> if (retry) "操作太频繁，请稍后重试" to R.string.designsystem_feedback_rate_limited_retry else "操作太频繁，请稍后查看状态" to R.string.designsystem_feedback_rate_limited
        FailureReason.NotDelivered -> "连不上 hub，刚才的操作没有生效" to R.string.designsystem_feedback_not_delivered
        FailureReason.Unknown -> "操作结果待确认，请回到会话核对" to R.string.designsystem_feedback_unknown
        FailureReason.Delivered -> "操作已送达，请回到会话核对结果" to R.string.designsystem_feedback_delivered
        FailureReason.Failed -> if (retry) "操作未完成，请重试" to R.string.designsystem_feedback_failed_retry else "操作未完成，请检查操作条件" to R.string.designsystem_feedback_failed
        else -> error("Coded refusals are worded by codedFeedback")
    }
    return OperationFeedback(text, failure.action, failure.offline, UiText.Res(id))
}

/** The refusals whose `error` the hub writes in plain words in the request's language (server apierr.codeActions). */
private val HubWordedCodes = setOf("no_machine", "machine_pending", "claude_missing", "launcher_missing")

/** The main conversation's coded refusals: an English stand-in for code that reads [OperationFeedback.message], and the words. */
private fun codedFeedback(reason: FailureReason): Pair<String, Int>? = when (reason) {
    FailureReason.NoMachine -> "No computer is connected yet." to R.string.designsystem_feedback_no_machine
    FailureReason.MachinePending -> "This computer is not confirmed yet." to R.string.designsystem_feedback_machine_pending
    FailureReason.ComputerOffline -> "The computer is offline." to R.string.designsystem_feedback_computer_offline
    FailureReason.ClaudeMissing -> "Claude Code is not installed on that computer." to R.string.designsystem_feedback_claude_missing
    FailureReason.LauncherMissing -> "That computer cannot find this model." to R.string.designsystem_feedback_launcher_missing
    FailureReason.NotLoggedIn -> "Claude Code is not signed in." to R.string.designsystem_feedback_not_logged_in
    FailureReason.AuthFailed -> "The model key is wrong or expired." to R.string.designsystem_feedback_auth_failed
    FailureReason.AssistantRestarting -> "The assistant is restarting." to R.string.designsystem_feedback_assistant_restarting
    else -> null
}
