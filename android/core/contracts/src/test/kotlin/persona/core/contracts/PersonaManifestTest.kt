package persona.core.contracts

import persona.core.contracts.api.PersonaListing
import persona.core.contracts.api.PersonaManifest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PersonaManifestTest {
    private fun manifest(id: String = "doudou", idle: Boolean = true, file: String = "dot/idle.png") = PersonaManifest(
        format = 1, id = id, name = "豆豆",
        avatar = PersonaManifest.Avatar(mapOf("64" to "avatar/64.png", "256" to "avatar/256.png")),
        dot = PersonaManifest.Dot(144, buildMap { if (idle) put("idle", PersonaManifest.DotState(file, 8.0)) }),
    )

    @Test fun readsTheHubListingAndIgnoresColors() {
        val body = """{"personas":[{"id":"demo","rev":"r1","bytes":9,"manifest":{"format":1,"id":"demo","name":"小薄荷",
            "avatar":{"sizes":{"64":"avatar/64.png"},"safe":1.0},"dot":{"size":144,"states":{"idle":{"file":"dot/idle.png","fps":8,"frames":24}}},
            "colors":{"light":{"primary":"#F2C230"}}}}]}"""
        val p = JsonCodec.decodeFromString<PersonaListing>(body).personas.single()
        assertEquals("小薄荷", p.manifest.name)
        assertNull(p.manifest.problem)
        assertEquals(listOf("avatar/64.png", "dot/idle.png"), p.manifest.files)
        assertEquals(false, p.usable) // "r1" is not a hub rev
        assertEquals(true, p.copy(rev = "0123456789abcdef").usable)
        assertEquals(false, p.copy(rev = "../../shared_prefs").usable)
        assertEquals(false, p.copy(id = "other", rev = "0123456789abcdef").usable)
    }

    @Test fun refusesWhatTheHubWouldLeaveOut() {
        assertNull(manifest().problem)
        assertEquals("id jarvis-circuit", manifest(id = "jarvis-circuit").problem)
        assertEquals("id Bad_Id", manifest(id = "Bad_Id").problem)
        assertEquals("no idle dot", manifest(idle = false).problem)
        assertEquals("file ../x.png", manifest(file = "../x.png").problem)
        assertEquals("file dot/.hidden.png", manifest(file = "dot/.hidden.png").problem)
    }

    @Test fun picksTheSmallestAvatarBigEnoughAndFallsBackToIdle() {
        assertEquals("avatar/256.png", manifest().avatarFile(192))
        assertEquals("avatar/64.png", manifest().avatarFile(48))
        assertEquals("avatar/256.png", manifest().avatarFile(1024))
        assertEquals("dot/idle.png", manifest().dotState("working")?.file)
    }
}
