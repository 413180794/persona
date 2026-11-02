package jarvis.feature.settings

import android.app.ActivityManager
import android.app.NotificationManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import android.util.Log
import java.net.Inet6Address
import java.util.concurrent.TimeUnit

data class SystemKind(val name: String, val version: String, val property: String)
internal data class SettingsTarget(val action: String? = null, val component: String? = null, val packageUri: Boolean = false)
internal data class ReadyItem(val id: String, val title: String, val detail: String, val satisfied: Boolean?, val targets: List<SettingsTarget>)
internal val appDetails = SettingsTarget(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, packageUri = true)
internal val notificationSettings = SettingsTarget(Settings.ACTION_APP_NOTIFICATION_SETTINGS)

fun detectedPhoneSystem(): SystemKind = PhoneReadiness.system()

internal object PhoneReadiness {
    private val systems = listOf(
        "卓易通" to "ro.product.anco.devicetype", "澎湃 OS" to "ro.mi.os.version.name",
        "ColorOS" to "ro.build.version.oplusrom", "OriginOS" to "ro.vivo.os.build.display.id",
        "MagicOS" to "msc.config.magic.version", "鸿蒙" to "hw_sc.build.platform.version",
    )
    fun system(): SystemKind {
        systems.forEach { (name, key) ->
            val value = runCatching {
                val process = ProcessBuilder("/system/bin/getprop", key).start()
                try {
                    if (process.waitFor(1, TimeUnit.SECONDS)) process.inputStream.bufferedReader().use { it.readText().trim() } else ""
                } finally { process.destroy() }
            }.getOrDefault("")
            if (value.isNotBlank()) return SystemKind(name, value, key)
        }
        return SystemKind("Android", Build.VERSION.RELEASE, "")
    }

    fun items(context: Context, system: SystemKind): List<ReadyItem> {
        val nm = context.getSystemService(NotificationManager::class.java)
        val pm = context.getSystemService(PowerManager::class.java)
        val am = context.getSystemService(ActivityManager::class.java)
        val result = mutableListOf(
            ReadyItem("notifications", "通知", "关闭后，无法收到需要你处理和任务完成的通知。", nm.areNotificationsEnabled(), listOf(notificationSettings)),
            ReadyItem("battery", "电池优化", "未放开时，锁屏后的连接可能中断。", pm.isIgnoringBatteryOptimizations(context.packageName), listOf(SettingsTarget(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))),
            ReadyItem("background", "后台限制", "受限时，后台连接可能被系统停止。", !am.isBackgroundRestricted, listOf(appDetails)),
        )
        nm.notificationChannels.forEach { channel ->
            val groupBlocked = channel.group?.let { nm.getNotificationChannelGroup(it)?.isBlocked } == true
            result += ReadyItem("channel:${channel.id}", channel.name.toString(), "系统渠道级别：${channel.importance}。横幅和锁屏显示仍需在系统里确认。", nm.areNotificationsEnabled() && channel.importance > 0 && !groupBlocked,
                listOf(SettingsTarget("channel:${channel.id}")))
        }
        if (nm.notificationChannels.isEmpty()) result += ReadyItem("channels", "通知渠道", "尚未创建通知渠道，请在通知服务启动后再检查。", false, listOf(notificationSettings))
        val instruction = when (system.name) {
            "澎湃 OS" -> "打开自启动；电量消耗选“无限制”；允许悬浮和锁屏通知。"
            "ColorOS" -> "打开自启动；耗电管理中打开“允许完全后台行为”。"
            "OriginOS" -> "打开自启动和“允许后台高耗电”；若曾拒绝通知，请在设置里重新打开。"
            "MagicOS" -> "应用启动管理关闭自动管理，打开自启动、关联启动和后台活动；关闭智能省流量。"
            "鸿蒙" -> "应用启动管理打开三个开关；允许休眠时保持网络；关闭静默通知，打开横幅和锁屏通知。"
            "卓易通" -> "在卓易通里允许通知并保持后台运行，体验可能不完整。"
            else -> "在应用电池设置里允许后台运行，检查锁屏通知设置。"
        }
        result += ReadyItem("autostart:${system.name}", "自启动",
            "在应用启动管理中允许 Jarvis 自启动。跳过后，重启手机可能需要先手动打开 Jarvis。系统无法核验，请设置后确认。",
            null, vendorTargets[system.name].orEmpty())
        result += ReadyItem("vendor:${system.name}", "${system.name} ${system.version} 后台设置", "$instruction 系统不能查询这些开关，请设置后点“已完成”。跳过可能漏收通知。", null, vendorTargets[system.name].orEmpty())
        result += ReadyItem("task-lock", "最近任务加锁", "打开最近任务，长按 Jarvis 卡片或打开卡片菜单，选择加锁。跳过后，一键清理可能关闭连接。", null, listOf(appDetails))
        if (Build.VERSION.SDK_INT >= 37 && context.applicationInfo.targetSdkVersion >= 37) {
            result += ReadyItem("local-network", "本地网络", "未允许时，在家里可能无法连接 hub。", context.checkSelfPermission("android.permission.ACCESS_LOCAL_NETWORK") == android.content.pm.PackageManager.PERMISSION_GRANTED, listOf(appDetails))
        }
        return result
    }

