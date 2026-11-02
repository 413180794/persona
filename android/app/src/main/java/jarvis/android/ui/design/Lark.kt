package jarvis.android.ui.design

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.Chat
import androidx.compose.material.icons.automirrored.rounded.HelpOutline
import androidx.compose.material.icons.automirrored.rounded.InsertDriveFile
import androidx.compose.material.icons.automirrored.rounded.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.rounded.KeyboardArrowRight
import androidx.compose.material.icons.automirrored.rounded.OpenInNew
import androidx.compose.material.icons.rounded.AccountCircle
import androidx.compose.material.icons.rounded.AddCircle
import androidx.compose.material.icons.rounded.AddReaction
import androidx.compose.material.icons.rounded.Alarm
import androidx.compose.material.icons.rounded.ArrowOutward
import androidx.compose.material.icons.rounded.AutoAwesome
import androidx.compose.material.icons.rounded.BarChart
import androidx.compose.material.icons.rounded.BugReport
import androidx.compose.material.icons.rounded.CalendarMonth
import androidx.compose.material.icons.rounded.Cancel
import androidx.compose.material.icons.rounded.CheckBoxOutlineBlank
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material.icons.rounded.Checklist
import androidx.compose.material.icons.rounded.Circle
import androidx.compose.material.icons.rounded.Code
import androidx.compose.material.icons.rounded.Delete
import androidx.compose.material.icons.rounded.Description
import androidx.compose.material.icons.rounded.Edit
import androidx.compose.material.icons.rounded.EditNote
import androidx.compose.material.icons.rounded.Error
import androidx.compose.material.icons.rounded.Flag
import androidx.compose.material.icons.rounded.Folder
import androidx.compose.material.icons.rounded.FolderZip
import androidx.compose.material.icons.rounded.GraphicEq
import androidx.compose.material.icons.rounded.Group
import androidx.compose.material.icons.rounded.Groups
import androidx.compose.material.icons.rounded.HideImage
import androidx.compose.material.icons.rounded.Hub
import androidx.compose.material.icons.rounded.Image
import androidx.compose.material.icons.rounded.Info
import androidx.compose.material.icons.rounded.Inventory2
import androidx.compose.material.icons.rounded.KeyboardArrowDown
import androidx.compose.material.icons.rounded.KeyboardArrowUp
import androidx.compose.material.icons.rounded.Link
import androidx.compose.material.icons.rounded.LocalFireDepartment
import androidx.compose.material.icons.rounded.Lock
import androidx.compose.material.icons.rounded.Mail
import androidx.compose.material.icons.rounded.Movie
import androidx.compose.material.icons.rounded.Notifications
import androidx.compose.material.icons.rounded.Pause
import androidx.compose.material.icons.rounded.Person
import androidx.compose.material.icons.rounded.PersonAdd
import androidx.compose.material.icons.rounded.Phone
import androidx.compose.material.icons.rounded.PictureAsPdf
import androidx.compose.material.icons.rounded.Place
import androidx.compose.material.icons.rounded.PlayArrow
import androidx.compose.material.icons.rounded.PlayCircle
import androidx.compose.material.icons.rounded.PushPin
import androidx.compose.material.icons.rounded.RadioButtonUnchecked
import androidx.compose.material.icons.rounded.Redeem
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material.icons.rounded.Schedule
import androidx.compose.material.icons.rounded.Search
import androidx.compose.material.icons.rounded.Sell
import androidx.compose.material.icons.rounded.Settings
import androidx.compose.material.icons.rounded.Slideshow
import androidx.compose.material.icons.rounded.Star
import androidx.compose.material.icons.rounded.TableChart
import androidx.compose.material.icons.rounded.ThumbUp
import androidx.compose.material.icons.rounded.UnfoldMore
import androidx.compose.material.icons.rounded.Videocam
import androidx.compose.material.icons.rounded.Warning
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.ui.AppStyle
import jarvis.android.ui.JTheme

// Feishu's own look inside the app (iOS `LarkColor`, `StyleSpec.lark`, lark-connector's message styles): the chrome of a
// Feishu chat in each 界面风格, the sizes Feishu content comes in, and the glyphs its cards and files name. Pages in
// ui/lark take these instead of writing numbers, colors or icons of their own (docs/android.md「设计系统」).

private fun rgb(v: Long) = Color(0xFF000000 or v)

