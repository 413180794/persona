package persona.core.designsystem.catalog

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.background
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import persona.core.designsystem.ChatNoteChip
import persona.core.designsystem.ChatPictureFrame
import persona.core.designsystem.DetailField
import persona.core.designsystem.ErrorCard
import persona.core.designsystem.ErrorTag
import persona.core.designsystem.ErrorTagTone
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JButtonSize
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.PathChip
import persona.core.designsystem.PhotoRow
import persona.core.designsystem.PhotoThumbnail
import persona.core.designsystem.UserBubbleFrame
import persona.core.designsystem.icons.PhIcons

/**
 * The catalog's "Attachments and errors in a conversation" section: the parts a conversation draws beyond its bubbles (ChatParts.kt) — photos
 * sent with a message (loading, failed), a picture's frame, file chips, the recap line and an API error card. Nothing
 * loads or leaves the page.
 */
@Composable
internal fun ColumnScope.ChatPartsSamples(onFeedback: (String) -> Unit) {
    val palette = PersonaTheme.palette
    var open by remember { mutableStateOf(true) }
    UserBubbleFrame {
        PhotoRow {
            PhotoThumbnail(null, failed = false, single = false, onClick = { onFeedback("打开照片") }, onRetry = {})
            PhotoThumbnail(null, failed = true, single = false, onClick = {}, onRetry = { onFeedback("重试照片") })
        }
        Text("看看这两张截图，深色模式下温度的对比度不够", style = PersonaTheme.type.body, color = palette.ink)
    }
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        PathChip("WidgetView.swift", PhIcons.Code, onClick = { onFeedback("打开 WidgetView.swift") })
        PathChip("README.md", PhIcons.FileText, onClick = { onFeedback("打开 README.md") })
    }
    ChatPictureFrame(16f / 9f, failed = false, onClick = { onFeedback("查看大图") }, onRetry = {}) {
        Box(Modifier.fillMaxSize().background(palette.working.tint))
    }
    ChatPictureFrame(3f / 4f, failed = true, onClick = null, onRetry = { onFeedback("重试图片") }) {}
    ChatNoteChip("离开期间回顾 3 条 · 21 分钟前", PhIcons.ClockCountdown, onClick = { onFeedback("打开回顾") })
    ErrorCard(
        "推理服务暂时没有可用的 GPU",
        listOf(ErrorTag("502", ErrorTagTone.Danger), ErrorTag("临时故障"), ErrorTag("连续 3 次")),
        open, { open = !open },
        action = { JButton("重试", { onFeedback("发送：继续") }, kind = JButtonKind.Secondary, size = JButtonSize.Medium, icon = PhIcons.ArrowClockwise) },
    ) {
        DetailField("请求 ID", "202611020801EXAMPLE")
        DetailField("时间", "11月2日 08:01:05", mono = false)
    }
    ErrorCard("登录过期了，要在电脑上重新 /login", listOf(ErrorTag("401", ErrorTagTone.Danger), ErrorTag("要处理", ErrorTagTone.Caution)),
        expanded = false, onToggle = { onFeedback("展开报错") }, modifier = Modifier.fillMaxWidth()) {}
}
