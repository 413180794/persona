package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** 语音对话 with the hub's voice plugin [pluginId] (docs/plugins.md「语音」); [title] is the plugin's name, if known. */
@Serializable @SerialName("jarvis.core.navigation.VoiceRoute")
data class VoiceRoute(val pluginId: String, val title: String = "") : NavKey
