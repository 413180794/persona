package persona.feature.agents

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.AgentStartRequest
import kotlinx.coroutines.flow.first
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.*

private val Context.agentCreationStore by preferencesDataStore("agent_creation")

internal data class AgentDefaults(val cwd: String = "", val launcher: String = "", val mode: String = "", val effort: String = "")
/**
 * One start, saved before it is sent. An async start keeps its first message here with the id it is sent under
 * ([firstMessageId]), so checking the start again after a lost answer or a restart queues that message only once.
 */
internal data class AgentStartOperation(
    val id: String,
    val request: AgentStartRequest,
    val firstMessage: String? = null,
    val firstMessageId: String? = null,
)

internal interface AgentCreationStore {
    suspend fun lastMachine(scope: String): String
    suspend fun defaults(scope: String, machine: String): AgentDefaults
    suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults)
    suspend fun pending(scope: String): AgentStartOperation?
    suspend fun pending(scope: String, value: AgentStartOperation?)
}

internal class AgentCreationPreferences(
    context: Context,
    private val store: androidx.datastore.core.DataStore<androidx.datastore.preferences.core.Preferences> = context.applicationContext.agentCreationStore,
) : AgentCreationStore {
    override suspend fun lastMachine(scope: String) = store.data.first()[stringPreferencesKey("$scope/last")].orEmpty()
    override suspend fun defaults(scope: String, machine: String): AgentDefaults {
        val p = store.data.first()
        fun read(name: String) = p[stringPreferencesKey("$scope/$machine/$name")].orEmpty()
        return AgentDefaults(read("cwd"), read("launcher"), read("mode"), read("effort"))
    }
    override suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults) {
        store.edit { p ->
            p[stringPreferencesKey("$scope/last")] = machine
            mapOf("cwd" to defaults.cwd, "launcher" to defaults.launcher, "mode" to defaults.mode, "effort" to defaults.effort).forEach { (key, value) ->
                p[stringPreferencesKey("$scope/$machine/$key")] = value
            }
        }
    }
    override suspend fun pending(scope: String): AgentStartOperation? =
        store.data.first()[stringPreferencesKey("$scope/pending")]?.let(::decodeOperation)
    override suspend fun pending(scope: String, value: AgentStartOperation?) {
        store.edit { p ->
            val key = stringPreferencesKey("$scope/pending")
            if (value == null) p.remove(key) else p[key] = encodeOperation(value)
        }
    }
}

internal fun encodeOperation(value: AgentStartOperation): String = buildJsonObject {
    put("id", value.id)
    put("request", JsonCodec.parseToJsonElement(JsonCodec.encodeToString(value.request)))
    value.firstMessage?.let { put("first", it) }
    value.firstMessageId?.let { put("first_id", it) }
}.toString()

/** Corrupt records fail closed (throw) rather than silently allow another launch; older records have no first message. */
internal fun decodeOperation(raw: String): AgentStartOperation {
    val value = JsonCodec.parseToJsonElement(raw).jsonObject
    return AgentStartOperation(value.getValue("id").jsonPrimitive.content, JsonCodec.decodeFromJsonElement(value.getValue("request")),
        value["first"]?.jsonPrimitive?.contentOrNull, value["first_id"]?.jsonPrimitive?.contentOrNull)
}
