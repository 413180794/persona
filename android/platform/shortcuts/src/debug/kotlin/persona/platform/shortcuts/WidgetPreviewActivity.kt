package persona.platform.shortcuts

import android.app.Activity
import android.appwidget.AppWidgetHost
import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.os.Bundle
import android.util.SizeF
import android.util.TypedValue
import android.view.Gravity
import android.widget.LinearLayout
import android.widget.TextView

/**
 * Debug builds only: the home-screen widget hosted the way a launcher hosts it (AppWidgetHost, RemoteViews), at its
 * three sizes over the wallpaper, for captures without dragging it onto a home screen. Needs the bind permission once:
 *
 *     adb shell appwidget grantbind --package <包名> --user 0
 *     adb shell am start -n <包名>/persona.platform.shortcuts.WidgetPreviewActivity
 */
class WidgetPreviewActivity : Activity() {
    private lateinit var host: AppWidgetHost
    private val ids = mutableListOf<Int>()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val manager = AppWidgetManager.getInstance(this)
        val provider = ComponentName(this, PersonaWidgetReceiver::class.java)
        val info = manager.installedProviders.firstOrNull { it.provider == provider }
        host = AppWidgetHost(this, HOST_ID)
        host.startListening()
        val column = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_HORIZONTAL
            fitsSystemWindows = true
        }
        fun dp(value: Float) = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, value, resources.displayMetrics).toInt()
        if (info == null) {
            column.addView(TextView(this).apply { text = "找不到 Persona 小组件" })
        } else for ((width, height) in SIZES) {
            val id = host.allocateAppWidgetId()
            ids += id
            if (!manager.bindAppWidgetIdIfAllowed(id, provider)) {
                column.addView(TextView(this).apply { text = "先运行 adb shell appwidget grantbind --package $packageName --user 0" })
                break
            }
            val view = host.createView(this, id, info)
            view.updateAppWidgetSize(Bundle(), listOf(SizeF(width, height)))
            column.addView(view, LinearLayout.LayoutParams(dp(width), dp(height)).apply { topMargin = dp(16f) })
        }
        setContentView(column)
    }

    override fun onDestroy() {
        ids.forEach { runCatching { host.deleteAppWidgetId(it) } }
        runCatching { host.stopListening() }
        super.onDestroy()
    }

    private companion object {
        const val HOST_ID = 0x4A57
        /** The widget's responsive sizes (PersonaWidget): small, medium, tall, large. */
        val SIZES = listOf(110f to 100f, 250f to 150f, 250f to 200f, 250f to 260f)
    }
}
