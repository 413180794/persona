package jarvis.android.ui.shell

import androidx.compose.animation.core.animateDpAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.statusBars
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.CircleButton
import jarvis.android.ui.design.CountBadge
import jarvis.android.ui.design.Haptics
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JMotion
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.TopFog
import jarvis.android.ui.design.hapticView
import jarvis.android.ui.design.surface

/** The shell's two pages under the top switch (docs/android.md「外壳」). */
enum class ShellPage(val title: String) { Assistant("助手"), Agents("Agents") }

/**
 * The shell's top bar (iOS `shellBar` / `AgentsTopBar`), floating over the page with a fog under it: ☰ (a dot while
 * the inbox has something for you), the 助手 | Agents switch, and the page's own round button. Both pages draw the
 * same bar, so nothing moves when switching.
 */
@Composable
fun ShellTopBar(
    page: ShellPage, onPage: (ShellPage) -> Unit, waiting: Int, inboxDot: Boolean,
    onDrawer: () -> Unit, trailing: @Composable () -> Unit,
    /** Tapping the page that is already selected (iOS: its menu: the conversation's, or the list's options). */
    onReselect: (ShellPage) -> Unit,
    reselectMenu: @Composable () -> Unit = {},
    /** Over two panes (a wide window): a solid bar with a hairline under it, not a fog over one page. */
    solid: Boolean = false,
) {
    Box(Modifier.fillMaxWidth()) {
        // From the top of the screen (under the status bar) to the bar's bottom edge, then fading out below it.
        val status = WindowInsets.statusBars.asPaddingValues().calculateTopPadding()
        if (solid) {
            Box(Modifier.fillMaxWidth().height(status + JSize.shellBar).background(JTheme.colors.page))
            Box(Modifier.padding(top = status + JSize.shellBar).fillMaxWidth().height(JSize.hairline).background(JTheme.colors.separator))
        } else TopFog(solid = status + JSize.shellBar, fade = JSpace.xl)
        Box(Modifier.fillMaxWidth().statusBarsPadding().height(JSize.shellBar).padding(horizontal = JSpace.page)) {
            CircleButton(JIcons.drawer, "侧边栏", onDrawer, Modifier.align(Alignment.CenterStart), dot = inboxDot)
            Box(Modifier.align(Alignment.Center)) {
                ShellSwitch(page, waiting, onPick = onPage, onReselect = onReselect)
                reselectMenu()
            }
            Box(Modifier.align(Alignment.CenterEnd)) { trailing() }
        }
    }
}

/**
 * 助手 | Agents in a capsule, the selected one on a soft gray pill that glides across (iOS `ShellSwitch`). Agents
 * carries the count of agents waiting on you.
 */
@Composable
fun ShellSwitch(page: ShellPage, waiting: Int, onPick: (ShellPage) -> Unit, onReselect: (ShellPage) -> Unit) {
    val view = hapticView()
    val pill by animateDpAsState(if (page == ShellPage.Assistant) 0.dp else JSize.segment, JMotion.state(), label = "pill")
    // The badge sits on the capsule's edge, outside its clip.
    Box {
        Box(Modifier.height(JSize.tapTarget).surface(CircleShape, floating = true).padding(JSpace.xs)) {
            Box(Modifier.offset(x = pill).width(JSize.segment).fillMaxHeight().clip(CircleShape).background(JTheme.shell.segment))
            Row {
                ShellPage.entries.forEach { p ->
                    val selected = p == page
                    Box(
                        Modifier.width(JSize.segment).fillMaxHeight().clip(CircleShape)
                            .clickable(remember { MutableInteractionSource() }, indication = null, role = Role.Tab) {
                                if (selected) onReselect(p) else { Haptics.tap(view); onPick(p) }
                            }
                            .semantics {
                                this.selected = selected
                                contentDescription = p.title + if (p == ShellPage.Agents && waiting > 0) "，$waiting 个在等你" else ""
                            },
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(
                            p.title, style = if (selected) JTheme.type.bodyStrong else JTheme.type.body,
                            color = if (selected) JTheme.colors.label else JTheme.colors.secondaryText, maxLines = 1, textAlign = TextAlign.Center,
                        )
                    }
                }
            }
        }
        if (waiting > 0) CountBadge(waiting, Modifier.align(Alignment.TopEnd).offset(x = JSpace.xs, y = -JSpace.xs), color = JTheme.needsYou.dot)
    }
}
