package persona.feature.assistant.settings

import persona.core.contracts.Machine
import persona.core.contracts.api.AssistantConfig
import persona.core.contracts.api.AssistantConfigUpdate
import persona.core.contracts.api.MemoryFile
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import java.time.Instant

class AssistantSettingsStateTest {
    private class FakeApi : AssistantSettingsApi {
        var config = AssistantConfig(machineId = "server", launcher = "claude", cwd = "/home/me/.jarvis/assistant",
            timezone = "Asia/Shanghai", notesEffective = "from hub.json", main = "main-id")
        val updates = mutableListOf<AssistantConfigUpdate>()
        var memory = listOf(
            MemoryFile("MEMORY.md", "- [早报格式](morning.md) — 一屏以内\n- [周报](weekly.md) — 先给草稿", "2026-10-31T08:00:00Z"),
            MemoryFile("morning.md", "---\nname: morning-format\ndescription: 早报一屏以内\nmetadata:\n  type: feedback\n---\n\n早报一屏以内，结论先行。",
                "2026-10-30T08:00:00Z"),
            MemoryFile("weekly.md", "---\nname: weekly\ntype: \"user\"\n---\n周报先给草稿。", "2026-10-31T09:00:00Z"),
        )
        val saved = mutableListOf<Pair<String, String>>()
        val deleted = mutableListOf<String>()
        var fail = false

        override suspend fun config(): AssistantConfig { check(!fail) { "hub unreachable" }; return config }
        override suspend fun updateConfig(update: AssistantConfigUpdate): AssistantConfig {
            check(!fail) { "hub refused" }
            updates += update
            config = config.copy(
                machineId = update.machineId ?: config.machineId,
                launcher = update.launcher ?: config.launcher,
                timezone = update.timezone ?: config.timezone,
                notes = update.notes ?: config.notes,
                notesEffective = update.notes ?: config.notesEffective,
                main = if (update.main == AssistantConfigUpdate.CLEAR_MAIN) null else update.main ?: config.main,
            )
            return config
        }
        override suspend fun memory(): List<MemoryFile> { check(!fail) { "assistant machine is offline" }; return memory }
        override suspend fun saveMemory(name: String, content: String) {
            check(!fail) { "write failed" }
            saved += name to content
            memory = memory.map { if (it.name == name) it.copy(content = content) else it }
        }
        override suspend fun deleteMemory(name: String) {
            check(!fail) { "delete failed" }
            deleted += name
            memory = memory.filterNot { it.name == name }
        }
    }

    private class Zone(override val phoneZone: String = "Asia/Shanghai", override var followPhone: Boolean = true) : TimezoneChoice

    private val server = Machine("server", name = "服务器", kind = "local", online = true, home = "/home/me")
    private val laptop = Machine("laptop", name = "笔记本", kind = "ssh", online = true, home = "/Users/me")
    private val offline = Machine("lab", name = "实验室", kind = "ssh", online = false)

    @Test fun readsWhereItRuns() = runTest {
        val api = FakeApi()
        val state = AssistantSettingsState(api, Zone())
        state.load()
        assertEquals("server", state.state.value.config?.machineId)
        assertTrue("the phone and the hub already agree on the zone", api.updates.isEmpty())
    }

    @Test fun movingWarnsThatMemoryAndConversationsStayAndStartsAFreshMainConversation() = runTest {
        val api = FakeApi()
        var moved = 0
        val state = AssistantSettingsState(api, Zone()) { moved++ }
        state.load()
        state.requestMove(server, listOf(server, laptop))
        assertNull("the machine it already runs on is not a move", state.state.value.move)

        state.requestMove(laptop, listOf(server, laptop))
        val move = requireNotNull(state.state.value.move)
        assertEquals("换到「笔记本」上运行？", move.title)
        assertTrue(move.text.startsWith("记忆和会话记录在原来那台机器上，不会跟过去。"))
        assertTrue(move.text.contains("「服务器」"))
        assertFalse(move.text.contains("离线"))
        assertTrue("nothing is written before the user confirms", api.updates.isEmpty())

        state.cancelMove()
        assertNull(state.state.value.move)
        assertTrue(api.updates.isEmpty())

        state.requestMove(laptop, listOf(server, laptop))
        state.confirmMove()
        // The main conversation is a transcript on the old machine, so it is cleared with the move.
        assertEquals(listOf(AssistantConfigUpdate(machineId = "laptop", main = AssistantConfigUpdate.CLEAR_MAIN)), api.updates)
        with(state.state.value) {
            assertEquals("laptop", config?.machineId)
            assertNull(config?.main)
            assertNull(this.move)
            assertNotNull("memory is read again from the new machine", memory.entries)
            assertFalse(notice!!.error)
        }
        assertEquals(1, moved)
    }

