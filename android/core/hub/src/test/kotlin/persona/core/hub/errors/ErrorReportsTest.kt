package persona.core.hub.errors

import java.io.File
import java.io.IOException
import java.nio.file.Files
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The Android error reporter's pure parts (docs/android/android.md "Error reports"). */
class ErrorReportsTest {
    private val context = ErrorReportContext(release = "1.4.0", build = "512", osVersion = "16", deviceModel = "Pixel 9")
    private var ids = 0
    private val builder = ErrorReportBuilder(context) { "id-${++ids}" }
    private val directory: File = Files.createTempDirectory("error-reports").toFile()
    private var now = 1_800_000_000_000L
    private val queue = ErrorReportQueue(directory) { now }

    @After fun cleanUp() { directory.deleteRecursively() }

    private fun element(className: String, method: String, file: String?, line: Int) =
        StackTraceElement(className, method, file, line)

    // Mapping

    @Test fun mapsTheCauseChainOutermostFirstWithFramesInnermostLast() {
        val cause = IllegalArgumentException("bad input").apply {
            stackTrace = arrayOf(element("persona.feature.inbox.Parser", "parse", "Parser.kt", 12), element("java.lang.Thread", "run", "Thread.java", 1000))
        }
        val error = IllegalStateException("load failed", cause).apply {
            stackTrace = arrayOf(
                element("persona.feature.inbox.InboxViewModel", "load", "InboxViewModel.kt", 88),
                element("kotlinx.coroutines.DispatchedTask", "run", "DispatchedTask.kt", 100),
                element("android.os.Handler", "dispatchMessage", null, -2),
            )
        }
        val report = builder.fromThrowable(error, kind = "crash", level = "fatal", timeMillis = 0L, previousLaunch = true)

        assertEquals("android", report.source)
        assertEquals("crash", report.kind)
        assertEquals("fatal", report.level)
        assertEquals("1970-01-01T00:00:00Z", report.time)
        assertEquals("1.4.0", report.release)
        assertEquals("512", report.build)
        assertEquals("android", report.os)
        assertEquals("16", report.osVersion)
        assertEquals("Pixel 9", report.deviceModel)
        assertEquals(true, report.previousLaunch)
        assertEquals("IllegalStateException: load failed", report.message)

        val exceptions = report.exceptions!!
        assertEquals(listOf("java.lang.IllegalStateException", "java.lang.IllegalArgumentException"), exceptions.map { it.type })
        assertEquals("load failed", exceptions[0].value)
        val frames = exceptions[0].frames!!
        // Sentry's order: the throwing frame last.
        assertEquals(listOf("dispatchMessage", "run", "load"), frames.map { it.function })
        val last = frames.last()
        assertEquals("persona.feature.inbox.InboxViewModel", last.module)
        assertEquals("InboxViewModel.kt", last.file)
        assertEquals(88, last.line)
        assertEquals(true, last.inApp)
        assertEquals(false, frames[1].inApp)
        assertNull("a native frame has no line", frames[0].line)
        assertNull(frames[0].file)
    }

    @Test fun stopsAtACycleInTheCauses() {
        val first = RuntimeException("a")
        val second = RuntimeException("b", first)
        first.initCause(second)
        assertEquals(2, ErrorReportBuilder.causeChain(first).size)
    }

    @Test fun keepsTheFramesNearestTheThrowWhenTheTraceIsLong() {
        val trace = Array(200) { element("persona.Deep", "level$it", "Deep.kt", it + 1) }
        val frames = ErrorReportBuilder.framesOf(trace)
        assertEquals(ErrorReportBuilder.MaxFrames, frames.size)
        assertEquals("level0", frames.last().function)
    }

    @Test fun truncatesTheMessageToOneKilobyte() {
        val error = RuntimeException("x".repeat(5000))
        val report = builder.fromThrowable(error, "crash", "fatal", 0L)
        assertEquals(ErrorReportBuilder.MaxMessage, report.message!!.length)
        assertTrue(report.message!!.startsWith("RuntimeException: xxx"))
        assertTrue(report.message!!.endsWith("…"))
        assertEquals(ErrorReportBuilder.MaxMessage, report.exceptions!![0].value!!.length)
        assertEquals("NullPointerException", ErrorReportBuilder.describe(NullPointerException()))
    }

