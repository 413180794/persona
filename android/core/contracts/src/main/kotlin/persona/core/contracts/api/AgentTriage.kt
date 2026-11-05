package persona.core.contracts.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * 设置 › 会话分诊 (docs/agent-work.md「开关」): what the hub does on its own when an agent stops. All off until the user
 * turns them on: the triage calls a model on every stopped turn, and continuing / waking run a turn of the agent.
 */
@Serializable
data class TriageSwitches(
    val enabled: Boolean = false,
    @SerialName("auto_continue") val autoContinue: Boolean = false,
    val wake: Boolean = false,
    val decide: Boolean = false,
)

/** GET / PUT /api/v1/agents/triage on the active pairing; [TriageUnsupported] from hubs without it. */
interface AgentTriage {
    suspend fun switches(): TriageSwitches
    suspend fun put(value: TriageSwitches): TriageSwitches
}

class TriageUnsupported : Exception("这台 hub 还不支持会话分诊，先更新 hub")
