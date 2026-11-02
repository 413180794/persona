package jarvis.android.mail

import jarvis.android.data.parseTime
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.time.Instant

// The mail API's shapes (server/internal/mail/types.go, docs/protocol.md「邮件 API」, iOS Shared/Models.swift「Mail」).
// Lenient like the rest of the app: everything has a default, unknown fields are ignored (JsonCodec).

@Serializable
data class MailAddress(val name: String? = null, val email: String = "") {
    /** The name when there is one, else the address (iOS `display`). */
    val display: String get() = if (!name.isNullOrBlank()) name else email
}

@Serializable
data class MailAccount(
    val id: String,
    val name: String? = null,
    val email: String = "",
    /** gmail | imap | feishu */
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
            auth == "oauth2" -> "Gmail · Google 账号登录"
            provider == "feishu" -> "飞书邮箱"
            else -> imapHost.orEmpty()
        }
}

@Serializable
data class MailAttachment(val filename: String? = null, val mime: String? = null, val size: Long? = null)

@Serializable
data class MailMessage(
    @SerialName("account_id") val accountId: String = "",
    val mailbox: String? = null,
    val uid: Long = 0,
    @SerialName("message_id") val messageId: String? = null,
    val subject: String? = null,
    val from: MailAddress? = null,
    val to: List<MailAddress>? = null,
    val cc: List<MailAddress>? = null,
    @SerialName("date") val dateText: String? = null,
    val text: String? = null,
    val html: String? = null,
    val attachments: List<MailAttachment>? = null,
    @SerialName("in_reply_to") val inReplyTo: String? = null,
    val references: List<String>? = null,
    val seen: Boolean? = null,
    val flagged: Boolean? = null,
) {
    val date: Instant? get() = parseTime(dateText)
}

/** Body of `POST /api/v1/mail/send`. `references` is one space-separated string on the hub (types.go `SendRequest`). */
@Serializable
data class SendMailRequest(
    @SerialName("account_id") val accountId: String,
    val to: List<MailAddress>,
    val cc: List<MailAddress> = emptyList(),
    val bcc: List<MailAddress> = emptyList(),
    val subject: String,
    val text: String,
    @SerialName("in_reply_to") val inReplyTo: String? = null,
    val references: String? = null,
)

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

/** `POST /api/v1/mail/oauth/google/start`: Google's consent URL (PKCE) and the scheme it calls back on. */
@Serializable
data class GoogleStart(
    @SerialName("auth_url") val authUrl: String = "",
    @SerialName("callback_scheme") val callbackScheme: String = "",
    val state: String = "",
)

@Serializable
data class GoogleFinish(val state: String, val code: String? = null, val error: String? = null)

/** `POST /api/v1/mail/accounts/{id}/test`: 200 either way, `ok: false` + `error` when the login fails. */
@Serializable
data class MailTestResult(val ok: Boolean = false, val error: String? = null)

/** GET /api/v1/lark/status (设置 › 连接器 › 飞书): whether the hub reaches lark-connector and who is signed in there. */
@Serializable
data class LarkStatus(
    val configured: Boolean = false,
    val connected: Boolean = false,
    val name: String? = null,
    val error: String? = null,
    /** Unix seconds of the last real-time event. */
    @SerialName("last_event_at") val lastEventAt: Long? = null,
)
