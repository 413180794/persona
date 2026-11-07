package persona.core.designsystem

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.LookCurrent
import persona.core.contracts.api.LookListing
import persona.core.contracts.api.PersonaInfo
import persona.core.contracts.api.PersonaManifest
import persona.core.contracts.api.PersonaPacks
import java.io.File
import java.util.concurrent.CopyOnWriteArrayList

/**
 * A choice made here goes to a hub that keeps it only once this phone shows it (#5703): the hub pushes what it keeps
 * back to every device, so a failed download or one overtaken by another choice must not reach it.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class PersonaStoreSyncTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val pack = PersonaInfo("aurora", "0123456789abcdef", manifest = PersonaManifest(
        format = 1, id = "aurora", name = "Aurora",
        avatar = PersonaManifest.Avatar(mapOf("64" to "avatar/64.png")),
        dot = PersonaManifest.Dot(144, mapOf("idle" to PersonaManifest.DotState("dot/idle.png"))),
    ))

    private class FakeHub(val bytes: suspend () -> ByteArray) : PersonaPacks {
        val chosen = CopyOnWriteArrayList<String>()
        override suspend fun list(): List<PersonaInfo> = emptyList()
        override suspend fun file(id: String, file: String): ByteArray = bytes()
        override suspend fun looks(): LookListing = LookListing(emptyList(), LookCurrent())
        override suspend fun choose(id: String, ifUnset: Boolean): LookCurrent {
            chosen += id
            return LookCurrent(id, id)
        }
    }

    private fun store(hub: FakeHub): Pair<AppStyleStore, PersonaStore> {
        context.getSharedPreferences("persona-appearance", 0).edit().clear().commit()
        File(context.filesDir, "personas").deleteRecursively()
        val style = AppStyleStore(context)
        val store = PersonaStore(context, style) { hub }
        runBlocking { store.refresh() }
        return style to store
    }

    /** What the store sent the hub: the built-in face chosen once more marks the end, and its own call is left out. */
    private fun sent(hub: FakeHub, store: PersonaStore): List<String> {
        val marker = PersonaDesign.Default.id
        val before = hub.chosen.count { it == marker }
        store.choose(PersonaDesign.Default)
        val end = System.currentTimeMillis() + 5_000
        while (hub.chosen.count { it == marker } <= before && System.currentTimeMillis() < end) Thread.sleep(20)
        Thread.sleep(200)
        return hub.chosen.toMutableList().apply { removeAt(lastIndexOf(marker)) }
    }

    @Test fun aFailedDownloadStaysOffTheHub() {
        val hub = FakeHub { error("offline") }
        val (style, store) = store(hub)
        runBlocking { store.choose(pack) }
        assertNotNull(store.error.value)
        assertEquals("persona", style.personaId.value)
        assertEquals(emptyList<String>(), sent(hub, store))
    }

    @Test fun aBuiltInChosenDuringTheDownloadIsAllTheHubGets() {
        val asked = CompletableDeferred<Unit>()
        val gate = CompletableDeferred<ByteArray>()
        val hub = FakeHub { asked.complete(Unit); gate.await() }
        val (style, store) = store(hub)
        runBlocking {
            val download = launch { store.choose(pack) }
            asked.await()
            store.choose(PersonaDesign.Default)
            gate.complete(byteArrayOf(1))
            download.join()
        }
        assertEquals(PersonaDesign.Default.id, style.personaId.value)
        assertNull(store.current.value)
        assertEquals(listOf(PersonaDesign.Default.id), sent(hub, store))
    }
}
