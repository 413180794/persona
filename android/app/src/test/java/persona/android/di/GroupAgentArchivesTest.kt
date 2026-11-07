package persona.android.di

import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.AgentArchives
import persona.feature.agents.AgentsWiringState

class GroupAgentArchivesTest {
    private class Archives(val agent: String) : AgentArchives {
        override var supported = true
        var failList = false
        val writes = mutableListOf<Pair<String, SessionKey>>()
        override suspend fun list(): List<Session> {
            check(!failList) { "Offline" }
            return listOf(Session(machineId = "desktop", sessionId = "thread", agent = agent))
        }
        override suspend fun archive(key: SessionKey) { writes += "archive" to key }
        override suspend fun restore(key: SessionKey) { writes += "restore" to key }
    }

    @Test fun mergedWiringListsBothSourcesAndWritesOnlyToOwningHubWithRawIds() = runBlocking {
        val home = GroupNames(null)
        val work = GroupNames("g0a1b2c3d")
        val codex = Archives("codex")
        val claude = Archives("claude")
        val merged = requireNotNull(mergedAgentsState(listOf(home, work), listOf(
            AgentsWiringState(archives = codex.inGroup(home)),
            AgentsWiringState(archives = claude.inGroup(work)),
        ), 0).archives)
        assertEquals(listOf("desktop", "desktop~g0a1b2c3d"), merged.list().map { it.machineId })
        assertEquals(listOf("codex", "claude"), merged.list().map { it.agent })
        merged.archive(SessionKey("desktop~g0a1b2c3d", "thread"))
        merged.restore(SessionKey("desktop~g0a1b2c3d", "thread"))
        assertEquals(listOf("archive" to SessionKey("desktop", "thread"), "restore" to SessionKey("desktop", "thread")), claude.writes)
        assertTrue(codex.writes.isEmpty())
        merged.archive(SessionKey("desktop", "thread"))
        assertEquals(listOf("archive" to SessionKey("desktop", "thread")), codex.writes)
        assertFalse(merged.supports(SessionKey("desktop~g12345678", "thread")))
        assertTrue(runCatching { merged.restore(SessionKey("desktop~g12345678", "thread")) }.isFailure)
    }

    @Test fun unsupportedGroupsAreSkippedButSupportedOfflineGroupsFailTheList() = runBlocking {
        val home = Archives("codex")
        val away = Archives("claude").apply { supported = false; failList = true }
        val names = listOf(GroupNames(null), GroupNames("g0a1b2c3d"))
        val merged = mergedAgentArchives(names, listOf(home, away.inGroup(names[1])))
        assertTrue(merged.supported)
        assertFalse(merged.supports(SessionKey("desktop~g0a1b2c3d", "thread")))
        assertEquals(listOf("codex"), merged.list().map { it.agent })
        away.supported = true
        assertTrue(merged.supports(SessionKey("desktop~g0a1b2c3d", "thread")))
        assertTrue(runCatching { merged.list() }.isFailure)
        home.supported = false
        away.supported = false
        assertFalse(merged.supported)
    }
}
