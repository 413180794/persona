package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.GroupRow
import jarvis.android.ui.design.Haptics
import jarvis.android.ui.design.IconTile
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.hapticView
import jarvis.android.ui.settings.KitGroup
import jarvis.android.ui.settings.KitPage

/** The 设置 row: the current style, opens [StylePickerScreen]. */
@Composable
fun StyleSettingsRow(onClick: () -> Unit) {
    val style by StyleStore.style.collectAsState()
    GroupRow(
        "界面风格", subtitle = "${style.title} · ${style.subtitle}", onClick = onClick,
        lead = { IconTile(JIcons.style, JTheme.hue.purple) },
        trailing = { Box(Modifier.size(JSize.markS).clip(CircleShape).background(JTheme.colors.accent)) },
    )
}

/**
 * 设置 › 界面风格 (iOS `StylePicker`): a sample of the current style (a reply, your bubble, the buttons), then the five
 * looks in a 3-column grid, each a tiny screen in its own colors (light or dark, as the phone is now). A tap switches
 * the whole app at once and is kept on the phone ([StyleStore]).
 */
@Composable
fun StylePickerScreen(onBack: () -> Unit) {
    val current by StyleStore.style.collectAsState()
    val view = hapticView()
    KitPage("界面风格", onBack) {
        item { KitGroup(header = "预览") { row { StyleSample(Modifier.padding(JSpace.l)) } } }
        item {
            KitGroup(header = "风格", footer = "深色和浅色跟着系统走。只改这台手机，不影响 iPhone。") {
                row {
                    Column(Modifier.fillMaxWidth().padding(JSpace.l), verticalArrangement = Arrangement.spacedBy(JSpace.l)) {
                        AppStyle.entries.chunked(3).forEach { line ->
                            Row(horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
                                line.forEach { s ->
                                    StyleCard(s, selected = s == current, modifier = Modifier.weight(1f)) {
                                        if (s != current) {
                                            Haptics.success(view)
                                            StyleStore.set(s)
                                        }
                                    }
                                }
                                repeat(3 - line.size) { Spacer(Modifier.weight(1f)) }
                            }
                        }
                    }
                }
            }
        }
    }
}

/** A few lines of a conversation in the current style, drawn with the real roles the screens use. */
@Composable
private fun StyleSample(modifier: Modifier) {
    val c = JTheme.colors
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            Text(
                "帮我看看这个 PR 能不能合", style = JTheme.type.body, color = c.userBubbleText,
                modifier = Modifier.widthIn(max = JSize.box280).background(c.userBubble, RoundedCornerShape(JRadius.bubble))
                    .padding(horizontal = JSpace.m, vertical = JSpace.s),
            )
        }
        Text("测试都过了，只差一处命名。要我直接改掉吗？", style = JTheme.type.body, color = c.label)
        Text("查看 diff", style = JTheme.type.headingMedium, color = c.accent, maxLines = 1)
        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
            PillButton("改吧", {}, kind = ButtonKind.Prominent)
            PillButton("先不用", {}, kind = ButtonKind.Neutral)
        }
    }
}

/** One look as a tiny screen in that style's own colors: a card, your bubble, a line of text, the accent. */
@Composable
private fun StyleCard(style: AppStyle, selected: Boolean, modifier: Modifier, onClick: () -> Unit) {
    val dark = JTheme.isDark
    val p = remember(style, dark) { paletteOf(style, dark) }
    val c = JTheme.colors
    val corner = style.corner * 0.45f
    val outer = RoundedCornerShape(JRadius.card)
    Column(
        modifier
            .semantics(mergeDescendants = true) { this.selected = selected; contentDescription = "${style.title}，${style.subtitle}" }
            .clip(outer)
            .clickable(role = Role.RadioButton, onClick = onClick),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        Box(
            Modifier.fillMaxWidth().height(JSize.box120)
                .background(if (style.systemSurfaces) p.accent.copy(alpha = 0.10f).compositeOver(p.page) else p.page, outer)
                .border(if (selected) JSize.rule else JSize.line, if (selected) p.accent else c.label.copy(alpha = 0.1f), outer)
                .padding(JSpace.m),
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                val cardShape = RoundedCornerShape(corner)
                Box(
                    Modifier.fillMaxWidth().height(JSize.markM)
                        // 黏土 and Island lift their cards on a soft shadow; 飞书 draws its hairline instead.
                        .shadow(style.cardShadow, cardShape, ambientColor = Color.Black.copy(alpha = 0.12f), spotColor = Color.Black.copy(alpha = 0.12f))
                        .background(p.card, cardShape)
                        .then(if (style == AppStyle.LARK) Modifier.border(JSize.hairline, p.cardStroke, cardShape) else Modifier),
                )
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                    Box(Modifier.width(JSize.avatar).height(JSize.markXS).background(p.userBubble, CircleShape))
                }
                Box(Modifier.width(JSize.avatarL).height(JSize.dot).background(p.secondaryLabel.copy(alpha = 0.25f), CircleShape))
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                    Box(Modifier.size(JSize.dotXL).background(p.accent, CircleShape))
                    Box(Modifier.weight(1f).height(JSize.dot).background(p.accent.copy(alpha = 0.35f), CircleShape))
                }
            }
        }
        Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                if (selected) Icon(JIcons.success, null, tint = p.accent, modifier = Modifier.size(JSize.markS))
                Text(style.title, style = JTheme.type.captionStrong, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Text(style.subtitle, style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}
