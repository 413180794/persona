package persona.feature.chat

import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.*
import androidx.compose.ui.text.style.TextOverflow
import persona.core.designsystem.*
import androidx.compose.ui.text.style.TextDecoration

@Composable
internal fun chatLinkAnnotations(text: String): AnnotatedString {
    val color = PersonaTheme.palette.accent
    val links = remember(text) { ChatLinks.matches(text) }
    return remember(text, color) {
        buildAnnotatedString {
            append(text)
            links.forEach { addLink(LinkAnnotation.Url(it.url, TextLinkStyles(SpanStyle(color = color, textDecoration = TextDecoration.Underline))), it.start, it.end) }
        }
    }
}

@Composable
fun ChatLinkText(text: String, modifier: Modifier = Modifier, style: TextStyle = PersonaTheme.type.body.copy(color = PersonaTheme.palette.ink)) {
    Text(chatLinkAnnotations(text), modifier, style = style)
}

@Composable
internal fun ChatContextCard(context: ChatLinkContext, onRemove: (() -> Unit)? = null) {
    val uriHandler = LocalUriHandler.current
    JCard(Modifier.fillMaxWidth()) {
        Row(Modifier.padding(Tokens.Space.s)) {
            Column(Modifier.weight(1f)) {
                Text(if (context.kind == "edit") "AI 修改" else "关联上下文", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink2)
                Text(context.title.ifBlank { context.url.ifBlank { "网页选文" } }, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(context.text, maxLines = 2, overflow = TextOverflow.Ellipsis, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
                if (ChatLinks.webUrl(context.url) != null) JTextButton("查看原文", { uriHandler.openUri(context.url) })
            }
            if (onRemove != null) JTextButton("移除", onRemove)
        }
    }
}
