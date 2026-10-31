package jarvis.android.settings

import android.content.Context
import java.io.File

/** What the app keeps on this phone, by kind (iOS `StorageKind`), measured from its own data directory. */
enum class StorageKind(val title: String, val detail: String) {
    AGENT_CHATS("Agent 对话", "每个对话最近两轮，打开时先显示，后台同步也写在这里"),
    CHAT_IMAGES("对话里的图片", "截图等图片，看过一次就不再下载"),
    FEISHU_MEDIA("飞书图片和文件", "飞书消息里的图片、视频和文件"),
    PAGE_STATE("列表和飞书会话", "Agents、收件箱列表和飞书会话最近的消息"),
    WEB("网页", "App 内打开的网页（不含登录状态）"),
    NETWORK("网络缓存", "系统为网络请求保存的副本"),
    OTHER("其他缓存", "临时文件和其他缓存"),
    APP_DATA("App 数据", "设置、配对信息，清除缓存时保留");

    val clearable: Boolean get() = this != APP_DATA
}

/**
 * Measures and clears the app's caches. Everything clearable can be fetched again from the hub. Layout (Android):
 * `files/cache/<area>/` = JsonCache (by area), `cache/<name>` = loaders' caches, `app_webview` = WebView (its cache
 * folders only are cleared: cookies and logins stay), everything else (shared_prefs, databases, files) = App 数据.
 * Blocking: call off the main thread.
 */
object StorageUsage {
    /** JsonCache areas (`files/cache/<area>`) → kind. */
    fun kindOfJsonCache(area: String): StorageKind = when (area.lowercase()) {
        "chat", "chats", "agents", "assistant", "sync" -> StorageKind.AGENT_CHATS
        "chat-images", "images" -> StorageKind.CHAT_IMAGES
        "lark-media" -> StorageKind.FEISHU_MEDIA
        else -> StorageKind.PAGE_STATE
    }

    /** Entries of the cache directory → kind. */
    fun kindOfCacheEntry(name: String): StorageKind {
        val n = name.lowercase()
        return when {
            n == "chats" || n == "chat" || n.startsWith("chat-items") -> StorageKind.AGENT_CHATS
            n.contains("image") && n.startsWith("chat") -> StorageKind.CHAT_IMAGES
            n == "images" || n == "image_cache" -> StorageKind.CHAT_IMAGES
            n.startsWith("lark") || n.startsWith("feishu") -> StorageKind.FEISHU_MEDIA
            n == "webview" || n.startsWith("org.chromium") || n == "web" -> StorageKind.WEB
            n == "http" || n == "okhttp" || n == "http-cache" || n == "image_manager_disk_cache" -> StorageKind.NETWORK
            else -> StorageKind.OTHER
        }
    }

    /** Directories under `app_webview` that are caches (cleared with 网页); the rest is cookies / storage (kept). */
    private val webCacheDirs = setOf("cache", "http cache", "code cache", "gpucache", "cachestorage", "scriptcache", "dawncache")

    fun scan(ctx: Context): Map<StorageKind, Long> {
        val out = HashMap<StorageKind, Long>()
        fun add(k: StorageKind, f: File) { out[k] = (out[k] ?: 0L) + size(f) }
        ctx.cacheDir.listFiles()?.forEach { add(kindOfCacheEntry(it.name), it) }
        ctx.externalCacheDir?.listFiles()?.forEach { add(kindOfCacheEntry(it.name), it) }
        val data = ctx.filesDir.parentFile ?: return out
        data.listFiles()?.forEach { f ->
            when (f.name) {
                "cache", "code_cache", "lib" -> Unit // the cache dir is counted above; the other two are the system's
                "files" -> f.listFiles()?.forEach { c ->
                    if (c.name == "cache" && c.isDirectory) c.listFiles()?.forEach { add(kindOfJsonCache(it.name), it) }
                    else add(StorageKind.APP_DATA, c)
                }
                "app_webview", "app_chrome" -> add(StorageKind.WEB, f)
                else -> add(StorageKind.APP_DATA, f)
            }
        }
        return out
    }

    /** Removes the files of one kind. Folders the loaders write into stay. */
    fun clear(ctx: Context, kind: StorageKind) {
        if (!kind.clearable) return
        ctx.cacheDir.listFiles()?.forEach { if (kindOfCacheEntry(it.name) == kind) removeContents(it) }
        ctx.externalCacheDir?.listFiles()?.forEach { if (kindOfCacheEntry(it.name) == kind) removeContents(it) }
        File(ctx.filesDir, "cache").listFiles()?.forEach { if (kindOfJsonCache(it.name) == kind) removeContents(it) }
        if (kind == StorageKind.WEB) {
            val data = ctx.filesDir.parentFile
            listOf("app_webview", "app_chrome").forEach { name -> data?.let { clearWebCaches(File(it, name)) } }
        }
    }

    private fun clearWebCaches(dir: File) {
        dir.listFiles()?.forEach { f ->
            if (!f.isDirectory) return@forEach
            if (f.name.lowercase() in webCacheDirs) f.deleteRecursively() else clearWebCaches(f)
        }
    }

    /** Bytes on disk (allocated blocks would need Os.stat per file; file lengths are close enough here). */
    fun size(f: File): Long {
        if (f.isFile) return f.length()
        var total = 0L
        f.walkTopDown().onFail { _, _ -> }.forEach { if (it.isFile) total += it.length() }
        return total
    }

    private fun removeContents(f: File) {
        if (f.isFile) { f.delete(); return }
        f.listFiles()?.forEach { it.deleteRecursively() }
    }
}

/** 12 KB, 3.4 MB, 1.2 GB (decimal units like Android's own storage page). */
fun storageText(bytes: Long): String {
    if (bytes <= 0) return "0 KB"
    if (bytes < 1000) return "$bytes 字节"
    val units = listOf("KB", "MB", "GB", "TB")
    var x = bytes / 1000.0
    var i = 0
    while (x >= 1000 && i < units.size - 1) { x /= 1000; i++ }
    return if (x >= 100 || i == 0) String.format(java.util.Locale.US, "%.0f %s", x, units[i])
    else String.format(java.util.Locale.US, "%.1f %s", x, units[i])
}
