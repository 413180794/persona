package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * One model use of 设置 › 模型配置 (docs/model-config.md), by the hub's purpose id: `agent.new`, `task.<id>`,
 * `plugin.<id>` (a voice plugin's model, which the voice page also opens).
 */
@Serializable @SerialName("jarvis.core.navigation.ModelPurposeRoute") data class ModelPurposeRoute(val purposeId: String) : NavKey

/** One plugin of 设置 › 插件 (docs/plugins.md「App：设置 › 插件」), by the hub's plugin id. */
@Serializable @SerialName("jarvis.core.navigation.PluginRoute") data class PluginRoute(val pluginId: String) : NavKey

/** A page a plugin draws (its `plugin_page` UI, docs/plugins.md「声明式 UI」), opened from the plugin's page. */
@Serializable @SerialName("persona.core.navigation.PluginUiRoute")
data class PluginUiRoute(val pluginId: String, val uiId: String, val title: String) : NavKey

/** 设置 › 插件 › 插件市场: what the hub's plugin markets offer (docs/plugins.md「插件市场」). */
@Serializable @SerialName("persona.core.navigation.PluginMarketRoute") data object PluginMarketRoute : NavKey

/** One plugin of a market's catalog: what it may do, and installing, upgrading or removing it. */
@Serializable @SerialName("persona.core.navigation.PluginCatalogRoute")
data class PluginCatalogRoute(val marketId: String, val pluginId: String) : NavKey

/** 插件市场 › 管理市场: the markets the hub reads, adding, removing, refreshing them and their tokens. */
@Serializable @SerialName("persona.core.navigation.PluginMarketsRoute") data object PluginMarketsRoute : NavKey

/** One launcher of a machine: what it runs, and the sections plugins draw for its gateway (`launcher_section`). */
@Serializable @SerialName("persona.core.navigation.LauncherRoute")
data class LauncherRoute(val machineId: String, val name: String) : NavKey
