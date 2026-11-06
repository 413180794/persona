package persona.core.data

import androidx.datastore.core.DataStoreFactory
import persona.core.contracts.HubScope
import persona.core.contracts.api.EndpointOverrides
import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class EndpointOverridesStoreTest {
    @get:Rule val temporary = TemporaryFolder()
    private val manual = "https://hub.example.org:18662"
    private val pairing = "https://hub.example.com:18662"
    private val ipv6 = "https://[2001:db8::1]:18662"
    private val choices = EndpointOverrides(manual = listOf(manual), disabled = setOf(pairing), order = listOf(ipv6, pairing, manual))

    /** Opens the store like HubDataStore does (minus encryption) and closes it like a process exit. */
    private suspend fun <T> withStore(file: File, block: suspend (EndpointOverridesStore) -> T): T {
        val job = SupervisorJob()
        try {
            val dataStore = DataStoreFactory.create(
                serializer = VersionedSerializer(EndpointOverridesDocument(), EndpointOverridesDocument.serializer()) { it.version },
                scope = CoroutineScope(Dispatchers.IO + job),
                produceFile = { file.apply { parentFile!!.mkdirs() } },
            )
            return block(EndpointOverridesStore.open(dataStore))
        } finally {
            // DataStore refuses two live instances of one file: the first must be gone before reopening.
            job.cancelAndJoin()
        }
    }

    private fun file(roots: List<File>, scope: HubScope) = File(roots[2], "hub-data/${scope.path}/endpoints.enc")

    @Test fun choicesAndTheOrderSurviveARestartAndAMissingFileIsTheDefault() = runBlocking<Unit> {
        val file = File(temporary.root, "hub-data/test-hub/first/endpoints.enc")
        withStore(file) { store ->
            assertFalse(file.exists())
            assertEquals(EndpointOverrides(), store.value.value)
            val saved = store.update { it.copy(manual = it.manual + manual, disabled = choices.disabled, order = choices.order) }
            assertEquals(choices, saved)
            assertEquals(choices, store.value.value)
        }
        withStore(file) { store ->
            assertEquals(choices, store.value.value)
            // A new order replaces the old one as a whole.
            val reordered = choices.copy(order = listOf(manual, ipv6, pairing))
            assertEquals(reordered, store.update { it.copy(order = reordered.order) })
        }
        withStore(file) { store ->
            assertEquals(listOf(manual, ipv6, pairing), store.value.value.order)
            // "Restore default order" is stored as no order at all.
            assertEquals(EndpointOverrides(manual = listOf(manual)), store.update { it.copy(disabled = emptySet(), order = null) })
        }
        withStore(file) { assertEquals(EndpointOverrides(manual = listOf(manual)), it.value.value) }
    }

    @Test fun unpairRevocationAndPairingAgainStartFromTheDefaultOrder() = runBlocking<Unit> {
        val roots = listOf("files", "cache", "no-backup").map { temporary.newFolder(it) }
        val first = HubScope("test-hub", "first")
        withStore(file(roots, first)) { it.update { choices } }
        roots.take(2).forEach { File(it, "hub-data/${first.path}/data").apply { parentFile!!.mkdirs() }.writeText("data") }
        // Pairing again uses a new generation: its directory starts without route choices, in the default order.
        val second = HubScope("test-hub", "second")
        withStore(file(roots, second)) { assertEquals(EndpointOverrides(), it.value.value) }
        withStore(file(roots, first)) { assertEquals(choices, it.value.value) }
        // Unpair and revocation erase the old scope, route choices and the order included.
        eraseScope(roots, first)
        roots.forEach { assertFalse(File(it, "hub-data/${first.path}").exists()) }
        assertTrue(File(roots[2], "hub-data/${second.path}").exists())
        withStore(file(roots, first)) { assertNull(it.value.value.order) }
        withStore(file(roots, first)) { assertEquals(EndpointOverrides(), it.value.value) }
        eraseScope(roots, HubScope("test-hub", "never-existed"))
    }

    @Test fun aPreferredCandidateFromTheFirstVersionStaysFirst() = runBlocking<Unit> {
        val file = File(temporary.root, "hub-data/test-hub/first/endpoints.enc").apply { parentFile!!.mkdirs() }
        // What the first version wrote: one preferred candidate (tried first), no order.
        file.writeText("""{"version":1,"manual":["$manual"],"disabled":["$pairing"],"preferred":"$manual"}""")
        withStore(file) { store ->
            assertEquals(EndpointOverrides(manual = listOf(manual), disabled = setOf(pairing), order = listOf(manual)), store.value.value)
            store.update { it.copy(disabled = emptySet()) }
        }
        // The next save writes it as the order and drops the preferred key.
        val written = Json.parseToJsonElement(file.readText()).jsonObject
        assertEquals(listOf(manual), (written.getValue("order") as JsonArray).map { it.jsonPrimitive.content })
        assertEquals(JsonNull, written.getValue("preferred"))
        withStore(file) { assertEquals(EndpointOverrides(manual = listOf(manual), order = listOf(manual)), it.value.value) }
        // Without a preferred candidate the first version's file is the default order.
        file.writeText("""{"version":1,"manual":["$manual"],"disabled":[],"preferred":null}""")
        withStore(file) { assertEquals(EndpointOverrides(manual = listOf(manual)), it.value.value) }
        // A repeated key in a stored order counts once.
        file.writeText("""{"version":1,"manual":[],"disabled":[],"order":["$pairing","$manual","$pairing"]}""")
        withStore(file) { assertEquals(listOf(pairing, manual), it.value.value.order) }
    }
}