/** The Feishu chrome in the current 界面风格 (iOS `StyleSpec.LarkChrome`). Card content keeps Feishu's palette. */
object LarkChrome {
    /** Links, the 以下为新消息 line, my reactions: the style's accent (Feishu blue in 飞书). */
    val blue: Color
        @Composable @ReadOnlyComposable get() = JTheme.colors.accent

    /** My messages: lark-connector's --message-outgoing in 飞书, the accent's soft tint elsewhere. */
    val myBubble: Color
        @Composable @ReadOnlyComposable get() = if (JTheme.style == AppStyle.LARK) JTheme.colors.userBubble
        else JTheme.colors.accent.copy(alpha = 0.16f).compositeOver(JTheme.colors.page)

    /** Everyone else's: lark-connector's --message-incoming gray in the glass styles and 飞书, the style's card elsewhere. */
    val otherBubble: Color
        @Composable @ReadOnlyComposable get() = when (JTheme.style) {
            AppStyle.LARK, AppStyle.GLASS, AppStyle.CLAUDE -> rgb(if (JTheme.isDark) 0x262626 else 0xEBECED)
            else -> JTheme.colors.card
        }

    /** Cards, files, removed shares: lark-connector's card white in 飞书, the style's card elsewhere. */
    val cardBackground: Color
        @Composable @ReadOnlyComposable get() = if (JTheme.style == AppStyle.LARK) rgb(if (JTheme.isDark) 0x1E1F23 else 0xFFFFFF) else JTheme.colors.card

    val cardStroke: Color
        @Composable @ReadOnlyComposable get() = if (JTheme.style == AppStyle.LARK) JTheme.colors.cardStroke else JTheme.colors.label.copy(alpha = 0.1f)

    /** Inline `code` in a message (Feishu's rose). */
    val inlineCode: Color
        @Composable @ReadOnlyComposable get() = rgb(if (JTheme.isDark) 0xF07C9A else 0xC7254E)

    /** A code block's colors: comments, strings, numbers, keywords. */
    @Composable @ReadOnlyComposable
    fun syntax(): List<Color> = if (JTheme.isDark) listOf(rgb(0x7D8590), rgb(0x7EE787), rgb(0xFFA657), rgb(0xFF7B9C))
    else listOf(rgb(0x8A8F98), rgb(0x0A7B3E), rgb(0xB35900), rgb(0xA626A4))

    /** Initials' discs, picked by name: never red or orange (beside 需要你 rows they read as an agent waiting). */
    val avatarHues: List<Color> = listOf(rgb(0x3370FF), rgb(0x00B42A), rgb(0x7B61FF), rgb(0x14C9C9), rgb(0x4F5BD5), rgb(0x64748B))

    /** A Feishu color from its light / dark RGB (the named palette in lark/LarkText.kt). */
    fun rgbOf(v: Long): Color = rgb(v)
}

/** A file's type tile: its glyph and color (Feishu's file icons). */
object LarkFileKinds {
    fun of(ext: String): Pair<ImageVector, Color> = when (ext.lowercase()) {
        "pdf" -> Icons.Rounded.PictureAsPdf to rgb(0xE5484D)
        "doc", "docx", "pages", "txt", "md", "rtf" -> Icons.Rounded.Description to rgb(0x3370FF)
        "xls", "xlsx", "csv", "numbers" -> Icons.Rounded.TableChart to rgb(0x2EA121)
        "ppt", "pptx", "key" -> Icons.Rounded.Slideshow to rgb(0xF77234)
        "zip", "rar", "7z", "gz", "tar", "tgz" -> Icons.Rounded.FolderZip to rgb(0x9C6B3C)
        "png", "jpg", "jpeg", "gif", "heic", "webp" -> Icons.Rounded.Image to rgb(0x14C9C9)
        "mp4", "mov", "m4v", "avi", "mkv" -> Icons.Rounded.Movie to rgb(0x7F3BF5)
        "mp3", "m4a", "wav", "aac", "opus" -> Icons.Rounded.GraphicEq to rgb(0xE0457E)
        "apk", "ipa", "dmg", "exe", "pkg" -> Icons.Rounded.Inventory2 to rgb(0x4954E6)
        "go", "py", "js", "ts", "swift", "json", "yaml", "yml", "sh", "java", "kt", "rs", "c", "cpp", "h", "html", "htm", "css", "xml", "sql" ->
            Icons.Rounded.Code to rgb(0x8F959E)
        else -> Icons.AutoMirrored.Rounded.InsertDriveFile to rgb(0x8F959E)
    }
}

