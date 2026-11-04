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
