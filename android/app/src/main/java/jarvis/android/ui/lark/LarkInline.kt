package jarvis.android.ui.lark

import android.graphics.Bitmap
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.InlineTextContent
import androidx.compose.foundation.text.appendInlineContent
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.Placeholder
import androidx.compose.ui.text.PlaceholderVerticalAlign
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import jarvis.android.data.Hub
import jarvis.android.lark.LarkFormat
import jarvis.android.lark.LarkInline
import jarvis.android.lark.LarkMessage
import jarvis.android.lark.LarkRunStyle
import jarvis.android.lark.LarkTextBase
import jarvis.android.lark.LarkTint
import jarvis.android.lark.TintRole
import jarvis.android.lark.larkThumbEdge
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.LarkChrome
import jarvis.android.ui.design.LarkIcons
import jarvis.android.ui.design.LarkType

// What every Feishu renderer shares (iOS LarkInline.swift / LarkMedia.swift): the page's actions, the message being
// drawn, Feishu's colors in this appearance, wrapped inline text, pictures, avatars, emoji.

/** Someone to show a profile card for (a sender's avatar or name, a removed author, a leader row). */
data class LarkPersonRef(val id: String, val name: String = "", val avatar: String? = null, val feedId: String? = null)

/**
 * What message renderers can ask of the page around them (iOS environment values `larkOpenFile`, `larkRefresh`,
 * `larkOpenProfile`, `larkReact`…). Null callbacks: read only (a forwarded message, a removed post).
 */
class LarkActions(
    val openUrl: (String) -> Unit = {},
    val openImages: (urls: List<String>, index: Int) -> Unit = { _, _ -> },
    val openFile: (url: String, name: String) -> Unit = { _, _ -> },
    val playMedia: (url: String, audio: Boolean, title: String) -> Unit = { _, _, _ -> },
    val refresh: (() -> Unit)? = null,
    val openProfile: ((LarkPersonRef) -> Unit)? = null,
    val react: ((LarkMessage, String) -> Unit)? = null,
    val pickReaction: ((LarkMessage) -> Unit)? = null,
    val openThread: ((LarkMessage) -> Unit)? = null,
    val openForwarded: ((title: String, meta: kotlinx.serialization.json.JsonElement) -> Unit)? = null,
    val openMoments: ((postId: String, link: String?) -> Unit)? = null,
)

val LocalLarkActions = staticCompositionLocalOf { LarkActions() }

/**
 * In a sheet (a window of its own), the link sheet and the image viewer would open under it: the sheet closes first.
 */
fun LarkActions.closing(dismiss: () -> Unit) = LarkActions(
    openUrl = { dismiss(); openUrl(it) }, openImages = { u, i -> dismiss(); openImages(u, i) }, openFile = openFile, playMedia = playMedia,
    refresh = refresh, openProfile = openProfile, react = react, pickReaction = pickReaction, openThread = openThread,
    openForwarded = openForwarded, openMoments = openMoments,
)

/** The hub the Feishu content is read through (media, previews, card buttons). */
val LocalLarkHub = staticCompositionLocalOf<Hub?> { null }

/** The message being drawn: its id (card buttons, link previews) and every picture in it (the viewer pages through them). */
data class LarkRenderContext(val messageId: String? = null, val images: List<String> = emptyList())

val LocalLarkContext = compositionLocalOf { LarkRenderContext() }

/** Text appearance cards hand down to nested elements (a note small and gray, a heading large and bold…). */
val LocalLarkTextBase = compositionLocalOf { LarkTextBase() }

/** A [LarkTint] in the current appearance: Feishu's palette as it is, the semantic roles from the theme. */
@Composable @ReadOnlyComposable
fun larkColor(t: LarkTint): Color {
    val c = JTheme.colors
    val base = when (t) {
        is LarkTint.Rgb -> LarkChrome.rgbOf(if (JTheme.isDark) t.dark else t.light)
        is LarkTint.Role -> when (t.role) {
            TintRole.LABEL -> c.label
            TintRole.SECONDARY -> c.secondaryText
            TintRole.TERTIARY -> c.tertiaryText
            TintRole.BLUE -> LarkChrome.blue
            TintRole.WHITE -> Color.White
        }
    }
    return if (t.alpha >= 1f) base else base.copy(alpha = base.alpha * t.alpha)
}

/**
 * Inline runs as one wrapped text: styled spans, links (opened in the app's link sheet), Feishu emoji and icons drawn
 * in the line like glyphs (a `[label]` until the catalogue knows them).
 */
