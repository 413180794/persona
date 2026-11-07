package persona.core.hub.errors

import java.time.Instant
import java.util.UUID
import persona.core.contracts.generated.ExtensionErrorBreadcrumb
import persona.core.contracts.generated.ExtensionErrorException
import persona.core.contracts.generated.ExtensionErrorFrame
import persona.core.contracts.generated.ExtensionErrorReport

/** The hub feature that says an extension takes error reports (docs/protocol/protocol.md "Error reports"). */
const val ErrorReportsFeature = "error_reports"

/**
 * What every report from this app carries about the build and the device. [deviceModel] is the model only
 * (`Build.MODEL`), never an id of the device.
 */
data class ErrorReportContext(
    val release: String,
    val build: String,
    val osVersion: String,
    val deviceModel: String,
)

/**
 * Builds [ExtensionErrorReport]s (docs/android/android.md "Error reports"). Only code-chosen text goes in: exception types,
 * frames (class, method, source file name, line) and the exception messages, cut to [MaxMessage]; never user content.
 */
class ErrorReportBuilder(
    private val context: ErrorReportContext,
    private val newId: () -> String = { UUID.randomUUID().toString() },
) {
    /** A Java/Kotlin throwable: its cause chain, outermost first, each with frames innermost last (Sentry's order). */
    fun fromThrowable(
        error: Throwable, kind: String, level: String, timeMillis: Long,
        breadcrumbs: List<ExtensionErrorBreadcrumb> = emptyList(), previousLaunch: Boolean = false,
    ): ExtensionErrorReport {
        val chain = causeChain(error)
        return report(
            kind = kind, level = level, timeMillis = timeMillis, previousLaunch = previousLaunch,
            message = describe(error),
            exceptions = chain.map { exceptionOf(it) },
            breadcrumbs = breadcrumbs,
        )
    }

    /** A report without a throwable (an ANR or a native crash read at the next start). */
    fun report(
        kind: String, level: String, timeMillis: Long, message: String?,
        exceptions: List<ExtensionErrorException> = emptyList(),
        breadcrumbs: List<ExtensionErrorBreadcrumb> = emptyList(), previousLaunch: Boolean = false,
    ) = ExtensionErrorReport(
        id = newId(),
        source = "android",
        kind = kind,
        level = level,
        time = Instant.ofEpochMilli(timeMillis).toString(),
        release = context.release.ifBlank { null },
        build = context.build.ifBlank { null },
        os = "android",
        osVersion = context.osVersion.ifBlank { null },
        deviceModel = context.deviceModel.ifBlank { null },
        message = message?.let { truncate(it, MaxMessage) },
        exceptions = exceptions.ifEmpty { null },
        breadcrumbs = breadcrumbs.ifEmpty { null },
        previousLaunch = previousLaunch.takeIf { it },
    )

    companion object {
        /** `message` and each exception's `value`, in UTF-16 units (at most about 1 KB for the ASCII they mostly are). */
        const val MaxMessage = 1024
        const val MaxExceptions = 5
        const val MaxFrames = 64

        /** The throwable and its causes, outermost first; a cycle or a very long chain stops at [MaxExceptions]. */
        fun causeChain(error: Throwable): List<Throwable> {
            val seen = java.util.IdentityHashMap<Throwable, Unit>()
            val chain = mutableListOf<Throwable>()
            var next: Throwable? = error
            while (next != null && chain.size < MaxExceptions && seen.put(next, Unit) == null) {
                chain += next
                next = next.cause
            }
            return chain
        }

        /** `<ExceptionClass>: <message>`, the class's simple name; just the class without a message. */
        fun describe(error: Throwable): String {
            val name = error.javaClass.simpleName.ifEmpty { error.javaClass.name }
            val detail = error.message?.takeIf { it.isNotBlank() }
            return truncate(if (detail == null) name else "$name: $detail", MaxMessage)
        }

        fun exceptionOf(error: Throwable) = ExtensionErrorException(
            type = error.javaClass.name,
            value = error.message?.takeIf { it.isNotBlank() }?.let { truncate(it, MaxMessage) },
            frames = framesOf(error.stackTrace).ifEmpty { null },
        )

        /**
         * Java lists the throwing frame first; Sentry wants it last. Past [MaxFrames] the frames nearest the throw
         * are kept.
         */
        fun framesOf(trace: Array<StackTraceElement>): List<ExtensionErrorFrame> =
            trace.take(MaxFrames).map { frame(it.className, it.methodName, it.fileName, it.lineNumber) }.reversed()

        fun frame(className: String, method: String, file: String?, line: Int) = ExtensionErrorFrame(
            function = method,
            module = className,
            file = file?.substringAfterLast('/')?.ifBlank { null },
            line = line.takeIf { it > 0 },
            inApp = className.startsWith("persona."),
        )

        fun truncate(text: String, max: Int): String {
            if (text.length <= max) return text
            var end = max - 1
            // Never split a surrogate pair: the JSON would carry half a character.
            if (Character.isHighSurrogate(text[end - 1])) end--
            return text.substring(0, end) + "…"
        }
    }
}

/**
 * The last few screens, for the reports' breadcrumbs: route type names only (`ChatRoute`, never its ids), and tab
 * names. Thread safe; [snapshot] is cheap enough for the crash handler.
 */
class ErrorBreadcrumbs(private val capacity: Int = 20, private val clock: () -> Long = System::currentTimeMillis) {
    private val items = ArrayDeque<ExtensionErrorBreadcrumb>()

    fun navigation(name: String) {
        val crumb = ExtensionErrorBreadcrumb(
            time = Instant.ofEpochMilli(clock()).toString(), category = "navigation",
            message = ErrorReportBuilder.truncate(name, 64), level = "info",
        )
        synchronized(items) {
            if (items.lastOrNull()?.message == crumb.message) return
            items.addLast(crumb)
            while (items.size > capacity) items.removeFirst()
        }
    }

    fun snapshot(): List<ExtensionErrorBreadcrumb> = synchronized(items) { items.toList() }

    companion object {
        /** The process's one trail: the navigator writes it, the crash handler reads it. */
        val shared = ErrorBreadcrumbs()
    }
}
