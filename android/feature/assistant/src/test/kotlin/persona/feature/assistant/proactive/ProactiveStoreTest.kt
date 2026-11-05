package persona.feature.assistant.proactive

import persona.core.contracts.Machine
import persona.core.contracts.SessionKey
import persona.core.contracts.parseTime
import persona.core.contracts.api.Followup
import persona.core.contracts.api.Routine
import persona.core.contracts.api.Run
import java.io.IOException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4

/** A hub that behaves like the real one for the schedule: PUT keeps its own fields by id and hands out ids. */
private class FakeHub(
    var table: List<Routine>,
    var followups: List<Followup> = emptyList(),
    var runs: List<Run> = emptyList(),
) : ProactiveApi {
    val puts = mutableListOf<List<Routine>>()
    val started = mutableListOf<String>()
    var failWrite: Exception? = null
    /** The write lands but the answer never comes back. */
    var loseAnswer = false
    var failRun: Exception? = null
    var failCancel: Exception? = null
    var cancelLandsAnyway = false
    var routineReads = 0

    override suspend fun routines(): List<Routine> { routineReads++; return table }

    override suspend fun setRoutines(list: List<Routine>): List<Routine> {
        puts += list
        failWrite?.let { throw it }
        val before = table.associateBy { it.id }
        table = list.mapIndexed { index, routine ->
            val id = routine.id.ifEmpty { "rt_new$index" }
            val hub = before[id]
            routine.copy(id = id, createdBy = hub?.createdBy, lastRun = hub?.lastRun, lastSessionId = hub?.lastSessionId,
                lastStatus = hub?.lastStatus, lastError = hub?.lastError,
                nextRun = if (routine.enabled) "2026-11-03T01:00:00Z" else null)
        }
        if (loseAnswer) throw IOException("connection reset")
        return table
    }

    override suspend fun runRoutine(id: String): SessionKey {
        failRun?.let { throw it }
        started += id
        return SessionKey("server", "run-$id")
    }

    override suspend fun followups() = followups

    override suspend fun cancelFollowup(id: String) {
        failCancel?.let {
            if (cancelLandsAnyway) followups = followups.filterNot { f -> f.id == id }
            throw it
        }
        followups = followups.filterNot { it.id == id }
    }

    override suspend fun runFollowup(id: String): SessionKey {
        failRun?.let { throw it }
        val followup = followups.first { it.id == id }
        followups = followups - followup
        started += id
        return SessionKey(followup.machineId.orEmpty(), followup.sessionId ?: "new-$id")
    }

    override suspend fun runs() = runs
    override suspend fun assistantMachine() = "server"
}

private val byPersona = Routine(
    id = "rt_weekly", title = "周报", prompt = "起草周报", time = "17:00", weekdays = listOf(5), enabled = true,
    createdBy = "assistant", lastRun = "2026-10-30T09:00:00Z", lastSessionId = "conversation-1", lastStatus = "failed",
    lastError = "assistant machine is offline", nextRun = "2026-11-06T09:00:00Z",
)
private val morning = Routine(
    id = "morning", title = "早报", prompt = "给我今天的早报", time = "09:00", weekdays = listOf(1, 2, 3, 4, 5), enabled = true,
    lastRun = "2026-11-02T01:00:00Z", lastSessionId = "conversation-2", lastStatus = "ok", nextRun = "2026-11-03T01:00:00Z",
)
private val review = Followup(id = "fu_review", at = "2026-11-02T03:00:00Z", title = "会前准备", prompt = "整理纪要",
    machineId = "server", sessionId = "conversation-3")
private val ci = Followup(id = "fu_ci", at = "2026-11-02T05:00:00Z", prompt = "看看 CI 跑完没有\n跑完告诉我", machineId = "server")

