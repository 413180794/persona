package persona.core.designsystem

import androidx.compose.runtime.staticCompositionLocalOf

/** Return true when the shell owns presentation, including deliberately silent refresh failures. */
interface AppFeedback {
    fun failure(error: Throwable, operation: Boolean = true): Boolean
    fun notice(message: String): Boolean
}
val LocalAppFeedback = staticCompositionLocalOf<AppFeedback?> { null }
