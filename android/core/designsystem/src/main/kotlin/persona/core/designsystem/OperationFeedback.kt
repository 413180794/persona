package persona.core.designsystem

import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import persona.core.domain.*
import persona.core.hub.HubError
import persona.core.hub.HubFailure

/**
 * What to tell the user after an operation failed. [message] and [actionLabel] are the Simplified Chinese words for
 * the code outside composition that still reads them; pages show [localizedMessage] / [localizedActionLabel], and
 * code with a [android.content.Context] resolves [text] and [actionLabelRes].
 */
data class OperationFeedback(val message: String, val action: FailureAction, val offline: Boolean = false, val text: UiText = UiText.Plain(message)) {
    val actionLabel: String? get() = when (action) {
        FailureAction.None -> null
        FailureAction.Retry -> "重试"
        FailureAction.Verify -> "核对"
        FailureAction.PairAgain -> "重新配对"
        FailureAction.OpenTerminal -> "打开终端"
        FailureAction.ReviewInput -> "检查输入"
        FailureAction.UpdateHub -> "更新 hub"
        FailureAction.GrantPermission -> "去授权"
    }

    @get:StringRes val actionLabelRes: Int? get() = when (action) {
        FailureAction.None -> null
        FailureAction.Retry -> R.string.designsystem_retry
        FailureAction.Verify -> R.string.designsystem_feedback_verify
        FailureAction.PairAgain -> R.string.designsystem_feedback_pair_again
        FailureAction.OpenTerminal -> R.string.designsystem_feedback_open_terminal
        FailureAction.ReviewInput -> R.string.designsystem_feedback_review_input
        FailureAction.UpdateHub -> R.string.designsystem_feedback_update_hub
        FailureAction.GrantPermission -> R.string.designsystem_feedback_grant_permission
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
    return operationFeedback(mapOperationFailure(code, delivered, failure.retryable, category))
}

fun operationFeedback(failure: OperationFailure): OperationFeedback {
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
    }
    return OperationFeedback(text, failure.action, failure.offline, UiText.Res(id))
}
