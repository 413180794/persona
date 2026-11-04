package persona.feature.settings

import persona.core.contracts.api.*
import kotlinx.coroutines.flow.flowOf
import kotlinx.serialization.json.JsonObject

/** A plugin API whose every call fails unless a test overrides it: each fake implements only what it is about. */
internal abstract class PluginApiStub : PluginManagement {
    override val available = flowOf(true)
    private fun unused(): Nothing = throw AssertionError("not used in this test")
    override suspend fun plugins(): List<HubPlugin> = unused()
    override suspend fun plugin(id: String): PluginDetail = unused()
    override suspend fun setEnabled(id: String, enabled: Boolean): PluginDetail = unused()
    override suspend fun setConfig(id: String, config: JsonObject): PluginDetail = unused()
    override suspend fun setSecret(id: String, name: String, value: String): PluginDetail = unused()
    override suspend fun clearSecret(id: String, name: String): PluginDetail = unused()
    override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?): List<PluginUiPlace> = unused()
    override suspend fun renderUi(plugin: String, ui: String, context: PluginUiContext?, state: String?): PluginView = unused()
    override suspend fun uiAction(plugin: String, ui: String, action: String, context: PluginUiContext?, state: String?,
        values: Map<String, String>?): PluginUiResult = unused()
    override suspend fun markets(): List<PluginMarket> = unused()
    override suspend fun addMarket(spec: String, token: String?): PluginMarket = unused()
    override suspend fun removeMarket(id: String): Unit = unused()
    override suspend fun refreshMarket(id: String): PluginMarket = unused()
    override suspend fun setMarketToken(id: String, token: String): PluginMarket = unused()
    override suspend fun catalog(query: String, market: String?, refresh: Boolean): PluginCatalog = unused()
    override suspend fun previewFromMarket(market: String, plugin: String): PluginPreview = unused()
    override suspend fun install(preview: PluginPreview): PluginDetail = unused()
    override suspend fun upgrade(id: String, preview: PluginPreview): PluginDetail = unused()
    override suspend fun uninstall(id: String, keepData: Boolean): Unit = unused()
}
