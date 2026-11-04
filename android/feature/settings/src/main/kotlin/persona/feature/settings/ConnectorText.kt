package persona.feature.settings

import persona.core.contracts.api.HubConnector
import persona.core.contracts.api.LarkStatus
import persona.core.contracts.api.MailAccount
import persona.core.designsystem.Status

/** What 设置 › 连接器 says on each row (iOS `ConnectorText`). Pure: unit-tested. */
object ConnectorText {
    fun mail(accounts: List<MailAccount>?): String = when {
        accounts == null -> ""
        accounts.isEmpty() -> "未添加"
        accounts.any { it.isError } -> "${accounts.size} 个 · 有错误"
        else -> "${accounts.size} 个账户"
    }

    fun lark(st: LarkStatus?): String = when {
        st == null -> ""
        !st.configured -> "未配置"
        st.connected -> st.name?.takeIf { it.isNotBlank() } ?: "已连接"
        else -> "连不上"
    }

    fun device(on: Int): String = if (on == 0) "未打开" else "$on 项已打开"

    fun state(c: HubConnector): String = when (c.status) {
        "ok" -> "已连接"
        "not_configured" -> "未配置"
        "not_signed_in" -> "未登录"
        "no_permission" -> "没有权限"
        "rate_limited" -> "请求太多，暂停中"
        "disabled" -> "已关闭"
        "error" -> "出错了"
        else -> "状态未知"
    }

    fun summary(c: HubConnector): String =
        if (c.status == "ok") (c.account ?: state(c)) else state(c)

    fun status(c: HubConnector): Status = when (c.status) {
        "ok" -> Status.Idle
        "rate_limited", "error", "no_permission" -> Status.NeedsYou
        else -> Status.Offline
    }
}

/** A row of 设置 › 连接器, in order (iOS `ConnectorRow`). */
internal sealed interface ConnectorRow {
    data object Phone : ConnectorRow
    data object Mail : ConnectorRow
    /** 飞书: its page reads `/api/v1/lark/…`, the lark-connector plugin's (`LarkFeatures`, #1530). */
    data object Lark : ConnectorRow
    data class Hub(val connector: HubConnector) : ConnectorRow
}

/**
 * 设置 › 连接器's rows: this phone, mail, 飞书 while the hub has Feishu chats ([lark], `LarkFeatures.chats`), then the
 * hub's other connectors (mail and Feishu have rows of their own).
 */
internal fun connectorRows(hubConnectors: List<HubConnector>, lark: Boolean): List<ConnectorRow> =
    listOfNotNull(ConnectorRow.Phone, ConnectorRow.Mail, ConnectorRow.Lark.takeIf { lark }) +
        hubConnectors.filter { it.id != "mail" && it.id != "lark" }.map(ConnectorRow::Hub)
