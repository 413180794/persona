package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * One model use of 设置 › 模型配置 (docs/model-config.md), by the hub's purpose id: `agent.new`, `task.<id>`,
 * `plugin.<id>` (a voice extension's model, which the voice page also opens).
 */
@Serializable @SerialName("jarvis.core.navigation.ModelPurposeRoute") data class ModelPurposeRoute(val purposeId: String) : NavKey

/** One extension of 设置 › 扩展 (docs/extensions.md「App：设置 › 扩展」), by the hub's extension id. */
@Serializable @SerialName("jarvis.core.navigation.PluginRoute") data class ExtensionRoute(val extensionId: String) : NavKey

/**
 * A page an extension draws (its `extension_page` UI, docs/extensions.md「声明式 UI」), opened from the extension's page.
 * A page the extension pushed on another (UI version 3) is first drawn with its [state], [depth] pages up.
 */
@Serializable @SerialName("persona.core.navigation.PluginUiRoute")
data class ExtensionUiRoute(val extensionId: String, val uiId: String, val title: String, val state: String? = null, val depth: Int = 0) : NavKey

/** 设置 › 扩展 › 扩展市场: what the hub's extension markets offer (docs/extensions.md「扩展市场」). */
@Serializable @SerialName("persona.core.navigation.PluginMarketRoute") data object ExtensionMarketRoute : NavKey

/** One extension of a market's catalog: what it may do, and installing, upgrading or removing it. */
@Serializable @SerialName("persona.core.navigation.PluginCatalogRoute")
data class ExtensionCatalogRoute(val marketId: String, val extensionId: String) : NavKey

/** 扩展市场 › 管理市场: the markets the hub reads, adding, removing, refreshing them and their tokens. */
@Serializable @SerialName("persona.core.navigation.PluginMarketsRoute") data object ExtensionMarketsRoute : NavKey

/** One launcher of a machine: what it runs, and the sections extensions draw for its gateway (`launcher_section`). */
@Serializable @SerialName("persona.core.navigation.LauncherRoute")
data class LauncherRoute(val machineId: String, val name: String) : NavKey

/** One channel of 设置 › 渠道 (docs/channels.md「界面」), by the hub's channel id. */
@Serializable @SerialName("persona.core.navigation.ChannelRoute") data class ChannelRoute(val channelId: String) : NavKey

/** One connector of 设置 › 连接器 (its sign-in, switches and inbox settings), by the hub's connector id; an extension's page opens it. */
@Serializable @SerialName("persona.core.navigation.ConnectorRoute") data class ConnectorRoute(val connectorId: String) : NavKey
