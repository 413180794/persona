package persona.feature.chat

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
internal fun approvalBlockedReason(session: Session?): String =
    if (session?.canAttach == true) "实时接入以后才能在手机上处理这个请求（会话菜单里的「实时接入」），也可以到电脑上处理。"
    else "这个会话不允许在手机上审批，请到电脑上处理。"

/** What the read-only note says after the source's name: the source's reason, or that the session can only be read. */
internal fun readOnlyNote(session: Session): String = session.source?.readOnlyReason?.let { "仅查看：$it" } ?: READ_ONLY

/**
 * In place of what would be typed, for a session whose source only lets the phone read it (a Codex history thread, a
 * runtime that did not declare sending): the source's name as a tag, then why.
 */
@Composable
internal fun ChatSourceNote(session: Session, modifier: Modifier = Modifier) {
    val source = session.source
    Row(modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.xs),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        source?.let { JTag(it.label) }
        Text(readOnlyNote(session), Modifier.weight(1f), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2,
            maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}
