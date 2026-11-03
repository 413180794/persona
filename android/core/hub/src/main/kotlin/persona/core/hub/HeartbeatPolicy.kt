package persona.core.hub

import kotlin.random.Random

enum class HubNetworkTransport { Wifi, Cellular, Other }

/** Defaults follow the Android design; transport-specific values require deployment measurements. */
data class HeartbeatPolicy(
    val foregroundSeconds: Int = 20,
    val backgroundWifiSeconds: Int = 240,
    val backgroundCellularSeconds: Int = 240,
    val backgroundOtherSeconds: Int = 240,
    val pongTimeoutSeconds: Int = 15,
    val backgroundJitterFraction: Double = 0.0,
) {
    init {
        require(listOf(foregroundSeconds, backgroundWifiSeconds, backgroundCellularSeconds,
            backgroundOtherSeconds, pongTimeoutSeconds).all { it > 0 })
        require(backgroundJitterFraction in 0.0..1.0)
    }

    fun interval(foreground: Boolean, transport: HubNetworkTransport, random: () -> Double = { Random.nextDouble() }): Int {
        if (foreground) return foregroundSeconds
        val ceiling = when (transport) {
            HubNetworkTransport.Wifi -> backgroundWifiSeconds
            HubNetworkTransport.Cellular -> backgroundCellularSeconds
            HubNetworkTransport.Other -> backgroundOtherSeconds
        }
        return (ceiling * (1.0 - backgroundJitterFraction * random().coerceIn(0.0, 1.0)))
            .toInt().coerceIn(1, ceiling)
    }
}