    @Test fun breadcrumbsKeepTheLastScreensWithoutRepeats() {
        val crumbs = ErrorBreadcrumbs(capacity = 3) { 0L }
        listOf("AgentsRoute", "AgentsRoute", "ChatRoute", "InboxRoute", "SettingsRoute").forEach(crumbs::navigation)
        assertEquals(listOf("ChatRoute", "InboxRoute", "SettingsRoute"), crumbs.snapshot().map { it.message })
        assertEquals("navigation", crumbs.snapshot().first().category)
    }

    // Queue

    @Test fun aWrittenCrashReadsBackTheSame() {
        val report = builder.fromThrowable(IllegalStateException("boom"), "crash", "fatal", now, previousLaunch = true)
        assertTrue(queue.write(report))
        val pending = queue.pending()
        assertEquals(1, pending.size)
        assertEquals(report, pending.single().report)
        val wire = Json.parseToJsonElement(pending.single().json) as JsonObject
        assertEquals("\"android\"", wire["source"].toString())
        assertEquals("true", wire["previous_launch"].toString())
        assertFalse("never sends the manager's fields", "fingerprint" in wire)
    }

    @Test fun keepsAtMostFiftyAndNothingOlderThanAWeek() {
        val day = 24L * 60 * 60 * 1000
        queue.write(builder.report("crash", "fatal", now - 8 * day, "old"))
        repeat(60) { queue.write(builder.report("error", "error", now - 60 + it, "n$it")) }
        File(directory, "garbage.json").writeText("not json")
        File(directory, "${now - 1000}-broken.json").writeText("{")
        val pending = queue.pending()
        assertEquals(ErrorReportQueue.MaxReports, pending.size)
        assertEquals("the newest are kept", "n59", pending.last().report.message)
        assertEquals("n10", pending.first().report.message)
        assertEquals(ErrorReportQueue.MaxReports, directory.listFiles()!!.size)
    }

    // Exit reasons

    @Test fun reportsEachExitOnceAndSkipsJavaCrashesTheHandlerWrote() {
        val exits = listOf(
            ExitRecord(1_000, ExitRecord.Kind.Anr),
            ExitRecord(2_000, ExitRecord.Kind.NativeCrash),
            ExitRecord(3_000, ExitRecord.Kind.JavaCrash),
            ExitRecord(4_000, ExitRecord.Kind.JavaCrash),
            ExitRecord(5_000, ExitRecord.Kind.Other),
        )
        // First run: remembers, reports nothing from before.
        val first = ExitReasonPlanner.plan(exits, lastSeen = null, javaCrashTimes = emptyList(), builder = builder)
        assertTrue(first.reports.isEmpty())
        assertEquals(5_000L, first.lastSeen)

        val plan = ExitReasonPlanner.plan(exits, lastSeen = 1_500, javaCrashTimes = listOf(3_995), builder = builder)
        assertEquals(listOf("crash", "crash"), plan.reports.map { it.kind })
        assertEquals(listOf("Native crash", "Crash"), plan.reports.map { it.message })
        assertEquals("the one without a handler crash", "1970-01-01T00:00:03Z", plan.reports[1].time)
        assertTrue(plan.reports.all { it.previousLaunch == true && it.level == "fatal" })
        assertEquals(5_000L, plan.lastSeen)

        val again = ExitReasonPlanner.plan(exits, lastSeen = plan.lastSeen, javaCrashTimes = listOf(3_995), builder = builder)
        assertTrue(again.reports.isEmpty())
    }