@Composable
fun LarkInlineText(
    runs: List<LarkInline>, modifier: Modifier = Modifier, base: LarkTextBase? = null,
    maxLines: Int = Int.MAX_VALUE, align: TextAlign = TextAlign.Start,
) {
    val b = base ?: LocalLarkTextBase.current
    val emojis by LarkMedia.emojis.collectAsState()
    val actions = LocalLarkActions.current
    val c = JTheme.colors
    val blue = LarkChrome.blue
    val codeColor = LarkChrome.inlineCode
    val codeBg = c.label.copy(alpha = 0.08f)
    val inline = HashMap<String, InlineTextContent>()
    // Every tint resolved up front (composable), then the string is built.
    val tints = HashMap<LarkTint, Color>()
    for (r in runs) when (r) {
        is LarkInline.Text -> { r.style.color?.let { tints.getOrPut(it) { larkColor(it) } }; r.style.background?.let { tints.getOrPut(it) { larkColor(it) } } }
        is LarkInline.Symbol -> r.tint?.let { tints.getOrPut(it) { larkColor(it) } }
        else -> Unit
    }
    val baseColor = b.color?.let { larkColor(it) } ?: c.label
    val text: AnnotatedString = buildAnnotatedString {
        for ((i, r) in runs.withIndex()) when (r) {
            is LarkInline.Text -> appendRun(r.text, r.style, b, tints, blue, codeColor, codeBg, actions)
            is LarkInline.Emoji -> {
                val url = emojis[r.key.lowercase()]?.url
                if (url.isNullOrEmpty()) appendRun(r.fallback, LarkRunStyle(), b, tints, blue, codeColor, codeBg, actions)
                else {
                    inline["e$i"] = InlineTextContent(Placeholder(EMOJI.em, EMOJI.em, PlaceholderVerticalAlign.TextCenter)) {
                        LarkRemoteImage(url, Modifier.size((b.size * EMOJI).dp), maxPx = EMOJI_PX, contentScale = ContentScale.Fit, placeholder = false)
                    }
                    appendInlineContent("e$i", r.fallback)
                }
            }
            is LarkInline.Symbol -> {
                val tint = r.tint?.let { tints[it] } ?: baseColor
                inline["s$i"] = InlineTextContent(Placeholder(GLYPH.em, GLYPH.em, PlaceholderVerticalAlign.TextCenter)) {
                    Icon(LarkIcons.of(r.name), null, Modifier.size(b.size.dp), tint = tint)
                }
                appendInlineContent("s$i", "□")
            }
            is LarkInline.Image -> {
                inline["i$i"] = InlineTextContent(Placeholder(EMOJI.em, EMOJI.em, PlaceholderVerticalAlign.TextCenter)) {
                    val side = (b.size * EMOJI).dp
                    LarkRemoteImage(r.url, Modifier.size(side), size = thumbEdge(side), maxPx = EMOJI_PX, contentScale = ContentScale.Fit, placeholder = false)
                }
                appendInlineContent("i$i", "□")
            }
        }
    }
    Text(
        text, modifier, inlineContent = inline, maxLines = maxLines,
        overflow = if (maxLines == Int.MAX_VALUE) TextOverflow.Clip else TextOverflow.Ellipsis,
        style = LarkType.text(b.size, b.bold, baseColor, align),
    )
}

/** An emoji or icon drawn in a line: as tall as the text around it. */
private const val EMOJI = 1.25f
private const val GLYPH = 1.05f
private const val EMOJI_PX = 96

private fun AnnotatedString.Builder.appendRun(
    s: String, style: LarkRunStyle, base: LarkTextBase, tints: Map<LarkTint, Color>, blue: Color, codeColor: Color, codeBg: Color,
    actions: LarkActions,
) {
    val text = if (style.code) s.replace(' ', '\u00A0') else s
    var color = style.color?.let { tints[it] } ?: Color.Unspecified
    if (style.link != null && style.color == null) color = blue
    var bg = Color.Unspecified
    if (style.code) {
        bg = codeBg
        if (style.color == null) color = codeColor
    }
    style.background?.let { bg = tints[it] ?: bg }
    val deco = buildList {
        if (style.strike) add(TextDecoration.LineThrough)
        if (style.underline || style.dotted) add(TextDecoration.Underline)
    }
    val span = LarkType.span(
        color, style.size ?: if (style.code) base.size else null, style.bold || base.bold, style.italic, style.code, bg,
        if (deco.isEmpty()) null else TextDecoration.combine(deco),
    )
    val link = style.link
    if (link != null) withLink(LinkAnnotation.Clickable(link, TextLinkStyles(span)) { actions.openUrl(link) }) { append(text) }
    else withStyle(span) { append(text) }
}

