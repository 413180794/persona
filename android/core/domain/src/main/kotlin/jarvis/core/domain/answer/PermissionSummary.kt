package jarvis.core.domain.answer

import jarvis.core.contracts.Interaction
import jarvis.core.contracts.Item
import jarvis.core.domain.toolSubject
import jarvis.core.domain.toolVerb
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

data class PermissionSummary(val text: String, val description: String? = null, val monospace: Boolean = false)

/** Keep hub summaries intact and prefer useful tool fields over raw input. */
fun Interaction.permissionSummary(): PermissionSummary {
    summary?.takeIf(String::isNotBlank)?.let { return PermissionSummary(it) }
    val input = toolInput as? JsonObject
    fun field(key: String) = (input?.get(key) as? JsonPrimitive)?.takeIf { it.isString }
        ?.content?.takeIf(String::isNotBlank)
    val description = field("description")
    if (toolName == "Bash") {
        field("command")?.let { return PermissionSummary(it, description, monospace = true) }
        if (description != null) return PermissionSummary(description)
    }
    val subject = toolSubject(Item(id = id, toolName = toolName, toolInput = toolInput)).takeIf(String::isNotBlank)
    if (subject != null) return PermissionSummary("${toolVerb(toolName)} $subject")
    if (description != null) return PermissionSummary(description)
    val raw = toolInput?.toString().orEmpty()
    return PermissionSummary(if (raw.isBlank()) toolVerb(toolName) else raw.take(240) + if (raw.length > 240) "…" else "")
}
