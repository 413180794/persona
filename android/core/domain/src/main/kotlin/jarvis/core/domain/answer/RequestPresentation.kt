package jarvis.core.domain.answer

import jarvis.core.contracts.Interaction
import jarvis.core.contracts.Item
import kotlinx.serialization.json.*

/** Only metadata supplied by the hook is a source; tool names are not proof. */
fun Interaction.requestSourceLabel(): String? = listOfNotNull(
    agentType?.trim()?.takeIf(String::isNotEmpty)?.let { "来自子 Agent · $it" },
    mcpServerName?.trim()?.takeIf(String::isNotEmpty)?.let { server ->
        "来自 MCP 服务器 $server" + mcpConfigSource?.trim()?.takeIf(String::isNotEmpty)?.let { "（配置来源：$it）" }.orEmpty()
    },
    if (kind == "screen") "这个请求来自终端：请不要同时在电脑上处理" else null,
).takeIf { it.isNotEmpty() }?.joinToString("\n")

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
