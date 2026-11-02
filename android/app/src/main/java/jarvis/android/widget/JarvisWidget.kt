package jarvis.android.widget

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.LocalSize
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.SizeMode
import androidx.glance.appwidget.action.actionStartActivity
import androidx.glance.appwidget.cornerRadius
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.updateAll
import androidx.glance.background
import androidx.glance.color.ColorProvider
import androidx.glance.layout.Alignment
import androidx.glance.layout.Box
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.size
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import androidx.glance.unit.ColorProvider as GlanceColor
import jarvis.android.JarvisApp
import jarvis.android.fleet.FleetItem
import jarvis.android.fleet.FleetState
import jarvis.android.fleet.FleetNotification
import jarvis.android.fleet.FleetStore
import jarvis.android.fleet.FleetText

/**
 * The home-screen widget (iOS widgets / the Fleet activity's lock-screen view): how many agents need you, are working
 * or finished, and the first few of them. Starts from [FleetStore]'s saved snapshot, so it draws when the process
 * was not running; while the session lives it follows [FleetStore.state]. Tapping an agent opens it, the rest
 * opens Agents. Taps only navigate, nothing here can answer.
 */
class JarvisWidget : GlanceAppWidget() {
    override val sizeMode: SizeMode = SizeMode.Responsive(setOf(SMALL, MEDIUM, LARGE))

    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val initial = FleetStore.saved(context)
        // Read inside the composition: while Glance keeps this session, updateAll() only recomposes it, so values
        // captured out here would be drawn again unchanged.
        provideContent {
            val live by FleetStore.state.collectAsState()
            val hub by JarvisApp.instance.hub.collectAsState()
            Content(context, hub != null, live ?: initial)
        }
    }

    @Composable
    private fun Content(ctx: Context, paired: Boolean, st: FleetState) {
        val rows = when {
            LocalSize.current.height < 120.dp -> 0
            LocalSize.current.height < 200.dp -> 2
            else -> 3
        }
        Column(
            GlanceModifier.fillMaxSize().cornerRadius(22.dp).background(CARD).padding(14.dp)
                .clickable(actionStartActivity(FleetNotification.openTab(ctx, "agents"))),
        ) {
            Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text("✦", style = TextStyle(color = ACCENT, fontSize = 15.sp, fontWeight = FontWeight.Bold))
                Spacer(GlanceModifier.width(6.dp))
                Text("Jarvis", style = TextStyle(color = SECONDARY, fontSize = 13.sp, fontWeight = FontWeight.Medium), maxLines = 1)
            }
            Spacer(GlanceModifier.height(6.dp))
            if (!paired) {
                Text("还没配对", style = TextStyle(color = LABEL, fontSize = 17.sp, fontWeight = FontWeight.Bold), maxLines = 1)
                Text("打开 Jarvis 扫 hub 的二维码", style = TextStyle(color = SECONDARY, fontSize = 13.sp), maxLines = 2)
                return@Column
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (!st.isEmpty) {
                    Box(GlanceModifier.size(9.dp).cornerRadius(5.dp).background(phaseColor(st.phase))) {}
                    Spacer(GlanceModifier.width(6.dp))
                }
                Text(FleetText.headline(st), style = TextStyle(color = LABEL, fontSize = 17.sp, fontWeight = FontWeight.Bold), maxLines = 1)
            }
            val rest = FleetText.rest(st)
            if (rest.isNotEmpty()) Text(rest, style = TextStyle(color = SECONDARY, fontSize = 13.sp), maxLines = 1)
            if (st.isEmpty) {
                Text("没有 Agent 需要你", style = TextStyle(color = SECONDARY, fontSize = 13.sp), maxLines = 1)
                return@Column
            }
            if (rows > 0) {
                Spacer(GlanceModifier.height(8.dp))
                st.items.take(rows).forEach { ItemRow(ctx, it) }
                val hidden = st.hidden(minOf(rows, st.items.size))
                if (hidden > 0) Text(FleetText.more(hidden), style = TextStyle(color = SECONDARY, fontSize = 12.sp), maxLines = 1)
            }
        }
    }

    @Composable
    private fun ItemRow(ctx: Context, item: FleetItem) {
        val key = item.key
        val mod = GlanceModifier.fillMaxWidth().padding(vertical = 3.dp).let { m ->
            if (key != null) m.clickable(actionStartActivity(FleetNotification.openSession(ctx, key))) else m
        }
        Row(mod, verticalAlignment = Alignment.CenterVertically) {
            Box(GlanceModifier.size(7.dp).cornerRadius(4.dp).background(kindColor(item.kind))) {}
            Spacer(GlanceModifier.width(8.dp))
            Column(GlanceModifier.defaultWeight()) {
                Text(item.title, style = TextStyle(color = LABEL, fontSize = 14.sp, fontWeight = FontWeight.Medium), maxLines = 1)
                // Which computer it runs on, after what it is doing (「工作中 · MacBook · 空间」).
                val sub = if (item.place.isEmpty()) FleetText.line(item) else FleetText.line(item) + " · " + item.place
                Text(sub, style = TextStyle(color = SECONDARY, fontSize = 12.sp), maxLines = 1)
            }
        }
    }

    // The app's state colours (ui/Theme.kt AgentState.color): needs you red-orange, working green, done blue.
    private fun phaseColor(phase: String): GlanceColor = when (phase) {
        "needsYou" -> NEEDS_YOU
        "working" -> WORKING
        else -> DONE
    }

    private fun kindColor(kind: String): GlanceColor = when (kind) {
        "working" -> WORKING
        "unread" -> DONE
        else -> NEEDS_YOU
    }

    companion object {
        private val SMALL = DpSize(110.dp, 100.dp)
        private val MEDIUM = DpSize(250.dp, 150.dp)
        private val LARGE = DpSize(250.dp, 260.dp)
        private val CARD = ColorProvider(Color(0xFFFFFFFF), Color(0xFF1C1C1E))
        private val LABEL = ColorProvider(Color(0xFF000000), Color(0xFFFFFFFF))
        private val SECONDARY = ColorProvider(Color(0x993C3C43), Color(0x99EBEBF5))
        private val ACCENT = ColorProvider(Color(0xFF007AFF), Color(0xFF0A84FF))
        // The agent states' dots, the same table as the app (ui/design/Colors.kt StatusPalette, iOS StatusPalette):
        // needs you red, working amber, finished-unseen green.
        private val NEEDS_YOU = ColorProvider(jarvis.android.ui.design.StatusPalette.needsYou(false).dot, jarvis.android.ui.design.StatusPalette.needsYou(true).dot)
        private val WORKING = ColorProvider(jarvis.android.ui.design.StatusPalette.working(false).dot, jarvis.android.ui.design.StatusPalette.working(true).dot)
        private val DONE = ColorProvider(jarvis.android.ui.design.StatusPalette.unread(false).dot, jarvis.android.ui.design.StatusPalette.unread(true).dot)

        /** Redraws every placed widget (cheap when none is placed). */
        suspend fun refresh(ctx: Context) {
            runCatching { JarvisWidget().updateAll(ctx.applicationContext) }
        }
    }
}

class JarvisWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = JarvisWidget()
}
