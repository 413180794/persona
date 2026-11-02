package jarvis.feature.chat

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import jarvis.core.contracts.Interaction
import jarvis.core.designsystem.JarvisTypography
import jarvis.core.domain.answer.permissionSummary

/** Shared by the request inbox and the conversation authorization panel. */
@Composable
fun PermissionRequestSummary(request: Interaction, maxLines: Int = Int.MAX_VALUE, linkify: Boolean = true) {
    val summary = remember(request) { request.permissionSummary() }
    Text(if (summary.monospace || !linkify) AnnotatedString(summary.text) else chatLinkAnnotations(summary.text),
        style = JarvisTypography.cardBody,
        fontFamily = if (summary.monospace) FontFamily.Monospace else null,
        maxLines = if (summary.monospace) 2 else maxLines, overflow = TextOverflow.Ellipsis)
    summary.description?.let {
        Text(it, style = JarvisTypography.meta, color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}
