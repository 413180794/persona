package persona.core.hub

import persona.core.contracts.*
import persona.core.contracts.api.MachineMetricsApi
import persona.core.contracts.api.MetricsUnavailable
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request

class HttpMachineMetricsApi(
    baseUrl: String,
    private val token: String,
    client: OkHttpClient,
    private val isCurrent: () -> Boolean = { true },
    private val dispatcher: CoroutineDispatcher = Dispatchers.Default,
) : MachineMetricsApi {
    private val base = baseUrl.toHttpUrl()
    private val http = client.newBuilder().followRedirects(false).followSslRedirects(false)
        .retryOnConnectionFailure(false).build()

    private fun checkScope() { if (!isCurrent()) throw CancellationException("Pairing changed") }

    private suspend inline fun <reified T> get(url: HttpUrl): T = withContext(dispatcher) {
        checkScope()
        val request = Request.Builder().url(url).header("Authorization", "Bearer $token").build()
        http.newCall(request).awaitResponse().use { response ->
            checkScope()
            if (response.code == 404) throw MetricsUnavailable()
            if (!response.isSuccessful) throw HubFailure.from(response)
            val result = JsonCodec.decodeFromString<T>(response.body!!.string())
            checkScope()
            result
        }
    }

    override suspend fun dashboard(): MetricsDashboard = get(base.newBuilder()
        .addPathSegments("api/v1/metrics").build())

    override suspend fun history(machineId: String, range: MetricRange): MetricsHistory = get(base.newBuilder()
        .addPathSegments("api/v1/machines").addPathSegment(machineId).addPathSegment("metrics")
        .addQueryParameter("range", range.wire).build())
}
