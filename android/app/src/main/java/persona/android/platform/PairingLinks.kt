package persona.android.platform

import persona.core.contracts.api.PairingRequest
import persona.feature.settings.PairingInput

// 两种完整配对链接在权限检查和实际联网前使用同一解析器；普通地址保留调用方原有处理。
internal fun PairingRequest.parsedPairingLink(): PairingRequest? =
    if (listOf("persona:", "jarvis:").any(url.trim()::startsWith)) PairingInput.parse(url) else null
