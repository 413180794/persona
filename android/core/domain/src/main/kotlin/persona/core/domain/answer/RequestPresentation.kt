package persona.core.domain.answer

import persona.core.contracts.Interaction
import persona.core.contracts.Item
import persona.core.domain.mcpServerLabel
import kotlinx.serialization.json.*

/** Only metadata supplied by the hook is a source; tool names are not proof. */
fun Interaction.requestSourceLabel(): String? = listOfNotNull(
    agentType?.trim()?.takeIf(String::isNotEmpty)?.let { "来自子 Agent · $it" },
    mcpServerName?.trim()?.takeIf(String::isNotEmpty)?.let { server ->
        "来自 MCP 服务器 ${mcpServerLabel(server)}" + mcpConfigSource?.trim()?.takeIf(String::isNotEmpty)?.let { "（配置来源：$it）" }.orEmpty()
    },
    if (kind == "screen") "这个请求来自终端：请不要同时在电脑上处理" else null,
).takeIf { it.isNotEmpty() }?.joinToString("\n")

/**
 * "Always allow" for this card: Claude's offers it (本会话总是允许); another agent's card (Codex, TRAEX, a runtime's)
 * only when its TUI names what it would allow ([Interaction.alwaysLabel]), as on iOS.
 */
val Interaction.offersAlways: Boolean get() = agentDisplayName == null || !alwaysLabel.isNullOrBlank()

/** The button for it: Claude's 本会话总是允许, another agent's 总是同意 (what it allows is shown beside it). */
val Interaction.alwaysActionLabel: String get() = if (agentDisplayName == null) "本会话总是允许" else "总是同意"

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
