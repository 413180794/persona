@file:OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)

package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.json.JsonNames
import kotlinx.serialization.Serializable

/**
 * 扩展市场 (docs/extensions.md「扩展市场」): indexes of extensions the hub has registered; a full device browses them,
 * installs, upgrades and removes what they offer (always after a preview of what the extension may do), and adds,
 * removes and refreshes the markets themselves. Tokens are write-only.
 */
interface ExtensionMarkets {
    /** `GET /api/v1/extensions/-/markets`. */
    suspend fun markets(): List<ExtensionMarket>

    /** `POST /api/v1/extensions/-/markets`: read once first; one the hub cannot read is refused with the reason. */
    suspend fun addMarket(spec: String, token: String? = null): ExtensionMarket

    /** `DELETE /api/v1/extensions/-/markets/{id}`: extensions installed from it stay. */
    suspend fun removeMarket(id: String)

    /** `POST /api/v1/extensions/-/markets/{id}/refresh`. */
    suspend fun refreshMarket(id: String): ExtensionMarket

    /** `PUT /api/v1/extensions/-/markets/{id}/token`; an empty token removes it. */
    suspend fun setMarketToken(id: String, token: String): ExtensionMarket

    /** `GET /api/v1/extensions/-/catalog?q=&market=&refresh=1`. */
    suspend fun catalog(query: String = "", market: String? = null, refresh: Boolean = false): ExtensionCatalog

    /** `POST /api/v1/extensions/-/catalog/preview`: downloads and checks it; an installed one previews its upgrade. */
    suspend fun previewFromMarket(market: String, extension: String): ExtensionPreview

    /** `POST /api/v1/extensions/-/install` with the preview and the permissions the user agreed to. */
    suspend fun install(preview: ExtensionPreview): ExtensionDetail

    /** `POST /api/v1/extensions/{id}/-/upgrade`. */
    suspend fun upgrade(id: String, preview: ExtensionPreview): ExtensionDetail

    /** `DELETE /api/v1/extensions/{id}/-/` (only those installed from a market, on a phone); its data stays unless told otherwise. */
    suspend fun uninstall(id: String, keepData: Boolean = true)

    /** `GET /api/v1/extensions/-/upgrades`: the auto-upgrade switch for all market extensions; null on a hub without it. */
    suspend fun autoUpgrades(): Boolean? = null

    /** `PUT /api/v1/extensions/-/upgrades`: turns it on or off; the hub's answer. */
    suspend fun setAutoUpgrades(auto: Boolean): Boolean =
        throw UnsupportedOperationException("Auto-upgrades unsupported by this hub")
}

/** `GET /api/v1/extensions`: the list, and what this device may do with it. */
data class ExtensionListing(
    val extensions: List<HubExtension>,
    val canInstall: Boolean = false,
    /** The hub has extension markets: 设置 › 扩展 shows 扩展市场. */
    val markets: Boolean = false,
)

/** A registered market. */
@Serializable
data class ExtensionMarket(
    val id: String = "",
    val spec: String = "",
    val name: String = "",
    val description: String? = null,
    val homepage: String? = null,
    @SerialName("has_token") val hasToken: Boolean = false,
    /** How many extensions its index offers. */
    @SerialName("extensions") @JsonNames("plugins") val extensions: Int = 0,
    val warnings: List<String>? = null,
    /** Why it could not be read the last time. */
    val error: String? = null,
    @SerialName("fetched_at") val fetchedAt: String? = null,
    @SerialName("added_at") val addedAt: String? = null,
    /** Whether its index is signed and checks out (#1848); a hub before it says nothing. */
    val signature: MarketSignature? = null,
    /** The official market every hub has (#2351), which the hub lists first; false on others and older hubs. */
    val official: Boolean = false,
) {
    val label: String get() = name.ifBlank { spec }
}

/**
 * A market's signature (docs/extensions.md「签名」): `verified` (signed with the key the hub trusts), `unsigned` (an
 * older market: still usable), or anything else (the signature fails or the key changed): the hub then neither refreshes
 * nor installs from it until its owner trusts the new key on the hub's machine. [detail] says why, for people.
 */
@Serializable
data class MarketSignature(
    val state: String = "",
    @SerialName("key_id") val keyId: String? = null,
    val detail: String? = null,
) {
    val verified: Boolean get() = state == "verified"
    val unsigned: Boolean get() = state.isEmpty() || state == "unsigned"
    val failed: Boolean get() = !verified && !unsigned
}

/** `GET /api/v1/extensions/-/catalog`. */
@Serializable
data class ExtensionCatalog(
    val markets: List<ExtensionMarket> = emptyList(),
    @SerialName("extensions") @JsonNames("plugins") val extensions: List<CatalogExtension> = emptyList(),
    /** This hub's `GOOS-GOARCH`. */
    val platform: String? = null,
)

/** An extension a market offers, with how it stands on this hub. */
@Serializable
data class CatalogExtension(
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
    val permissions: List<ExtensionPermission> = emptyList(),
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
data class ExtensionPreview(
    @SerialName("preview_id") val previewId: String = "",
    val manifest: PreviewManifest? = null,
    val permissions: List<ExtensionPermission> = emptyList(),
    @SerialName("permissions_digest") val permissionsDigest: String = "",
    val warnings: List<String>? = null,
    @SerialName("expires_at") val expiresAt: String? = null,
    val upgrade: Boolean = false,
    /** The version an upgrade replaces. */
    val from: String? = null,
    @SerialName("more_permissions") val morePermissions: Boolean = false,
    /** What an upgrade asks for beyond the installed version. */
    @SerialName("added_permissions") val addedPermissions: List<ExtensionPermission>? = null,
    val market: String? = null,
    val depends: List<ExtensionDependency> = emptyList(),
    val bundle: List<ExtensionPreview> = emptyList(),
    val unresolved: List<String> = emptyList(),
) {
    val canConfirm: Boolean get() {
        val previews = listOf(this) + bundle
        val bundled = bundle.mapNotNull { it.manifest?.id }.toSet()
        return previews.all { p -> p.unresolved.isEmpty() && p.depends.all { d ->
            d.state == "ok" || d.state in setOf("missing", "old") && d.id in bundled
        } }
    }
}

/** The few keys of the previewed manifest the app shows. */
@Serializable
data class PreviewManifest(
    val id: String = "",
    val name: String? = null,
    val version: String? = null,
    val description: String? = null,
    val publisher: String? = null,
    @SerialName("extension_pack") val extensionPack: List<ExtensionPackMember> = emptyList(),
)
