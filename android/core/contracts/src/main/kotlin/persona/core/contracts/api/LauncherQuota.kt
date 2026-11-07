package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * `GET /api/v1/launchers/{name}/quota` (docs/integrations/launchers.md "Quota"): what a launcher's key may still spend at its gateway, as
 * a quota extension read it. [error] when there is nothing to read ([unsupported]: no extension knows the gateway) or the gateway
 * said no; [machine] is the machine that read it (a ~/.zshrc launcher's key stays there).
 */
@Serializable
data class LauncherQuotaAnswer(
    @SerialName("machine") val machine: String? = null,
    @SerialName("checked_at") val checkedAt: String? = null,
    @SerialName("quota") val quota: LauncherQuota? = null,
    @SerialName("error") val error: String? = null,
    @SerialName("unsupported") val unsupported: Boolean = false,
)

@Serializable
data class LauncherQuota(
    /** The gateway's id (the extension's contribution). */
    @SerialName("gateway") val gateway: String = "",
    /** What to call it, from the hub; an older hub leaves it out and the id is shown. */
    @SerialName("gateway_name") val gatewayName: String? = null,
    /** The key's billing name at the gateway (not the key). */
    @SerialName("key_name") val keyName: String? = null,
    @SerialName("usage") val usage: List<QuotaUsage> = emptyList(),
    /** A ceiling on the key itself. */
    @SerialName("budget") val budget: List<QuotaWindow> = emptyList(),
    /** The accounts / providers the key draws on. */
    @SerialName("pools") val pools: List<QuotaPool> = emptyList(),
    @SerialName("note") val note: String? = null,
)

@Serializable
data class QuotaWindow(
    /** 5 hours / weekly / monthly / total budget… */
    @SerialName("label") val label: String = "",
    @SerialName("used_ratio") val usedRatio: Double? = null,
    @SerialName("used") val used: Double? = null,
    @SerialName("limit") val limit: Double? = null,
    /** usd | tokens | requests | credits | percent */
    @SerialName("unit") val unit: String? = null,
    @SerialName("resets_at") val resetsAt: String? = null,
    @SerialName("exhausted") val exhausted: Boolean = false,
)

@Serializable
data class QuotaPool(
    @SerialName("name") val name: String = "",
    /** available | ok | unsupported | cooldown | exhausted | restricted | unavailable | partial | error */
    @SerialName("state") val state: String? = null,
    /** Why, or the state in words. */
    @SerialName("detail") val detail: String? = null,
    @SerialName("windows") val windows: List<QuotaWindow> = emptyList(),
    @SerialName("models") val models: List<QuotaModelWindows> = emptyList(),
)

@Serializable
data class QuotaModelWindows(
    @SerialName("model") val model: String = "",
    @SerialName("windows") val windows: List<QuotaWindow> = emptyList(),
)

@Serializable
data class QuotaUsage(
    /** Today / this week / this month */
    @SerialName("label") val label: String = "",
    @SerialName("requests") val requests: Long = 0,
    @SerialName("tokens") val tokens: Long = 0,
    @SerialName("cost_usd") val costUsd: Double? = null,
)