    @Test fun anAnrCarriesTheMainThreadFramesOnly() {
        val trace = """
            ----- pid 1234 at 2026-11-06 09:00:00 -----
            "Signal Catcher" daemon prio=10 tid=2 Runnable
              at java.lang.Object.wait(Native method)

            "main" prio=5 tid=1 Sleeping
              | group="main" sCount=1 ucsCount=0 flags=1 obj=0x0 self=0x0
              at java.lang.Thread.sleep(Native method)
              - sleeping on <0x0abc> (a java.lang.Object)
              at persona.feature.inbox.InboxViewModel.load(InboxViewModel.kt:88)
              at android.os.Handler.dispatchMessage(Handler.java:106)
              native: #00 pc 000000 /system/lib64/libc.so (nanosleep+8)

            "worker" prio=5 tid=3 Waiting
              at persona.other.Worker.run(Worker.kt:5)
        """.trimIndent()
        val exits = listOf(ExitRecord(9_000, ExitRecord.Kind.Anr, trace = { trace.byteInputStream() }))
        val report = ExitReasonPlanner.plan(exits, 0, emptyList(), builder).reports.single()
        assertEquals("anr", report.kind)
        assertEquals("error", report.level)
        val frames = report.exceptions!!.single().frames!!
        assertEquals(listOf("dispatchMessage", "load", "sleep"), frames.map { it.function })
        assertEquals(listOf("android.os.Handler", "persona.feature.inbox.InboxViewModel", "java.lang.Thread"), frames.map { it.module })
        assertEquals(88, frames[1].line)
        assertEquals(true, frames[1].inApp)
        assertNull(frames[2].file)
    }

    @Test fun anAnrWithoutATraceStillReports() {
        val exits = listOf(ExitRecord(9_000, ExitRecord.Kind.Anr, trace = { throw IOException("gone") }))
        val report = ExitReasonPlanner.plan(exits, 0, emptyList(), builder).reports.single()
        assertEquals("Application Not Responding", report.message)
        assertNull(report.exceptions!!.single().frames)
    }

    // Sending

    private class FakeTransport(private val answers: MutableList<() -> Pair<Int, String>>) : ErrorReportTransport {
        val bodies = mutableListOf<String>()
        override suspend fun post(body: String): Pair<Int, String> { bodies += body; return answers.removeAt(0)() }
    }

    private fun fill(count: Int) = repeat(count) { queue.write(builder.report("error", "error", now - count + it, "r$it")) }

    @Test fun sendsNothingWithoutTheFeatureAndClearsTheQueue() = runBlocking {
        fill(3)
        val transport = FakeTransport(mutableListOf())
        val result = ErrorReportSender(queue).flush(setOf("app_ping"), transport)
        assertTrue(result.cleared)
        assertTrue(transport.bodies.isEmpty())
        assertTrue(directory.listFiles().orEmpty().isEmpty())
    }

    @Test fun sendsInBatchesOfTwentyAndDeletesOnSuccess() = runBlocking {
        fill(45)
        val ok = { 202 to """{"enabled":true,"accepted":20,"dropped":0}""" }
        val transport = FakeTransport(mutableListOf(ok, ok, ok))
        val result = ErrorReportSender(queue).flush(setOf(ErrorReportsFeature), transport)
        assertEquals(45, result.sent)
        assertEquals(listOf(20, 20, 5), transport.bodies.map { (Json.parseToJsonElement(it) as JsonObject)["reports"]!!.jsonArray.size })
        assertTrue(queue.pending().isEmpty())
    }

    @Test fun keepsTheQueueOnServerErrorsAndNoAnswer() = runBlocking {
        fill(3)
        val sender = ErrorReportSender(queue)
        assertEquals(3, sender.flush(setOf(ErrorReportsFeature), FakeTransport(mutableListOf({ 503 to "" }))).kept)
        assertEquals(3, sender.flush(setOf(ErrorReportsFeature), FakeTransport(mutableListOf({ throw IOException("offline") }))).kept)
        assertEquals(3, sender.flush(setOf(ErrorReportsFeature), FakeTransport(mutableListOf({ 429 to "" }))).kept)
        assertEquals(3, queue.pending().size)
    }

    @Test fun dropsWhatTheHubRefuses() = runBlocking {
        fill(3)
        val result = ErrorReportSender(queue).flush(setOf(ErrorReportsFeature), FakeTransport(mutableListOf({ 400 to """{"error":"bad"}""" })))
        assertEquals(3, result.dropped)
        assertTrue(queue.pending().isEmpty())
    }

    @Test fun aHubThatTurnedTheFeatureOffClearsTheRest() = runBlocking {
        fill(30)
        val result = ErrorReportSender(queue).flush(setOf(ErrorReportsFeature),
            FakeTransport(mutableListOf({ 200 to """{"enabled":false,"accepted":0}""" })))
        assertTrue(result.cleared)
        assertTrue(directory.listFiles().orEmpty().isEmpty())
    }
}
