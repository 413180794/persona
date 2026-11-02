package jarvis.core.designsystem

import jarvis.core.domain.*
import jarvis.core.hub.HubError
import jarvis.core.hub.HubFailure

data class OperationFeedback(val message: String, val action: FailureAction, val offline: Boolean = false) {
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
}

fun operationFeedback(error: Throwable): OperationFeedback {
    if (error is jarvis.core.contracts.api.InteractionGoneException)
        return operationFeedback(mapOperationFailure("already_handled", "none", false))
    if (error is jarvis.core.contracts.api.InteractionPromptChangedException)
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
    val delivered = if ((error as? jarvis.core.contracts.api.AgentOperationFailure)?.uncertain == true && failure.delivered == null)
        "unknown" else failure.delivered
    return operationFeedback(mapOperationFailure(code, delivered, failure.retryable, category))
}

fun operationFeedback(failure: OperationFailure): OperationFeedback {
    val text = when (failure.reason) {
        FailureReason.Demo -> "体验模式下不会真的发送"
        FailureReason.PairingExpired -> "配对已失效，请重新扫码"
        FailureReason.CertificateChanged -> "hub 的证书变了，请重新扫码"
        FailureReason.Incompatible -> "hub 版本或安全配置不兼容，请更新 hub 后重新连接"
        FailureReason.LocalPermission -> "请允许访问本地网络后重新连接"
        FailureReason.InputDraft -> "电脑上有草稿，请先到终端处理；手机输入已保留"
        FailureReason.DialogOpen -> "电脑上有对话框，请先处理交互或打开终端"
        FailureReason.StaleSession -> "会话已变化，请刷新并核对目标会话"
        FailureReason.Invalid -> "操作内容不符合要求，请检查输入"
        FailureReason.IdReused -> "操作标识已被使用，请先核对原操作结果"
        FailureReason.PromptChanged -> "问题已变化，请刷新后重新选择"
        FailureReason.Expired -> "请求已过期或已处理，请回到终端核对"
        FailureReason.RateLimited -> if (failure.action == FailureAction.Retry) "操作太频繁，请稍后重试" else "操作太频繁，请稍后查看状态"
        FailureReason.NotDelivered -> "连不上 hub，刚才的操作没有生效"
        FailureReason.Unknown -> "操作结果待确认，请回到会话核对"
        FailureReason.Delivered -> "操作已送达，请回到会话核对结果"
        FailureReason.Failed -> if (failure.action == FailureAction.Retry) "操作未完成，请重试" else "操作未完成，请检查操作条件"
    }
    return OperationFeedback(text, failure.action, failure.offline)
}
