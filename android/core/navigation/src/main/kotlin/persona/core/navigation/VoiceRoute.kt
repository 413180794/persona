package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Voice chat with the hub's voice extension [extensionId] (docs/extensions/extensions.md "Voice"); [title] is the extension's name, if known. */
@Serializable @SerialName("persona.core.navigation.VoiceRoute")
data class VoiceRoute(val extensionId: String, val title: String = "") : NavKey
