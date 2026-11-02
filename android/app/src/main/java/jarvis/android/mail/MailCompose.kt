package jarvis.android.mail

import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** A mail being written (写邮件 / 回复 / 转发): addresses as typed, the quote already in [body]. */
data class MailDraft(
    val accountId: String,
    val to: String = "",
    val cc: String = "",
    val subject: String = "",
    val body: String = "",
    val inReplyTo: String? = null,
    val references: List<String> = emptyList(),
) {
    /** The page's title. */
    val title: String
        get() = when {
            inReplyTo != null -> "回复"
            subject.startsWith("Fwd:") -> "转发"
            else -> "写邮件"
        }
}

/**
 * Building replies / forwards and reading address fields (iOS `ComposeDraft.reply`, `ComposeView.addresses`).
 * Pure functions: unit-tested in MailComposeTest.
 */
object MailCompose {
    /** How many lines of the original a reply quotes (the hub's /reply quotes the same 40). */
    const val QUOTE_LINES = 40

    private val DATE = DateTimeFormatter.ofPattern("yyyy年M月d日 HH:mm")

    /**
     * Addresses typed into 收件人 / 抄送: split on commas, semicolons (half and full width) and new lines;
     * `Name <a@b>` keeps the name. Empty pieces are dropped.
     */
    fun parseAddresses(s: String): List<MailAddress> =
        s.split(',', '，', ';', '；', '\n')
            .map { it.trim() }
            .filter { it.isNotEmpty() }
            .map { piece ->
                val lt = piece.lastIndexOf('<')
                val gt = piece.lastIndexOf('>')
                if (lt >= 0 && gt > lt) {
                    val name = piece.substring(0, lt).trim().trim('"').trim()
                    MailAddress(name.ifEmpty { null }, piece.substring(lt + 1, gt).trim())
                } else MailAddress(null, piece)
            }

    /** Back to the text field: `Name <a@b>` when there is a name, else the bare address. */
    fun formatAddresses(list: List<MailAddress>): String =
        list.joinToString(", ") { if (!it.name.isNullOrBlank() && it.name != it.email) "${it.name} <${it.email}>" else it.email }

    /** A plausible address (one @, something on both sides, a dot after it). Only gates the send button. */
    fun looksValid(email: String): Boolean {
        val at = email.indexOf('@')
        if (at <= 0 || at != email.lastIndexOf('@') || email.any { it.isWhitespace() }) return false
        val domain = email.substring(at + 1)
        return domain.contains('.') && !domain.startsWith('.') && !domain.endsWith('.')
    }

    /** Every address of a field is valid (and there is at least one when [required]). */
    fun fieldValid(s: String, required: Boolean): Boolean {
        val list = parseAddresses(s)
        if (list.isEmpty()) return !required
        return list.all { looksValid(it.email) }
    }

    fun replySubject(subject: String?): String {
        val s = subject.orEmpty().trim()
        val l = s.lowercase()
        return if (l.startsWith("re:") || l.startsWith("re：") || s.startsWith("回复：") || s.startsWith("回复:")) s else "Re: $s"
    }

    fun forwardSubject(subject: String?): String {
        val s = subject.orEmpty().trim()
        val l = s.lowercase()
        return if (l.startsWith("fwd:") || l.startsWith("fw:") || s.startsWith("转发：") || s.startsWith("转发:")) s else "Fwd: $s"
    }

    /** The References header of a reply: the original's chain plus its own Message-ID, without duplicates. */
    fun replyReferences(original: MailMessage): List<String> =
        (original.references.orEmpty() + listOfNotNull(original.messageId?.takeIf { it.isNotBlank() })).distinct()

    fun formatDate(t: Instant?, zone: ZoneId = ZoneId.systemDefault()): String = t?.atZone(zone)?.format(DATE).orEmpty()

    /** The original's text for quoting / forwarding: plain text, or the HTML reduced to text. */
    fun plainText(m: MailMessage): String =
        m.text?.takeIf { it.isNotBlank() } ?: MailHtml.toText(m.html.orEmpty())

    /** 「\n\n<date>，<sender> 写道：\n> …」 (first [QUOTE_LINES] lines). */
    fun quote(m: MailMessage, zone: ZoneId = ZoneId.systemDefault()): String {
        val lines = plainText(m).trimEnd().split('\n').take(QUOTE_LINES).joinToString("\n") { "> $it" }
        val who = m.from?.display.orEmpty()
        val date = formatDate(m.date, zone)
        return "\n\n" + listOf(date, who).filter { it.isNotEmpty() }.joinToString("，") + " 写道：\n" + lines
    }

    /**
     * 回复 / 回复全部. [me] = the receiving account's address (left out of reply-all). Reply-all sends to the sender plus
     * the original 收件人, and copies the original 抄送.
     */
    fun reply(m: MailMessage, accountId: String, me: String?, all: Boolean, zone: ZoneId = ZoneId.systemDefault()): MailDraft {
        val mine = me?.lowercase()
        val sender = listOfNotNull(m.from?.takeIf { it.email.isNotBlank() })
        var to = sender
        var cc = emptyList<MailAddress>()
        if (all) {
            val seen = HashSet<String>()
            fun keep(a: MailAddress) = a.email.isNotBlank() && a.email.lowercase() != mine && seen.add(a.email.lowercase())
            to = (sender + m.to.orEmpty()).filter(::keep)
            cc = m.cc.orEmpty().filter(::keep)
            // Replying to yourself (a mail you sent): the original recipients become the To.
            if (to.isEmpty()) to = sender
        }
        return MailDraft(
            accountId = accountId,
            to = formatAddresses(to),
            cc = formatAddresses(cc),
            subject = replySubject(m.subject),
            body = quote(m, zone),
            inReplyTo = m.messageId?.takeIf { it.isNotBlank() },
            references = replyReferences(m),
        )
    }

    /** 转发: no recipients, the original's header block and full text below. */
    fun forward(m: MailMessage, accountId: String, zone: ZoneId = ZoneId.systemDefault()): MailDraft {
        val head = buildString {
            append("\n\n---------- 转发的邮件 ----------\n")
            m.from?.let { append("发件人：").append(formatAddresses(listOf(it))).append('\n') }
            if (m.date != null) append("日期：").append(formatDate(m.date, zone)).append('\n')
            append("主题：").append(m.subject.orEmpty()).append('\n')
            if (!m.to.isNullOrEmpty()) append("收件人：").append(formatAddresses(m.to)).append('\n')
            if (!m.cc.isNullOrEmpty()) append("抄送：").append(formatAddresses(m.cc)).append('\n')
            append('\n')
        }
        return MailDraft(accountId = accountId, subject = forwardSubject(m.subject), body = head + plainText(m).trimEnd())
    }

    /** The request for [draft] as edited (`references` space-joined, the way the hub's SendRequest takes it). */
    fun request(draft: MailDraft, bcc: String = ""): SendMailRequest = SendMailRequest(
        accountId = draft.accountId,
        to = parseAddresses(draft.to),
        cc = parseAddresses(draft.cc),
        bcc = parseAddresses(bcc),
        subject = draft.subject,
        text = draft.body,
        inReplyTo = draft.inReplyTo,
        references = draft.references.takeIf { it.isNotEmpty() }?.joinToString(" "),
    )
}
