package persona.feature.chat

import persona.core.designsystem.resolve
import persona.core.designsystem.text
import androidx.compose.ui.platform.LocalResources
import android.content.res.Resources
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.Session
import persona.core.designsystem.JTag
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.designsystem.pagePadding
import persona.core.domain.sort.READ_ONLY
import persona.core.domain.sort.canAttach
import persona.core.domain.sort.source

/** Why a request in this session cannot be answered from the phone: a Codex thread not joined yet, or a read-only source. */
internal fun approvalBlockedReason(res: Resources, session: Session?): String =
    res.getString(if (session?.canAttach == true) R.string.chat_source_attach_to_answer else R.string.chat_source_no_phone_approval)

/** What the read-only note says after the source's name: the source's reason, or that the session can only be read. */
internal fun readOnlyNote(res: Resources, session: Session): String =
    session.source?.readOnlyReason?.let { res.getString(R.string.chat_source_read_only_reason, it.resolve(res)) } ?: READ_ONLY.resolve(res)

/**
 * In place of what would be typed, for a session whose source only lets the phone read it (a Codex history thread, a
 * runtime that did not declare sending): the source's name as a tag, then why.
 */
@Composable
internal fun ChatSourceNote(session: Session, modifier: Modifier = Modifier) {
    val source = session.source
    Row(modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.xs),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        source?.let { JTag(it.label.text()) }
        Text(readOnlyNote(LocalResources.current, session), Modifier.weight(1f), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2,
            maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}
