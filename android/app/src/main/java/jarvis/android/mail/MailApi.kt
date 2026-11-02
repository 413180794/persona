package jarvis.android.mail

import jarvis.android.data.ApiClient
import jarvis.android.data.JsonCodec
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString

// /api/v1/mail/* (server/internal/mail/http.go, docs/protocol.md「邮件 API」) and the Feishu status, as ApiClient
// extensions over its `raw` call (Dispatchers.IO there).

@Serializable private data class AccountsBody(val accounts: List<MailAccount> = emptyList())
@Serializable private data class AccountBody(val account: MailAccount)
@Serializable private data class MessageBody(val message: MailMessage)
@Serializable private data class FlagsBody(val mailbox: String, val seen: Boolean? = null, val flagged: Boolean? = null)
@Serializable private data class LoginHint(@SerialName("login_hint") val loginHint: String)

suspend fun ApiClient.mailAccounts(): List<MailAccount> =
    JsonCodec.decodeFromString<AccountsBody>(raw("GET", "/api/v1/mail/accounts")).accounts

suspend fun ApiClient.mailMessage(accountId: String, uid: Long, mailbox: String): MailMessage =
    JsonCodec.decodeFromString<MessageBody>(raw("GET", "/api/v1/mail/messages/$accountId/$uid", query = mapOf("mailbox" to mailbox))).message

suspend fun ApiClient.mailSetFlags(accountId: String, uid: Long, mailbox: String, seen: Boolean? = null, flagged: Boolean? = null) {
    raw("POST", "/api/v1/mail/messages/$accountId/$uid/flags", JsonCodec.encodeToString(FlagsBody(mailbox, seen, flagged)))
}

/** Sends a mail through the account's SMTP / Gmail API (the hub copies it into 已发送). */
suspend fun ApiClient.sendMail(req: SendMailRequest) {
    raw("POST", "/api/v1/mail/send", JsonCodec.encodeToString(req))
}

/** Adds an account; the hub logs in to IMAP and SMTP first and refuses what does not work. */
suspend fun ApiClient.addMailAccount(req: AddMailAccountRequest): MailAccount =
    JsonCodec.decodeFromString<AccountBody>(raw("POST", "/api/v1/mail/accounts", JsonCodec.encodeToString(req))).account

suspend fun ApiClient.deleteMailAccount(id: String) {
    raw("DELETE", "/api/v1/mail/accounts/$id")
}

suspend fun ApiClient.testMailAccount(id: String): MailTestResult =
    JsonCodec.decodeFromString(raw("POST", "/api/v1/mail/accounts/$id/test", "{}"))

/** Gmail's 「使用 Google 账号登录」: the hub builds the consent URL (PKCE, state sealed with its key). */
suspend fun ApiClient.googleSignInStart(loginHint: String? = null): GoogleStart =
    JsonCodec.decodeFromString(
        raw("POST", "/api/v1/mail/oauth/google/start", if (loginHint.isNullOrBlank()) "{}" else JsonCodec.encodeToString(LoginHint(loginHint))),
    )

/** Hands the callback's code to the hub, which swaps it for a refresh token, checks the mailbox and saves the account. */
suspend fun ApiClient.googleSignInFinish(f: GoogleFinish): MailAccount =
    JsonCodec.decodeFromString<AccountBody>(raw("POST", "/api/v1/mail/oauth/google/finish", JsonCodec.encodeToString(f))).account

suspend fun ApiClient.larkStatus(): LarkStatus = JsonCodec.decodeFromString(raw("GET", "/api/v1/lark/status"))
