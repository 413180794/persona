package persona.feature.chat

import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import persona.core.contracts.Session
import persona.core.designsystem.InlineTone
import persona.core.designsystem.JInlineStatus
import persona.core.designsystem.Tokens

internal data class StartingNote(val text: String, val failed: Boolean)

/**
 * A session Persona is still starting (docs/protocol.md「新建会话」): what is sent now goes in once the agent is ready.
 * Or why it did not start; sends then fail with the same reason and can be sent again.
 */
internal fun startingNote(session: Session?): StartingNote? = when {
    session == null -> null
    !session.startError.isNullOrBlank() -> StartingNote("没有启动起来：${session.startError?.trim()}", failed = true)
    session.starting -> StartingNote("正在启动 ${session.agentLabel}，消息会在它就绪后发出", failed = false)
    else -> null
}

/** Above the composer while the session starts, or when starting it failed. */
@Composable
internal fun ChatStartingNote(session: Session?, modifier: Modifier = Modifier) {
    val note = startingNote(session) ?: return
    JInlineStatus(note.text, modifier.padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.xs),
        tone = if (note.failed) InlineTone.Failed else InlineTone.Busy)
}
