package persona.core.hub.errors

import java.io.BufferedReader
import java.io.InputStream
import persona.core.contracts.generated.ExtensionErrorException
import persona.core.contracts.generated.ExtensionErrorFrame
import persona.core.contracts.generated.ExtensionErrorReport

/**
 * One of `ActivityManager.getHistoricalProcessExitReasons`, reduced to what the reports use. [trace] opens the ANR
 * trace when there is one.
 */
data class ExitRecord(
    val timestampMillis: Long,
    val reason: Kind,
    val trace: (() -> InputStream?)? = null,
) {
    enum class Kind { Anr, NativeCrash, JavaCrash, Other }
}

/**
 * Which earlier exits become reports (docs/android/android.md "Error reports"). Each exit is reported once: [lastSeen] is the
 * newest timestamp handled before, null on the very first run, which only remembers where it is (exits from before the
 * reporter existed are not sent). A Java crash the uncaught-exception handler already wrote is not reported again:
 * the handler's crash time is within [JavaCrashWindowMillis] before the exit's.
 */
object ExitReasonPlanner {
    const val JavaCrashWindowMillis = 10_000L

    data class Plan(val reports: List<ExtensionErrorReport>, val lastSeen: Long?)

    fun plan(
        exits: List<ExitRecord>, lastSeen: Long?, javaCrashTimes: List<Long>, builder: ErrorReportBuilder,
    ): Plan {
        val newest = exits.maxOfOrNull { it.timestampMillis }
        if (lastSeen == null) return Plan(emptyList(), newest)
        val reports = exits.filter { it.timestampMillis > lastSeen }.sortedBy { it.timestampMillis }.mapNotNull { exit ->
            when (exit.reason) {
                ExitRecord.Kind.Anr -> {
                    val frames = exit.trace?.let { open -> runCatching { open()?.use(AnrTrace::mainThreadFrames) }.getOrNull() }
                    builder.report(
                        kind = "anr", level = "error", timeMillis = exit.timestampMillis, previousLaunch = true,
                        message = "Application Not Responding",
                        exceptions = listOf(ExtensionErrorException(type = "ANR", value = "Main thread blocked",
                            frames = frames?.ifEmpty { null })),
                    )
                }
                ExitRecord.Kind.NativeCrash -> builder.report(
                    kind = "crash", level = "fatal", timeMillis = exit.timestampMillis, previousLaunch = true,
                    message = "Native crash",
                )
                ExitRecord.Kind.JavaCrash -> if (javaCrashTimes.any { exit.timestampMillis - it in 0..JavaCrashWindowMillis }) null
                    else builder.report(
                        kind = "crash", level = "fatal", timeMillis = exit.timestampMillis, previousLaunch = true,
                        message = "Crash",
                    )
                ExitRecord.Kind.Other -> null
            }
        }
        return Plan(reports, maxOf(lastSeen, newest ?: lastSeen))
    }
}

/**
 * The main thread's Java frames from an ANR trace (`"main" prio=5 tid=1 …`, then `  at a.b.C.m(C.kt:12)` lines until
 * a blank line). Only those frames are kept; the rest of the trace (other threads, native frames, locks) never leaves
 * the phone.
 */
object AnrTrace {
    private val frameLine = Regex("""^\s*at (.+)\.([^.(]+)\((.*)\)\s*$""")
    private const val MaxLines = 20_000

    fun mainThreadFrames(input: InputStream): List<ExtensionErrorFrame> =
        mainThreadFrames(input.bufferedReader(Charsets.UTF_8))

    fun mainThreadFrames(reader: BufferedReader): List<ExtensionErrorFrame> {
        val frames = mutableListOf<ExtensionErrorFrame>()
        var inMain = false
        var lines = 0
        while (lines++ < MaxLines) {
            val line = reader.readLine() ?: break
            if (!inMain) {
                if (line.startsWith("\"main\"")) inMain = true
                continue
            }
            if (line.isBlank()) break
            val match = frameLine.matchEntire(line) ?: continue
            if (frames.size >= ErrorReportBuilder.MaxFrames) break
            val (className, method, location) = match.destructured
            val file = location.substringBefore(':').takeIf { ':' in location || it.endsWith(".java") || it.endsWith(".kt") }
            val number = location.substringAfter(':', "").toIntOrNull() ?: -1
            frames += ErrorReportBuilder.frame(className, method, file, number)
        }
        // The trace lists the blocked frame first, as Java does; Sentry wants it last.
        return frames.reversed()
    }
}
