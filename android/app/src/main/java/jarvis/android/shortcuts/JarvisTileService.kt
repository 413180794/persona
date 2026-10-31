package jarvis.android.shortcuts

import android.app.PendingIntent
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.graphics.drawable.Icon
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import androidx.core.content.pm.ShortcutInfoCompat
import androidx.core.content.pm.ShortcutManagerCompat
import androidx.core.graphics.drawable.IconCompat
import jarvis.android.JarvisApp
import jarvis.android.R
import jarvis.android.fleet.FleetNotification
import jarvis.android.fleet.FleetState
import jarvis.android.fleet.FleetStore
import jarvis.android.fleet.FleetText

/**
 * Quick Settings tile (iOS 「Jarvis 状态」 intent): 「2 个需要你」 / 「3 个在工作」 at a glance; lit while something
 * needs you or works. Tapping opens the agent when there is exactly one, else the Agents tab.
 */
class JarvisTileService : TileService() {
    override fun onStartListening() {
        super.onStartListening()
        render()
    }

    override fun onClick() {
        super.onClick()
        val intent = FleetNotification.open(this, FleetStore.saved(this))
        if (Build.VERSION.SDK_INT >= 34) {
            startActivityAndCollapse(PendingIntent.getActivity(this, REQUEST, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
        } else {
            @Suppress("DEPRECATION", "StartActivityAndCollapseDeprecated")
            startActivityAndCollapse(intent)
        }
    }

    private fun render() {
        val tile = qsTile ?: return
        val paired = (application as JarvisApp).hub.value != null
        val sub = subtitle(paired, FleetStore.saved(this))
        tile.label = "Jarvis"
        if (Build.VERSION.SDK_INT >= 29) tile.subtitle = sub else tile.label = "Jarvis · $sub"
        tile.icon = Icon.createWithResource(this, R.drawable.ic_stat)
        val st = FleetStore.saved(this)
        tile.state = when {
            !paired -> Tile.STATE_UNAVAILABLE
            st.phase == "needsYou" || st.phase == "working" -> Tile.STATE_ACTIVE
            else -> Tile.STATE_INACTIVE
        }
        tile.contentDescription = "Jarvis：$sub"
        tile.updateTile()
    }

    companion object {
        private const val REQUEST = 0x7400

        fun subtitle(paired: Boolean, st: FleetState): String = when {
            !paired -> "还没配对"
            st.isEmpty -> "都处理完了"
            else -> FleetText.headline(st)
        }

        /** Asks the system to redraw the tile (it calls onStartListening while the shade is open). */
        fun refresh(ctx: Context) {
            runCatching { requestListeningState(ctx, ComponentName(ctx, JarvisTileService::class.java)) }
        }
    }
}

/**
 * The launcher shortcuts (long-press the Jarvis icon; iOS App Intents `JarvisShortcuts`): 「问 Jarvis」 opens the Jarvis
 * tab with its composer focused, 「批准第一个待处理」 shows the oldest permission / plan and settles it after a tap.
 * Published at runtime rather than from res/xml because a static shortcut's intent has to spell out the application
 * id, which is personal configuration (docs/config.md).
 */
object Shortcuts {
    fun publish(ctx: Context) {
        fun shortcut(id: String, short: String, long: String, intent: Intent, icon: Int) =
            ShortcutInfoCompat.Builder(ctx, id)
                .setShortLabel(short)
                .setLongLabel(long)
                .setIcon(IconCompat.createWithResource(ctx, icon))
                .setIntent(intent)
                .build()
        val ask = FleetNotification.openTab(ctx, "ask").setAction(Intent.ACTION_VIEW)
        val approve = Intent(ctx, ShortcutActivity::class.java).setAction(ShortcutActivity.ACTION_APPROVE)
        runCatching {
            ShortcutManagerCompat.setDynamicShortcuts(
                ctx,
                listOf(
                    shortcut("ask", "问 Jarvis", "问 Jarvis", ask, R.drawable.ic_shortcut_ask),
                    shortcut("approve", "批准待处理", "批准第一个待处理", approve, R.drawable.ic_shortcut_approve),
                ),
            )
        }
    }
}
