package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.NewPaneTab
import persona.core.contracts.api.PanesResponse
import persona.core.contracts.api.TerminalPanes
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/**
 * The terminal browser's two calls on one hub (docs/integrations/terminal.md "Standalone terminal"), with that hub's plain machine ids.
 * [request] is the pairing's connection: it waits for the route, adds the token and turns refusals into [HubFailure].
 */
class HttpTerminalPanes internal constructor(
    override val available: Flow<Boolean>,
    private val request: suspend (String, String, RequestBody?, Long?) -> Response,
) : TerminalPanes {
    constructor(connection: OkHttpHubConnection) : this(connection.serverFeatures.map { TERMINAL_PANES in it }, { path, method, body, timeout ->
        connection.request(path, method, body, readTimeoutSeconds = timeout)
    })

    override suspend fun panes(machineId: String): PanesResponse =
        request(panesPath(machineId), "GET", null, null).use { JsonCodec.decodeFromString(body(it)) }

    override suspend fun newTab(machineId: String, workspaceId: String, kind: String?): NewPaneTab {
        require(workspaceId.isNotBlank())
        val payload = newTabBody(workspaceId, kind).toString().toRequestBody("application/json; charset=utf-8".toMediaType())
        // The hub gives the node 30 s to open the tab (without taking the computer's focus).
        return request(panesPath(machineId), "POST", payload, 35).use { JsonCodec.decodeFromString(body(it)) }
    }

    private fun body(response: Response): String {
        if (!response.isSuccessful) throw HubFailure.from(response)
        return response.body?.string().orEmpty()
    }

    companion object {
        /** The hub's feature for `/machines/{mid}/panes` and the `pane:` terminal address. */
        const val TERMINAL_PANES = "terminal_panes"
    }
}

/** `/api/v1/machines/{mid}/panes`, the machine id encoded as one segment. */
internal fun panesPath(machineId: String): String = HttpUrl.Builder().scheme("https").host("example.com")
    .addPathSegment("api").addPathSegment("v1").addPathSegment("machines").addPathSegment(machineId).addPathSegment("panes")
    .build().encodedPath

/** `{workspace_id, kind?}`: the workspace and its host; nothing else (no directory, no command) goes to the node. */
internal fun newTabBody(workspaceId: String, kind: String?): JsonObject = buildJsonObject {
    put("workspace_id", workspaceId)
    if (!kind.isNullOrBlank()) put("kind", kind)
}
