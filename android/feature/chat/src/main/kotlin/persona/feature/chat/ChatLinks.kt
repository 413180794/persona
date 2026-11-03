package persona.feature.chat

import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.staticCompositionLocalOf
import persona.core.contracts.Item
import persona.core.contracts.parseTime
import java.net.URI
import java.net.URLDecoder
import java.time.Duration
import java.time.Instant
import java.util.UUID

internal data class ChatLinkMatch(val url: String, val start: Int, val end: Int)

/** Where a tapped link may go: a web page in the sheet, a page of this app, another app (after asking), or nowhere. */
enum class ChatLinkKind { Web, App, External, Refused }

/**
 * A `jarvis://` link in a conversation (iOS writes `jarvis://session/…` as the url of a picked session). They only
 * navigate: anything can open the app's links, so nothing reached this way may send, answer or change anything, and
 * `jarvis://pair` (replacing the hub) is not navigation: tapped in a conversation it is ignored.
 */
sealed interface ChatAppLink {
    data class Session(val machineId: String, val sessionId: String) : ChatAppLink
    /** `jarvis` (also `assistant`), `agents` or `inbox`. */
    data class Tab(val tab: String) : ChatAppLink
}

/** Supplied by the app shell: opens a conversation or a tab for a tapped `jarvis://` link. */
val LocalChatAppLinks = staticCompositionLocalOf<((ChatAppLink) -> Unit)?> { null }

/** What to ask before a link leaves for another app: a title and one line. */
internal data class ExternalPrompt(val title: String, val text: String)

internal object ChatLinks {
    private val pattern = Regex("https?://[\\x21-\\x7e]+", RegexOption.IGNORE_CASE)
    private val protectedMarkdown = Regex("```[\\s\\S]*?```|~~~[\\s\\S]*?~~~|`[^`]*`|!?\\[[^\\]]*]\\([^\\n]*?\\)|<https?://[^>]*>")
    /** Local files and providers, scripts, inline documents: never followed, from a message or from a page. */
    private val refused = setOf("content", "file", "javascript", "data", "about", "blob", "vbscript")

    fun webUrl(value: String): String? = runCatching {
        val uri = URI(value.trim())
        value.trim().takeIf { uri.scheme?.lowercase() in setOf("http", "https") && !uri.host.isNullOrEmpty() && uri.rawUserInfo == null }
    }.getOrNull()

    fun matches(text: String): List<ChatLinkMatch> = pattern.findAll(text).mapNotNull { match ->
        var url = match.value.substringBefore('<').substringBefore('>').substringBefore('"')
        do {
            val before = url
            url = url.trimEnd('.', ',', ';', ':', '!', '?')
            for ((close, open) in listOf(')' to '(', ']' to '[')) {
                while (url.endsWith(close) && url.count { it == close } > url.count { it == open }) url = url.dropLast(1)
            }
        } while (url != before)
        webUrl(url)?.let { ChatLinkMatch(it, match.range.first, match.range.first + it.length) }
    }.toList()

    fun markdown(text: String): String {
        val protected = protectedMarkdown.findAll(text).map { it.range }.toList()
        val links = matches(text).filter { link -> protected.none { link.start in it } }
        return buildString {
            var cursor = 0
            for (link in links) { append(text.substring(cursor, link.start)); append('<'); append(link.url); append('>'); cursor = link.end }
            append(text.substring(cursor))
        }
    }

    /**
     * Links worth loading before a tap: the first three in Agent replies written in the last 15 minutes, among the
     * last 40 items. Old history never preloads (on iOS, preloading links while paging old replies froze scrolling),
     * and hosts without a dot (localhost and the like, a computer's own) are not pages the phone can load.
     */
    fun toPreload(items: List<Item>, now: Instant): List<String> {
        val cutoff = now.minus(Duration.ofMinutes(15))
        return items.asReversed().asSequence().take(40)
            .filter { it.role == "assistant" && it.kind == "text" && (parseTime(it.ts) ?: Instant.EPOCH).isAfter(cutoff) }
            .flatMap { item -> matches(item.text.orEmpty()).asSequence().map { it.url } }
            .filter { url -> runCatching { URI(url).host }.getOrNull()?.contains('.') == true }
            .distinct().take(3).toList()
    }

    /** http(s) opens in the sheet, `jarvis:` navigates in the app, mailto: / tel: / intent: and other apps go out. */
    fun kind(url: String): ChatLinkKind {
        val scheme = url.trim().substringBefore(':', "").lowercase()
        return when {
            scheme == "http" || scheme == "https" -> ChatLinkKind.Web
            scheme == "jarvis" -> ChatLinkKind.App
            scheme.isEmpty() || scheme in refused || !scheme.all { it.isLetterOrDigit() || it == '+' || it == '-' || it == '.' } ->
                ChatLinkKind.Refused
            else -> ChatLinkKind.External
        }
    }

    fun appLink(url: String): ChatAppLink? {
        val uri = runCatching { URI(url.trim()) }.getOrNull() ?: return null
        if (!uri.scheme.equals("jarvis", ignoreCase = true)) return null
        val path = uri.rawPath.orEmpty().split('/').filter { it.isNotEmpty() }.map { URLDecoder.decode(it, "UTF-8") }
        return when (uri.host?.lowercase()) {
            "session" -> if (path.size >= 2 && path[0].isNotBlank() && path[1].isNotBlank()) ChatAppLink.Session(path[0], path[1]) else null
            "assistant" -> ChatAppLink.Tab("jarvis")
            "inbox" -> ChatAppLink.Tab("inbox")
            "tab" -> path.firstOrNull()?.lowercase()?.let { if (it == "assistant") "jarvis" else it }
                ?.takeIf { it in setOf("jarvis", "agents", "inbox") }?.let { ChatAppLink.Tab(it) }
            else -> null // pair, open and anything else: not from a tapped link
        }
    }

