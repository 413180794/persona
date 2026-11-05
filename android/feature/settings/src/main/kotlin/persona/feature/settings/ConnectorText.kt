package persona.feature.settings

import persona.core.contracts.api.HubConnector
import persona.core.contracts.api.LarkStatus
import persona.core.contracts.api.MailAccount
import persona.core.designsystem.Status
import android.content.res.Resources
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalResources

/** What 设置 › 连接器 says on each row (iOS `ConnectorText`). Pure given [Resources]: unit-tested. */
object ConnectorText {
    fun mail(res: Resources, accounts: List<MailAccount>?): String = when {
        accounts == null -> ""
        accounts.isEmpty() -> res.getString(R.string.settings_src_connector_none_added)
        accounts.any { it.isError } -> res.getQuantityString(R.plurals.settings_src_connector_mail_errors, accounts.size, accounts.size)
        else -> res.getQuantityString(R.plurals.settings_src_connector_mail_accounts, accounts.size, accounts.size)
    }

    fun lark(res: Resources, st: LarkStatus?): String = when {
        st == null -> ""
        !st.configured -> res.getString(R.string.settings_src_state_not_configured)
        st.connected -> st.name?.takeIf { it.isNotBlank() } ?: res.getString(R.string.settings_src_state_connected)
        else -> res.getString(R.string.settings_src_state_unreachable)
    }

    fun device(res: Resources, on: Int): String =
        if (on == 0) res.getString(R.string.settings_src_connector_device_none) else res.getQuantityString(R.plurals.settings_src_connector_device_on, on, on)

    fun state(res: Resources, c: HubConnector): String = res.getString(when (c.status) {
        "ok" -> R.string.settings_src_state_connected
        "not_configured" -> R.string.settings_src_state_not_configured
        "not_signed_in" -> R.string.settings_src_state_signed_out
        "no_permission" -> R.string.settings_src_state_no_permission
        "rate_limited" -> R.string.settings_src_state_rate_limited
        "disabled" -> R.string.settings_src_state_off
        "error" -> R.string.settings_src_state_error
        else -> R.string.settings_src_state_unknown
    })

    fun summary(res: Resources, c: HubConnector): String =
        if (c.status == "ok") (c.account ?: state(res, c)) else state(res, c)

    @Composable fun mail(accounts: List<MailAccount>?): String = mail(resources(), accounts)
    @Composable fun lark(st: LarkStatus?): String = lark(resources(), st)
    @Composable fun device(on: Int): String = device(resources(), on)
    @Composable fun state(c: HubConnector): String = state(resources(), c)
    @Composable fun summary(c: HubConnector): String = summary(resources(), c)

    @Composable
    private fun resources(): Resources = LocalResources.current

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
    /** 飞书: its page reads `/api/v1/lark/…`, the lark-connector extension's (`LarkFeatures`, #1530). */
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
