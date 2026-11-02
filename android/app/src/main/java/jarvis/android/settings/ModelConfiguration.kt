package jarvis.android.settings

import jarvis.android.data.ApiClient
import jarvis.android.data.JsonCodec
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

@Serializable
data class ModelBinding(val provider: String? = null, val model: String? = null, val effort: String? = null)

@Serializable
data class ModelProvider(
    val id: String,
    val name: String = "",
    val kind: String = "launcher",
    val api: String? = null,
    val model: String? = null,
    val machines: List<String> = emptyList(),
    val hidden: Boolean = false,
    @SerialName("has_key") val hasKey: Boolean = false,
)

@Serializable
data class ModelPurpose(
    val id: String,
    val name: String = "",
    val group: String = "",
    val kind: String = "launcher",
    val parent: String? = null,
    @SerialName("machine_id") val machineId: String? = null,
    val override: ModelBinding = ModelBinding(),
    val effective: ModelBinding = ModelBinding(),
    val revision: Long = 0,
    @SerialName("inherited_from") val inheritedFrom: String? = null,
    val status: String? = null,
    val fields: List<String>? = null,
    @SerialName("provider_ids") val providerIds: List<String>? = null,
) {
    fun supports(field: String): Boolean = field in (fields ?: listOf("provider", "model"))

    fun summary(providers: List<ModelProvider> = emptyList()): String = listOfNotNull(
        providers.firstOrNull { it.id == effective.provider }?.name?.ifBlank { null } ?: effective.provider,
        effective.model,
        effective.effort?.let { "思考 $it" },
    ).joinToString(" · ").ifEmpty { "未配置" }

    fun providersFrom(providers: List<ModelProvider>): List<ModelProvider> = providers.filter {
        (providerIds?.contains(it.id) ?: (it.kind == kind)) &&
            (!it.hidden || it.id == override.provider || it.id == effective.provider)
    }
}

@Serializable
data class ModelConfigurationBody(
    val purposes: List<ModelPurpose> = emptyList(),
    val providers: List<ModelProvider> = emptyList(),
)

@Serializable
data class ModelPurposeBody(val purpose: ModelPurpose, val providers: List<ModelProvider> = emptyList())

/** Empty fields mean restore inheritance, not empty overrides. Switching providers clears incompatible settings. */
data class ModelConfigurationDraft(val provider: String = "", val model: String = "", val effort: String = "") {
    fun choosingProvider(id: String): ModelConfigurationDraft =
        if (id == provider) this else ModelConfigurationDraft(provider = id)

    fun patch(p: ModelPurpose): String = buildJsonObject {
        put("revision", p.revision)
        for ((key, value) in listOf("provider" to provider, "model" to model, "effort" to effort)) {
            if (p.supports(key)) put(key, value.trim().takeIf { it.isNotEmpty() }?.let(::JsonPrimitive) ?: JsonNull)
        }
    }.toString()

    companion object {
        fun from(p: ModelPurpose) = ModelConfigurationDraft(p.override.provider.orEmpty(), p.override.model.orEmpty(), p.override.effort.orEmpty())
    }
}

suspend fun ApiClient.modelConfiguration(): ModelConfigurationBody =
    JsonCodec.decodeFromString(raw("GET", "/api/v1/model-config"))

suspend fun ApiClient.modelPurpose(id: String): ModelPurposeBody =
    JsonCodec.decodeFromString(raw("GET", "/api/v1/model-config/purposes/$id"))

suspend fun ApiClient.saveModelPurpose(p: ModelPurpose, draft: ModelConfigurationDraft): ModelPurposeBody =
    JsonCodec.decodeFromString(raw("PATCH", "/api/v1/model-config/purposes/${p.id}", draft.patch(p)))

fun modelPurposeGroups(purposes: List<ModelPurpose>, query: String): Map<String, List<ModelPurpose>> {
    val q = query.trim()
    return purposes.filter { q.isEmpty() || listOf(it.name, it.group, it.id, it.summary()).any { value -> value.contains(q, ignoreCase = true) } }
        .groupBy { it.group.ifEmpty { "其他" } }
}
