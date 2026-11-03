package persona.feature.chat

import android.view.HapticFeedbackConstants
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.platform.LocalView

/** Only an observed foreground working-to-complete transition can produce feedback. */
internal class CompletionFeedbackGate {
    private var workingTurn: String? = null

    fun update(turn: String?, status: String?, visible: Boolean, completedTurn: String?): Boolean {
        val armed = workingTurn
        workingTurn = if (visible && status == "working") turn else null
        return visible && turn != null && turn != completedTurn && turn == armed && status in setOf("done", "idle")
    }
}

@Composable
internal fun ChatCompletionFeedback(turn: String?, status: String?, visible: Boolean) {
    val view = LocalView.current
    val gate = remember { CompletionFeedbackGate() }
    var completedTurn by rememberSaveable { mutableStateOf<String?>(null) }
    LaunchedEffect(turn, status, visible) {
        if (gate.update(turn, status, visible && view.hasWindowFocus() && view.isShown, completedTurn)) {
            completedTurn = turn
            // The View API respects both the system setting and this view's haptic setting.
            view.performHapticFeedback(HapticFeedbackConstants.CONFIRM)
        }
    }
}
