package persona.core.domain.answer

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.Interaction
import persona.core.contracts.Item
import persona.core.contracts.mcpServerLabel
import kotlinx.serialization.json.*

/** Only metadata supplied by the hook is a source; tool names are not proof. */
fun Interaction.requestSourceLabel(): CoreText? = listOfNotNull(
    agentType?.trim()?.takeIf(String::isNotEmpty)?.let { CoreText.of(Key.RequestFromSubagent, it) },
    mcpServerName?.trim()?.takeIf(String::isNotEmpty)?.let { server ->
        val name = mcpServerLabel(server)
        mcpConfigSource?.trim()?.takeIf(String::isNotEmpty)?.let { CoreText.of(Key.RequestFromMcpConfigured, name, it) }
            ?: CoreText.of(Key.RequestFromMcp, name)
    },
    if (kind == "screen") CoreText.of(Key.RequestFromTerminal) else null,
).takeIf { it.isNotEmpty() }?.let { CoreText.joined("\n", it) }

/**
 * "Always allow" for this card: Claude's offers it ("Always allow this session"); another agent's card (Codex, a compatible CLI, a runtime's)
 * only when its TUI names what it would allow ([Interaction.alwaysLabel]), as on iOS.
 */
val Interaction.offersAlways: Boolean get() = agentDisplayName == null || !alwaysLabel.isNullOrBlank()

/**
 * A permission card that needs its fields filled in (a password) before it can be allowed. This app has no form for
 * them yet, so allow is not offered anywhere (as web's quick answers); deny still is (#4987).
 */
val Interaction.needsFieldValues: Boolean get() = kind == "permission" && fields.orEmpty().any { !it.optional }

/** The button for it: Claude's "Always allow this session", another agent's "Always allow" (what it allows is shown beside it). */
val Interaction.alwaysActionLabel: CoreText
    get() = CoreText.of(if (agentDisplayName == null) Key.AlwaysAllowSession else Key.AlwaysAgree)

data class RequestCommand(val requestId: String, val toolUseId: String, val command: String)

/** Match the exact tool use ID within this request's session, never a nearby command. */
fun Interaction.requestCommand(items: List<Item> = emptyList()): RequestCommand? {
    if (kind != "screen" || secret == true) return null
    val useId = toolUseId?.takeIf(String::isNotBlank) ?: return null
    val input = (toolInput as? JsonObject) ?: items.firstOrNull {
        it.kind == "tool_use" && it.toolUseId == useId
    }?.toolInput as? JsonObject ?: return null
    val command = (input["command"] as? JsonPrimitive)?.takeIf { it.isString }?.content?.takeIf(String::isNotBlank) ?: return null
    return RequestCommand(id, useId, command)
}
