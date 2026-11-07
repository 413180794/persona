package persona.feature.settings

import android.app.Activity
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.os.Build
import android.os.Bundle
import android.widget.Toast
import persona.core.contracts.api.AppUpdate
import persona.core.contracts.api.AppUpdates
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.io.InputStream
import java.security.MessageDigest

internal fun verifySha256(input: InputStream, expected: String): Boolean {
    if (!expected.matches(Regex("[a-fA-F0-9]{64}"))) return false
    val digest = MessageDigest.getInstance("SHA-256")
    val buffer = ByteArray(DEFAULT_BUFFER_SIZE)
    while (true) {
        val count = input.read(buffer)
        if (count < 0) break
        digest.update(buffer, 0, count)
    }
    return digest.digest().joinToString("") { "%02x".format(it) }.equals(expected, ignoreCase = true)
}

internal object AppInstaller {
    suspend fun install(context: Context, updates: AppUpdates, update: AppUpdate) = withContext(Dispatchers.IO) {
        val checksum = requireNotNull(update.sha256) { "Missing SHA-256" }
        require(checksum.matches(Regex("[a-fA-F0-9]{64}"))) { "Invalid SHA-256" }
        requireNewerVersion(context, update)
        check(context.packageManager.canRequestPackageInstalls()) { "Install permission required" }
        val apk = File.createTempFile("persona-update-", ".apk", context.cacheDir)
        try {
            apk.outputStream().use { updates.download(update, it) }
            check(apk.inputStream().use { verifySha256(it, checksum) }) { "SHA-256 mismatch" }
            val archive = context.packageManager.getPackageArchiveInfo(apk.absolutePath, 0)
            check(archive?.packageName == context.packageName && archive.longVersionCode == update.versionCode) { "Package identity mismatch" }
            // Another installation may have completed while this APK was downloading.
            requireNewerVersion(context, update)
            val installer = context.packageManager.packageInstaller
            val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
                setAppPackageName(context.packageName)
                setSize(apk.length())
                // API 30 always asks the user to confirm an update; from 31 the session must ask for it explicitly.
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                    setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_REQUIRED)
                }
            }
            val sessionId = installer.createSession(params)
            try {
                installer.openSession(sessionId).use { session ->
                    session.openWrite("base.apk", 0, apk.length()).use { output ->
                        apk.inputStream().use { it.copyTo(output) }
                        session.fsync(output)
                    }
                    val result = Intent(context, InstallResultActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    val pending = PendingIntent.getActivity(context, sessionId, result, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE)
                    session.commit(pending.intentSender)
                }
            } catch (error: Exception) {
                installer.abandonSession(sessionId)
                throw error
            }
        } finally { apk.delete() }
    }

    private fun requireNewerVersion(context: Context, update: AppUpdate) {
        val installed = context.packageManager.getPackageInfo(context.packageName, 0).longVersionCode
        check(update.versionCode > installed) { "Update version must be newer than installed version" }
    }
}

/** An explicit activity PendingIntent allows the system confirmation to open from the installer callback. */
class InstallResultActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
        if (status == PackageInstaller.STATUS_PENDING_USER_ACTION) {
            @Suppress("DEPRECATION")
            val confirmation = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT)
            if (confirmation != null) {
                try { startActivity(confirmation) } catch (_: Exception) { show(getString(R.string.settings_install_confirm_failed)) }
            } else show(getString(R.string.settings_install_no_confirm))
        } else {
            val raw = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE).orEmpty()
            val text = when {
                status == PackageInstaller.STATUS_SUCCESS -> getString(R.string.settings_install_done)
                raw.contains("INSTALL_FAILED_ABORTED") -> getString(R.string.settings_install_aborted_by_system)
                status == PackageInstaller.STATUS_FAILURE_ABORTED -> getString(R.string.settings_install_cancelled)
                else -> getString(R.string.settings_install_failed)
            }
            getSharedPreferences("settings-update", MODE_PRIVATE).edit().putString("result", text).apply()
            show(text)
        }
        finish()
    }
    private fun show(message: String) = Toast.makeText(this, message, Toast.LENGTH_LONG).show()
}
