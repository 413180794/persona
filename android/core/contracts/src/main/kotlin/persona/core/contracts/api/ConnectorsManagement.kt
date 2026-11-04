package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class MailAccount(
    val id: String,
    val name: String? = null,
    val email: String = "",
    /** gmail | imap | feishu | outlook */
    val provider: String? = null,
    /** password | oauth2 */
    val auth: String? = null,
    val username: String? = null,
    @SerialName("imap_host") val imapHost: String? = null,
    @SerialName("imap_port") val imapPort: Int? = null,
    @SerialName("smtp_host") val smtpHost: String? = null,
    @SerialName("smtp_port") val smtpPort: Int? = null,
    @SerialName("smtp_security") val smtpSecurity: String? = null,
    /** ok | error */
    val status: String? = null,
    val error: String? = null,
) {
    val isError: Boolean get() = status == "error"

    /** The line under the address in 邮箱 (iOS `MailAccountsView`). */
    val detail: String
        get() = when {
            isError -> error?.takeIf { it.isNotBlank() } ?: "错误"
            auth == "oauth2" -> if (provider == "outlook") "Outlook · Microsoft 账号登录" else "Gmail · Google 账号登录"
            provider == "feishu" -> "飞书邮箱"
            else -> imapHost.orEmpty()
        }
}

/** Body of `POST /api/v1/mail/accounts`; servers left out are filled from the domain's preset on the hub. */
@Serializable
data class AddMailAccountRequest(
    val name: String = "",
    val email: String = "",
    val provider: String? = null,
    val auth: String? = null,
    val username: String? = null,
    val password: String? = null,
    @SerialName("imap_host") val imapHost: String? = null,
    @SerialName("imap_port") val imapPort: Int? = null,
    @SerialName("smtp_host") val smtpHost: String? = null,
    @SerialName("smtp_port") val smtpPort: Int? = null,
    @SerialName("smtp_security") val smtpSecurity: String? = null,
)

/** `POST /api/v1/mail/accounts/{id}/test`: 200 either way, `ok: false` + `error` when the login fails. */
@Serializable
data class MailTestResult(val ok: Boolean = false, val error: String? = null)

/** `POST /api/v1/mail/oauth/google/start`: Google's consent URL (PKCE) and the scheme it calls back on. */
@Serializable
data class GoogleStart(
    @SerialName("auth_url") val authUrl: String = "",
    @SerialName("callback_scheme") val callbackScheme: String = "",
    val state: String = "",
)

@Serializable
data class GoogleFinish(val state: String, val code: String? = null, val error: String? = null)

/** `GET /api/v1/lark/status` (设置 › 连接器 › 飞书): whether the hub reaches lark-connector and who is signed in there. */
@Serializable
data class LarkStatus(
    val configured: Boolean = false,
    val connected: Boolean = false,
    val name: String? = null,
    val error: String? = null,
    /** Unix seconds of the last real-time event. */
    @SerialName("last_event_at") val lastEventAt: Long? = null,
)

@Serializable
data class LarkAccount(
    val id: String,
    val title: String = "",
    val avatar: String? = null,
    val tenant: String? = null,
    @SerialName("signed_in") val signedIn: Boolean = true,
    val unread: Int = 0,
    val quarantined: Boolean? = null,
)

@Serializable
data class LarkAccounts(
    val accounts: List<LarkAccount> = emptyList(),
    val default: String? = null,
)

/**
 * One of `GET /api/v1/connectors`: a source the hub reads (GitHub…) and how it is doing (docs/protocol.md「连接器 API」).
 */
@Serializable
data class HubConnector(
    val id: String,
    val name: String,
    val status: String = "",
    val account: String? = null,
    val via: String? = null,
    val error: String? = null,
    val guide: List<String>? = null,
    @SerialName("last_sync") val lastSync: String? = null,
    @SerialName("last_event") val lastEvent: String? = null,
    val capabilities: List<String>? = null,
    val hosts: List<String>? = null,
    val enabled: Boolean? = null,
    val archive: Boolean? = null,
    val inbox: Boolean? = null,
)

/** What deleting a connector's data removed (`DELETE /api/v1/connectors/{id}/data`, `…/archive`). */
@Serializable
data class ConnectorDataDeleted(
    val chats: Int? = null,
    val people: Int? = null,
    val inbox: Int? = null,
)

/**
 * 设置 › 连接器 (docs/connectors.md, docs/mail.md): managing mail accounts, Google OAuth,
 * lark-connector Feishu connection status and accounts, and general hub connectors.
 */
interface ConnectorsManagement {
    /** `GET /api/v1/connectors?all=1`. */
    suspend fun connectors(all: Boolean = true): List<HubConnector>

    /** `PUT /api/v1/connectors/{id}/settings` {"enabled": enabled}. */
    suspend fun setConnectorEnabled(id: String, enabled: Boolean): HubConnector

    /** `PUT /api/v1/connectors/{id}/settings` {"archive": on} or {"inbox": on}. */
    suspend fun setConnectorFeed(id: String, feed: String, on: Boolean): HubConnector

    /** `DELETE /api/v1/connectors/{id}/data`. */
    suspend fun deleteConnectorData(id: String): ConnectorDataDeleted

    /** `DELETE /api/v1/connectors/{id}/archive`. */
    suspend fun deleteConnectorArchive(id: String): ConnectorDataDeleted

    // Mail API
    suspend fun mailAccounts(): List<MailAccount>
    suspend fun addMailAccount(req: AddMailAccountRequest): MailAccount
    suspend fun deleteMailAccount(id: String)
    suspend fun testMailAccount(id: String): MailTestResult
    suspend fun googleSignInStart(loginHint: String? = null): GoogleStart
    suspend fun googleSignInFinish(finish: GoogleFinish): MailAccount

    // Lark API
    suspend fun larkStatus(): LarkStatus
    suspend fun larkAccounts(): LarkAccounts?

    /** 设置 › 翻译's chat picker: `GET /api/v1/lark/pinned` (when the hub has it), then `/api/v1/lark/conversations?scope=chats`. */
    suspend fun larkChatChoices(): List<LarkChatChoice>
}
