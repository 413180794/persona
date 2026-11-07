package persona.core.contracts.api

import persona.core.contracts.CoreTextException
import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText

/**
 * Opens a machine's port on the phone (docs/operations/ports.md): the hub is told first when no agent lists it (`POST …/open`),
 * then the phone listens for it and the page loads from the returned address, http://localhost:<phone port><path>.
 */
interface PortOpener {
    /** [listed]: an agent on the machine lists the port, so the hub already allows it. */
    suspend fun open(machineId: String, port: Int, path: String, secure: Boolean, listed: Boolean): String

    /** `GET …/ports/{port}`: whether something listens there, and its page's title; nothing is opened. */
    suspend fun probe(machineId: String, port: Int): PortProbe = throw UnsupportedOperationException("probe")
}

/** What the hub says of a machine's port (`portProbe`). */
data class PortProbe(val listening: Boolean, val title: String? = null)

/** Nothing listens on the port any more (the hub's `…/open` is 404): said in words, the link stays as it is. */
class PortGone(val port: Int) : CoreTextException(CoreText.of(Key.PortGone, port))