/** The page's words in the tests' language (Simplified Chinese, robolectric.properties). */
private fun pageText() = ProactiveText(ApplicationProvider.getApplicationContext())

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ProactiveStoreTest {
    private val say by lazy { pageText() }

    private fun TestScope.messagesOf(store: ProactiveStore): List<String> {
        val messages = mutableListOf<String>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { store.messages.toList(messages) }
        return messages
    }

    @Test fun wholeTablePutCarriesEveryRoutineAndChangesOnlyTheSwitch() = runTest {
        val hub = FakeHub(listOf(byPersona, morning))
        val store = ProactiveStore(hub, this, say)
        store.refresh()
        // Persona sets up another routine while the page is open; the write must not wipe it.
        val added = Routine(id = "rt_added", title = "巡检", prompt = "看看服务", time = "22:00", createdBy = "assistant")
        hub.table = hub.table + added
        store.toggle("morning", false)
        advanceUntilIdle()
        assertEquals(listOf(byPersona, morning.copy(enabled = false), added), hub.puts.single())
        val shown = store.state.value.shownRoutines.orEmpty()
        assertEquals(listOf("rt_weekly", "morning", "rt_added"), shown.map { it.id })
        assertEquals("assistant", shown.first().createdBy)
        assertFalse(shown[1].enabled)
    }

    @Test fun switchMovesAtOnceAndAFailedWriteShowsTheHubsValue() = runTest {
        val hub = FakeHub(listOf(morning))
        val store = ProactiveStore(hub, this, say)
        val messages = messagesOf(store)
        store.refresh()
        hub.failWrite = IOException("no route")
        store.toggle("morning", false)
        assertFalse(store.state.value.shownRoutines!!.single().enabled)
        advanceUntilIdle()
        assertTrue(store.state.value.pending.isEmpty())
        assertTrue(store.state.value.shownRoutines!!.single().enabled)
        assertEquals(listOf("没保存上：连不上 hub"), messages)
    }

    @Test fun aLostAnswerIsNotAFailureWhenTheHubHasTheChange() = runTest {
        val hub = FakeHub(listOf(morning))
        val store = ProactiveStore(hub, this, say)
        val messages = messagesOf(store)
        store.refresh()
        hub.loseAnswer = true
        store.toggle("morning", false)
        advanceUntilIdle()
        assertFalse(store.state.value.shownRoutines!!.single().enabled)
        assertTrue(messages.isEmpty())
    }

    @Test fun editWritesTheUserFieldsAndKeepsTheRest() = runTest {
        val hub = FakeHub(listOf(byPersona, morning))
        val store = ProactiveStore(hub, this, say)
        store.refresh()
        val draft = RoutineDraft("  周报（草稿）  ", " 先给我草稿 ", "16:30", (1..7).toList(), enabled = false)
        assertTrue(store.save(RoutineChange.Edit("rt_weekly", byPersona.draft(), draft)).isSuccess)
        val written = hub.puts.single()
        assertEquals(byPersona.copy(title = "周报（草稿）", prompt = "先给我草稿", time = "16:30", weekdays = emptyList(), enabled = false), written[0])
        assertEquals(morning, written[1])
        // The hub kept its own fields; the page shows what it answered.
        val shown = store.state.value.shownRoutines!!.first()
        assertEquals("assistant", shown.createdBy)
        assertEquals("failed", shown.lastStatus)
        assertNull(shown.nextRun)
    }

    @Test fun addAppendsTheNewRoutineAndARetryAfterALostAnswerDoesNotAddItTwice() = runTest {
        val hub = FakeHub(listOf(morning))
        val store = ProactiveStore(hub, this, say)
        val messages = messagesOf(store)
        store.refresh()
        val id = newRoutineId()
        assertTrue(id.matches(Regex("rt_[0-9a-f]{8}")))
        val saved = store.save(RoutineChange.Add(id, RoutineTemplates.first().draft(say))).getOrThrow()
        val written = hub.puts.single().last()
        assertEquals(id, written.id)
        assertEquals("会前准备", written.title)
        assertEquals("08:45", written.time)
        assertEquals(listOf(1, 2, 3, 4, 5), written.weekdays)
        assertNull(written.createdBy)
        assertEquals(listOf("morning", id), saved.map { it.id })

        // The next one lands but its answer is lost: that is still a success, and pressing 添加 again adds nothing.
        hub.loseAnswer = true
        val other = newRoutineId()
        assertTrue(store.save(RoutineChange.Add(other, BlankRoutine.copy(title = "提醒", prompt = "提醒我喝水"))).isSuccess)
        hub.loseAnswer = false
        assertTrue(store.save(RoutineChange.Add(other, BlankRoutine.copy(title = "提醒", prompt = "提醒我喝水"))).isSuccess)
        assertEquals(listOf("morning", id, other), hub.table.map { it.id })
        assertTrue(messages.isEmpty())
    }

    @Test fun anEditWritesOnlyWhatThePersonChanged() = runTest {
        val hub = FakeHub(listOf(byPersona, morning))
        val store = ProactiveStore(hub, this, say)
        store.refresh()
        val opened = morning.draft()
        // While the page is open Persona moves the routine to 08:00 from a conversation; the person edits the prompt.
        hub.table = listOf(byPersona, morning.copy(time = "8:00"))
        assertTrue(store.save(RoutineChange.Edit("morning", opened, opened.copy(prompt = "早报，三条以内"))).isSuccess)
        val written = hub.puts.single()[1]
        assertEquals("8:00", written.time)
        assertEquals("早报，三条以内", written.prompt)
        assertEquals("09:00", morning.copy(time = "9:00").draft().time)
        // Nothing changed: nothing is written.
        assertTrue(store.save(RoutineChange.Edit("morning", opened, opened)).isSuccess)
        assertEquals(1, hub.puts.size)
    }

    @Test fun editingARoutineDeletedMeanwhileDoesNotBringItBack() = runTest {
        val hub = FakeHub(listOf(byPersona, morning))
        val store = ProactiveStore(hub, this, say)
        val messages = messagesOf(store)
        store.refresh()
        hub.table = listOf(morning)
        val result = store.save(RoutineChange.Edit("rt_weekly", byPersona.draft(), byPersona.draft().copy(title = "改个名")))
        assertTrue(result.exceptionOrNull() is RoutineGoneException)
        assertTrue(hub.puts.isEmpty())
        assertEquals(listOf("morning"), store.state.value.shownRoutines!!.map { it.id })
        assertEquals(listOf("没保存上：这个定时任务已经不在了，可能刚被删掉"), messages)
    }

    @Test fun deleteTakesOutOnlyThatRoutine() = runTest {
        val hub = FakeHub(listOf(byPersona, morning))
        val store = ProactiveStore(hub, this, say)
        store.refresh()
        assertTrue(store.save(RoutineChange.Delete("rt_weekly")).isSuccess)
        assertEquals(listOf(morning), hub.puts.single())
        assertTrue(store.state.value.busy.isEmpty())
        // Already gone: nothing to write.
        assertTrue(store.save(RoutineChange.Delete("rt_weekly")).isSuccess)
        assertEquals(1, hub.puts.size)
    }

    @Test fun cancellingAFollowupRemovesItAndAFailureKeepsIt() = runTest {
        val hub = FakeHub(listOf(morning), listOf(review, ci))
        val store = ProactiveStore(hub, this, say)
        val messages = messagesOf(store)
        store.refresh()
        assertTrue(store.cancelFollowup("fu_ci").isSuccess)
        assertEquals(listOf("fu_review"), store.state.value.followupList!!.map { it.id })

        hub.failCancel = IOException("timeout")
        assertTrue(store.cancelFollowup("fu_review").isFailure)
        assertEquals(listOf("fu_review"), store.state.value.followupList!!.map { it.id })
        assertEquals(listOf("没能取消这个跟进：连不上 hub"), messages)

        // The answer was lost but the hub did cancel it: that is what the user asked for.
        hub.cancelLandsAnyway = true
        assertTrue(store.cancelFollowup("fu_review").isSuccess)
        assertTrue(store.state.value.followupList!!.isEmpty())
        assertEquals(1, messages.size)
        assertTrue(store.state.value.busy.isEmpty())
    }

    @Test fun runningAFollowupNowOpensItsConversationAndTakesItOffTheList() = runTest {
        val hub = FakeHub(listOf(morning), listOf(review, ci))
        val store = ProactiveStore(hub, this, say)
        store.refresh()
        assertEquals(SessionKey("server", "conversation-3"), store.runFollowup("fu_review").getOrThrow())
        assertEquals(listOf("fu_review"), hub.started)
        assertEquals(listOf("fu_ci"), store.state.value.followupList!!.map { it.id })
        advanceUntilIdle()
        assertEquals(listOf("fu_ci"), store.state.value.followupList!!.map { it.id })
        assertFalse(store.state.value.followupBusy("fu_review"))
    }

    @Test fun runningARoutineNowReturnsTheNewConversationOrSaysWhy() = runTest {
        val hub = FakeHub(listOf(morning))
        val store = ProactiveStore(hub, this, say)
        val messages = messagesOf(store)
        store.refresh()
        assertEquals(SessionKey("server", "run-morning"), store.runRoutine("morning").getOrThrow())
        assertFalse(store.state.value.routineBusy("morning"))
        hub.failRun = IOException("refused")
        assertTrue(store.runRoutine("morning").isFailure)
        assertEquals(listOf("没跑起来：连不上 hub"), messages)
        advanceUntilIdle()
        assertTrue(hub.routineReads >= 3)
    }

    @Test fun refreshKeepsWhatWasReadWhenAListFails() = runTest {
        val hub = object : ProactiveApi by FakeHub(listOf(morning), listOf(review)) {
            var fail = false
            override suspend fun followups(): List<Followup> = if (fail) throw IOException("down") else listOf(review)
        }
        val store = ProactiveStore(hub, this, say)
        store.refresh()
        hub.fail = true
        store.refresh()
        val state = store.state.value
        assertEquals(listOf(review), state.followups.value)
        assertEquals("连不上 hub，连上后下拉刷新。", state.followups.error)
        assertNull(state.routines.error)
        assertFalse(state.refreshing)
        assertEquals("server", state.assistantMachine)
    }

    @Test fun theTransportsOwnCancellationIsAFailureNotAStuckPage() = runTest {
        val hub = object : ProactiveApi by FakeHub(listOf(morning)) {
            override suspend fun setRoutines(list: List<Routine>): List<Routine> =
                throw kotlinx.coroutines.CancellationException("Pairing changed")
        }
        val store = ProactiveStore(hub, this, say)
        val messages = messagesOf(store)
        store.refresh()
        val result = store.save(RoutineChange.Delete("morning"))
        assertTrue(result.isFailure)
        assertTrue(store.state.value.busy.isEmpty())
        assertEquals(listOf("没删掉：连接刚换过，请再试一次"), messages)
    }

    @Test fun runsAreNewestFirstAndOneRoutinesHistoryIsItsOwn() = runTest {
        val runs = listOf(
            Run(id = "1", kind = "routine", refId = "morning", at = "2026-11-01T01:00:00Z", status = "ok"),
            Run(id = "2", kind = "followup", refId = "fu_lunch", at = "2026-11-02T04:00:00Z", status = "ok"),
            Run(id = "3", kind = "routine", refId = "morning", at = "2026-11-02T01:00:00Z", status = "ok"),
        )
        val store = ProactiveStore(FakeHub(listOf(morning), runs = runs), this, say)
        store.refresh()
        assertEquals(listOf("2", "3", "1"), store.state.value.runList!!.map { it.id })
        assertEquals(listOf("3", "1"), store.state.value.runsOf("morning")!!.map { it.id })
    }
}

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ProactiveTextTest {
    private val say by lazy { pageText() }
    private val zone = java.time.ZoneId.of("Asia/Shanghai")
    private val now = java.time.Instant.parse("2026-11-02T02:00:00Z") // Monday 10:00 in Shanghai

    @Test fun rulesReadLikeReminders() {
        assertEquals("每个工作日 上午 9:00", say.sentence("09:00", listOf(5, 4, 3, 2, 1)))
        assertEquals("每天 晚上 9:30", say.sentence("21:30", (1..7).toList()))
        assertEquals("每天 早上 7:05", say.sentence("07:05", emptyList()))
        assertEquals("每个周末 中午 12:00", say.sentence("12:00", listOf(6, 7)))
        assertEquals("每周一、三、五 下午 5:00", say.sentence("17:00", listOf(5, 1, 3, 3)))
        assertEquals("凌晨 12:15", say.clock("00:15"))
        assertEquals("工作日", say.repeats(listOf(1, 2, 3, 4, 5)))
        assertEquals(emptyList<Int>(), normalizeDays(listOf(7, 6, 5, 4, 3, 2, 1, 9)))
        assertEquals(listOf(1, 3), normalizeDays(listOf(3, 1, 3, 0)))
        assertTrue(validTime("23:59"))
        assertFalse(validTime("24:00"))
        assertFalse(validTime("9:00"))
    }

    @Test fun hubTimesWithAnOffsetOrZoneZero() {
        assertEquals(java.time.Instant.parse("2026-11-03T01:00:00Z"), parseTime("2026-11-03T09:00:00+08:00"))
        assertEquals(java.time.Instant.parse("2026-11-03T01:00:00Z"), parseTime("2026-11-03T01:00:00Z"))
        assertEquals(java.time.Instant.parse("2026-11-03T01:00:00.5Z"), parseTime("2026-11-03T09:00:00.5+08:00"))
        assertNull(parseTime("0001-01-01T00:00:00Z"))
        assertNull(parseTime("not a time"))
        assertNull(parseTime(null))
        assertEquals("09:05", normalizeTime("9:05"))
        assertEquals("21:30", normalizeTime("21:30"))
        assertTrue(RoutineDraft("早报", "做", "9:00").valid)
    }

    @Test fun momentsAndCountdowns() {
        val at = { text: String -> java.time.Instant.parse(text) }
        assertEquals("今天 15:30", say.moment(at("2026-11-02T07:30:00Z"), now, zone))
        assertEquals("明天 09:00", say.moment(at("2026-11-03T01:00:00Z"), now, zone))
        assertEquals("昨天 21:30", say.moment(at("2026-11-01T13:30:00Z"), now, zone))
        assertEquals("周五 17:00", say.moment(at("2026-11-06T09:00:00Z"), now, zone))
        assertEquals("11月20日 09:00", say.moment(at("2026-11-20T01:00:00Z"), now, zone))
        assertEquals("马上", say.until(at("2026-11-02T02:00:30Z"), now, zone))
        assertEquals("40 分钟后", say.until(at("2026-11-02T02:40:00Z"), now, zone))
        assertEquals("2 小时 30 分钟后", say.until(at("2026-11-02T04:30:00Z"), now, zone))
        assertEquals("7 小时后", say.until(at("2026-11-02T09:05:00Z"), now, zone))
        assertEquals("4 天后", say.until(at("2026-11-06T09:00:00Z"), now, zone))
        assertEquals("已过时间", say.until(at("2026-11-02T01:00:00Z"), now, zone))
        assertEquals("准时", say.lateness(at("2026-11-02T01:00:00Z"), at("2026-11-02T01:00:01Z")))
        assertEquals("晚了 21 秒", say.lateness(at("2026-11-02T01:00:00Z"), at("2026-11-02T01:00:21Z")))
        assertEquals("晚了 45 分钟", say.lateness(at("2026-11-02T01:00:00Z"), at("2026-11-02T01:45:00Z")))
        assertNull(say.lateness(null, now))
    }

    @Test fun runsAndRoutinesSayWhatHappened() {
        assertEquals("准时", Run(status = "ok", due = "2026-11-02T01:00:00Z", at = "2026-11-02T01:00:02Z").detail(say))
        assertEquals("手动运行", Run(status = "ok", manual = true).detail(say))
        assertEquals("没跑起来：助手所在的机器离线（手动运行）", Run(status = "failed", error = "assistant machine is offline", manual = true).detail(say))
        assertEquals("跳过了：Persona 停机，错过了时间", Run(status = "skipped", error = "Persona 停机，错过了时间").detail(say))
        assertEquals("已跳过", Run(status = "skipped").detail(say))
        assertEquals(SessionKey("server", "s"), Run(sessionId = "s").conversation("server"))
        assertNull(Run(sessionId = "s").conversation(null))
        assertEquals("跟进", Run(kind = "followup").displayTitle(say))
        val noHistory = ProactiveState(assistantMachine = "server")
        assertEquals("上次 周五 17:00 没跑起来：助手所在的机器离线", noHistory.lastRunOf(byPersona)!!.line(say, now, zone))
        assertEquals("上次 今天 09:00 已运行", noHistory.lastRunOf(morning)!!.line(say, now, zone))
        assertEquals(SessionKey("server", "conversation-2"), noHistory.lastRunOf(morning)!!.conversation)
        assertNull(noHistory.lastRunOf(Routine()))
        // 立即运行 does not move the hub's last_run: the history knows better.
        val manual = Run(id = "m", kind = "routine", refId = "morning", at = "2026-11-02T01:30:00Z", status = "failed",
            error = "assistant machine is offline", manual = true, machineId = "laptop", sessionId = "")
        val withHistory = ProactiveState(runs = Loadable(listOf(manual)), assistantMachine = "server")
        assertEquals("上次 今天 09:30 没跑起来：助手所在的机器离线", withHistory.lastRunOf(morning)!!.line(say, now, zone))
        assertTrue(withHistory.lastRunOf(morning)!!.failed)
        assertNull(withHistory.lastRunOf(morning)!!.conversation)
        val ran = ProactiveState(runs = Loadable(listOf(manual.copy(status = "ok", sessionId = "s1"))))
        assertEquals("上次 今天 09:30 手动运行", ran.lastRunOf(morning)!!.line(say, now, zone))
        assertEquals(SessionKey("laptop", "s1"), ran.lastRunOf(morning)!!.conversation)
        assertEquals("下次 明天 09:00，23 小时后", morning.nextLine(say, now, zone))
        assertEquals("已关闭，不会自动运行", morning.copy(enabled = false).nextLine(say, now, zone))
        assertEquals("这个跟进已经不在了", hubErrorText(say, " no such follow-up "))
        assertEquals("routine \"x\" needs a title", hubErrorText(say, "routine \"x\" needs a title"))
    }

    @Test fun followupsSayWhereTheyGo() {
        val machines = listOf(Machine("server", name = "服务器"))
        assertEquals("会前准备", review.displayTitle(say))
        assertEquals("看看 CI 跑完没有", ci.displayTitle(say))
        assertEquals("在 服务器 上接着原来的对话", review.whereLine(say, machines))
        assertEquals("在 服务器 上开一段新对话", ci.whereLine(say, machines))
        assertEquals("在 laptop 上开一段新对话", ci.copy(machineId = "laptop").whereLine(say, machines))
        assertEquals(SessionKey("server", "conversation-3"), review.conversation())
        assertNull(ci.conversation())
    }

    @Test fun timelineAndSummary() {
        val off = morning.copy(id = "evening", title = "晚间回顾", enabled = false, nextRun = "2026-11-02T13:30:00Z")
        val list = upcoming(say, listOf(byPersona, morning, off), listOf(ci, review))
        assertEquals(listOf("f-fu_review", "f-fu_ci", "r-morning", "r-rt_weekly"), list.map { it.id })
        assertEquals("跟进，接着原来的对话", list.first().detail)
        assertEquals("每个工作日 上午 9:00", list[2].detail)
        assertEquals("2 个定时任务开着，下一件在今天 11:00。", summaryLine(say, listOf(byPersona, morning, off), listOf(review, ci), now, zone))
        assertEquals("定时任务都关着，下一件在今天 11:00。", summaryLine(say, listOf(off), listOf(review), now, zone))
        assertEquals("还没有安排，Persona 只在你开口时做事。", summaryLine(say, emptyList(), emptyList(), now, zone))
        assertNull(summaryLine(say, null, null, now, zone))
    }

    @Test fun pagesSurviveAsKeys() {
        assertEquals(listOf(ProactivePage.Overview), startPages(null))
        assertEquals(listOf(ProactivePage.Overview, ProactivePage.EditRoutine("morning")), startPages("routine:morning"))
        assertEquals(listOf(ProactivePage.Overview, ProactivePage.AddRoutine(-1)), startPages("new"))
        assertEquals(listOf(ProactivePage.Overview, ProactivePage.RunList("morning")), startPages("runs:morning"))
        assertEquals(listOf(ProactivePage.Overview), startPages("nonsense"))
        listOf(ProactivePage.Overview, ProactivePage.EditRoutine("a:b"), ProactivePage.AddRoutine(2), ProactivePage.FollowupList,
            ProactivePage.RunList(null), ProactivePage.RunList("x")).forEach { assertEquals(it, ProactivePage.parse(it.key)) }
    }

    @Test fun draftsCompareByWhatTheHubStores() {
        assertTrue(RoutineDraft(" 早报 ", "做", "09:00", (1..7).toList()).sameAs(RoutineDraft("早报", "做 ", "09:00", emptyList())))
        assertFalse(RoutineDraft("早报", "做", "09:00").sameAs(RoutineDraft("早报", "做", "09:30")))
        assertFalse(RoutineDraft(" ", "做").valid)
        assertTrue(RoutineDraft("早报", "做").valid)
        assertEquals(RoutineDraft("周报", "起草周报", "17:00", listOf(5), true), byPersona.draft())
    }
}
