package persona.feature.settings

import androidx.annotation.StringRes
import androidx.compose.runtime.saveable.Saver
import persona.core.contracts.api.AddMailAccountRequest

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
    enum class Kind(@StringRes val title: Int) {
        GMAIL(R.string.settings_src_mail_kind_gmail), OUTLOOK(R.string.settings_src_mail_kind_outlook),
        FEISHU(R.string.settings_src_mail_kind_feishu), IMAP(R.string.settings_src_mail_kind_imap),
    }

    /** 连接 is possible: address and password typed (Gmail only through the app-password fallback; Outlook signs in with Microsoft). */
    fun canConnect(gmailPasswordAllowed: Boolean): Boolean =
        kind != Kind.OUTLOOK && email.contains('@') && password.isNotEmpty() && (kind != Kind.GMAIL || gmailPasswordAllowed)

    /** The POST /api/v1/mail/accounts body (iOS `save`); servers only when typed (else the domain preset). */
    fun request(): AddMailAccountRequest {
        val base = AddMailAccountRequest(name = email, email = email, auth = "password", password = password)
        return when (kind) {
            Kind.GMAIL -> base.copy(provider = "gmail")
            Kind.OUTLOOK -> base.copy(provider = "outlook")
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
