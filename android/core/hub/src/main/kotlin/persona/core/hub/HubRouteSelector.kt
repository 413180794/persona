package persona.core.hub

import persona.core.contracts.Endpoint
import persona.core.contracts.Health
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

data class HubRoute(val endpoint: Endpoint, val health: Health)

class HubRouteSelector internal constructor(
    private val clockMillis: () -> Long,
    private val probe: suspend (Endpoint) -> Health,
) {
    constructor(http: HubHttpClient, clockMillis: () -> Long = { System.nanoTime() / 1_000_000 }) :
        this(clockMillis, { http.health(it) })

    /**
     * Races pinned health probes strictly in the order given (the order settings lists; nothing is re-sorted here).
     * Each candidate starts once the one before it has failed or [STEP_MILLIS] after that one started, whichever is
     * first, so every candidate has had that long on its own before the next one can overtake it. The first to pass
     * the identity check wins. [probeTimeoutMillis] gives up on a probe that has not passed by then (a Timeout failure).
     */
    suspend fun select(
        ordered: List<Endpoint>,
        probeTimeoutMillis: Long? = null,
        onProbeMeasured: (Endpoint, Long, Result<HubRoute>) -> Unit = { _, _, _ -> },
        onProbe: (Endpoint, Result<HubRoute>) -> Unit = { _, _ -> },
    ): HubRoute = coroutineScope {
        val candidates = ordered.distinctBy { endpointKey(it.url) }
        if (candidates.isEmpty()) throw HubFailure(HubError.Unreachable)
        val started = List(candidates.size) { CompletableDeferred<Unit>() }
        val failed = List(candidates.size) { CompletableDeferred<Unit>() }
        val results = Channel<Result<HubRoute>>(Channel.UNLIMITED)
        val probes = candidates.mapIndexed { index, endpoint ->
            launch {
                if (index > 0) {
                    started[index - 1].await()
                    withTimeoutOrNull(STEP_MILLIS) { failed[index - 1].await() }
                }
                started[index].complete(Unit)
                val begin = clockMillis()
                val result = try {
                    val health = if (probeTimeoutMillis == null) probe(endpoint)
                        else withTimeoutOrNull(probeTimeoutMillis) { probe(endpoint) } ?: throw HubFailure(HubError.Timeout)
                    Result.success(HubRoute(endpoint, health))
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (error: Exception) {
                    Result.failure(HubFailure.from(error))
                }
                if (result.isFailure) failed[index].complete(Unit)
                onProbeMeasured(endpoint, (clockMillis() - begin).coerceAtLeast(0), result)
                onProbe(endpoint, result)
                results.send(result)
            }
        }
        try {
            val failures = mutableListOf<HubFailure>()
            repeat(candidates.size) {
                val result = results.receive()
                result.getOrNull()?.let { winner -> return@coroutineScope winner }
                failures += HubFailure.from(result.exceptionOrNull()!!)
            }
            throw failures.firstOrNull { it.kind == HubError.CertificateMismatch }
                ?: failures.firstOrNull { it.kind == HubError.HubMismatch }
                ?: failures.first()
        } finally {
            probes.forEach { it.cancel() }
            results.close()
        }
    }

    companion object {
        /** How long a candidate has on its own before the next one starts; a configured value, not measured. */
        const val STEP_MILLIS = 1000L
    }
}