    private val vendorTargets = mapOf(
        "澎湃 OS" to listOf(SettingsTarget(component = "com.miui.securitycenter/com.miui.permcenter.autostart.AutoStartManagementActivity")),
        "ColorOS" to listOf(SettingsTarget(component = "com.coloros.safecenter/com.coloros.safecenter.permission.startup.StartupAppListActivity")),
        "OriginOS" to listOf(SettingsTarget(component = "com.vivo.permissionmanager/com.vivo.permissionmanager.activity.BgStartUpManagerActivity")),
        "MagicOS" to listOf(SettingsTarget(component = "com.hihonor.systemmanager/com.hihonor.systemmanager.startupmgr.ui.StartupNormalAppListActivity")),
        "鸿蒙" to listOf(SettingsTarget(component = "com.huawei.systemmanager/com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity")),
    )

    fun open(context: Context, targets: List<SettingsTarget>): Boolean {
        for (target in targets + appDetails) {
            val intent = Intent().addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            target.component?.let { intent.component = ComponentName.unflattenFromString(it) }
            target.action?.let { action ->
                if (action.startsWith("channel:")) {
                    intent.action = Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS
                    intent.putExtra(Settings.EXTRA_CHANNEL_ID, action.removePrefix("channel:"))
                } else intent.action = action
            }
            intent.putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
            if (target.packageUri) intent.data = Uri.parse("package:${context.packageName}")
            try {
                context.startActivity(intent)
                Log.i("SettingsJump", "${target.action ?: target.component}: opened")
                return true
            } catch (_: android.content.ActivityNotFoundException) {
                Log.i("SettingsJump", "${target.action ?: target.component}: unavailable")
            } catch (_: SecurityException) {
                Log.i("SettingsJump", "${target.action ?: target.component}: denied")
            }
        }
        return false
    }

    fun network(context: Context): String = runCatching {
        val cm = context.getSystemService(ConnectivityManager::class.java)
        val network = cm.activeNetwork ?: return "没有可用网络"
        val caps = cm.getNetworkCapabilities(network)
        val props = cm.getLinkProperties(network)
        val ipv6 = props?.linkAddresses?.any { val a = it.address; a is Inet6Address && !a.isLinkLocalAddress && !a.isLoopbackAddress && !a.isSiteLocalAddress && (a.address[0].toInt() and 0xe0) == 0x20 } == true &&
            props.routes.any { it.isDefaultRoute && it.destination.address is Inet6Address }
        "IPv6：${if (ipv6) "有全局地址和默认路由" else "未发现可用路径"}\nVPN：${if (caps?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true) "是" else "否"}\n网页登录：${if (caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_CAPTIVE_PORTAL) == true) "需要" else "未检测到"}"
    }.getOrDefault("无法读取网络状态")
}
