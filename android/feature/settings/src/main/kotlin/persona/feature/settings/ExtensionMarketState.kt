package persona.feature.settings

import persona.core.contracts.api.CatalogExtension
import persona.core.contracts.api.ExtensionCatalog
import persona.core.contracts.api.ExtensionDetail
import persona.core.contracts.api.ExtensionMarket
import persona.core.contracts.api.ExtensionMarkets
import persona.core.contracts.api.ExtensionPreview
import persona.core.designsystem.failureReason
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

// 设置 › 扩展 › 扩展市场 (docs/extensions.md「扩展市场」): the words, the catalog, one extension of it (preview, then install,
// upgrade or remove) and the markets. The pages are in ExtensionMarketScreen.kt.

/** Where a catalog extension stands on this hub. */
internal enum class CatalogStanding { Available, Installed, Upgradable, InstalledElsewhere, Unavailable }

/** What the page may do with a catalog extension. */
internal enum class CatalogAction { Install, Upgrade, Uninstall }

internal object CatalogText {
    fun standing(p: CatalogExtension): CatalogStanding = when {
        p.upgradable && p.problem.isNullOrBlank() -> CatalogStanding.Upgradable
        !p.installed.isNullOrBlank() && (p.fromThisMarket || p.installedSource == "installed") -> CatalogStanding.Installed
        !p.installed.isNullOrBlank() -> CatalogStanding.InstalledElsewhere
        !p.problem.isNullOrBlank() -> CatalogStanding.Unavailable
        else -> CatalogStanding.Available
    }

    /** The short word beside an extension in the catalog. */
    fun badge(p: CatalogExtension): String? = when (standing(p)) {
        CatalogStanding.Upgradable -> "可升级"
        CatalogStanding.Installed, CatalogStanding.InstalledElsewhere -> "已安装"
        CatalogStanding.Unavailable -> "装不了"
        CatalogStanding.Available -> null
    }

    /** The line under it: the version (and the installed one), or why it cannot be installed. */
    fun line(p: CatalogExtension): String = when (standing(p)) {
        CatalogStanding.Upgradable -> "${p.installed} → ${p.version}"
        CatalogStanding.Installed -> "${p.version} · 已安装"
        CatalogStanding.InstalledElsewhere -> "${p.version} · 已装 ${p.installed}（${ExtensionText.source(p.installedSource) ?: "别处"}）"
        CatalogStanding.Unavailable -> p.problem.orEmpty()
        CatalogStanding.Available -> p.version
    }

    /**
     * Install what is not installed and can be; upgrade what is older here (a built-in or hub.json extension the hub
     * marks not upgradable); remove only what came from this market (the hub refuses the rest from a phone).
     */
    fun actions(p: CatalogExtension): List<CatalogAction> = buildList {
        val usable = p.problem.isNullOrBlank()
        if (usable && p.installed.isNullOrBlank()) add(CatalogAction.Install)
        if (usable && p.upgradable) add(CatalogAction.Upgrade)
        if (!p.installed.isNullOrBlank() && p.fromThisMarket) add(CatalogAction.Uninstall)
    }

    /** Why the page offers nothing: installed some other way (only the hub's machine changes it). */
    fun noActionReason(p: CatalogExtension): String? = when {
        actions(p).isNotEmpty() -> null
        !p.problem.isNullOrBlank() -> p.problem
        !p.installed.isNullOrBlank() && !p.fromThisMarket ->
            "这个扩展不是从这个市场装的：升级和卸载在 hub 所在的机器上（personad extension）或 hub 的管理界面里做。"
        else -> null
    }

    /** The confirm button of a preview: red when an upgrade asks for more. */
    fun confirmLabel(preview: ExtensionPreview): String = when {
        !preview.upgrade -> "同意并安装"
        preview.morePermissions || !preview.addedPermissions.isNullOrEmpty() -> "同意新权限并升级"
        else -> "升级"
    }

    fun asksForMore(preview: ExtensionPreview): Boolean = preview.upgrade && (preview.morePermissions || !preview.addedPermissions.isNullOrEmpty())

    /** What the page says once it is in. */
    fun installed(detail: ExtensionDetail, upgrade: Boolean): String {
        val missing = detail.extension.needsSetup.orEmpty()
        val done = if (upgrade) "已升级到 ${detail.extension.version.orEmpty()}".trim() else "已安装"
        return if (missing.isEmpty()) done else "$done，还缺：" + missing.joinToString("、")
    }

