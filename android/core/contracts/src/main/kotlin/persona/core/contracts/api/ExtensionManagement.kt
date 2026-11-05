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
 * 设置 › 扩展 (docs/extensions.md「hub 的接口」「App：设置 › 扩展」): the hub's extensions, each one's switch, its
 * configuration (a form drawn from its schema) and its secrets, write-only; the pages extensions draw ([ExtensionUi]); and
 * the extension markets ([ExtensionMarkets]), the only way a phone installs, upgrades or removes an extension (anything else
 * happens on the hub's machine, `personad extension`). Bound to the active pairing.
 */
interface ExtensionManagement : ExtensionUi, ExtensionMarkets {
    /** Whether the paired hub manages extensions (`extension_manager` in its features); the settings row shows only then. */
    val available: Flow<Boolean>

    /**
     * Whether the paired hub has extensions' UI slots (`extension_ui_slots`, docs/extensions.md「UI 插槽」): only then the app
     * asks for nav pages, cards and machine sections. An older hub has none.
     */
    val slotsAvailable: Flow<Boolean> get() = kotlinx.coroutines.flow.flowOf(false)

    /** Badge changes the hub pushes (`extension_ui` events) for nav pages. */
    val uiEvents: Flow<persona.core.contracts.ExtensionUiEvent> get() = kotlinx.coroutines.flow.emptyFlow()

    /** `GET /api/v1/extensions`. */
    suspend fun extensions(): List<HubExtension>

    /** `GET /api/v1/extensions` with what the device may do (`can_install`, `markets`). */
    suspend fun listing(): ExtensionListing = ExtensionListing(extensions())

    /** `GET /api/v1/extensions/{id}/-/`. */
    suspend fun extension(id: String): ExtensionDetail

    /** `PUT /api/v1/extensions/{id}/-/enabled`. */
    suspend fun setEnabled(id: String, enabled: Boolean): ExtensionDetail

    suspend fun setContribution(id: String, point: String, contribution: String, enabled: Boolean): ExtensionDetail =
        throw UnsupportedOperationException("Contribution switches unsupported by this hub")

    val changes: Flow<Unit> get() = kotlinx.coroutines.flow.emptyFlow()
    suspend fun quickPrompts(): List<ExtensionQuickPrompt> = emptyList()
    suspend fun themes(): List<ExtensionTheme> = emptyList()
    suspend fun triageTemplates(): List<ExtensionTriageTemplate> = emptyList()

    /** `PUT /api/v1/extensions/{id}/-/config`, the whole configuration; a field the hub refuses is a [ExtensionConfigRejected]. */
    suspend fun setConfig(id: String, config: JsonObject): ExtensionDetail

    /** `PUT /api/v1/extensions/{id}/-/secrets/{name}`; the value is never read back. */
    suspend fun setSecret(id: String, name: String, value: String): ExtensionDetail

    /** `DELETE /api/v1/extensions/{id}/-/secrets/{name}`. */
    suspend fun clearSecret(id: String, name: String): ExtensionDetail
}

/** The hub refused the configuration (400 `invalid_config`): [field] is its dotted path (`proxy.host`). */
class ExtensionConfigRejected(val field: String, message: String) : Exception(message)

/** One field of an extension's form (`form.fields`): the hub's reading of the manifest's JSON Schema subset. */
@Serializable
data class ExtensionField(
    val name: String = "",
    /** `string | integer | number | boolean | array | object`. */
    val type: String = "string",
    val title: String? = null,
    val description: String? = null,
    val default: JsonElement? = null,
    val required: Boolean? = null,
    val enum: List<ExtensionChoice>? = null,
    val format: String? = null,
    val pattern: String? = null,
    @SerialName("min_length") val minLength: Int? = null,
    @SerialName("max_length") val maxLength: Int? = null,
    val minimum: Double? = null,
    val maximum: Double? = null,
    /** An array's items (strings, maybe from a list of choices). */
    val items: ExtensionItems? = null,
    @SerialName("min_items") val minItems: Int? = null,
    @SerialName("max_items") val maxItems: Int? = null,
    @SerialName("unique_items") val uniqueItems: Boolean? = null,
    /** An object's fields (a group of the form). */
    val fields: List<ExtensionField>? = null,
) {
    val label: String get() = title?.takeIf { it.isNotBlank() } ?: name
    /** The default it falls back to, when there is one (JSON null is none). */
    val fallback: JsonElement? get() = default?.takeIf { it != JsonNull }
}

/** An array field's items. */
@Serializable
data class ExtensionItems(val type: String = "string", val enum: List<ExtensionChoice>? = null)

/** One value of a field's choices, with its label. */
@Serializable
data class ExtensionChoice(val value: JsonElement = JsonNull, val title: String? = null) {
    val label: String get() = title?.takeIf { it.isNotBlank() } ?: (value as? JsonPrimitive)?.contentOrNull.orEmpty()
}

/** A declared secret and whether it has a value (never the value). */
@Serializable
data class ExtensionSecret(
    val name: String = "",
    val title: String = "",
    val description: String? = null,
    val required: Boolean? = null,
    val set: Boolean = false,
) {
    val label: String get() = title.ifBlank { name }
}

/** A line of what an extension may do (`permissions`). */
@Serializable
data class ExtensionPermission(val key: String = "", val title: String = "", val detail: String? = null)

/** `GET /api/v1/extensions/{id}/-/`: the extension with its form, settings, secrets and permissions. */
data class ExtensionDetail(
    val extension: HubExtension,
    val publisher: String? = null,
    val homepage: String? = null,
    val origin: String? = null,
    val previousVersion: String? = null,
    /** hub.json's: switched and set up there only (the hub sends no config or secrets for it). */
    val readOnly: Boolean = false,
    val form: List<ExtensionField> = emptyList(),
    val config: JsonObject = JsonObject(emptyMap()),
    val secrets: List<ExtensionSecret> = emptyList(),
    val permissions: List<ExtensionPermission> = emptyList(),
    /** The pages it draws: its `extension_page` and `nav_page` UI (both whole pages, opened from its settings too). */
    val ui: List<ExtensionUiPlace> = emptyList(),
    val contributions: List<ExtensionContribution>? = null,
) {
    /** Installed from an extension market (`market:<market>/<extension>`): a phone may upgrade and remove it. */
    val fromMarket: Boolean get() = origin?.startsWith("market:") == true

    @Serializable
    private data class Extra(
        val publisher: String? = null,
        val homepage: String? = null,
        val origin: String? = null,
        @SerialName("previous_version") val previousVersion: String? = null,
        @SerialName("read_only") val readOnly: Boolean = false,
        val form: Form? = null,
        val config: JsonObject? = null,
        val secrets: List<ExtensionSecret> = emptyList(),
        val permissions: List<ExtensionPermission> = emptyList(),
        val ui: List<ExtensionUiPlace> = emptyList(),
        val contributions: List<ExtensionContribution>? = null,
    )

    @Serializable
    private data class Form(val fields: List<ExtensionField>? = null)

    companion object {
        /** The detail is the extension's list entry with more keys beside it, in one object. */
        fun decode(json: JsonElement): ExtensionDetail {
            val extension = JsonCodec.decodeFromJsonElement(HubExtension.serializer(), json)
            val extra = JsonCodec.decodeFromJsonElement(Extra.serializer(), json)
            return ExtensionDetail(
                extension, extra.publisher, extra.homepage, extra.origin, extra.previousVersion, extra.readOnly,
                extra.form?.fields.orEmpty(), extra.config ?: JsonObject(emptyMap()), extra.secrets, extra.permissions,
                extra.ui.filter { it.placement.isEmpty() || it.placement == "extension_page" || it.placement == "plugin_page" || it.placement == "nav_page" },
                extra.contributions,
            )
        }
    }
}