/** The hub thumbnail edge for something drawn [side] wide. */
@Composable
fun thumbEdge(side: Dp): Int? = larkThumbEdge(with(LocalDensity.current) { side.roundToPx() })

/** A Feishu picture with a quiet placeholder while it loads and a broken-image mark (tap to retry) when it fails. */
@Composable
fun LarkRemoteImage(
    url: String, modifier: Modifier = Modifier, size: Int? = null, maxPx: Int = 1600,
    contentScale: ContentScale = ContentScale.Crop, contentDescription: String? = null, placeholder: Boolean = true,
) {
    val hub = LocalLarkHub.current
    var bitmap by remember(url, size, maxPx) { mutableStateOf<Bitmap?>(hub?.let { LarkMedia.cached(it.api, url, size, maxPx) }) }
    var failed by remember(url, size, maxPx) { mutableStateOf(false) }
    var attempt by remember(url) { mutableIntStateOf(0) }
    LaunchedEffect(url, size, maxPx, attempt, hub) {
        if (bitmap != null || hub == null) return@LaunchedEffect
        failed = false
        bitmap = LarkMedia.image(hub.api, url, size, maxPx)
        failed = bitmap == null
    }
    val c = JTheme.colors
    val b = bitmap
    when {
        b != null -> Image(b.asImageBitmap(), contentDescription, modifier, contentScale = contentScale)
        failed -> Box(modifier.background(c.label.copy(alpha = 0.05f)).clickable { attempt++ }, contentAlignment = Alignment.Center) {
            Icon(JIcons.brokenImage, "加载失败，点一下重试", Modifier.size(JSize.iconS), tint = c.tertiaryText)
        }
        placeholder -> Box(modifier.background(c.label.copy(alpha = 0.05f)), contentAlignment = Alignment.Center) {
            CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.rule, color = c.tertiaryText)
        }
        else -> Box(modifier)
    }
}

/** A person's or a chat's avatar: the real picture when there is one, otherwise the colored initial. */
@Composable
fun LarkAvatar(name: String, url: String?, modifier: Modifier = Modifier, isGroup: Boolean = false, size: Dp = JSize.wellL) {
    if (!url.isNullOrEmpty()) {
        val px = with(LocalDensity.current) { size.roundToPx() }
        LarkRemoteImage(
            url, modifier.size(size).clip(CircleShape).border(JSize.hairline, JTheme.colors.label.copy(alpha = 0.06f), CircleShape),
            size = if (url.startsWith("/")) larkThumbEdge(px) else null, maxPx = px * 2, placeholder = false,
        )
    } else LarkInitialAvatar(name, isGroup, modifier, size)
}

/** Initials (or a group glyph) on a color picked from the name. */
@Composable
fun LarkInitialAvatar(name: String, isGroup: Boolean, modifier: Modifier = Modifier, size: Dp = JSize.avatarM) {
    val hues = LarkChrome.avatarHues
    val base = hues[Math.floorMod(name.sumOf { it.code }, hues.size)]
    Box(modifier.size(size).clip(CircleShape).background(Brush.verticalGradient(listOf(base.copy(alpha = 0.85f), base))), contentAlignment = Alignment.Center) {
        if (isGroup) Icon(LarkIcons.groupAvatar, null, Modifier.size(size * 0.5f), tint = Color.White)
        else Text(name.take(1), style = LarkType.initial(size), maxLines = 1)
    }
}

/** One emoji from the Feishu catalogue as a picture (its `[label]` while the catalogue is unknown). */
@Composable
fun LarkEmojiImage(key: String, size: Dp, modifier: Modifier = Modifier) {
    val emojis by LarkMedia.emojis.collectAsState()
    val e = emojis[key.lowercase()]
    if (!e?.url.isNullOrEmpty()) {
        val px = with(LocalDensity.current) { size.roundToPx() }
        LarkRemoteImage(e!!.url, modifier.size(size), maxPx = maxOf(px * 2, MIN_EMOJI_PX), contentScale = ContentScale.Fit, placeholder = false,
            contentDescription = e.label)
    } else {
        Box(modifier.size(size), contentAlignment = Alignment.Center) {
            Text(LarkFormat.labelFallback(key, null), style = LarkType.emojiLabel(size, JTheme.colors.secondaryText), maxLines = 1, overflow = TextOverflow.Clip)
        }
    }
}

private const val MIN_EMOJI_PX = 48
