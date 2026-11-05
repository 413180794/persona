package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class ExtensionContribution(
    val point: String = "",
    val id: String = "",
    val title: String? = null,
    val enabled: Boolean = false,
    @SerialName("can_disable") val canDisable: Boolean = false,
    val via: String? = null,
)

@Serializable
data class ExtensionDependency(
    val id: String = "",
    val version: String? = null,
    val state: String = "",
    val installed: String? = null,
    val name: String? = null,
)

@Serializable
data class ExtensionPackMember(val id: String = "", val market: String? = null, val version: String? = null)

@Serializable
data class ExtensionSandbox(
    val enabled: Boolean = false,
    val default: Boolean = false,
    val chosen: Boolean = false,
    val active: Boolean = false,
    val why: String? = null,
    val trusted: Boolean? = null,
    val egress: ExtensionEgress? = null,
)

@Serializable
data class ExtensionEgress(
    val allowed: Long = 0,
    val denied: Long = 0,
    @SerialName("last_denied") val lastDenied: String? = null,
    @SerialName("denied_at") val deniedAt: String? = null,
)

@Serializable
data class ExtensionQuickPrompt(
    val extension: String = "",
    @SerialName("extension_name") val extensionName: String = "",
    val id: String = "",
    val title: String = "",
    val text: String = "",
)

@Serializable
data class ExtensionTriageTemplate(
    val extension: String = "",
    @SerialName("extension_name") val extensionName: String = "",
    val id: String = "",
    val title: String = "",
    val text: String = "",
)

@Serializable
data class ExtensionTheme(
    val extension: String = "",
    @SerialName("extension_name") val extensionName: String = "",
    val id: String = "",
    val name: String = "",
    val light: Map<String, String> = emptyMap(),
    val dark: Map<String, String> = emptyMap(),
    val problems: List<String> = emptyList(),
) {
    val key: String get() = "$extension/$id"
}