/** Glyphs Feishu content names (card icons, summary cards) and the chat's own controls. */
object LarkIcons {
    val doc: ImageVector = Icons.Rounded.Description
    val react: ImageVector = Icons.Rounded.AddReaction
    val profile: ImageVector = Icons.Rounded.AccountCircle
    val moments: ImageVector = Icons.Rounded.Hub
    val expired: ImageVector = Icons.Rounded.HideImage
    val play: ImageVector = Icons.Rounded.PlayArrow
    val pause: ImageVector = Icons.Rounded.Pause
    val playVoice: ImageVector = Icons.Rounded.PlayCircle
    val checked: ImageVector = Icons.Rounded.CheckCircle
    val unchecked: ImageVector = Icons.Rounded.RadioButtonUnchecked
    val checkbox: ImageVector = Icons.Rounded.CheckBoxOutlineBlank
    val chart: ImageVector = Icons.Rounded.BarChart
    val chat: ImageVector = Icons.AutoMirrored.Rounded.Chat
    val down: ImageVector = Icons.Rounded.KeyboardArrowDown
    val outward: ImageVector = Icons.Rounded.ArrowOutward
    val groupAvatar: ImageVector = Icons.Rounded.Groups

    /** A card form control's trailing glyph. */
    fun field(tag: String): ImageVector = when (tag) {
        "input" -> Icons.Rounded.EditNote
        "date_picker", "picker_datetime" -> Icons.Rounded.CalendarMonth
        "picker_time" -> Icons.Rounded.Schedule
        "select_person", "multi_select_person" -> Icons.Rounded.PersonAdd
        else -> Icons.Rounded.UnfoldMore
    }

    /** A summary card's glyph by its variant (meeting, event, vote, red packet…). */
    fun summary(variant: String): ImageVector = when (variant) {
        "video-chat", "meeting" -> Icons.Rounded.Videocam
        "calendar" -> Icons.Rounded.CalendarMonth
        "vote" -> Icons.Rounded.BarChart
        "hongbao" -> Icons.Rounded.Redeem
        "todo", "task" -> Icons.Rounded.Checklist
        "share-user" -> Icons.Rounded.AccountCircle
        "location" -> Icons.Rounded.Place
        "mail" -> Icons.Rounded.Mail
        else -> Icons.Rounded.Description
    }

    /** The closest glyph for an icon name of lark/LarkCardModel.kt's `LarkCardIcons` (iOS draws SF Symbols). */
    fun of(name: String): ImageVector = when (name) {
        "doc" -> Icons.Rounded.Description
        "warning" -> Icons.Rounded.Warning
        "error" -> Icons.Rounded.Error
        "cancel" -> Icons.Rounded.Cancel
        "info" -> Icons.Rounded.Info
        "help" -> Icons.AutoMirrored.Rounded.HelpOutline
        "success" -> Icons.Rounded.CheckCircle
        "clock" -> Icons.Rounded.Schedule
        "calendar" -> Icons.Rounded.CalendarMonth
        "alarm" -> Icons.Rounded.Alarm
        "group" -> Icons.Rounded.Group
        "person" -> Icons.Rounded.Person
        "link" -> Icons.Rounded.Link
        "external" -> Icons.AutoMirrored.Rounded.OpenInNew
        "file" -> Icons.AutoMirrored.Rounded.InsertDriveFile
        "folder" -> Icons.Rounded.Folder
        "bell" -> Icons.Rounded.Notifications
        "fire" -> Icons.Rounded.LocalFireDepartment
        "star" -> Icons.Rounded.Star
        "like" -> Icons.Rounded.ThumbUp
        "chat" -> Icons.AutoMirrored.Rounded.Chat
        "mail" -> Icons.Rounded.Mail
        "search" -> Icons.Rounded.Search
        "settings" -> Icons.Rounded.Settings
        "edit" -> Icons.Rounded.Edit
        "delete" -> Icons.Rounded.Delete
        "add" -> Icons.Rounded.AddCircle
        "refresh" -> Icons.Rounded.Refresh
        "down" -> Icons.Rounded.KeyboardArrowDown
        "up" -> Icons.Rounded.KeyboardArrowUp
        "right" -> Icons.AutoMirrored.Rounded.KeyboardArrowRight
        "left" -> Icons.AutoMirrored.Rounded.KeyboardArrowLeft
        "sparkles" -> Icons.Rounded.AutoAwesome
        "lock" -> Icons.Rounded.Lock
        "location" -> Icons.Rounded.Place
        "chart" -> Icons.Rounded.BarChart
        "code" -> Icons.Rounded.Code
        "bug" -> Icons.Rounded.BugReport
        "video" -> Icons.Rounded.Videocam
        "phone" -> Icons.Rounded.Phone
        "image" -> Icons.Rounded.Image
        "tag" -> Icons.Rounded.Sell
        "flag" -> Icons.Rounded.Flag
        "pin" -> Icons.Rounded.PushPin
        else -> Icons.Rounded.Circle
    }
}

