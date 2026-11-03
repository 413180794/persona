package persona.platform.notify

/**
 * Starts of [HubService] whose onStartCommand has not run yet. A service started with startForegroundService must
 * reach startForeground before anything stops it, or Android kills the app with ForegroundServiceDidNotStartInTimeException
 * ("Bringing down service while still waiting for start foreground"). A stop asked for while a start is pending is
 * therefore left to the service, which stops itself right after it has promoted.
 */
internal class ForegroundStarts {
    private var pending = 0
    private var stopAsked = false

    /** A start was sent: the latest wish wins, so an earlier deferred stop no longer applies. */
    @Synchronized fun requested() {
        pending++
        stopAsked = false
    }

    /** The start could not be sent (startForegroundService threw). */
    @Synchronized fun failed() {
        if (pending > 0) pending--
    }

    /** True while a start is pending: the stop is recorded for it instead of stopping the service now. */
    @Synchronized fun deferStop(): Boolean = (pending > 0).also { if (it) stopAsked = true }

    /** One start reached onStartCommand and promoted; true when a stop was asked for while starts were pending. */
    @Synchronized fun promoted(): Boolean {
        if (pending > 0) pending--
        val stop = stopAsked
        if (pending == 0) stopAsked = false
        return stop
    }
}
