package persona.core.contracts.api

import kotlinx.coroutines.flow.StateFlow

/** Device-local display settings; the Android host owns the system overlay permission. */
interface FloatingCapsuleSettings {
    /** One of off, active, always. */
    val mode: StateFlow<String>
    val permissionGranted: StateFlow<Boolean>
    fun setMode(mode: String)
    fun refreshPermission()
    fun requestPermission()
}
