package persona.core.designsystem

import persona.core.contracts.Session

/**
 * The mark of a session's agent (#6390), the same reading as iOS `AgentKind` and the web's `sessionAgent`: a Codex
 * thread from an app-server or history is Codex whatever it reports; "" (an older hub) is Claude Code; an agent with
 * no mark of its own is named by [Session.agentLabel] and drawn as its letter. Persona's own conversations get none.
 */
val Session.agentMark: AgentMarkSpec?
    get() {
        if (assistant) return null
        val surface = when (agentSurface) {
            "desktop" -> AgentSurface.Desktop
            "cli" -> AgentSurface.Cli
            else -> null
        }
        return when {
            isCodex -> AgentMarkSpec(AgentMarkKind.Codex, "Codex", surface)
            agent?.lowercase() in TRAE -> AgentMarkSpec(AgentMarkKind.Trae, "TRAE", surface)
            isClaude -> AgentMarkSpec(AgentMarkKind.Claude, "Claude Code", surface)
            else -> AgentMarkSpec(null, agentLabel, surface)
        }
    }

private val TRAE = setOf("trae", "trae-cli", "traecli")