    @Test fun movingToAnOfflineMachineSaysSoAndAFailedMoveChangesNothing() = runTest {
        val api = FakeApi()
        var moved = 0
        val state = AssistantSettingsState(api, Zone()) { moved++ }
        state.load()
        state.requestMove(offline, listOf(server, offline))
        assertTrue(state.state.value.move!!.text.contains("「实验室」现在离线"))

        api.fail = true
        state.confirmMove()
        assertEquals("server", state.state.value.config?.machineId)
        assertEquals("main-id", state.state.value.config?.main)
        assertEquals("没换成，Persona 还在「服务器」上运行，请重试。", state.state.value.notice?.text)
        assertEquals(0, moved)
    }

    @Test fun theHubZoneFollowsThePhoneUnlessTheUserPicksOne() = runTest {
        val api = FakeApi()
        val zone = Zone(phoneZone = "Europe/Paris")
        val state = AssistantSettingsState(api, zone)
        state.load()
        assertEquals(listOf(AssistantConfigUpdate(timezone = "Europe/Paris")), api.updates)
        assertEquals("Europe/Paris", state.state.value.config?.timezone)

        state.chooseZone("Asia/Tokyo")
        assertEquals("picking a zone needs following the phone turned off first", 1, api.updates.size)
        state.setFollowPhone(false)
        assertFalse(zone.followPhone)
        assertEquals("turning following off keeps the hub's zone", 1, api.updates.size)
        state.chooseZone("Asia/Tokyo")
        assertEquals(AssistantConfigUpdate(timezone = "Asia/Tokyo"), api.updates.last())

        // A phone that no longer follows leaves the hub alone when the page opens again.
        val reopened = AssistantSettingsState(api, zone)
        reopened.load()
        assertEquals(2, api.updates.size)
        assertEquals("Asia/Tokyo", reopened.state.value.config?.timezone)

        reopened.setFollowPhone(true)
        assertTrue(zone.followPhone)
        assertEquals(AssistantConfigUpdate(timezone = "Europe/Paris"), api.updates.last())
    }

    @Test fun phoneZoneSyncOnlySendsWhatDiffers() = runTest {
        assertEquals("Europe/Paris", zoneToSync(Zone("Europe/Paris"), "Asia/Shanghai"))
        assertEquals("Europe/Paris", zoneToSync(Zone("Europe/Paris"), null))
        assertNull(zoneToSync(Zone("Asia/Shanghai"), "Asia/Shanghai"))
        assertNull(zoneToSync(Zone("Europe/Paris", followPhone = false), "Asia/Shanghai"))
        val api = FakeApi()
        assertEquals("Asia/Shanghai", followPhoneZone(api, Zone()).timezone)
        assertTrue(api.updates.isEmpty())
        assertEquals("Europe/Paris", followPhoneZone(api, Zone("Europe/Paris")).timezone)
        assertEquals(listOf(AssistantConfigUpdate(timezone = "Europe/Paris")), api.updates)
    }

    @Test fun notesSaveWhatTheUserWroteEvenEmpty() = runTest {
        val api = FakeApi()
        val state = AssistantSettingsState(api, Zone())
        state.load()
        assertEquals("never saved in the app: shows what hub.json tells the assistant", "from hub.json", state.state.value.config!!.notesShown())
        assertTrue(state.saveNotes("代码在 ~/code 下"))
        assertEquals("代码在 ~/code 下", state.state.value.config!!.notesShown())
        assertTrue(state.saveNotes(""))
        assertEquals(AssistantConfigUpdate(notes = ""), api.updates.last())
        assertEquals("", state.state.value.config!!.notesShown())

        api.fail = true
        assertFalse(state.saveNotes("lost?"))
        assertTrue(state.state.value.notice!!.error)
        assertEquals("", state.state.value.config!!.notes)
    }