    /** A market's line in the list: how many extensions, signed or not, a token, when it was read. */
    fun marketLine(m: ExtensionMarket): String = listOfNotNull(
        "${m.extensions} 个扩展",
        m.signature?.let { if (it.verified) "已签名" else if (it.unsigned) "未签名" else null },
        "有 token".takeIf { m.hasToken },
        m.fetchedAt?.let(::time)?.let { "读取于 $it" },
    ).joinToString(" · ")

    /** A market whose signature fails or whose key changed: what the page says under the list (null when it checks out). */
    fun signatureProblem(m: ExtensionMarket): String? = m.signature?.takeIf { it.failed }?.let { s ->
        s.detail?.takeIf { it.isNotBlank() } ?: "签名对不上或换了签名 key：hub 先不从这个市场安装和升级，到 hub 所在的机器上确认后恢复。"
    }

    /** `2026-11-04T06:00:00Z` → `2026-11-04 06:00` (UTC as the hub wrote it; enough to tell old from fresh). */
    fun time(iso: String): String? = Regex("^(\\d{4}-\\d{2}-\\d{2})T(\\d{2}:\\d{2})").find(iso)?.let { "${it.groupValues[1]} ${it.groupValues[2]}" }
}

internal data class CatalogState(
    val catalog: ExtensionCatalog? = null,
    val query: String = "",
    /** The market the list is narrowed to; null is all. */
    val market: String? = null,
    val loading: Boolean = false,
    val error: String? = null,
) {
    val markets: List<ExtensionMarket> get() = catalog?.markets.orEmpty()
    val extensions: List<CatalogExtension> get() = catalog?.extensions.orEmpty()
}

/** 扩展市场: the catalog, searched and narrowed to a market by the hub. */
internal class CatalogModel(private val api: ExtensionMarkets) {
    private val mutable = MutableStateFlow(CatalogState())
    val state = mutable.asStateFlow()
    private var generation = 0

    fun setQuery(query: String) = mutable.update { it.copy(query = query) }

    fun setMarket(market: String?) = mutable.update { it.copy(market = market) }

    /** Reads the catalog for the query and market as they are; an older answer that comes in late is dropped. */
    suspend fun load(refresh: Boolean = false) {
        val mine = ++generation
        val (query, market) = state.value.let { it.query to it.market }
        mutable.update { it.copy(loading = true) }
        try {
            val catalog = api.catalog(query, market, refresh)
            if (mine == generation) mutable.update {
                // A market that went away is not a filter any more.
                val keep = it.market?.takeIf { id -> catalog.markets.any { m -> m.id == id } }
                it.copy(catalog = catalog, error = null, market = keep)
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { if (mine == generation) mutable.update { it.copy(error = "读取失败：${failureReason(error)}") } }
        finally { if (mine == generation) mutable.update { it.copy(loading = false) } }
    }
}

internal data class CatalogExtensionState(
    val extension: CatalogExtension? = null,
    val marketName: String? = null,
    val loading: Boolean = false,
    val error: String? = null,
    /** The preview waiting for the user's yes: what it is and may do. */
    val preview: ExtensionPreview? = null,
    /** Downloading and checking the package. */
    val previewing: Boolean = false,
    /** Installing, upgrading or removing. */
    val acting: Boolean = false,
    /** Why the last step did not happen. */
    val actionError: String? = null,
    /** It is gone from the catalog (the market dropped it, or the market was removed). */
    val missing: Boolean = false,
) {
    val busy: Boolean get() = loading || previewing || acting
}

/** One extension of a market: its permissions, preview → install / upgrade, remove. */
internal class CatalogExtensionModel(private val api: ExtensionMarkets, private val market: String, private val id: String) {
    private val mutable = MutableStateFlow(CatalogExtensionState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val catalog = api.catalog(market = market)
            val extension = catalog.extensions.firstOrNull { it.market == market && it.id == id }
            mutable.update {
                it.copy(extension = extension ?: it.extension, missing = extension == null, error = null,
                    marketName = catalog.markets.firstOrNull { m -> m.id == market }?.label ?: extension?.marketName)
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = "读取失败：${failureReason(error)}") } }
        finally { mutable.update { it.copy(loading = false) } }
    }

