package persona.android.ui

import persona.core.contracts.api.ConnectionState

internal typealias FeedbackGate = persona.core.designsystem.FeedbackGate

internal fun ConnectionState.title(): String? = when (this) {
    ConnectionState.Online -> null
    ConnectionState.Connecting -> "连接中…"
    ConnectionState.Updating -> "更新中…"
    ConnectionState.WaitingForNetwork -> "等待网络"
    ConnectionState.NeedsLocalNetworkPermission -> "需要本地网络权限"
    ConnectionState.CertificateMismatch -> "hub 的证书变了"
    ConnectionState.Revoked -> "配对已失效"
}
