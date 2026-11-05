package persona.core.hub

import persona.core.contracts.Endpoint
import persona.core.contracts.Hello
import persona.core.contracts.ServerEvent
import kotlinx.coroutines.delay
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

class MultiHubTests {

    @Test
    fun theCenterComesFirst() {
        // Paired hub b is a member; the center C is known only from b's mesh.
        val bEndpoints = listOf(Endpoint("http://127.0.0.1:18761"))
        val cEndpoints = listOf(Endpoint("http://127.0.0.1:18762"))
        val memberC = HubMember(hubId = "C", fingerprint = null, token = null, endpoints = cEndpoints, center = true)
        val config = HubConnectionConfig(hubId = "B", fingerprint = null, token = "t", endpoints = bEndpoints, version = "test", members = listOf(memberC))
        OkHttpHubConnection(config, HubNetwork("test", available = false)).use { connection ->
            assertEquals("C", (connection.centerId).zhAny())
            val c = connection.routeCandidates()
            assertEquals(listOf("http://127.0.0.1:18762", "http://127.0.0.1:18761"), (c.map { it.endpoint.url }).zhAny())
            assertEquals(listOf(true, false), c.map { it.center })
        }

        // The center paired here too: that one is the center candidate.
        val pairedC = HubMember(hubId = "C", fingerprint = null, token = "c-token", endpoints = cEndpoints, center = true)
        val bothConfig = config.copy(members = listOf(pairedC))
        OkHttpHubConnection(bothConfig, HubNetwork("test", available = false)).use { bothConnection ->
            val both = bothConnection.routeCandidates()
            assertEquals("C", (both.first().hub).zhAny())
            assertTrue(both.first().center)
            assertEquals(1, both.count { it.center })
        }

        // A group whose hellos name no center (hubs before the single center): the order of before, no center.
        val dEndpoints = listOf(Endpoint("http://127.0.0.1:18763"))
        val memberD = HubMember(hubId = "D", fingerprint = null, token = null, endpoints = dEndpoints, center = false)
        val oldConfig = HubConnectionConfig(hubId = "B", fingerprint = null, token = "t", endpoints = bEndpoints, version = "test", members = listOf(memberD))
        OkHttpHubConnection(oldConfig, HubNetwork("test", available = false)).use { plainConnection ->
            val plain = plainConnection.routeCandidates()
            assertEquals(listOf("B", "D"), (plain.map { it.hub }).zhAny())
            assertFalse(plain.any { it.center })
        }
    }

    @Test
    fun pickPrefersTheCenter() = runTest {
        val member = HubRoute.Candidate("http://member.example.com", hub = "m")
        val center = HubRoute.Candidate("http://center.example.com", center = true)

        // The member answers first, the center a moment later, within the grace: the center.
        val slowCenter = HubRoute.pick(listOf(center, member)) { candidate ->
            if (candidate.endpoint.url.contains("center.example.com")) delay(100)
            true
        }
        assertEquals(center, slowCenter)

        // The center does not answer: the member, without waiting out the grace.
        val start = System.currentTimeMillis()
        val down = HubRoute.pick(listOf(center, member), graceMillis = 30_000) { candidate ->
            candidate.endpoint.url.contains("member.example.com")
        }
        assertEquals(member, down)
        assertTrue(System.currentTimeMillis() - start < 5000)

        // The center hangs: the member once the grace is over.
        val hanging = HubRoute.pick(listOf(center, member), graceMillis = 50) { candidate ->
            if (candidate.endpoint.url.contains("center.example.com")) {
                delay(10_000)
                true
            } else true
        }
        assertEquals(member, hanging)

        // Nobody answers: the center, for the socket's own retries.
        val none = HubRoute.pick(listOf(center, member)) { false }
        assertEquals(center, none)
    }

    @Test
    fun helloMarksTheCenter() {
        val json = """{"type":"hello","hub_id":"hub-b","mesh":{"hubs":[{"hub_id":"hub-a","center":true},{"hub_id":"hub-b","self":true}]}}"""
        val event = ServerEvent.decode(json) as? Hello
        assertNotNull(event)
        assertEquals(listOf(true, null), event?.mesh?.hubs?.map { it.center })
    }
}
