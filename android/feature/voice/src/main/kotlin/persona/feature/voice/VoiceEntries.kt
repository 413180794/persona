package persona.feature.voice

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import persona.core.contracts.api.HubPlugin
import persona.core.contracts.api.VoicePlugins
import persona.core.navigation.Navigator
import persona.core.navigation.VoiceRoute
import kotlinx.coroutines.CancellationException

/**
 * Registers the voice page ([VoiceRoute]) on the paired hub's [plugins]. [identity] is the pairing's (its scope path):
 * the picked voices are kept with it. [onModelConfiguration] opens the model configuration of one purpose, given its
 * id (`plugin.<plugin id>`, [voicePurposeId]); without it the page offers no way there and only says why the model
 * cannot be used.
 */
fun EntryProviderScope<NavKey>.voiceEntries(
    navigator: Navigator,
    plugins: VoicePlugins,
    identity: String,
    onModelConfiguration: ((String) -> Unit)? = null,
) {
    entry<VoiceRoute> { route ->
        // A new pairing is a new page: nothing of the old one's conversation carries over.
        key(plugins, identity) { VoiceScreen(route, navigator, plugins, identity, onModelConfiguration) }
    }
}

/** The model configuration purpose of a voice plugin (docs/model-config.md): `plugin.<plugin id>`. */
fun voicePurposeId(pluginId: String): String = "plugin.$pluginId"

/**
 * The hub's running voice plugin, for an entry to the voice page: read once each time [active] turns true (and again
 * when the pairing changes, a new [plugins]); null without one, on a hub without plugins, or when it cannot be read.
 */
@Composable
fun rememberVoicePlugin(plugins: VoicePlugins?, active: Boolean): HubPlugin? {
    var plugin by remember(plugins) { mutableStateOf<HubPlugin?>(null) }
    LaunchedEffect(plugins, active) {
        if (plugins == null || !active) return@LaunchedEffect
        plugin = try {
            plugins.voicePlugin()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Exception) {
            null
        }
    }
    return plugin
}
