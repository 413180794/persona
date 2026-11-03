package persona.android.di

import android.app.Application
import java.io.File

/** Unversioned data cannot safely be assigned to the current pairing. */
internal fun clearUnversionedPairingData(app: Application, disconnect: Boolean = false) {
    val marker = File(app.noBackupFilesDir, "pairing-legacy-cleanup-v2")
    if (!marker.exists()) {
        persona.android.data.clearLegacyData(app)
        val directories = listOf("chat-data", "inbox-downloads", "inbox-replies", "list-snapshots", "chat-images")
        listOf(app.filesDir, app.cacheDir, app.noBackupFilesDir).forEach { root ->
            directories.forEach { name ->
                val file = File(root, name)
                check(!file.exists() || file.deleteRecursively()) { "Cannot remove legacy pairing data" }
            }
        }
        listOf("settings-readiness", "notify_recovery", "notification_actions").forEach { name ->
            check(app.getSharedPreferences(name, Application.MODE_PRIVATE).edit().clear().commit())
        }
        val exits = File(app.filesDir, "notify-exits.log")
        check(!exits.exists() || exits.delete())
        marker.writeText("done")
    }
    // These compatibility records can still be written by older entry points during this pairing.
    if (disconnect) {
        persona.android.navigation.ShellPositionStore.clear(app)
        val exits = File(app.filesDir, "notify-exits.log")
        check(!exits.exists() || exits.delete())
        // Lark reading anchors are keyed by pairing scope but live outside the scoped directories.
        val larkReading = File(app.noBackupFilesDir, "lark-reading")
        check(!larkReading.exists() || larkReading.deleteRecursively())
    }
    if (disconnect) {
        listOf("settings-readiness", "notification_actions", "notify_recovery", "platform-entry").forEach { name ->
            check(app.getSharedPreferences(name, Application.MODE_PRIVATE).edit().clear().commit())
        }
        check(app.getSharedPreferences("notify_recovery", Application.MODE_PRIVATE).edit()
            .putLong("last_exit", System.currentTimeMillis()).commit())
    }
}
