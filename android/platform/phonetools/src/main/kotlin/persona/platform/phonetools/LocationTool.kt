package persona.platform.phonetools

import android.content.Context
import android.location.Geocoder
import android.location.Location
import android.location.LocationManager
import androidx.core.location.LocationManagerCompat
import androidx.core.os.CancellationSignal
import persona.core.contracts.api.PhoneToolResult
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
import kotlinx.serialization.json.*
import java.time.ZoneId
import java.util.Locale
import kotlin.coroutines.resume

internal class LocationTool(private val context: Context) {
    suspend fun current(): PhoneToolResult {
        val manager = context.getSystemService(LocationManager::class.java)
        check(manager.isLocationEnabled) { "请先打开系统定位。" }
        val enabled = manager.getProviders(true)
        val providers = listOf("fused", LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER).filter { it in enabled }
        check(providers.isNotEmpty()) { "当前没有可用的定位服务。" }
        val location = withTimeoutOrNull(15_000) {
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
        } ?: error("没有取得位置，请检查系统定位后重试。")
        val address = try { if (Geocoder.isPresent() && android.os.Build.VERSION.SDK_INT >= 33) {
            withTimeoutOrNull(3000) { suspendCancellableCoroutine<String?> { continuation ->
                Geocoder(context, Locale.getDefault()).getFromLocation(location.latitude, location.longitude, 1,
                    object : Geocoder.GeocodeListener {
                        override fun onGeocode(addresses: MutableList<android.location.Address>) {
                            if (continuation.isActive) continuation.resume(addresses.firstOrNull()?.getAddressLine(0))
                        }
                        override fun onError(errorMessage: String?) { if (continuation.isActive) continuation.resume(null) }
                    })
            } }
        } else null } catch (cancelled: CancellationException) { throw cancelled } catch (_: Exception) { null }
        return PhoneToolResult.text(buildJsonObject {
            put("latitude", location.latitude); put("longitude", location.longitude); put("accuracy", location.accuracy)
            if (location.hasAltitude()) put("altitude", location.altitude)
            put("timestamp", iso(location.time)); put("timezone", ZoneId.systemDefault().id)
            put("provider", location.provider.orEmpty())
            put("coordinate_system", if (location.provider == LocationManager.GPS_PROVIDER) "WGS-84" else "provider_defined")
            put("coordinate_note", "GPS 使用 WGS-84；厂商融合或网络定位的坐标系须由地图侧核对，未转换为 GCJ-02。")
            address?.let { put("address", it) }
        }.toString())
    }
    private suspend fun locate(manager: LocationManager, provider: String): Location? = suspendCancellableCoroutine { continuation ->
        val cancellation = CancellationSignal()
        continuation.invokeOnCancellation { cancellation.cancel() }
        LocationManagerCompat.getCurrentLocation(manager, provider, cancellation, context.mainExecutor) { location ->
            if (continuation.isActive) continuation.resume(location)
        }
    }
}
