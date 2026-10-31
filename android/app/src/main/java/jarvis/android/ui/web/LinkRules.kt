package jarvis.android.ui.web

import jarvis.android.data.Item
import jarvis.android.data.SessionKey
import jarvis.android.data.parseTime
import java.net.URI
import java.net.URLDecoder
import java.time.Duration
import java.time.Instant

// The pure half of in-app links (CLAUDE.md「链接在 App 内打开」, docs/android.md「链接」): no Android types, so the
// JVM unit tests cover every rule here.

/**
 * A piece of a page the user picked, carried in a message as
 * `<jarvis-context kind="context|edit" title="…" url="…">text</jarvis-context>` (the same tag iOS `ContextRef` writes).
 */
data class ContextRef(
    val kind: String, // "context" | "edit"
    val title: String,
    val url: String,
    val text: String,
    val id: String = java.util.UUID.randomUUID().toString(),
) {
    val host: String get() = runCatching { URI(url).host }.getOrNull() ?: url

    val xml: String
        get() = "<jarvis-context kind=\"${esc(kind)}\" title=\"${esc(title)}\" url=\"${esc(url)}\">\n" +
            text.replace("</jarvis-context>", "") + "\n</jarvis-context>"

    companion object {
        private fun esc(s: String) = s.replace("&", "&amp;").replace("\"", "&quot;").replace("<", "&lt;").replace(">", "&gt;")

        /** A message with pending contexts in front of it, as iOS sends it. */
        fun message(refs: List<ContextRef>, body: String): String =
            if (refs.isEmpty()) body else refs.joinToString("\n") { it.xml } + "\n\n" + body

        /** 「AI 修改」: the selection and what to do with it, one message to the agent. */
        fun editMessage(ref: ContextRef, instruction: String): String {
            val ask = instruction.trim()
            return ref.xml + "\n\n" + if (ask.isEmpty()) "按上面选中的内容修改。" else "按要求修改上面选中的内容：$ask"
        }
    }
}

/** Where a link tapped in the app may go. */
object LinkSafety {
    enum class Kind { WEB, APP_LINK, EXTERNAL, REFUSED }

    /** Schemes that are never followed: local files and providers, scripts, inline documents. */
    private val refused = setOf("content", "file", "javascript", "data", "about", "blob", "vbscript")

    /** http(s) opens in the sheet, `jarvis:` navigates inside the app, mailto: / tel: / intent: go to the system. */
    fun kind(url: String): Kind {
        val scheme = url.trim().substringBefore(':', "").lowercase()
        return when {
            scheme == "http" || scheme == "https" -> Kind.WEB
            scheme == "jarvis" -> Kind.APP_LINK
            scheme.isEmpty() || scheme in refused || !scheme.all { it.isLetterOrDigit() || it == '+' || it == '-' || it == '.' } -> Kind.REFUSED
            else -> Kind.EXTERNAL
        }
    }

    fun isRefusedScheme(scheme: String?): Boolean = scheme == null || scheme.lowercase() in refused
}

/**
 * `jarvis://` links a chat may contain. They only navigate: the activity is exported, so anything can open them, and
 * nothing reached this way may send, answer or change anything. `jarvis://pair` (re-pairing the app) is not
 * navigation and is dropped when tapped in a chat; it still works when opened from outside (MainActivity).
 */
sealed interface AppLink {
    data class Session(val key: SessionKey) : AppLink
    data class Tab(val name: String) : AppLink // jarvis | agents | inbox

    companion object {
        fun parse(link: String): AppLink? {
            val uri = runCatching { URI(link.trim()) }.getOrNull() ?: return null
            if (!uri.scheme.equals("jarvis", ignoreCase = true)) return null
            val p = uri.rawPath.orEmpty().split('/').filter { it.isNotEmpty() }.map { URLDecoder.decode(it, "UTF-8") }
            return when (uri.host?.lowercase()) {
                "session" -> if (p.size >= 2) Session(SessionKey(p[0], p[1])) else null
                "assistant" -> Tab("jarvis")
                "inbox" -> Tab("inbox")
                "tab" -> p.firstOrNull()?.lowercase()?.let { if (it == "assistant") "jarvis" else it }
                    ?.takeIf { it in setOf("jarvis", "agents", "inbox") }?.let(::Tab)
                else -> null
            }
        }
    }
}

/**
 * Where the sheet can rest, as the sheet top's offset in fractions of the height: 0 full screen, [MID] (the sheet covers
 * the bottom 60 %, the screen above stays usable), 1 gone.
 */
object SheetStops {
    const val MID = 0.4f

    /** Where a drag released at [offset] with [velocity] (fractions per second, + is down) settles. */
    fun settle(offset: Float, velocity: Float): Float {
        val fling = 1.2f
        return when {
            velocity > fling -> if (offset < MID) MID else 1f
            velocity < -fling -> if (offset > MID) MID else 0f
            offset < MID / 2 -> 0f
            offset < (MID + 1f) / 2 -> MID
            else -> 1f
        }
    }

    /** How much of the height the screen behind gives up to the sheet: it never shrinks past [MID]. */
    fun behindInset(offset: Float): Float = 1f - offset.coerceIn(MID, 1f)
}

/** Bare links in text, and which ones to load ahead of a tap. */
object LinkText {
    // Printable ASCII only: Chinese prose glues 「。还有」 straight onto a link.
    val bareLink = Regex("""https?://[\x21-\x7E&&[^<>"'`\\]]+""")

    /** URLs in [text], without the punctuation Markdown and prose leave at their end; hosts without a dot skipped. */
    fun links(text: String): Sequence<String> = bareLink.findAll(text).map { trim(it.value) }
        .filter { runCatching { URI(it).host }.getOrNull()?.contains('.') == true }

    fun trim(raw: String): String {
        var u = raw.trimEnd('.', ',', ';', ':', '!', '?', '*', '_', '\'', '"')
        // A Markdown link `[x](url)` or a parenthesised URL leaves a closing parenthesis the URL did not open.
        while (u.endsWith(")") && u.count { it == '(' } < u.count { it == ')' }) u = u.dropLast(1)
        return u
    }

    /**
     * Links worth preloading in a chat: the first three of assistant text written in the last 15 minutes (iOS: only
     * fresh replies; preloading while paging old history froze scrolling there).
     */
    fun toPreload(items: List<Item>, now: Instant): List<String> {
        val cutoff = now.minus(Duration.ofMinutes(15))
        return items.asReversed().asSequence()
            .take(40)
            .filter { it.role == "assistant" && it.kind == "text" && (parseTime(it.ts) ?: Instant.EPOCH).isAfter(cutoff) }
            .flatMap { links(it.text.orEmpty()) }
            .distinct()
            .take(3)
            .toList()
    }
}
