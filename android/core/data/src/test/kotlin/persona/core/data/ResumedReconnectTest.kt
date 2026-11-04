package persona.core.data

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.*
import org.junit.Test

/** A socket that resumed the hub's event stream does not refetch what its snapshots and replay already gave (#1574). */
@OptIn(ExperimentalCoroutinesApi::class)
class ResumedReconnectTest {
    @Test fun aResumedSocketSkipsTheRefetchAndAFreshOneRefetches() = runTest {
        val connection = Connection()
        val transport = Transport()
        val sessions = HubSessionsRepository(transport, connection, backgroundScope)
        val interactions = HubInteractionCenter(transport, connection, backgroundScope)
        suspend fun TestScope.goOnline(resumed: Boolean) {
            connection.state.value = ConnectionState.Connecting
            runCurrent()
            connection.resumed = resumed
            connection.state.value = ConnectionState.Online
            runCurrent()
        }

        goOnline(resumed = false)
        assertEquals(1, transport.machineFetches)
        assertEquals(1, transport.sessionFetches)
        assertEquals(1, transport.interactionFetches)
        assertEquals(listOf("pending"), interactions.pending.value.map { it.id })

        // Dropped and resumed: the hub's snapshots and the missed broadcasts arrive while the state is not yet Online.
        connection.state.value = ConnectionState.Connecting
        runCurrent()
        assertNull(sessions.verifiedLists.value.machines)
        connection.events.emit(MachinesEvent(listOf(Machine("mac", online = true), Machine("pc", online = true))))
        connection.events.emit(SessionsEvent("pc", listOf(Session("pc", "s1"))))
        connection.events.emit(InteractionResolvedEvent("pending"))
        connection.resumed = true
        connection.state.value = ConnectionState.Online
        runCurrent()
        assertEquals(1, transport.machineFetches)
        assertEquals(1, transport.sessionFetches)
        assertEquals(1, transport.interactionFetches)
        // What is held is the hub's: the chat screens can trust it as after a refetch.
        assertEquals(listOf("mac", "pc"), sessions.verifiedLists.value.machines?.map { it.id })
        assertEquals(listOf("s1"), sessions.verifiedLists.value.sessions["pc"]?.map { it.sessionId })
        assertTrue(interactions.pending.value.isEmpty())

        // A socket that started over refetches as it always did.
        goOnline(resumed = false)
        assertEquals(2, transport.machineFetches)
        assertEquals(2, transport.sessionFetches)
        assertEquals(2, transport.interactionFetches)
    }

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.WaitingForNetwork)
        override val events = MutableSharedFlow<ServerEvent>()
        override var resumed = false
        override fun reconnect() = Unit
        override fun send(message: JsonObject): Boolean = true
    }

    private class Transport : ChatTransport {
        var machineFetches = 0
        var sessionFetches = 0
        var interactionFetches = 0
        override suspend fun machines(): List<Machine> { machineFetches++; return listOf(Machine("mac", online = true)) }
        override suspend fun sessions(): List<Session> { sessionFetches++; return emptyList() }
        override suspend fun items(session: SessionKey, query: ItemsQuery): ItemsResponse = error("Unexpected items request")
        override suspend fun interactions(): List<Interaction> { interactionFetches++; return listOf(Interaction("pending")) }
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
        override suspend fun image(session: SessionKey, itemId: String) = byteArrayOf()
        override suspend fun fullItem(session: SessionKey, itemId: String) = Item(itemId)
    }
}