    /**
     * Whether an intent built from an external link may start: never with data the app's own (`jarvis:`), another
     * intent, local files or providers, and never into this app's own package.
     */
    fun allowedIntent(dataScheme: String?, targetPackage: String?, ownPackage: String): Boolean {
        val scheme = dataScheme?.lowercase()
        if (scheme != null && (scheme == "jarvis" || scheme == "intent" || scheme in refused)) return false
        return targetPackage != ownPackage
    }

    /** Asked before mailto: / tel: / another app's link leaves the app. */
    fun externalPrompt(url: String): ExternalPrompt {
        val trimmed = url.trim()
        val scheme = trimmed.substringBefore(':').lowercase()
        // The scheme-specific part, percent-decoded (a + in a phone number stays a +).
        val target = (runCatching { URI(trimmed).schemeSpecificPart }.getOrNull() ?: trimmed.substringAfter(':')).substringBefore('?').trim()
        return when (scheme) {
            "mailto" -> ExternalPrompt("写邮件？", if (target.isEmpty()) "打开邮件 App 写一封邮件。" else "打开邮件 App，写给 $target。")
            "tel" -> ExternalPrompt("拨打电话？", if (target.isEmpty()) "打开拨号。" else "打开拨号，号码 $target。")
            "sms", "smsto" -> ExternalPrompt("发短信？", if (target.isEmpty()) "打开短信 App。" else "打开短信 App，发给 $target。")
            else -> ExternalPrompt("用其他 App 打开？", "这个链接要交给手机上的其他 App：${trimmed.take(80)}")
        }
    }
}

/**
 * A piece of a page picked in the link sheet, carried in a message as
 * `<jarvis-context kind="context|edit" title="…" url="…">text</jarvis-context>`, written exactly as iOS's `ContextRef`
 * does: the attributes escaped, the text as it is with only `</jarvis-context>` taken out (it cannot close the tag).
 */
data class ChatLinkContext(
    val kind: String = "context", val title: String, val url: String, val text: String,
    val id: String = UUID.randomUUID().toString(),
) {
    fun xml(): String = "<jarvis-context kind=\"${if (kind == "edit") "edit" else "context"}\" title=\"${escape(title)}\" url=\"${escape(url)}\">\n" +
        text.replace("</jarvis-context>", "") + "\n</jarvis-context>"

    companion object {
        private val tags = Regex("<jarvis-context\\b([^>]*)>([\\s\\S]*?)</jarvis-context>\\s*")
        private val attributes = Regex("([a-z]+)=\"([^\"]*)\"")
        private fun escape(s: String) = s.replace("&", "&amp;").replace("\"", "&quot;").replace("<", "&lt;").replace(">", "&gt;")
        private fun unescape(s: String) = s.replace("&quot;", "\"").replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&")

        /** The contexts of a message and the message without them; the text is read as iOS reads it (entities decoded). */
        internal fun parse(text: String): Pair<List<ChatLinkContext>, String> {
            val refs = tags.findAll(text).map { tag ->
                val attrs = attributes.findAll(tag.groupValues[1]).associate { it.groupValues[1] to unescape(it.groupValues[2]) }
                ChatLinkContext(attrs["kind"] ?: "context", attrs["title"].orEmpty(), attrs["url"].orEmpty(), unescape(tag.groupValues[2].trim()))
            }.toList()
            return refs to tags.replace(text, "").trim()
        }

        /** 「AI 修改」: the selection and what to do with it, one message to the agent (iOS `LinkSheet`). */
        fun editMessage(selection: ChatLinkContext, instruction: String): String {
            val ask = instruction.trim()
            return selection.copy(kind = "edit").xml() + "\n\n" + if (ask.isEmpty()) "按上面选中的内容修改。" else "按要求修改上面选中的内容：$ask"
        }

        /** 「交给 Persona」 from a page with no conversation behind it: the selection and what Persona is to do with it. */
        fun handToPersonaMessage(selection: ChatLinkContext, instruction: String): String =
            selection.copy(kind = "context").xml() + "\n\n" + instruction.trim().ifEmpty { "看看上面这段。" }
    }
}

/**
 * A message with the picked contexts in front of it, as iOS sends it: the tags a line apart, then a blank line and
 * what was typed (「参考上面的内容。」 when only contexts go).
 */
internal fun linkMessage(draft: String, contexts: List<ChatLinkContext>): String =
    if (contexts.isEmpty()) draft
    else contexts.joinToString("\n") { it.xml() } + "\n\n" + draft.ifBlank { "参考上面的内容。" }

internal val ChatLinkContextsSaver = listSaver<List<ChatLinkContext>, String>(
    save = { refs -> refs.flatMap { listOf(it.kind, it.title, it.url, it.text, it.id) } },
    restore = { values -> values.chunked(5).map { ChatLinkContext(it[0], it[1], it[2], it[3], it[4]) } },
)
