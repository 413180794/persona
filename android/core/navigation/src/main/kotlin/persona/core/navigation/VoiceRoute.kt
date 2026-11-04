package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** 语音对话 with the hub's voice extension [extensionId] (docs/extensions.md「语音」); [title] is the extension's name, if known. */
@Serializable @SerialName("jarvis.core.navigation.VoiceRoute")
data class VoiceRoute(val extensionId: String, val title: String = "") : NavKey
