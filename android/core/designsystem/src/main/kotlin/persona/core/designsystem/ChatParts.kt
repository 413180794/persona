package persona.core.designsystem

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons

// Parts of a conversation beyond the bubbles in Chat.kt: pictures, the photos sent with a message, the API error card.

/** How tall a picture in a conversation gets at most: a tall screenshot would otherwise fill several screens. */
val ChatPictureMaxHeight: Dp = 280.dp

/**
 * The frame of a picture in a conversation (iOS `Base64Image`): it holds the picture's [ratio] (width / height, known
 * from the page) while the picture loads, so the list does not jump; at most [ChatPictureMaxHeight] tall and as wide as
 * that allows. [onClick] opens it (the image viewer). [failed] shows 「加载失败，点一下重试」 and a tap calls [onRetry].
 * [alignEnd] puts a narrow picture on the right (the user's side).
 */
@Composable
fun ChatPictureFrame(
    ratio: Float?,
    failed: Boolean,
    onClick: (() -> Unit)?,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
    description: String = "图片",
    alignEnd: Boolean = false,
    content: @Composable BoxScope.() -> Unit,
) {
    val palette = PersonaTheme.palette
    val aspect = ratio?.takeIf { it.isFinite() && it > 0f } ?: 1f
    BoxWithConstraints(modifier.fillMaxWidth(), contentAlignment = if (alignEnd) Alignment.TopEnd else Alignment.TopStart) {
        val width = minOf(maxWidth, ChatPictureMaxHeight * aspect)
        val click = when {
            failed -> Modifier.clickable(role = Role.Button, onClickLabel = "重试", onClick = onRetry)
            onClick != null -> Modifier.clickable(role = Role.Image, onClickLabel = "查看大图", onClick = onClick)
            else -> Modifier
        }
        Box(
            Modifier.width(width).aspectRatio(aspect).clip(PersonaTheme.shapes.controlShape).background(palette.sunken)
                .then(click).semantics { contentDescription = if (failed) "$description，加载失败" else description },
        ) {
            content()
            if (failed) Column(
                Modifier.align(Alignment.Center).padding(12.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                JIcon(PhIcons.ArrowClockwise, null, size = 20.dp, tint = palette.ink2)
                Spacer(Modifier.height(4.dp))
                Text("加载失败，点一下重试", style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 2,
                    textAlign = TextAlign.Center, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

private val SinglePhoto = 120.dp
private val GroupPhoto = 76.dp

/**
 * A photo sent with a message, as a square thumbnail above its text (iOS `AttachedImages`): [single] (the only one) is
 * larger. A spinner while [image] is null; [failed] shows a retry mark and a tap calls [onRetry]; otherwise a tap
 * calls [onClick] (the image viewer).
 */
@Composable
fun PhotoThumbnail(
    image: ImageBitmap?,
    failed: Boolean,
    single: Boolean,
    onClick: () -> Unit,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
    description: String = "照片",
) {
    val palette = PersonaTheme.palette
    // The photos sit in the user's bubble; brief's wells are that bubble's own colour (sunken == surface), so an empty
    // box (loading, failed) would not show there: it takes the stronger fill instead.
    val well = if (palette.sunken == palette.surface) palette.surfaceStrong else palette.sunken
    Box(
        modifier.size(if (single) SinglePhoto else GroupPhoto).clip(PersonaTheme.shapes.controlShape).background(well)
            .clickable(role = Role.Button, onClickLabel = if (failed) "重试" else "查看大图", onClick = if (failed) onRetry else onClick)
            .semantics { contentDescription = if (failed) "$description，加载失败" else description },
        contentAlignment = Alignment.Center,
    ) {
        when {
            image != null -> Image(image, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            failed -> Column(horizontalAlignment = Alignment.CenterHorizontally) {
                JIcon(PhIcons.ArrowClockwise, null, size = 18.dp, tint = palette.ink2)
                if (single) {
                    Spacer(Modifier.height(4.dp))
                    Text("加载失败", style = PersonaTheme.type.micro, color = palette.ink2, maxLines = 1)
                }
            }
            else -> JSpinner(Modifier.size(18.dp))
        }
    }
}

/**
 * The photos of one message in a row, wrapping when they do not fit; as wide as they are, so a bubble with one photo
 * and a few words stays small. [alignEnd] lines wrapped rows up on the right (the user's side).
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun PhotoRow(modifier: Modifier = Modifier, alignEnd: Boolean = true, content: @Composable () -> Unit) {
    FlowRow(
        modifier,
        horizontalArrangement = Arrangement.spacedBy(8.dp, if (alignEnd) Alignment.End else Alignment.Start),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) { content() }
}

/** How an [ErrorTag] reads: plain, the error's red, or caution (someone has to act). */
enum class ErrorTagTone { Plain, Danger, Caution }

/** One fact under an error card's reason: the status code, 临时故障 / 要处理, 连续 N 次. */
@Immutable
data class ErrorTag(val text: String, val tone: ErrorTagTone = ErrorTagTone.Plain)

/**
 * A model API error in a conversation (iOS `APIErrorCard`): a warning mark and [reason], [tags] under it, a tap on the
 * header shows or hides [details] (ids, time, the raw text), [action] (重试) at the end. A red-tinted panel in glass and
 * brief, a strip with a red holder on the board.
 */
@Composable
fun ErrorCard(
    reason: String,
    tags: List<ErrorTag>,
    expanded: Boolean,
    onToggle: () -> Unit,
    modifier: Modifier = Modifier,
    action: (@Composable () -> Unit)? = null,
    details: @Composable ColumnScope.() -> Unit,
) {
    val palette = PersonaTheme.palette
    val style = PersonaTheme.style
    val shape = PersonaTheme.shapes.cardShape
    val frame = when (style) {
        AppStyle.Glass -> Modifier.clip(shape).background(palette.danger.tint).border(1.dp, palette.danger.dot.copy(alpha = 0.35f), shape)
        AppStyle.Brief -> Modifier.clip(shape).background(palette.danger.tint)
        AppStyle.Board -> Modifier.clip(shape).background(palette.surface).holder(palette.danger.dot, 6.dp).border(1.dp, palette.line, shape)
    }
    val inner = if (style == AppStyle.Board) PaddingValues(start = 20.dp, end = 14.dp, top = 12.dp, bottom = 12.dp)
        else PaddingValues(horizontal = 16.dp, vertical = 14.dp)
    Column(modifier.fillMaxWidth().padding(vertical = 6.dp).then(frame).padding(inner), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Column(
            Modifier.fillMaxWidth().clickable(role = Role.Button, onClickLabel = if (expanded) "收起详情" else "展开详情", onClick = onToggle)
                .semantics { contentDescription = "API 错误：$reason" },
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                JIcon(PhIcons.WarningCircle, null, size = 20.dp, tint = palette.danger.dot, selected = true)
                Spacer(Modifier.width(10.dp))
                Text(reason, Modifier.weight(1f), style = PersonaTheme.type.bodyStrong, color = palette.ink)
                Spacer(Modifier.width(6.dp))
                JIcon(if (expanded) PhIcons.CaretUp else PhIcons.CaretDown, null, size = 14.dp, tint = palette.ink3)
            }
            if (tags.isNotEmpty()) Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) { tags.forEach { ErrorTagLabel(it) } }
        }
        if (expanded) details()
        action?.invoke()
    }
}

@Composable
private fun ErrorTagLabel(tag: ErrorTag) {
    val palette = PersonaTheme.palette
    val (ink, fill) = when (tag.tone) {
        ErrorTagTone.Plain -> palette.ink2 to palette.surfaceStrong
        ErrorTagTone.Danger -> palette.danger.text to palette.danger.tint
        ErrorTagTone.Caution -> palette.cautionText to palette.needsYou.tint
    }
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(if (PersonaTheme.style == AppStyle.Board) 4.dp else 6.dp)
    Text(
        tag.text, style = PersonaTheme.type.micro.copy(fontWeight = FontWeight.SemiBold), color = ink, maxLines = 1,
        modifier = Modifier.clip(shape).background(fill).padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

/** A named value inside a card's details (请求 ID, Session, 时间): the name in a fixed column, [trailing] at the end. */
@Composable
fun DetailField(name: String, value: String, modifier: Modifier = Modifier, mono: Boolean = true, trailing: (@Composable () -> Unit)? = null) {
    val palette = PersonaTheme.palette
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(name, Modifier.width(72.dp), style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 1)
        Text(value, Modifier.weight(1f), style = if (mono) PersonaTheme.type.code else PersonaTheme.type.caption, color = palette.ink,
            maxLines = 1, overflow = TextOverflow.Ellipsis)
        trailing?.invoke()
    }
}

/** A quiet capsule for a note in the flow of a conversation (离开期间回顾 · 21 分钟前 ›): [onClick] opens what it stands for. */
@Composable
fun ChatNoteChip(text: String, icon: PhIcon, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(if (PersonaTheme.style == AppStyle.Board) 8.dp else 50.dp)
    Row(
        modifier.padding(vertical = 4.dp).heightIn(min = 36.dp).clip(shape)
            .background(if (PersonaTheme.style == AppStyle.Board) palette.surface else palette.surfaceStrong)
            .then(if (PersonaTheme.style == AppStyle.Board) Modifier.border(1.dp, palette.line, shape) else Modifier)
            .clickable(role = Role.Button, onClick = onClick).padding(horizontal = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        JIcon(icon, null, size = 16.dp, tint = palette.accent)
        Spacer(Modifier.width(6.dp))
        Text(text, style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f, fill = false))
        Spacer(Modifier.width(4.dp))
        JIcon(PhIcons.CaretRight, null, size = 14.dp, tint = palette.ink3)
    }
}

/**
 * A file named in a message or a tool card, as a chip that opens it on the session's machine: the file's icon and its
 * name, one line.
 */
@Composable
fun PathChip(text: String, icon: PhIcon, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    val style = PersonaTheme.style
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(PersonaTheme.shapes.chip)
    Row(
        modifier.heightIn(min = 32.dp).clip(shape)
            .background(if (style == AppStyle.Board) palette.surface else palette.accentSoft)
            .then(if (style == AppStyle.Board) Modifier.border(1.dp, palette.line, shape) else Modifier)
            .clickable(role = Role.Button, onClickLabel = "查看文件", onClick = onClick).padding(horizontal = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        JIcon(icon, null, size = 14.dp, tint = palette.accent)
        Spacer(Modifier.width(5.dp))
        Text(text, style = PersonaTheme.type.labelSmall, color = if (style == AppStyle.Brief) palette.ink else palette.accent,
            maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}
