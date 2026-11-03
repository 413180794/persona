package persona.core.contracts

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/** Optional hook payload; unsupported modes remain available in the terminal. */
@Serializable
data class Elicitation(
    val mode: String = "form",
    val message: String = "",
    @SerialName("requested_schema") val requestedSchema: JsonObject? = null,
    val url: String? = null,
    @SerialName("elicitation_id") val elicitationId: String? = null,
)
