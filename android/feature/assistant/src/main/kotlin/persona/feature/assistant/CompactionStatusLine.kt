package persona.feature.assistant

import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import persona.core.designsystem.InlineTone
import persona.core.designsystem.JIconButton
import persona.core.designsystem.JInlineStatus
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PhIcons
import persona.core.designsystem.pagePadding

/** The main conversation's compaction (正在压缩上下文…, 已压缩, 压缩失败：…) as one line over it; a failure closes by hand. */
@Composable
internal fun CompactionStatusLine(line: CompactionLine, onDismiss: () -> Unit, modifier: Modifier = Modifier) {
    JInlineStatus(
        line.text,
        modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s),
        tone = when (line) {
            CompactionLine.Running -> InlineTone.Busy
            CompactionLine.Done -> InlineTone.Done
            is CompactionLine.Failed -> InlineTone.Failed
        },
        action = if (line is CompactionLine.Failed) ({ JIconButton(PhIcons.X, "关闭", onDismiss) }) else null,
    )
}
