package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * 新增模型供应商 (docs/launchers.md「接口」「从扩展添加」): a provider kept by the hub (gateway, key sealed there, models),
 * typed in or taken from an extension's preset. Bound to the active pairing, like 模型配置.
 */
interface ProviderSetup {
    /** `GET /api/v1/launchers/presets`: the providers the hub's extensions offer; none from a hub without them (404). */
    suspend fun presets(): List<ProviderPreset>

    /** `POST /api/v1/launchers`; the hub's refusal (a taken name, a bad address) comes back as its message. */
    suspend fun add(provider: NewProvider)
}

/** One provider an extension offers: the address, the kind and the models come from it; the user adds a name and a key. */
@Serializable
data class ProviderPreset(
    /** `<extension>/<provider>`. */
    @SerialName("id") val id: String,
    @SerialName("plugin") val extension: String = "",
    @SerialName("name") val name: String = "",
    @SerialName("description") val description: String? = null,
    /** launcher | api */
    @SerialName("kind") val kind: String = "launcher",
    @SerialName("api") val api: String? = null,
    @SerialName("base_url") val baseUrl: String = "",
    @SerialName("model") val model: String? = null,
    @SerialName("small_model") val smallModel: String? = null,
    @SerialName("models") val models: List<PresetModel> = emptyList(),
    /** What the key looks like or where to get one. */
    @SerialName("key_hint") val keyHint: String? = null,
    /** Why the extension's own list of models could not be had (the declared ones are still there). */
    @SerialName("models_error") val modelsError: String? = null,
) {
    /** The model a launcher gets when none is picked: the declared default, else the first listed. */
    val defaultModel: String get() = model?.takeIf { it.isNotBlank() } ?: models.firstOrNull()?.id.orEmpty()

    /** What can be picked: the listed models, with the default first when it is not among them. */
    val choices: List<PresetModel> get() {
        val default = defaultModel
        return if (default.isEmpty() || models.any { it.id == default }) models else listOf(PresetModel(default)) + models
    }
}

@Serializable
data class PresetModel(@SerialName("id") val id: String, @SerialName("name") val name: String? = null) {
    val label: String get() = name?.takeIf { it.isNotBlank() } ?: id
}

/**
 * `POST /api/v1/launchers`' body. From a preset only [name], [apiKey], [preset] and maybe [model] are set: the hub fills
 * in the rest. Blank fields are left out.
 */
@Serializable
data class NewProvider(
    @SerialName("name") val name: String,
    @SerialName("api_key") val apiKey: String,
    @SerialName("preset") val preset: String? = null,
    /** launcher | api; null with a preset (the preset's). */
    @SerialName("kind") val kind: String? = null,
    /** messages | responses */
    @SerialName("api") val api: String? = null,
    @SerialName("base_url") val baseUrl: String? = null,
    @SerialName("model") val model: String? = null,
    @SerialName("small_model") val smallModel: String? = null,
    @SerialName("subagent_model") val subagentModel: String? = null,
    @SerialName("args") val args: List<String>? = null,
)