/**
 * Feishu content's text: Feishu gives sizes of its own (a card's `heading-2`, a note's 12), so its text is drawn at
 * those sizes, in sp (it follows the system's font scale like the type scale does).
 */
object LarkType {
    /** The base of a block of Feishu text at [size] sp. */
    fun text(size: Float, bold: Boolean, color: Color, align: TextAlign = TextAlign.Start): TextStyle = TextStyle(
        fontSize = size.sp, lineHeight = (size * LINE).sp, fontWeight = if (bold) FontWeight.SemiBold else FontWeight.Normal,
        color = color, textAlign = align,
    )

    /** One run's look inside such a block; null fields inherit. */
    fun span(
        color: Color, size: Float?, bold: Boolean, italic: Boolean, mono: Boolean, background: Color, decoration: TextDecoration?,
    ): SpanStyle = SpanStyle(
        color = color,
        fontSize = size?.let { (if (mono) it * MONO else it).sp } ?: androidx.compose.ui.unit.TextUnit.Unspecified,
        fontWeight = if (bold) FontWeight.SemiBold else null,
        fontStyle = if (italic) FontStyle.Italic else null,
        fontFamily = if (mono) FontFamily.Monospace else null,
        background = background,
        textDecoration = decoration,
    )

    /** A code block (Feishu's 13, monospaced). */
    fun code(color: Color): TextStyle = TextStyle(fontFamily = FontFamily.Monospace, fontSize = 13.sp, lineHeight = 18.sp, color = color)

    /** A list marker as tall as its line. */
    fun marker(size: Float, color: Color, align: TextAlign = TextAlign.Start): TextStyle =
        TextStyle(fontSize = size.sp, lineHeight = (size * LINE).sp, color = color, textAlign = align)

    /** An initial on an avatar of [diameter]. */
    fun initial(diameter: Dp): TextStyle = TextStyle(fontSize = (diameter.value * 0.42f).sp, fontWeight = FontWeight.SemiBold, color = Color.White)

    /** An emoji's `[label]` while its picture is unknown, in a box of [box]. */
    fun emojiLabel(box: Dp, color: Color): TextStyle = TextStyle(fontSize = (box.value * 0.4f).sp, color = color)

    /** Feishu's line height for its own text. */
    const val LINE = 1.4f
    private const val MONO = 0.88f
}

/** Feishu's own geometry (lark-connector): what the type scale and the shared sizes do not have. */
object LarkSize {
    /** A picture in a message fits this box (lark-connector's 240 × 300). */
    const val PICTURE_W = 240.0
    const val PICTURE_H = 300.0
    /** A sticker's box. */
    const val STICKER = 120f
    /** A file card's width. */
    val fileCard: Dp = 260.dp
    /** A card image is cropped past this height. */
    val cardImageMax: Dp = 320.dp
    /** A wide table's column, scrolled sideways. */
    val tableColumn: Dp = 132.dp
    /** The narrowest a column of a card's bisect / trisect row gets before it wraps. */
    val gridColumnMin: Dp = 64.dp
    /** The widest a bubble gets on a wide screen. */
    val bubbleMax: Dp = 560.dp
    /** The reactions under a message wrap within this. */
    val reactionsMax: Dp = 300.dp
    /** A profile card's cover and the avatar on it. */
    val cover: Dp = 150.dp
    val coverWithAvatar: Dp = 190.dp
    val profileAvatar: Dp = 74.dp
    /** The 表情回复 picker's height at most. */
    val pickerMax: Dp = 560.dp
    /** A chart's plot. */
    val chart: Dp = 180.dp
}
