package persona.feature.chat

import androidx.compose.ui.platform.LocalResources
import android.content.res.Resources
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import persona.core.contracts.Session
import persona.core.designsystem.InlineTone
import persona.core.designsystem.JInlineStatus
import persona.core.designsystem.Tokens

internal data class StartingNote(val text: String, val failed: Boolean)

/**
 * A session Persona is still starting (docs/protocol.md "New session"): what is sent now goes in once the agent is ready.
 * Or why it did not start; sends then fail with the same reason and can be sent again.
 */
internal fun startingNote(res: Resources, session: Session?): StartingNote? = when {
    session == null -> null
    !session.startError.isNullOrBlank() -> StartingNote(res.getString(R.string.chat_starting_failed, session.startError.orEmpty().trim()), failed = true)
    session.starting -> StartingNote(res.getString(R.string.chat_starting, session.agentLabel), failed = false)
    else -> null
}

/** Above the composer while the session starts, or when starting it failed. */
@Composable
internal fun ChatStartingNote(session: Session?, modifier: Modifier = Modifier) {
    val note = startingNote(LocalResources.current, session) ?: return
    JInlineStatus(note.text, modifier.padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.xs),
        tone = if (note.failed) InlineTone.Failed else InlineTone.Busy)
}
