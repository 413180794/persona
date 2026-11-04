package persona.core.contracts.api

import persona.core.contracts.JsonCodec
import kotlinx.coroutines.flow.Flow
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

/**
 * 设置 › 插件 (docs/plugins.md「hub 的接口」「App：设置 › 插件」): the hub's plugins, each one's switch, its
 * configuration (a form drawn from its schema) and its secrets, write-only. Installing, upgrading and removing happen
 * on the hub's machine (`personad plugin`), never here. Bound to the active pairing.
 */
interface PluginManagement {
    /** Whether the paired hub manages plugins (`plugin_manager` in its features); the settings row shows only then. */
    val available: Flow<Boolean>

    /** `GET /api/v1/plugins`. */
    suspend fun plugins(): List<HubPlugin>

    /** `GET /api/v1/plugins/{id}/-/`. */
    suspend fun plugin(id: String): PluginDetail

    /** `PUT /api/v1/plugins/{id}/-/enabled`. */
    suspend fun setEnabled(id: String, enabled: Boolean): PluginDetail

    /** `PUT /api/v1/plugins/{id}/-/config`, the whole configuration; a field the hub refuses is a [PluginConfigRejected]. */
    suspend fun setConfig(id: String, config: JsonObject): PluginDetail

    /** `PUT /api/v1/plugins/{id}/-/secrets/{name}`; the value is never read back. */
    suspend fun setSecret(id: String, name: String, value: String): PluginDetail

    /** `DELETE /api/v1/plugins/{id}/-/secrets/{name}`. */
    suspend fun clearSecret(id: String, name: String): PluginDetail
}

/** The hub refused the configuration (400 `invalid_config`): [field] is its dotted path (`proxy.host`). */
class PluginConfigRejected(val field: String, message: String) : Exception(message)

/** One field of a plugin's form (`form.fields`): the hub's reading of the manifest's JSON Schema subset. */
@Serializable
data class PluginField(
    val name: String = "",
    /** `string | integer | number | boolean | array | object`. */
    val type: String = "string",
    val title: String? = null,
    val description: String? = null,
    val default: JsonElement? = null,
    val required: Boolean? = null,
    val enum: List<PluginChoice>? = null,
    val format: String? = null,
    val pattern: String? = null,
    @SerialName("min_length") val minLength: Int? = null,
    @SerialName("max_length") val maxLength: Int? = null,
    val minimum: Double? = null,
    val maximum: Double? = null,
    /** An array's items (strings, maybe from a list of choices). */
    val items: PluginItems? = null,
    @SerialName("min_items") val minItems: Int? = null,
    @SerialName("max_items") val maxItems: Int? = null,
    @SerialName("unique_items") val uniqueItems: Boolean? = null,
    /** An object's fields (a group of the form). */
    val fields: List<PluginField>? = null,
) {
    val label: String get() = title?.takeIf { it.isNotBlank() } ?: name
    /** The default it falls back to, when there is one (JSON null is none). */
    val fallback: JsonElement? get() = default?.takeIf { it != JsonNull }
}

/** An array field's items. */
@Serializable
data class PluginItems(val type: String = "string", val enum: List<PluginChoice>? = null)

/** One value of a field's choices, with its label. */
@Serializable
data class PluginChoice(val value: JsonElement = JsonNull, val title: String? = null) {
    val label: String get() = title?.takeIf { it.isNotBlank() } ?: (value as? JsonPrimitive)?.contentOrNull.orEmpty()
}

/** A declared secret and whether it has a value (never the value). */
@Serializable
data class PluginSecret(
    val name: String = "",
    val title: String = "",
    val description: String? = null,
    val required: Boolean? = null,
    val set: Boolean = false,
) {
    val label: String get() = title.ifBlank { name }
}

/** A line of what a plugin may do (`permissions`). */
@Serializable
data class PluginPermission(val key: String = "", val title: String = "", val detail: String? = null)

/** `GET /api/v1/plugins/{id}/-/`: the plugin with its form, settings, secrets and permissions. */
data class PluginDetail(
    val plugin: HubPlugin,
    val publisher: String? = null,
    val homepage: String? = null,
    val origin: String? = null,
    val previousVersion: String? = null,
    /** hub.json's: switched and set up there only (the hub sends no config or secrets for it). */
    val readOnly: Boolean = false,
    val form: List<PluginField> = emptyList(),
    val config: JsonObject = JsonObject(emptyMap()),
    val secrets: List<PluginSecret> = emptyList(),
    val permissions: List<PluginPermission> = emptyList(),
) {
    @Serializable
    private data class Extra(
        val publisher: String? = null,
        val homepage: String? = null,
        val origin: String? = null,
        @SerialName("previous_version") val previousVersion: String? = null,
        @SerialName("read_only") val readOnly: Boolean = false,
        val form: Form? = null,
        val config: JsonObject? = null,
        val secrets: List<PluginSecret> = emptyList(),
        val permissions: List<PluginPermission> = emptyList(),
    )

    @Serializable
    private data class Form(val fields: List<PluginField>? = null)

    companion object {
        /** The detail is the plugin's list entry with more keys beside it, in one object. */
        fun decode(json: JsonElement): PluginDetail {
            val plugin = JsonCodec.decodeFromJsonElement(HubPlugin.serializer(), json)
            val extra = JsonCodec.decodeFromJsonElement(Extra.serializer(), json)
            return PluginDetail(
                plugin, extra.publisher, extra.homepage, extra.origin, extra.previousVersion, extra.readOnly,
                extra.form?.fields.orEmpty(), extra.config ?: JsonObject(emptyMap()), extra.secrets, extra.permissions,
            )
        }
    }
}
