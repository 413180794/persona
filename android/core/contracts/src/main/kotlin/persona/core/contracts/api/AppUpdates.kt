package persona.core.contracts.api

import java.io.OutputStream

data class AppUpdate(
    val versionCode: Long,
    val versionName: String,
    val downloadUrl: String,
    val sha256: String? = null,
    val releaseNotes: String = "",
)

interface AppUpdates {
    suspend fun check(): AppUpdate?
    /** Stream through the same authenticated, pinned TLS transport as the hub. */
    suspend fun download(update: AppUpdate, destination: OutputStream) {
        throw UnsupportedOperationException("Update download is unavailable")
    }
}
