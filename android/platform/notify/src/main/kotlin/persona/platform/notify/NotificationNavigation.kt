package persona.platform.notify

import persona.core.navigation.TerminalRoute
import java.net.URLEncoder

/** Carries the terminal target and session identifiers for the shell VIEW intent integration. */
internal fun terminalLink(route: TerminalRoute): String {
    fun encode(value: String) = URLEncoder.encode(value, "UTF-8").replace("+", "%20")
    return "persona://open?target=terminal&machine=${encode(route.machineId)}&session=${encode(route.sessionId)}"
}
