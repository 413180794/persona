@file:OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)

package persona.core.contracts.api

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.Machine
import persona.core.contracts.WireSerializer
import kotlinx.serialization.KeepGeneratedSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull

/**
 * What the app changes on a machine (docs/protocol.md「REST」): its launchers, reconnecting and redeploying its node,
 * its terminal engine and its whitelisted Claude Code and Codex settings. Bound to the active pairing; every call goes to the
 * hub that is paired when it is made.
 */
interface MachineManagement {
    /** `GET /api/v1/machines/{id}/launchers`. */
    suspend fun launchers(machineId: String): List<AgentLauncher>

    /**
     * `GET /api/v1/launchers/{name}/quota?machine=`: [name]'s quota at its gateway, read by [machineId] when it has the
     * launcher (the hub picks another that does otherwise); [refresh] reads again instead of the hub's minute-old answer.
     */
    suspend fun launcherQuota(machineId: String, name: String, refresh: Boolean = false): LauncherQuotaAnswer

    /** `POST /api/v1/machines/{id}/reconnect`: the hub drops and redials the node; the result shows in `machines`. */
    suspend fun reconnect(machineId: String)

    /** `POST /api/v1/machines/{id}/deploy`: the hub installs its own jarvisd there; progress comes with `machines`. */
    suspend fun deploy(machineId: String)

    /** `GET /api/v1/machines/{id}/herdr`: whether the node knows classic herdr is running. */
    suspend fun herdr(machineId: String): HerdrState

    /** `POST /api/v1/machines/{id}/herdr/start`: an explicit start, independent of the autostart setting. */
    suspend fun startHerdr(machineId: String): HerdrState

    /** `PUT /api/v1/machines/{id}/engine`: allowed offline (applied when it connects); returns the updated machine. */
    suspend fun setEngine(machineId: String, engine: String): Machine

    /** `GET /api/v1/machines/{id}/claude-settings`. */
    suspend fun claudeSettings(machineId: String): List<ClaudeSetting>

    /** `PUT /api/v1/machines/{id}/claude-settings`: [value] [JsonNull] goes back to Claude Code's default. */
    suspend fun setClaudeSetting(machineId: String, key: String, value: JsonElement): List<ClaudeSetting>

    /** `GET /api/v1/machines/{id}/codex-settings`: the whitelisted `~/.codex/config.toml` options, the same shape as Claude's. */
    suspend fun codexSettings(machineId: String): List<ClaudeSetting>

    /** `PUT /api/v1/machines/{id}/codex-settings`: [value] [JsonNull] removes the key, back to Codex's default. */
    suspend fun setCodexSetting(machineId: String, key: String, value: JsonElement): List<ClaudeSetting>

    /** `GET /api/v1/machines/{id}/claude-dirs`: its Claude Code config directories and the blocked patterns. */
    suspend fun claudeDirs(machineId: String): ClaudeDirsState = throw UnsupportedOperationException()

    /** `PUT /api/v1/machines/{id}/claude-dirs/blocked`: the whole list of patterns; the hub answers the new state. */
    suspend fun setBlockedClaudeDirs(machineId: String, blocked: List<String>): ClaudeDirsState = throw UnsupportedOperationException()

    /** `GET /api/v1/machines/{id}/doctor`: what `personad node doctor` finds there (full devices only). */
    suspend fun doctor(machineId: String): DoctorReport = throw UnsupportedOperationException("doctor")

    /** `POST /api/v1/machines/{id}/doctor/fix`: the node writes Persona's own generated files again and re-checks. */
    suspend fun fixDoctor(machineId: String): DoctorReport = throw UnsupportedOperationException("doctor/fix")

    /** `GET /api/v1/machines/{id}/browser`: ego lite there, asked of the node now. */
    suspend fun browser(machineId: String): persona.core.contracts.MachineBrowser = throw UnsupportedOperationException("browser")

    /** `PUT /api/v1/machines/{id}/browser`: whether the assistant may drive that Mac's ego lite. */
    suspend fun setBrowser(machineId: String, enabled: Boolean): persona.core.contracts.MachineBrowser =
        throw UnsupportedOperationException("browser")

    /** `POST /api/v1/machines/{id}/browser/{action}`: install | cancel | open. */
    suspend fun browserJob(machineId: String, action: String): persona.core.contracts.MachineBrowser =
        throw UnsupportedOperationException("browser")

    /** `GET /api/v1/usage/windows` of the selected hub: the subscription accounts and their windows (#2432). */
    suspend fun usageWindows(): List<UsageAccount> = emptyList()
}

/** One Claude Code config directory of a machine (设置 › Claude Code › 屏蔽的配置目录, docs/claude-integration.md). */
@Serializable
data class ClaudeDir(
    /** As Claude Code was given it ("~/…" or absolute). */
    @SerialName("path") val path: String,
    /** The node's own default (~/.claude): never blocked. */
    @SerialName("default") val isDefault: Boolean = false,
    @SerialName("blocked") val blocked: Boolean = false,
    /** The pattern that blocks it, as written (a glob, or its own path). */
    @SerialName("blocked_by") val blockedBy: String? = null,
    /** default | config (hub.json) | found (a claude used it) */
    @SerialName("source") val source: String = "found",
    @SerialName("running") val running: Int = 0,
    @SerialName("sessions") val sessions: Int = 0,
)

/** [live]: the node's own list (every directory it knows, with counts); false: the hub's record (offline, older node). */
@Serializable
data class ClaudeDirsState(
    @SerialName("dirs") val dirs: List<ClaudeDir> = emptyList(),
    @SerialName("blocked") val blocked: List<String> = emptyList(),
    @SerialName("home") val home: String? = null,
    @SerialName("live") val live: Boolean = false,
)

/**
 * One whitelisted `~/.claude/settings.json` (or `~/.codex/config.toml`) option of a machine. [value] null means unset:
 * the agent uses [default].
 */
@KeepGeneratedSerializer
@Serializable(with = ClaudeSettingSerializer::class)
data class ClaudeSetting(
    @SerialName("key") val key: String,
    @SerialName("title") val title: String = "",
    @SerialName("detail") val detail: String = "",
    /** bool | enum | string. */
    @SerialName("type") val type: String = "string",
    @SerialName("options") val options: List<String>? = null,
    @SerialName("default") val default: JsonElement? = null,
    @SerialName("value") val value: JsonElement? = null,
) {
    /** What is in effect: the set value, else the default. */
    val effective: JsonElement? get() = value?.takeUnless { it is JsonNull } ?: default
    val isDefault: Boolean get() = value == null || value is JsonNull
    val boolValue: Boolean get() = (effective as? JsonPrimitive)?.booleanOrNull ?: false
    val stringValue: String get() = (effective as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull.orEmpty()

    /** The text field's placeholder: the default when it has one. */
    val placeholder: CoreText
        get() = (default as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull?.takeIf { it.isNotEmpty() }?.let(CoreText::plain)
            ?: CoreText.of(Key.NotSet)
}

object ClaudeSettingSerializer : WireSerializer<ClaudeSetting>(ClaudeSetting.generatedSerializer())

@KeepGeneratedSerializer
@Serializable(with = HerdrStateSerializer::class)
data class HerdrState(
    val autostart: Boolean,
    val running: Boolean,
    val known: Boolean,
)

object HerdrStateSerializer : WireSerializer<HerdrState>(HerdrState.generatedSerializer())
