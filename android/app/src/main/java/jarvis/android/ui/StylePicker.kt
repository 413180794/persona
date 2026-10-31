package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/** The 设置 row: the current style, opens [StylePickerScreen]. */
@Composable
fun StyleSettingsRow(onClick: () -> Unit) {
    val style by StyleStore.style.collectAsState()
    ListItem(
        headlineContent = { Text("界面风格", maxLines = 1) },
        supportingContent = { Text("${style.title} · ${style.subtitle}", maxLines = 1, overflow = TextOverflow.Ellipsis) },
        trailingContent = { Box(Modifier.size(14.dp).background(JTheme.colors.accent, CircleShape)) },
        modifier = Modifier.clickable(onClick = onClick),
    )
}

/**
 * 设置 › 界面风格 (iOS `StylePicker`): a sample of the current style (a reply, your bubble, the buttons), then the five
 * looks in a 3-column grid, each a tiny screen in its own colors (light or dark, as the phone is now). A tap switches
 * the whole app at once and is kept on the phone ([StyleStore]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun StylePickerScreen(onBack: () -> Unit) {
    val current by StyleStore.style.collectAsState()
    val haptics = LocalHapticFeedback.current
    Scaffold(topBar = {
        TopAppBar(
            title = { Text("界面风格", maxLines = 1) },
            navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
        )
    }) { pad ->
        Column(Modifier.padding(pad).fillMaxSize().verticalScroll(rememberScrollState())) {
            StyleSample(Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
            Column(
                Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
                verticalArrangement = Arrangement.spacedBy(14.dp),
            ) {
                AppStyle.entries.chunked(3).forEach { row ->
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        row.forEach { s ->
                            StyleCard(s, selected = s == current, modifier = Modifier.weight(1f)) {
                                if (s != current) {
                                    haptics.performHapticFeedback(HapticFeedbackType.LongPress)
                                    StyleStore.set(s)
                                }
                            }
                        }
                        repeat(3 - row.size) { Spacer(Modifier.weight(1f)) }
                    }
                }
            }
            Text(
                "深色和浅色跟着系统走。只改这台手机，不影响 iPhone。",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
    }
}

/** A few lines of a conversation in the current style, drawn with the real roles the screens use. */
@Composable
private fun StyleSample(modifier: Modifier) {
    val c = JTheme.colors
    val shape = RoundedCornerShape(JTheme.style.corner)
    Column(
        modifier.fillMaxWidth()
            .shadow(JTheme.style.cardShadow, shape, ambientColor = Color.Black.copy(alpha = 0.12f), spotColor = Color.Black.copy(alpha = 0.12f))
            .background(c.card, shape)
            .border(0.5.dp, c.cardStroke, shape)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            Text(
                "帮我看看这个 PR 能不能合",
                color = MaterialTheme.colorScheme.onPrimaryContainer,
                modifier = Modifier.widthIn(max = 260.dp)
                    .background(MaterialTheme.colorScheme.primaryContainer, RoundedCornerShape(16.dp))
                    .padding(horizontal = 12.dp, vertical = 8.dp),
            )
        }
        Text("测试都过了，只差一处命名。要我直接改掉吗？", color = c.label)
        Text("查看 diff", color = c.accent, fontWeight = FontWeight.Medium)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(onClick = {}) { Text("改吧", maxLines = 1) }
            OutlinedButton(onClick = {}) { Text("先不用", maxLines = 1) }
        }
    }
}

@Composable
private fun StyleCard(style: AppStyle, selected: Boolean, modifier: Modifier, onClick: () -> Unit) {
    val dark = JTheme.isDark
    val p = remember(style, dark) { paletteOf(style, dark) }
    val c = JTheme.colors
    val corner = style.corner * 0.45f
    val outer = RoundedCornerShape(14.dp)
    Column(
        modifier
            .semantics(mergeDescendants = true) { this.selected = selected; contentDescription = "${style.title}，${style.subtitle}" }
            .clickable(role = Role.RadioButton, onClick = onClick),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Box(
            Modifier.fillMaxWidth().height(120.dp)
                .background(if (style.systemSurfaces) p.accent.copy(alpha = 0.10f).compositeOver(p.page) else p.page, outer)
                .border(if (selected) 2.5.dp else 1.dp, if (selected) p.accent else c.label.copy(alpha = 0.1f), outer)
                .padding(10.dp),
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                val shadow = if (style.cardShadow > 0.dp) 4.dp else 0.dp
                val cardShape = RoundedCornerShape(corner)
                Box(
                    Modifier.fillMaxWidth().height(16.dp)
                        .shadow(shadow, cardShape, ambientColor = Color.Black.copy(alpha = 0.12f), spotColor = Color.Black.copy(alpha = 0.12f))
                        .background(p.card, cardShape)
                        .let { if (style == AppStyle.LARK) it.border(0.5.dp, p.cardStroke, cardShape) else it },
                )
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                    Box(Modifier.width(40.dp).height(12.dp).background(p.userBubble, CircleShape))
                }
                Box(Modifier.width(46.dp).height(8.dp).background(p.secondaryLabel.copy(alpha = 0.25f), CircleShape))
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    Box(Modifier.size(10.dp).background(p.accent, CircleShape))
                    Box(Modifier.weight(1f).height(8.dp).background(p.accent.copy(alpha = 0.35f), CircleShape))
                }
            }
        }
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(3.dp)) {
                if (selected) Icon(Icons.Filled.CheckCircle, null, tint = p.accent, modifier = Modifier.size(14.dp))
                Text(style.title, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Text(style.subtitle, fontSize = 11.sp, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}
