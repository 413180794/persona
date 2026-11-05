package persona.platform.phonetools

import android.content.Context
import android.location.Address
import android.location.Geocoder
import android.location.Location
import android.location.LocationManager
import android.os.Build
import androidx.core.location.LocationManagerCompat
import androidx.core.os.CancellationSignal
import persona.core.contracts.api.PhoneToolResult
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
import kotlinx.serialization.json.*
import java.time.ZoneId
import java.util.Locale
import kotlin.coroutines.resume

/** phone_location_current, answering as the iOS app's LocationTool does, plus which coordinate system it is in. */
internal class LocationTool(private val context: Context) {
    @Suppress("MissingPermission") // PhoneToolRegistry checks the location permission before any call gets here.
    suspend fun current(): PhoneToolResult {
        val manager = context.getSystemService(LocationManager::class.java)
        if (!manager.isLocationEnabled) unavailable("定位服务已关闭 / Location services are turned off on the phone.")  // i18n-ignore: tool result for the model
        val enabled = manager.getProviders(true)
        val providers = listOf("fused", LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER).filter { it in enabled }
        val location = (if (providers.isEmpty()) null else withTimeoutOrNull(FIX_TIMEOUT_MILLIS) {
            coroutineScope {
                val results = Channel<Location?>(providers.size)
                val jobs = providers.map { provider -> launch {
                    val fix = try { locate(manager, provider) }
                    catch (cancelled: CancellationException) { throw cancelled }
                    catch (_: Exception) { null }
                    results.send(fix)
                } }
                try {
                    var fix: Location? = null
                    repeat(providers.size) { if (fix == null) fix = results.receive() }
                    fix
                } finally { jobs.forEach { it.cancel() }; results.close() }
            }
        })
            // No fresh fix in time (indoors): the newest one any provider still has, as the old Android app did.
            ?: enabled.mapNotNull { runCatching { manager.getLastKnownLocation(it) }.getOrNull() }.maxByOrNull { it.time }
            ?: unavailable("15 秒内没拿到定位 / No location fix within 15 seconds. The phone may be indoors or location services may be off.")  // i18n-ignore: tool result for the model
        val address = try { geocode(location) }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { buildJsonObject { put("note", "Reverse geocoding failed: ${error.message ?: error.javaClass.simpleName}") } }
        return PhoneToolResult.text(buildJsonObject {
            put("latitude", location.latitude); put("longitude", location.longitude)
            put("accuracy_meters", location.accuracy.toDouble())
            if (location.hasAltitude()) put("altitude_meters", location.altitude)
            put("timestamp", iso(location.time)); put("timezone", ZoneId.systemDefault().id)
            address?.let { put("address", it) }
            put("provider", location.provider.orEmpty())
            put("coordinate_system", if (location.provider == LocationManager.GPS_PROVIDER) "WGS-84" else "provider_defined")
            put("coordinate_note", "GPS 使用 WGS-84；厂商融合或网络定位的坐标系须由地图侧核对，未转换为 GCJ-02。")  // i18n-ignore: tool result for the model
        }.toString())
    }

    /** The address in iOS's shape; null when the phone has no geocoder or found nothing there. */
    private suspend fun geocode(location: Location): JsonObject? {
        if (!Geocoder.isPresent()) return null
        val geocoder = Geocoder(context, Locale.getDefault())
        val place = withTimeoutOrNull(GEOCODE_TIMEOUT_MILLIS) {
            if (Build.VERSION.SDK_INT >= 33) suspendCancellableCoroutine<Address?> { continuation ->
                geocoder.getFromLocation(location.latitude, location.longitude, 1, object : Geocoder.GeocodeListener {
                    override fun onGeocode(addresses: MutableList<Address>) {
                        if (continuation.isActive) continuation.resume(addresses.firstOrNull())
                    }
                    override fun onError(errorMessage: String?) {
                        if (continuation.isActive) continuation.resume(null)
                    }
                })
            } else runInterruptible(Dispatchers.IO) {
                @Suppress("DEPRECATION") geocoder.getFromLocation(location.latitude, location.longitude, 1)?.firstOrNull()
            }
        } ?: return null
        return addressJson(place)
    }

    private suspend fun locate(manager: LocationManager, provider: String): Location? = suspendCancellableCoroutine { continuation ->
        val cancellation = CancellationSignal()
        continuation.invokeOnCancellation { cancellation.cancel() }
        @Suppress("MissingPermission")
        LocationManagerCompat.getCurrentLocation(manager, provider, cancellation, context.mainExecutor) { location ->
            if (continuation.isActive) continuation.resume(location)
        }
    }

    private companion object {
        const val FIX_TIMEOUT_MILLIS = 15_000L
        const val GEOCODE_TIMEOUT_MILLIS = 3_000L
    }
}

/** iOS LocationTool's address keys; Chinese addresses read best joined without separators (iOS `formatted`). */
internal fun addressJson(place: Address): JsonObject = buildJsonObject {
    fun opt(key: String, value: String?) { value?.takeIf { it.isNotBlank() }?.let { put(key, it) } }
    opt("name", place.featureName)
    opt("country", place.countryName); opt("province", place.adminArea); opt("city", place.locality)
    opt("district", place.subLocality); opt("street", place.thoroughfare); opt("street_number", place.subThoroughfare)
    opt("postal_code", place.postalCode)
    val parts = listOf(place.countryName, place.adminArea, place.locality, place.subLocality, place.thoroughfare, place.subThoroughfare)
        .filterNot { it.isNullOrBlank() }
    opt("formatted", place.getAddressLine(0)?.takeIf { it.isNotBlank() } ?: parts.joinToString("").ifEmpty { place.featureName })
}
