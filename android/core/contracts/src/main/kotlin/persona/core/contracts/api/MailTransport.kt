package persona.core.contracts.api

import persona.core.contracts.CoreTextFailure
import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import java.io.OutputStream

/** Bound to one pairing; replace the transport when the pairing changes. */
interface MailTransport {
    val scope: String
    suspend fun item(itemId: String): MailInboxItem
    suspend fun message(ref: MailReference, uid: Long): MailMessage
    suspend fun setSeen(ref: MailReference, uid: Long, seen: Boolean)
    suspend fun setFlagged(ref: MailReference, uid: Long, flagged: Boolean) {
        setFlags(ref, uid, flagged = flagged)
    }
    suspend fun setFlags(ref: MailReference, uid: Long, seen: Boolean? = null, flagged: Boolean? = null) {
        if (seen != null) setSeen(ref, uid, seen)
    }
    suspend fun accounts(): List<MailAccount> = emptyList()
    suspend fun sendMail(request: SendMailRequest) {
        throw UnsupportedOperationException("Sending mail unavailable")
    }
    suspend fun refreshInbox()
    suspend fun downloadAttachment(ref: MailReference, uid: Long, attachmentId: String, output: OutputStream,
        offset: Long = 0, progress: (Long, Long?) -> Unit = { _, _ -> }): Unit =
        throw UnsupportedOperationException("Attachment downloads unavailable")
}

@Serializable
data class MailReference(
    @SerialName("account_id") val accountId: String,
    val mailbox: String = "INBOX",
    val uids: List<Long> = emptyList(),
)

@Serializable
data class MailInboxItem(val id: String, val mail: MailReference? = null,
    @SerialName("reply_target") val replyTarget: MailReplyTarget? = null)

@Serializable
data class MailReplyTarget(val uid: Long, val version: String)

class MailReplyTargetChanged : java.io.IOException(CoreText.of(Key.MailReplyTargetChanged).english()), CoreTextFailure {
    override val text: CoreText get() = CoreText.of(Key.MailReplyTargetChanged)
    override val message: String get() = CoreText.words(text)
}

@Serializable
data class SendMailRequest(
    @SerialName("account_id") val accountId: String,
    val to: List<MailAddress>,
    val cc: List<MailAddress> = emptyList(),
    val bcc: List<MailAddress> = emptyList(),
    val subject: String = "",
    val text: String = "",
    @SerialName("in_reply_to") val inReplyTo: String? = null,
    val references: String? = null,
)

@Serializable
data class MailAddress(val name: String = "", val email: String = "") {
    fun display(): String = if (name.isBlank()) email else "$name <$email>"
}

@Serializable
data class MailAttachment(val filename: String = "", val mime: String = "", val size: Long = 0, val id: String = "")

@Serializable
data class MailMessage(
    @SerialName("account_id") val accountId: String,
    val mailbox: String = "INBOX",
    val uid: Long,
    val subject: String = "",
    val from: MailAddress = MailAddress(),
    val to: List<MailAddress> = emptyList(),
    val cc: List<MailAddress> = emptyList(),
    val date: String = "",
    val seen: Boolean = false,
    val flagged: Boolean = false,
    val text: String = "",
    val html: String = "",
    val attachments: List<MailAttachment> = emptyList(),
    @SerialName("message_id") val messageId: String = "",
    @SerialName("in_reply_to") val inReplyTo: String = "",
    val references: List<String> = emptyList(),
)
