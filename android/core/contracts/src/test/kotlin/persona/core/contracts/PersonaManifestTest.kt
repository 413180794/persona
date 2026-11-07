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
        assertEquals("小薄荷", (p.manifest.name).zhAny())
        assertNull(p.manifest.problem)
        assertEquals(listOf("avatar/64.png", "dot/idle.png"), (p.manifest.files).zhAny())
        assertEquals(false, p.usable) // "r1" is not a hub rev
        assertEquals(true, p.copy(rev = "0123456789abcdef").usable)
        assertEquals(false, p.copy(rev = "../../shared_prefs").usable)
        assertEquals(false, p.copy(id = "other", rev = "0123456789abcdef").usable)
    }

    /** A tier of a tiered look (Milk Frog): a plain manifest whose files sit under the tier, and the words for its card. */
    @Test fun readsATierWithAbout() {
        val body = """{"format":1,"id":"nwverse","name":"奶蛙","subtitle":"蝌蚪","about":"NWVERSE 等级",
            "avatar":{"sizes":{"64":"tiers/tadpole/avatar/64.png"}},"dot":{"size":144,"states":{"idle":{"file":"tiers/tadpole/dot/idle.png"}}}}"""
        val m = JsonCodec.decodeFromString<PersonaManifest>(body)
        assertNull(m.problem)
        assertEquals("NWVERSE 等级", (m.about).zhAny())
        assertEquals(listOf("tiers/tadpole/avatar/64.png", "tiers/tadpole/dot/idle.png"), (m.files).zhAny())
        assertNull(manifest().about)
    }

    @Test fun refusesWhatTheHubWouldLeaveOut() {
        assertNull(manifest().problem)
        PersonaManifest.RESERVED_IDS.forEach { id ->
            assertEquals("id $id", manifest(id = id).problem)
        }
        assertNull(manifest(id = "nwverse").problem)
        assertNull(manifest(id = "custom-look").problem)
        assertEquals("id Bad_Id", (manifest(id = "Bad_Id").problem).zhAny())
        assertEquals("no idle dot", (manifest(idle = false).problem).zhAny())
        assertEquals("file ../x.png", (manifest(file = "../x.png").problem).zhAny())
        assertEquals("file dot/.hidden.png", (manifest(file = "dot/.hidden.png").problem).zhAny())
    }

    @Test fun picksTheSmallestAvatarBigEnoughAndFallsBackToIdle() {
        assertEquals("avatar/256.png", (manifest().avatarFile(192)).zhAny())
        assertEquals("avatar/64.png", (manifest().avatarFile(48)).zhAny())
        assertEquals("avatar/256.png", (manifest().avatarFile(1024)).zhAny())
        assertEquals("dot/idle.png", (manifest().dotState("working")?.file).zhAny())
    }

    /** An extension's look (#3200): names in other languages, a dark avatar the phone downloads too, the SVG left to the web. */
    @Test fun readsAnExtensionsLook() {
        val body = """{"format":1,"id":"aurora","name":"极光","names":{"en":"Aurora","ja":"オーロラ"},
            "avatar":{"sizes":{"128":"a/128.png","256":"a/256.png"},"svg":"a/a.svg","dark":{"sizes":{"256":"a/d256.png"},"svg":"a/d.svg"}},
            "dot":{"size":144,"states":{"idle":{"file":"a/idle.png"}}},"app_icon_fallback":"persona",
            "source":{"kind":"extension","extension":"example-looks","extension_name":"Example Looks"}}"""
        val m = JsonCodec.decodeFromString<PersonaManifest>(body)
        assertNull(m.problem)
        assertEquals(listOf("a/128.png", "a/256.png", "a/d256.png", "a/idle.png"), m.files)
        assertEquals("a/128.png", m.avatarFile(100))
        assertEquals("a/d256.png", m.avatarFile(100, dark = true))
        assertEquals("オーロラ", m.displayName("ja"))
        assertEquals("Aurora", m.displayName("en"))
        assertEquals("极光", m.displayName("zh-Hans"))
        assertEquals("Example Looks", m.source?.extensionName)
        assertEquals("豆豆", manifest().displayName("ja"))
        assertEquals("avatar/64.png", manifest().avatarFile(10, dark = true))
    }
}
