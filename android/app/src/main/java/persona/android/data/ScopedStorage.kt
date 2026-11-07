package persona.android.data

import persona.android.AppText
import persona.android.R
import android.app.Application
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import persona.android.di.ConnectionRuntime
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.distinctUntilChanged
import java.io.File

/**
 * Keep the injected facade stable while each pairing owns its actual DataStore. The merged lists' settings live with
 * the first hub group, so changing the group the hub-level pages show does not change them (docs/android/android.md "Multiple hubs").
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ScopedListPreferences(private val runtime: ConnectionRuntime) : DataStore<Preferences> {
    private val owner = runtime.groups.map { groups -> groups.firstOrNull()?.data }.distinctUntilChanged()
    override val data = owner.flatMapLatest { it?.listPreferences?.data ?: flowOf(emptyPreferences()) }
    override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences {
        val data = checkNotNull(runtime.groups.value.firstOrNull()?.data) { AppText.get(R.string.app_error_pair_first) }
        return data.coroutineScope.async(Dispatchers.IO) {
            check(runtime.groups.value.firstOrNull()?.data === data) { AppText.get(R.string.app_error_switched) }
            data.listPreferences.updateData(transform)
        }.await()
    }
}

/** Unversioned stores cannot be attributed to a pairing and must never be imported. */
internal fun clearLegacyData(app: Application) {
    val locations = listOf(
        File(app.noBackupFilesDir, "chat-data"), File(app.noBackupFilesDir, "inbox-replies"),
        File(app.noBackupFilesDir, "list-snapshots"), File(app.filesDir, "chat-images"),
        File(app.filesDir, "datastore/agents_list.preferences_pb"), File(app.cacheDir, "inbox-downloads"),
    )
    locations.forEach { file -> check(!file.exists() || file.deleteRecursively()) { "Cannot remove legacy data" } }
    check(app.getSharedPreferences("chat-cache-scope", Application.MODE_PRIVATE).edit().clear().commit())
}
