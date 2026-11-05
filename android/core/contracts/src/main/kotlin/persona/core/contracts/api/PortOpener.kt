package persona.core.contracts.api

/**
 * Opens a machine's port on the phone (docs/ports.md): the hub is told first when no agent lists it (`POST …/open`),
 * then the phone listens for it and the page loads from the returned address, http://localhost:<phone port><path>.
 */
interface PortOpener {
    /** [listed]: an agent on the machine lists the port, so the hub already allows it. */
    suspend fun open(machineId: String, port: Int, path: String, secure: Boolean, listed: Boolean): String
}

/** Nothing listens on the port any more (the hub's `…/open` is 404): said in words, the link stays as it is. */
class PortGone(val port: Int) : Exception("端口 :$port 已经没有在监听")
