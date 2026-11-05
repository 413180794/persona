package persona.feature.chat

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import persona.core.contracts.ListeningPort
import persona.core.designsystem.PathChip
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PhIcons

/** A port's chip text: `:5173` and the page's title when its `/` gave one (iOS `PortBadgeRow`). */
internal fun portChipLabel(port: ListeningPort): String =
    ":${port.port}" + (port.title?.trim()?.takeIf(String::isNotEmpty)?.let { " $it" } ?: "")

/** The session's ports, one each, in the order the hub lists them. */
internal fun chipPorts(ports: List<ListeningPort>?): List<ListeningPort> = ports.orEmpty().distinctBy { it.port }

/**
 * The ports an agent listens on (`Session.ports`, docs/ports.md), under the conversation's header (iOS
 * `SessionPortChips`): a tap opens the page through the hub, by way of the link host, as a link to it would.
 * Nothing when the session lists none or the conversation cannot open ports.
 */
@Composable
internal fun ChatPortChips(ports: List<ListeningPort>?) {
    val shown = chipPorts(ports)
    if (shown.isEmpty() || LocalChatPorts.current == null) return
    val links = LocalUriHandler.current
    LazyRow(
        Modifier.fillMaxWidth().padding(vertical = Tokens.Space.xs),
        contentPadding = PaddingValues(horizontal = Tokens.Space.l), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s),
    ) {
        items(shown, key = { it.port }) { port ->
            PathChip(portChipLabel(port), PhIcons.GlobeSimple, onClick = { links.openUri("http://localhost:${port.port}/") },
                modifier = Modifier.semantics { contentDescription = "端口 ${port.port}" + (port.title?.let { "，$it" } ?: "") },
                onClickLabel = "在 App 里打开这个页面")
        }
    }
}
