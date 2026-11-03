package persona.feature.settings

import android.content.Context
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.os.Build
import persona.core.contracts.api.PairingRequest
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.net.InetAddress
import java.net.URI
import java.net.UnknownHostException

internal object PairingLocalNetwork {
    const val Permission = "android.permission.ACCESS_LOCAL_NETWORK"

    fun enforced(sdk: Int, target: Int): Boolean = sdk >= 37 && target >= 37

    fun missing(context: Context): Boolean =
        enforced(Build.VERSION.SDK_INT, context.applicationInfo.targetSdkVersion) &&
            context.checkSelfPermission(Permission) != PackageManager.PERMISSION_GRANTED

    suspend fun needsPermission(context: Context, request: PairingRequest): Boolean {
        if (!missing(context)) return false
        return withContext(Dispatchers.IO) {
            val manager = context.getSystemService(ConnectivityManager::class.java)
            val network = manager.activeNetwork
            val prefixes = network?.let(manager::getLinkProperties)?.linkAddresses.orEmpty()
                .map { it.address to it.prefixLength }
            (listOf(request.url) + request.alt).any { url ->
                val host = URI(url).host.removePrefix("[").removeSuffix("]")
                if (host.endsWith(".local", ignoreCase = true)) true
                else try {
                    val addresses = network?.getAllByName(host) ?: InetAddress.getAllByName(host)
                    addresses.any { isLocal(it, prefixes) }
                } catch (_: UnknownHostException) { false }
                catch (_: SecurityException) { true }
            }
        }
    }

    fun isLocal(address: InetAddress, prefixes: List<Pair<InetAddress, Int>>): Boolean {
        if (address.isLoopbackAddress || address.isAnyLocalAddress) return false
        val bytes = address.address
        if (address.isLinkLocalAddress || address.isSiteLocalAddress ||
            (bytes.size == 16 && (bytes[0].toInt() and 0xfe) == 0xfc)) return true
        return prefixes.any { (local, bits) ->
            val other = local.address
            if (other.size != bytes.size || bits !in 1..bytes.size * 8) false
            else (0 until bits).all { bit ->
                val mask = 1 shl (7 - bit % 8)
                (bytes[bit / 8].toInt() and mask) == (other[bit / 8].toInt() and mask)
            }
        }
    }
}
