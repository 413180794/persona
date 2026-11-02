package jarvis.android.ui.design

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.unit.dp
import jarvis.android.ui.AppStyle
import jarvis.android.ui.JTheme

/**
 * How a style draws a surface (iOS `.surface(…)`, `StyleSpec.surface`): the glass styles a white (dark: raised gray)
 * card with a hairline, 黏土 a soft block with a colored shadow and a light rim, Island a floating white island with a
 * deep soft shadow, 飞书 a flat card with its gray hairline. [floating]: it sits over content (a button over the page,
 * a panel over the chat) and gets the style's lift; a group in a list does not.
 */
@Composable
fun Modifier.surface(
    shape: Shape = RoundedCornerShape(JRadius.card), floating: Boolean = false, fill: Color? = null,
    /** False for a slice of a card drawn row by row: each slice's own shadow would show as a seam between rows. */
    shadow: Boolean = true,
): Modifier {
    val c = JTheme.colors
    val bg = fill ?: if (floating) c.floating else c.card
    val lift = if (!shadow) 0.dp else when (c.style) {
        AppStyle.CLAY -> if (floating) 10.dp else 4.dp
        AppStyle.ISLAND -> if (floating) 16.dp else 8.dp
        AppStyle.LARK -> 0.dp
        else -> if (floating) 8.dp else 0.dp
    }
    val shadowColor = when (c.style) {
        AppStyle.CLAY -> c.accent.copy(alpha = if (c.dark) 0.5f else 0.28f)
        AppStyle.ISLAND -> Color(0xFF0B3B36).copy(alpha = if (c.dark) 0.6f else 0.22f)
        else -> Color.Black.copy(alpha = if (c.dark) 0.6f else 0.16f)
    }
    val rim = surfaceRim()
    return this
        .then(if (lift > 0.dp) Modifier.shadow(lift, shape, clip = false, ambientColor = shadowColor, spotColor = shadowColor) else Modifier)
        .clip(shape)
        .background(bg, shape)
        .then(if (rim != Color.Transparent) Modifier.border(JSize.hairline * if (c.style == AppStyle.CLAY) 2 else 1, rim, shape) else Modifier)
}

/** The hairline around a style's surfaces (黏土's light rim, 飞书's gray line; none on Island in light). */
@Composable
@ReadOnlyComposable
fun surfaceRim(): Color {
    val c = JTheme.colors
    return when (c.style) {
        AppStyle.CLAY -> Color.White.copy(alpha = if (c.dark) 0.08f else 0.7f)
        AppStyle.LARK -> c.cardStroke
        AppStyle.ISLAND -> if (c.dark) Color.White.copy(alpha = 0.06f) else Color.Transparent
        else -> c.label.copy(alpha = if (c.dark) 0.12f else 0.07f)
    }
}

/**
 * One row's slice of a group card drawn row by row (a lazy list's items): the card's fill, its corners only where the
 * card starts ([first]) and ends ([last]), and its rim only along the card's outside — a rim around every slice would
 * draw a line between rows.
 */
@Composable
fun Modifier.cardSlice(first: Boolean, last: Boolean): Modifier {
    val c = JTheme.colors
    val r = JTheme.style.corner
    val zero = 0.dp
    val shape = RoundedCornerShape(
        topStart = if (first) r else zero, topEnd = if (first) r else zero,
        bottomStart = if (last) r else zero, bottomEnd = if (last) r else zero,
    )
    val rim = surfaceRim()
    val width = JSize.hairline * if (c.style == AppStyle.CLAY) 2 else 1
    return this.clip(shape).background(c.card).drawWithContent {
        drawContent()
        if (rim == Color.Transparent) return@drawWithContent
        val w = width.toPx()
        val rad = r.toPx()
        val h = size.height
        val x1 = size.width - w / 2
        val x0 = w / 2
        val p = androidx.compose.ui.graphics.Path()
        if (first) {
            p.moveTo(x0, if (last) h - rad else h)
            p.lineTo(x0, rad)
            p.arcTo(androidx.compose.ui.geometry.Rect(x0, w / 2, x0 + 2 * rad, w / 2 + 2 * rad), 180f, 90f, false)
            p.lineTo(x1 - rad, w / 2)
            p.arcTo(androidx.compose.ui.geometry.Rect(x1 - 2 * rad, w / 2, x1, w / 2 + 2 * rad), 270f, 90f, false)
            p.lineTo(x1, if (last) h - rad else h)
        } else {
            p.moveTo(x0, 0f); p.lineTo(x0, if (last) h - rad else h)
            p.moveTo(x1, 0f); p.lineTo(x1, if (last) h - rad else h)
        }
        if (last) {
            p.moveTo(x1, h - rad)
            p.arcTo(androidx.compose.ui.geometry.Rect(x1 - 2 * rad, h - w / 2 - 2 * rad, x1, h - w / 2), 0f, 90f, false)
            p.lineTo(x0 + rad, h - w / 2)
            p.arcTo(androidx.compose.ui.geometry.Rect(x0, h - w / 2 - 2 * rad, x0 + 2 * rad, h - w / 2), 90f, 90f, false)
        }
        drawPath(p, rim, style = androidx.compose.ui.graphics.drawscope.Stroke(w))
    }
}

/** The round floating surface of a circle button or a small control over the page. */
@Composable
fun Modifier.floatingCircle(): Modifier = surface(CircleShape, floating = true)

/**
 * A group of rows in a list (iOS's inset grouped section): the style's card with its corners, 16 from the screen's
 * edges. Rows inside draw their own separators ([RowDivider]).
 */
@Composable
fun GroupCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Column(
        modifier.padding(horizontal = JSpace.page).surface(RoundedCornerShape(groupCorner())),
        content = content,
    )
}

/** The corner of a group card: the style's own (glass 20, 黏土 24, Island 26, 飞书 10). */
@Composable
fun groupCorner() = JTheme.style.corner
