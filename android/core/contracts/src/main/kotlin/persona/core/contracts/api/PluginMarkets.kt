package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * 插件市场 (docs/plugins.md「插件市场」): indexes of plugins the hub has registered; a full device browses them,
 * installs, upgrades and removes what they offer (always after a preview of what the plugin may do), and adds,
 * removes and refreshes the markets themselves. Tokens are write-only.
 */
interface PluginMarkets {
    /** `GET /api/v1/plugins/-/markets`. */
    suspend fun markets(): List<PluginMarket>

    /** `POST /api/v1/plugins/-/markets`: read once first; one the hub cannot read is refused with the reason. */
    suspend fun addMarket(spec: String, token: String? = null): PluginMarket

    /** `DELETE /api/v1/plugins/-/markets/{id}`: plugins installed from it stay. */
    suspend fun removeMarket(id: String)

    /** `POST /api/v1/plugins/-/markets/{id}/refresh`. */
    suspend fun refreshMarket(id: String): PluginMarket

    /** `PUT /api/v1/plugins/-/markets/{id}/token`; an empty token removes it. */
    suspend fun setMarketToken(id: String, token: String): PluginMarket

    /** `GET /api/v1/plugins/-/catalog?q=&market=&refresh=1`. */
    suspend fun catalog(query: String = "", market: String? = null, refresh: Boolean = false): PluginCatalog

    /** `POST /api/v1/plugins/-/catalog/preview`: downloads and checks it; an installed one previews its upgrade. */
    suspend fun previewFromMarket(market: String, plugin: String): PluginPreview

    /** `POST /api/v1/plugins/-/install` with the preview and the permissions the user agreed to. */
    suspend fun install(preview: PluginPreview): PluginDetail

    /** `POST /api/v1/plugins/{id}/-/upgrade`. */
    suspend fun upgrade(id: String, preview: PluginPreview): PluginDetail

    /** `DELETE /api/v1/plugins/{id}/-/` (only those installed from a market, on a phone). */
    suspend fun uninstall(id: String, keepData: Boolean = false)
}

/** `GET /api/v1/plugins`: the list, and what this device may do with it. */
data class PluginListing(
    val plugins: List<HubPlugin>,
    val canInstall: Boolean = false,
    /** The hub has plugin markets: 设置 › 插件 shows 插件市场. */
    val markets: Boolean = false,
)

/** A registered market. */
@Serializable
data class PluginMarket(
    val id: String = "",
    val spec: String = "",
    val name: String = "",
    val description: String? = null,
    val homepage: String? = null,
    @SerialName("has_token") val hasToken: Boolean = false,
    /** How many plugins its index offers. */
    val plugins: Int = 0,
    val warnings: List<String>? = null,
    /** Why it could not be read the last time. */
    val error: String? = null,
    @SerialName("fetched_at") val fetchedAt: String? = null,
    @SerialName("added_at") val addedAt: String? = null,
) {
    val label: String get() = name.ifBlank { spec }
}

/** `GET /api/v1/plugins/-/catalog`. */
@Serializable
data class PluginCatalog(
    val markets: List<PluginMarket> = emptyList(),
    val plugins: List<CatalogPlugin> = emptyList(),
    /** This hub's `GOOS-GOARCH`. */
    val platform: String? = null,
)

/** A plugin a market offers, with how it stands on this hub. */
@Serializable
data class CatalogPlugin(
    val market: String = "",
    @SerialName("market_name") val marketName: String? = null,
    val id: String = "",
    val name: String = "",
    val version: String = "",
    val description: String? = null,
    val publisher: String? = null,
    val homepage: String? = null,
    val tags: List<String>? = null,
    val contributes: Map<String, List<String>>? = null,
    val permissions: List<PluginPermission> = emptyList(),
    /** Why this hub cannot install it (no package for its platform, a newer hub needed). */
    val problem: String? = null,
    /** The installed version, when it is installed (from anywhere). */
    val installed: String? = null,
    @SerialName("installed_source") val installedSource: String? = null,
    @SerialName("from_this_market") val fromThisMarket: Boolean = false,
    val upgradable: Boolean = false,
) {
    val label: String get() = name.ifBlank { id }
}

/** A downloaded, checked package waiting to be installed (10 minutes): what it is and may do. */
@Serializable
data class PluginPreview(
    @SerialName("preview_id") val previewId: String = "",
    val manifest: PreviewManifest? = null,
    val permissions: List<PluginPermission> = emptyList(),
    @SerialName("permissions_digest") val permissionsDigest: String = "",
    val warnings: List<String>? = null,
    @SerialName("expires_at") val expiresAt: String? = null,
    val upgrade: Boolean = false,
    /** The version an upgrade replaces. */
    val from: String? = null,
    @SerialName("more_permissions") val morePermissions: Boolean = false,
    /** What an upgrade asks for beyond the installed version. */
    @SerialName("added_permissions") val addedPermissions: List<PluginPermission>? = null,
    val market: String? = null,
)

/** The few keys of the previewed manifest the app shows. */
@Serializable
data class PreviewManifest(
    val id: String = "",
    val name: String? = null,
    val version: String? = null,
    val description: String? = null,
    val publisher: String? = null,
)
