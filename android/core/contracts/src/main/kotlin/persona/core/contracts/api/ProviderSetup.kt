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

    /** `GET /api/v1/launchers`: every provider, kept by Persona or found in a machine's ~/.zshrc (hidden ones too). */
    suspend fun launchers(): List<HubLauncher> = throw UnsupportedOperationException()

    /** `GET /api/v1/launchers/{name}`: one, a ~/.zshrc one with its function on each machine (secrets masked). */
    suspend fun launcher(name: String): HubLauncher = throw UnsupportedOperationException()

    /**
     * `DELETE /api/v1/launchers/{name}`: a Persona one is deleted with its key (the hub refuses one a use runs on); a
     * ~/.zshrc one is only hidden in Persona, the file is not touched.
     */
    suspend fun remove(name: String): Unit = throw UnsupportedOperationException()

    /** `POST /api/v1/launchers/{name}/restore`: a hidden ~/.zshrc one is listed again. */
    suspend fun restore(name: String): Unit = throw UnsupportedOperationException()

    /** `POST /api/v1/launchers/{name}/check?machine=`: one real request through it ([machine] null: the assistant's). */
    suspend fun check(name: String, machine: String?): LauncherCheckAnswer = throw UnsupportedOperationException()

    /** `GET /api/v1/launchers/import?machine=`: what the machine's claude aliases come to, merged by gateway and key. */
    suspend fun importPreview(machine: String): List<ImportedProvider> = throw UnsupportedOperationException()

    /** `POST /api/v1/launchers/import`: the ticked [providers]; the hub reads the keys on the machine again itself. */
    suspend fun import(machine: String, providers: List<String>): LauncherImportResult = throw UnsupportedOperationException()
}

/** One provider of 设置 › 模型供应商 (the hub's `LauncherView`): never its key or headers, only whether it has them. */
@Serializable
data class HubLauncher(
    @SerialName("name") val name: String,
    /** jarvis (kept by Persona) | zshrc (a machine's ~/.zshrc function) */
    @SerialName("source") val source: String = "zshrc",
    /** launcher | api */
    @SerialName("kind") val kind: String? = null,
    @SerialName("api") val api: String? = null,
    @SerialName("base_url") val baseUrl: String? = null,
    @SerialName("model") val model: String? = null,
    @SerialName("small_model") val smallModel: String? = null,
    @SerialName("subagent_model") val subagentModel: String? = null,
    @SerialName("custom_model") val customModel: String? = null,
    @SerialName("args") val args: List<String> = emptyList(),
    @SerialName("has_key") val hasKey: Boolean = false,
    @SerialName("has_headers") val hasHeaders: Boolean = false,
    /** api_key (x-api-key) | bearer */
    @SerialName("auth") val auth: String? = null,
    /** ~/.zshrc ones: the machines whose ~/.zshrc defines it. */
    @SerialName("machines") val machines: List<String> = emptyList(),
    @SerialName("hidden") val hidden: Boolean = false,
    @SerialName("models") val models: List<PresetModel> = emptyList(),
    /** "" made by hand, "zshrc:<alias>@<machine>", "preset:<id>". */
    @SerialName("origin") val origin: String? = null,
    /** The detail's: each machine's function body, keys masked. */
    @SerialName("definitions") val definitions: Map<String, String> = emptyMap(),
) {
    val managed: Boolean get() = source == "jarvis"
    val isApi: Boolean get() = kind == "api"
}

/** What 测试可用性 answered: [machine] it ran on (none for a direct API), and the result. */
data class LauncherCheckAnswer(val machine: String?, val check: LauncherCheck)

@Serializable
data class LauncherCheck(
    @SerialName("ok") val ok: Boolean = false,
    @SerialName("agent") val agent: String? = null,
    @SerialName("model") val model: String? = null,
    @SerialName("answer") val answer: String? = null,
    @SerialName("millis") val millis: Int = 0,
    @SerialName("error") val error: String? = null,
)

/** One provider 从 ~/.zshrc 导入 found: merged from [aliases]; [existing] is the stored one it would add models to. */
@Serializable
data class ImportedProvider(
    @SerialName("name") val name: String,
    @SerialName("base_url") val baseUrl: String = "",
    @SerialName("model") val model: String = "",
    @SerialName("has_key") val hasKey: Boolean = false,
    @SerialName("aliases") val aliases: List<String> = emptyList(),
    @SerialName("existing") val existing: String? = null,
    @SerialName("models") val models: List<PresetModel> = emptyList(),
)

@Serializable
data class LauncherImportResult(
    @SerialName("created") val created: List<String> = emptyList(),
    @SerialName("updated") val updated: List<String> = emptyList(),
)

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
