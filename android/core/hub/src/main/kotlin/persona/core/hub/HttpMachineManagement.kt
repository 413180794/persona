package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.Machine
import persona.core.contracts.api.AgentLauncher
import persona.core.contracts.api.ClaudeSetting
import persona.core.contracts.api.MachineManagement
import kotlinx.serialization.json.*
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/**
 * The machine page's and settings' calls on one machine. [request] is the pairing's connection: it waits for the
 * route, adds the token and turns refusals into [HubFailure] (the hub's own words in its message).
 */
class HttpMachineManagement internal constructor(
    private val request: suspend (String, String, RequestBody?) -> Response,
) : MachineManagement {
    constructor(connection: OkHttpHubConnection) : this({ path, method, body -> connection.request(path, method, body) })

    override suspend fun launchers(machineId: String): List<AgentLauncher> =
        (call(api("machines", machineId, "launchers"), "GET")["launchers"] as? JsonArray).orEmpty()
            .map { JsonCodec.decodeFromJsonElement<AgentLauncher>(it) }

    override suspend fun reconnect(machineId: String) { call(api("machines", machineId, "reconnect"), "POST", buildJsonObject { }) }

    override suspend fun deploy(machineId: String) { call(api("machines", machineId, "deploy"), "POST", buildJsonObject { }) }

    override suspend fun setEngine(machineId: String, engine: String): Machine {
        require(engine.isNotBlank())
        val machine = JsonCodec.decodeFromJsonElement<Machine>(
            call(api("machines", machineId, "engine"), "PUT", buildJsonObject { put("engine", engine) }))
        // An answer for another machine is not this machine's state.
        if (machine.id != machineId) throw HubFailure(HubError.Protocol, "hub 返回了另一台机器", retryable = false)
        return machine
    }

    override suspend fun claudeSettings(machineId: String): List<ClaudeSetting> =
        settings(call(api("machines", machineId, "claude-settings"), "GET"))

    override suspend fun setClaudeSetting(machineId: String, key: String, value: JsonElement): List<ClaudeSetting> {
        require(key.isNotBlank())
        return settings(call(api("machines", machineId, "claude-settings"), "PUT", buildJsonObject {
            put("key", key)
            // JSON null (not an absent field) takes the option back to Claude Code's default.
            put("value", value)
        }))
    }

    private fun settings(body: JsonObject): List<ClaudeSetting> =
        (body["settings"] as? JsonArray).orEmpty().map { JsonCodec.decodeFromJsonElement<ClaudeSetting>(it) }

    private suspend fun call(path: String, method: String, body: JsonObject? = null): JsonObject =
        request(path, method, body?.toString()?.toRequestBody("application/json; charset=utf-8".toMediaType())).use {
            if (!it.isSuccessful) throw HubFailure.from(it)
            val text = it.body?.string().orEmpty()
            if (text.isBlank()) buildJsonObject { } else JsonCodec.parseToJsonElement(text).jsonObject
        }

    private fun api(vararg segments: String) = HttpUrl.Builder().scheme("https").host("example.com")
        .addPathSegment("api").addPathSegment("v1").apply { segments.forEach(::addPathSegment) }.build().encodedPath
}
