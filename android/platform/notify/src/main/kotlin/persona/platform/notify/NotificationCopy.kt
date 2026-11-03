package persona.platform.notify

import persona.core.contracts.api.ConnectionState
import persona.core.designsystem.PendingStatus
import persona.core.domain.answer.RequestExpiry

internal fun ongoingTitle(state: ConnectionState): String = when (state) {
    ConnectionState.Online -> "已连接 hub"
    ConnectionState.Connecting -> "连接中…"
    ConnectionState.Updating -> "更新中…"
    ConnectionState.WaitingForNetwork -> PendingStatus.WaitingForNetwork.label
    ConnectionState.NeedsLocalNetworkPermission -> "需要本地网络权限"
    ConnectionState.CertificateMismatch -> "hub 的证书变了"
    ConnectionState.Revoked -> "配对已失效"
}

internal fun actionResultCopy(result: NotificationActionResult): Pair<String, String>? = when (result) {
    NotificationActionResult.Expired -> RequestExpiry.expiredLabel to "请打开终端处理。"
    NotificationActionResult.Gone -> "已在电脑上处理" to "这条请求已不在待处理列表中，请打开会话查看。"
    NotificationActionResult.Locked -> "请先解锁" to "解锁后再提交你的选择。"
    NotificationActionResult.Invalid -> "操作不可用" to "请打开会话查看最新请求。"
    NotificationActionResult.Duplicate -> PendingStatus.Unconfirmed.label to "已记录这次操作，请打开会话核对结果，不要重复提交。"
    NotificationActionResult.Submitted -> null
}
