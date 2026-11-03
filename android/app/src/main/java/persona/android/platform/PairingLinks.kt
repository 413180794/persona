package persona.android.platform

import persona.core.contracts.api.PairingRequest
import persona.feature.settings.PairingInput

// Parse both full pairing link schemes before permission checks and networking; ordinary addresses keep the caller's behavior.
internal fun PairingRequest.parsedPairingLink(): PairingRequest? =
    if (PairingInput.isLink(url)) PairingInput.parse(url) else null
