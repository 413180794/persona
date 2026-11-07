package persona.feature.chat

import androidx.compose.runtime.Stable
import androidx.compose.runtime.mutableStateMapOf
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * What the chat knows of the ports its links point at (docs/operations/ports.md, iOS `PortPresence`): a port an agent on the
 * machine lists (`Session.ports`) is live; any other is asked about (`GET …/ports/{port}`) as its badge shows, and
 * remembered a minute (half that when gone). A badge whose port is known gone is gray; opening one that turned out gone
 * marks it too. Used from the main thread, with a main-thread [scope] (the chat page's).
 */
@Stable
internal class PortPresence(
    private val scope: CoroutineScope,
    /** Whether something listens on the machine's port, and its page's title. */
    private val probe: suspend (Int) -> Pair<Boolean, String?>,
    private val now: () -> Long = System::currentTimeMillis,
) {
    sealed interface State {
        data class Live(val title: String?) : State
        data object Gone : State
    }

    private class Entry(val state: State, val at: Long)

    private val known = mutableStateMapOf<Int, Entry>()
    private val asking = HashSet<Int>()

    /** A port's state: listed by an agent on the machine, else what the hub said last; null while unknown. */
    fun state(port: Int, listed: Map<Int, String?>): State? =
        if (port in listed) State.Live(listed[port]) else known[port]?.state

    /** Asks about the ports not listed and not known lately. */
    fun ask(ports: List<Int>, listed: Map<Int, String?>) {
        val at = now()
        for (port in ports) {
            if (port in listed || port in asking) continue
            val entry = known[port]
            if (entry != null && at - entry.at < if (entry.state == State.Gone) GONE_MS else LIVE_MS) continue
            asking += port
            scope.launch {
                val got = try { probe(port) } catch (e: CancellationException) { asking -= port; throw e } catch (_: Exception) { null }
                asking -= port
                // No answer (an old hub, no network): leave it unknown, so the badge stays as it was.
                got?.let { (listening, title) -> known[port] = Entry(if (listening) State.Live(title) else State.Gone, now()) }
            }
        }
    }

    fun noteGone(port: Int) { known[port] = Entry(State.Gone, now()) }

    private companion object {
        const val LIVE_MS = 60_000L
        const val GONE_MS = 30_000L
    }
}
