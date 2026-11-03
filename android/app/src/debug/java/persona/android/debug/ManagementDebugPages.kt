package persona.android.debug

import persona.core.navigation.*

/**
 * Captures of pages that take an id (debug builds only): `machine:<id>`, `engine:<id>`, `purpose:<id>`, and
 * `new-agent` or `new-agent:<machine>:<folder>` (a prefilled form, as the knowledge base opens it).
 */
internal fun managementPage(name: String): ResolvedRoute? {
    val parts = name.split(':', limit = 3)
    val argument = parts.getOrNull(1)?.takeIf { it.isNotBlank() }
    return when (parts[0]) {
        "machine" -> argument?.let { ResolvedRoute(AppTab.Agents, MachineDetailRoute(it)) }
        "engine" -> argument?.let { ResolvedRoute(AppTab.Agents, EngineRoute(it)) }
        "purpose" -> argument?.let { ResolvedRoute(AppTab.Settings, ModelPurposeRoute(it)) }
        "new-agent" -> ResolvedRoute(AppTab.Jarvis, NewAgentRoute(argument, parts.getOrNull(2)?.takeIf { it.isNotBlank() },
            prompt = if (argument == null) null else "这是知识库里这个项目的档案（示例）。\n\n"))
        else -> null
    }
}
