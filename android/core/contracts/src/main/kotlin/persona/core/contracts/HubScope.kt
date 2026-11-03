package persona.core.contracts

/** A pairing generation is never reused, including when pairing with the same hub. */
data class HubScope(val hubId: String, val pairingGeneration: String) {
    init {
        require(hubId.matches(Regex("[A-Za-z0-9_-]{1,128}")))
        require(pairingGeneration.matches(Regex("[A-Za-z0-9_-]{1,128}")))
    }

    val path: String get() = "$hubId/$pairingGeneration"
}
