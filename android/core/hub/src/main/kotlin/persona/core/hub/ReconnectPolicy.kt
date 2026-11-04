package persona.core.hub

import kotlin.random.Random

/**
 * How the connection rides out a drop without the app noticing (#1574): how long the published state stays Online
 * after the socket is gone, and how quickly it tries again after the hub said it is restarting (`hub_restarting`, or a
 * close with 1001 "going away"). Pure: the connection supplies the clock and the randomness.
 */
data class ReconnectPolicy(
    /** An ordinary drop keeps showing Online this long; most reconnects finish well inside it. */
    val dropGraceMillis: Long = 8_000,
    /** A planned hub restart keeps showing Online this long; the next hub process normally answers within 1–3 s. */
    val restartGraceMillis: Long = 30_000,
    /** Quick retries after a restart go on this long; then the ordinary backoff takes over. */
    val restartWindowMillis: Long = 30_000,
    /** The first try after a restart: soon, but spread out so every app does not arrive in the same instant. */
    val firstRestartDelayMillis: LongRange = 150L..400L,
    /** Each later try within the restart window; no exponential growth. */
    val restartRetryMillis: LongRange = 500L..1_000L,
    /** States other than Connecting / Updating (waiting for the network, a changed certificate…) show after this. */
    val otherStateDelayMillis: Long = 1_500,
    /**
     * A reconnect that did not resume the event stream surfaces once the drop lasted this long, as it always did
     * before the grace window existed: the screens then refetch what the gap may have missed when Online comes back.
     */
    val freshStartSurfaceMillis: Long = 1_500,
) {
    init {
        require(dropGraceMillis >= 0 && restartGraceMillis >= 0 && restartWindowMillis >= 0)
        require(otherStateDelayMillis >= 0 && freshStartSurfaceMillis >= 0)
        require(firstRestartDelayMillis.first >= 0 && !firstRestartDelayMillis.isEmpty())
        require(restartRetryMillis.first >= 0 && !restartRetryMillis.isEmpty())
    }

    /** Whether a restart that began at [since] (clock millis) still gets quick retries and the longer grace at [now]. */
    fun restarting(since: Long?, now: Long): Boolean = since != null && now - since in 0 until restartWindowMillis

    /** How long a change away from Online to a connecting state ([connecting]) or any other state is held back. */
    fun graceMillis(connecting: Boolean, restarting: Boolean): Long = when {
        !connecting -> otherStateDelayMillis
        restarting -> restartGraceMillis
        else -> dropGraceMillis
    }

    /** The wait before retry number [attempt] (0 is the first after the close) of a restart. */
    fun restartDelay(attempt: Int, random: () -> Double = { Random.nextDouble() }): Long {
        val range = if (attempt <= 0) firstRestartDelayMillis else restartRetryMillis
        val span = (range.last - range.first).toDouble()
        return range.first + (span * random().coerceIn(0.0, 1.0)).toLong()
    }

    /** Whether a reconnect that did not resume, after the state was held Online for [heldMillis], should surface. */
    fun surfaceFreshStart(heldMillis: Long?): Boolean = heldMillis != null && heldMillis >= freshStartSurfaceMillis
}
