package jarvis.android.mail

import androidx.compose.runtime.saveable.Saver

/** The 添加邮箱 form (iOS `AddMailAccountView` state) and what it sends. Pure: unit-tested in MailAccountFormTest. */
data class MailAccountForm(
    val kind: Kind = Kind.GMAIL,
    val email: String = "",
    val password: String = "",
    val imapHost: String = "",
    val imapPort: String = "993",
    val smtpHost: String = "",
    val smtpPort: String = "465",
) {
    enum class Kind(val title: String) { GMAIL("Gmail"), FEISHU("飞书"), IMAP("其他 IMAP") }

    /** 连接 is possible: address and password typed (Gmail only through the app-password fallback). */
    fun canConnect(gmailPasswordAllowed: Boolean): Boolean =
        email.contains('@') && password.isNotEmpty() && (kind != Kind.GMAIL || gmailPasswordAllowed)

    /** The POST /api/v1/mail/accounts body (iOS `save`); servers only when typed (else the domain preset). */
    fun request(): AddMailAccountRequest {
        val base = AddMailAccountRequest(name = email, email = email, auth = "password", password = password)
        return when (kind) {
            Kind.GMAIL -> base.copy(provider = "gmail")
            Kind.FEISHU -> base.copy(provider = "feishu")
            Kind.IMAP -> {
                var r = base.copy(provider = "imap")
                if (imapHost.isNotEmpty()) r = r.copy(imapHost = imapHost, imapPort = imapPort.toIntOrNull())
                if (smtpHost.isNotEmpty()) {
                    r = r.copy(smtpHost = smtpHost, smtpPort = smtpPort.toIntOrNull(), smtpSecurity = if (smtpPort == "587") "starttls" else "tls")
                }
                r
            }
        }
    }

    companion object {
        /** Keeps the form across rotation / process death — without the password (it is not written to disk). */
        val Saver: Saver<MailAccountForm, Any> = Saver(
            save = { f -> arrayListOf(f.kind.name, f.email, f.imapHost, f.imapPort, f.smtpHost, f.smtpPort) },
            restore = { v ->
                @Suppress("UNCHECKED_CAST") val l = v as List<String>
                MailAccountForm(Kind.valueOf(l[0]), l[1], "", l[2], l[3], l[4], l[5])
            },
        )
    }
}

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
}
