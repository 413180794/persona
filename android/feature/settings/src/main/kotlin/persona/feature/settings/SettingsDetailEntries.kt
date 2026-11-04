package persona.feature.settings

import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import persona.core.navigation.ChannelRoute
import persona.core.navigation.EngineRoute
import persona.core.navigation.ModelPurposeRoute
import persona.core.navigation.Navigator
import persona.core.navigation.LauncherRoute
import persona.core.navigation.PluginCatalogRoute
import persona.core.navigation.PluginMarketRoute
import persona.core.navigation.PluginMarketsRoute
import persona.core.navigation.PluginRoute
import persona.core.navigation.PluginUiRoute

/**
 * Settings pages opened by key rather than from the settings list: a machine's engine (from its page in Agents) and
 * one AI use of 模型配置 (from that list, or from the voice page for its plugin's model), one plugin of 插件 and the
 * pages it draws, 插件市场 with its plugins and markets, a launcher of a machine (from the machine's page), and one channel
 * of 渠道.
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
        PluginPage(services.plugins.takeUnless { demo }, route.pluginId, onBack = navigator::back,
            onPage = { page -> navigator.navigate(PluginUiRoute(page.plugin.ifBlank { route.pluginId }, page.id, page.title.ifBlank { page.id })) },
            onChannel = { navigator.navigate(ChannelRoute(it)) })
    }
    entry<PluginUiRoute> { route ->
        PluginUiPage(services.plugins.takeUnless { demo }, route.pluginId, route.uiId, route.title, onBack = navigator::back)
    }
    entry<PluginMarketRoute> {
        PluginMarketPage(services.plugins.takeUnless { demo }, onBack = navigator::back,
            onPlugin = { navigator.navigate(PluginCatalogRoute(it.market, it.id)) }, onMarkets = { navigator.navigate(PluginMarketsRoute) })
    }
    entry<PluginCatalogRoute> { route ->
        PluginCatalogPage(services.plugins.takeUnless { demo }, route.marketId, route.pluginId, onBack = navigator::back,
            onInstalled = { navigator.navigate(PluginRoute(it)) })
    }
    entry<PluginMarketsRoute> { PluginMarketsPage(services.plugins.takeUnless { demo }, onBack = navigator::back) }
    entry<ChannelRoute> { route ->
        ChannelPage(services.channels.takeUnless { demo }, route.channelId, onBack = navigator::back, onOpen = navigator::navigate)
    }
    entry<LauncherRoute> { route ->
        LauncherPage(services.machines.takeUnless { demo }, services.plugins.takeUnless { demo }, route.machineId, route.name, onBack = navigator::back)
    }
}
