package persona.platform.phonetools

import android.content.*
import android.os.*
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import persona.core.contracts.api.PhoneToolResult
import kotlinx.coroutines.*
import kotlinx.serialization.json.*
import java.time.ZoneId
import java.util.Locale

class AndroidPhoneTools(context: Context, private val foreground: () -> Boolean) {
    private val context = context.applicationContext
    suspend fun execute(name: String, args: JsonObject): PhoneToolResult = withContext(Dispatchers.IO) {
        when (name) {
            "phone_contacts_search" -> ContactsTool(context).search(args)
            "phone_calendar_events" -> CalendarTools(context).events(args)
            "phone_calendar_create_event" -> CalendarTools(context).create(args)
            "phone_photos_recent" -> PhotosTool(context).recent(args)
            "phone_photo_get" -> PhotosTool(context).get(args)
            "phone_location_current" -> { check(foreground()) { "请打开 Persona 后获取位置。" }; LocationTool(context).current() }
            "phone_device_info" -> device()
            "phone_clipboard_get", "phone_clipboard_set" -> withContext(Dispatchers.Main.immediate) {
                check(foreground()) { "请打开 Persona 后访问剪贴板。" }
                val clipboard = context.getSystemService(ClipboardManager::class.java)
                if (name == "phone_clipboard_set") {
                    clipboard.setPrimaryClip(ClipData.newPlainText("Persona", requireNotNull(args.string("text"))))
                    PhoneToolResult.text("剪贴板已更新。")
                } else {
                    val text = clipboard.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.text?.toString()
                    PhoneToolResult.text(buildJsonObject {
                        put("text", text.orEmpty()); if (text.isNullOrEmpty()) put("note", "剪贴板没有文字；若刚复制过内容，请检查系统的剪贴板权限。")
                    }.toString())
                }
            }
            "phone_notify" -> PhoneNotifications(context).schedule(args)
            "phone_notifications_list" -> PhoneNotifications(context).list(args.int("limit", 50))
            else -> PhoneToolResult.text("手机不支持这个工具。", true)
        }
    }
    private fun device(): PhoneToolResult {
        val battery = context.getSystemService(BatteryManager::class.java)
        val power = context.getSystemService(PowerManager::class.java)
        val storage = StatFs(context.filesDir.absolutePath)
        val network = context.getSystemService(ConnectivityManager::class.java)
        val capabilities = network.getNetworkCapabilities(network.activeNetwork)
        val kind = when {
            capabilities == null -> "offline"
            capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN) -> "vpn"
            capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
            capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
            capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
            else -> "other"
        }
        return PhoneToolResult.text(buildJsonObject {
            put("model", Build.MODEL); put("manufacturer", Build.MANUFACTURER)
            put("system", "Android"); put("version", Build.VERSION.RELEASE); put("sdk", Build.VERSION.SDK_INT)
            put("battery_percent", battery.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY)); put("charging", battery.isCharging)
            put("low_power_mode", power.isPowerSaveMode); put("thermal_state", power.currentThermalStatus)
            put("storage_total_bytes", storage.totalBytes); put("storage_available_bytes", storage.availableBytes)
            put("network", kind); put("timezone", ZoneId.systemDefault().id); put("language", Locale.getDefault().toLanguageTag())
        }.toString())
    }
}
