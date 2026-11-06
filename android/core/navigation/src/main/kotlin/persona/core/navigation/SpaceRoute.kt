package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * One herdr space's page (its sessions in tab order). [spaceId] is the Agents list's space id, `<machine>/<space>`;
 * a wide window shows the page beside the list, a phone pushes it. [title] is the space's name when it was opened, for
 * the page's first frame (before the sessions are read).
 */
@Serializable @SerialName("persona.core.navigation.SpaceRoute")
data class SpaceRoute(val spaceId: String, val title: String = "") : NavKey
