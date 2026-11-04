package persona.feature.settings

import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import persona.core.navigation.EngineRoute
import persona.core.navigation.ModelPurposeRoute
import persona.core.navigation.Navigator
import persona.core.navigation.PluginRoute

/**
 * Settings pages opened by key rather than from the settings list: a machine's engine (from its page in Agents) and
 * one AI use of 模型配置 (from that list, or from the voice page for its plugin's model), and one plugin of 插件.
 */
fun EntryProviderScope<NavKey>.settingsDetailEntries(navigator: Navigator, services: SettingsServices) {
    val demo = services.demo
    entry<EngineRoute> { route ->
        EngineSelectionScreen(route.machineId, services.sessions, services.machines.takeUnless { demo }, pairing = false,
            onDone = navigator::back, onBack = navigator::back)
    }
    entry<ModelPurposeRoute> { route ->
        ModelPurposePage(services.models.takeUnless { demo }, route.purposeId, onBack = navigator::back,
            providers = services.providers.takeUnless { demo })
    }
    entry<PluginRoute> { route ->
        PluginPage(services.plugins.takeUnless { demo }, route.pluginId, onBack = navigator::back)
    }
}