    @Test fun memoryIsReadEditedAndDeleted() = runTest {
        val api = FakeApi()
        val state = AssistantSettingsState(api, Zone())
        state.loadMemory()
        val memory = state.state.value.memory
        // Newest first, titled from the index, the index itself kept apart.
        assertEquals(listOf("weekly.md", "morning.md"), memory.entries?.map { it.file.name })
        assertEquals(listOf("周报", "早报格式"), memory.entries?.map { it.title })
        assertEquals("MEMORY.md", memory.index?.name)
        val morning = memory.entries!!.last()
        assertEquals("早报一屏以内", morning.description)
        assertEquals("反馈", morning.typeLabel)
        assertEquals("早报一屏以内，结论先行。", morning.body)
        val weekly = memory.entries!!.first()
        assertEquals("from the index line when the front matter has none", "先给草稿", weekly.description)
        assertEquals("用户", weekly.typeLabel)

        val edited = morning.file.content.replace("结论先行", "先说结论")
        assertTrue(state.saveMemory("morning.md", edited))
        assertEquals(listOf("morning.md" to edited), api.saved)
        assertEquals("早报一屏以内，先说结论。", state.state.value.memory.entries!!.first { it.file.name == "morning.md" }.body)
        assertEquals(SettingsNotice("记忆已更新", error = false), state.state.value.notice)

        api.fail = true
        assertFalse(state.saveMemory("morning.md", "won't land"))
        assertEquals("没保存上，修改的内容还在，请重试。", state.state.value.notice?.text)
        assertFalse(state.deleteMemory("weekly.md"))
        assertEquals(2, state.state.value.memory.entries?.size)

        api.fail = false
        assertTrue(state.deleteMemory("weekly.md"))
        assertEquals(listOf("weekly.md"), api.deleted)
        assertEquals(listOf("morning.md"), state.state.value.memory.entries?.map { it.file.name })
        assertEquals(SettingsNotice("已删除这条记忆", error = false), state.state.value.notice)
    }

    @Test fun unreadableMemoryIsReportedNotThrown() = runTest {
        val api = FakeApi()
        api.fail = true
        val state = AssistantSettingsState(api, Zone())
        state.loadMemory()
        assertTrue(state.state.value.memory.failed)
        assertNull(state.state.value.memory.entries)
        state.load()
        assertTrue(state.state.value.loadFailed)
        assertNull(state.state.value.config)
    }

    @Test fun memoryFilesParseWithAndWithoutFrontMatter() {
        val plain = parseMemory(MemoryFile("plain-note.md", "只有正文\n第二行"))
        assertEquals("plain-note", plain.title)
        assertNull(plain.description)
        assertNull(plain.typeLabel)
        assertEquals("只有正文\n第二行", plain.body)
        val unterminated = parseMemory(MemoryFile("open.md", "---\nname: open\n正文"))
        assertEquals("open", unterminated.title)
        assertEquals("---\nname: open\n正文", unterminated.body)
        val quoted = parseMemory(MemoryFile("q.md", "---\nname: 'quoted name'\ndescription: \"说明\"\ntype: project\n---\n\n"))
        assertEquals("quoted name", quoted.title)
        assertEquals("说明", quoted.description)
        assertEquals("项目", quoted.typeLabel)
        assertEquals("", quoted.body)
        assertEquals(mapOf("a.md" to ("标题 A" to "说明"), "b.md" to ("标题 B" to null)),
            memoryIndexLines("# Memory\n- [标题 A](a.md) — 说明\n- [标题 B](b.md)\n不是索引的一行"))
    }

    @Test fun pathsAndZonesReadWell() {
        assertEquals("~/.jarvis/assistant", homeRelative("/home/me/.jarvis/assistant", "/home/me"))
        assertEquals("~/.jarvis/assistant", homeRelative("/home/me/.jarvis/assistant", "/home/me/"))
        assertEquals("/srv/assistant", homeRelative("/srv/assistant", "/home/me"))
        assertEquals("/home/user/x", homeRelative("/home/user/x", "/home/me"))
        assertEquals("", homeRelative("", null))
        val winter = Instant.parse("2026-01-15T00:00:00Z")
        assertEquals("UTC+8", zoneOffset("Asia/Shanghai", winter))
        assertEquals("UTC+5:30", zoneOffset("Asia/Kolkata", winter))
        assertEquals("UTC-5", zoneOffset("America/New_York", winter))
        assertEquals("UTC", zoneOffset("Europe/London", winter))
        assertNull(zoneOffset("Not/AZone", winter))
        val zones = pickableZones(winter)
        assertTrue("Asia/Shanghai" in zones)
        assertFalse(zones.any { it.startsWith("Etc/") })
        assertTrue(zones.indexOf("America/New_York") < zones.indexOf("Asia/Shanghai"))
        assertTrue(zoneMatches("America/New_York", "new york"))
        assertTrue(zoneMatches("Asia/Shanghai", "shanghai"))
        assertFalse(zoneMatches("Asia/Shanghai", "Paris"))
    }
}