    /** Downloads and checks the package; the page then shows what it may do and waits for a yes. */
    suspend fun preview(): Boolean {
        if (state.value.busy) return false
        mutable.update { it.copy(previewing = true, actionError = null) }
        return try {
            val preview = api.previewFromMarket(market, id)
            mutable.update { it.copy(preview = preview) }
            true
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) {
            mutable.update { it.copy(actionError = "没有拿到安装包：${failureReason(error)}") }
            false
        } finally { mutable.update { it.copy(previewing = false) } }
    }

    fun dismissPreview() = mutable.update { it.copy(preview = null) }

    /** The user agreed: installs or upgrades with the preview's permissions. Returns what to tell them, or null. */
    suspend fun confirm(): String? {
        val preview = state.value.preview ?: return null
        if (state.value.acting) return null
        mutable.update { it.copy(acting = true, actionError = null) }
        return try {
            val detail = if (preview.upgrade) api.upgrade(id, preview) else api.install(preview)
            mutable.update { it.copy(preview = null, acting = false) }
            load()
            CatalogText.installed(detail, preview.upgrade)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) {
            mutable.update { it.copy(preview = null, actionError = (if (preview.upgrade) "没有升级：" else "没有安装：") + failureReason(error)) }
            null
        } finally { mutable.update { it.copy(acting = false) } }
    }

    suspend fun uninstall(keepData: Boolean): Boolean {
        if (state.value.busy) return false
        mutable.update { it.copy(acting = true, actionError = null) }
        return try {
            api.uninstall(id, keepData)
            mutable.update { it.copy(acting = false) }
            load()
            true
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) {
            mutable.update { it.copy(actionError = "没有卸载：${failureReason(error)}") }
            false
        } finally { mutable.update { it.copy(acting = false) } }
    }
}

internal data class MarketsState(
    val markets: List<ExtensionMarket>? = null,
    val loading: Boolean = false,
    val error: String? = null,
    /** The market a call is on its way for. */
    val working: String? = null,
    val adding: Boolean = false,
    /** Why the hub did not add the market (its own words: wrong address, private needs a token…). */
    val addError: String? = null,
    /** Why the last refresh, removal or token change did not happen. */
    val actionError: String? = null,
)

/** 管理市场: the markets the hub reads. */
internal class MarketsModel(private val api: ExtensionMarkets) {
    private val mutable = MutableStateFlow(MarketsState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        if (state.value.loading) return
        mutable.update { it.copy(loading = true) }
        try {
            val markets = api.markets()
            mutable.update { it.copy(markets = markets, error = null) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = "读取失败：${failureReason(error)}") } }
        finally { mutable.update { it.copy(loading = false) } }
    }

    fun clearAddError() = mutable.update { it.copy(addError = null) }

    /**
     * Adds a market (read once by the hub first); a token, when given, is sent once and never shown. The address goes
     * to the hub as typed: it knows the forms (https, github:, git: and the ones it adds later) and says what is wrong.
     */
    suspend fun add(spec: String, token: String): Boolean {
        val s = spec.trim()
        if (s.isEmpty() || state.value.adding) return false
        mutable.update { it.copy(adding = true, addError = null) }
        return try {
            val market = api.addMarket(s, token.trim().takeIf { it.isNotEmpty() })
            mutable.update { it.copy(markets = it.markets.orEmpty().filter { m -> m.id != market.id } + market) }
            true
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) {
            mutable.update { it.copy(addError = failureReason(error)) }
            false
        } finally { mutable.update { it.copy(adding = false) } }
    }

    suspend fun remove(id: String): Boolean = working(id, "没有删除") {
        api.removeMarket(id)
        mutable.update { it.copy(markets = it.markets?.filter { m -> m.id != id }) }
    }

    suspend fun refresh(id: String): Boolean = working(id, "没有刷新") { replace(api.refreshMarket(id)) }

    /** A new token, or "" to remove it; the hub reads the market again with it. */
    suspend fun setToken(id: String, token: String): Boolean = working(id, "没有换成") { replace(api.setMarketToken(id, token.trim())) }

    private fun replace(market: ExtensionMarket) = mutable.update {
        it.copy(markets = it.markets?.map { m -> if (m.id == market.id) market else m })
    }

    private suspend fun working(id: String, failed: String, block: suspend () -> Unit): Boolean {
        if (state.value.working != null) return false
        mutable.update { it.copy(working = id, actionError = null) }
        return try { block(); true }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) {
            mutable.update { it.copy(actionError = "$failed：${failureReason(error)}") }
            false
        } finally { mutable.update { it.copy(working = null) } }
    }
}
