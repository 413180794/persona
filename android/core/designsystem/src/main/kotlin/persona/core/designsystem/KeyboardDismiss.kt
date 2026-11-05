package persona.core.designsystem

import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.SuspendingPointerInputModifierNode
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.node.CompositionLocalConsumerModifierNode
import androidx.compose.ui.node.DelegatingNode
import androidx.compose.ui.node.ModifierNodeElement
import androidx.compose.ui.node.currentValueOf
import androidx.compose.ui.platform.InspectorInfo
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalSoftwareKeyboardController

/**
 * A tap on blank space inside this area puts the keyboard away. It only watches: the tap reaches what is under the
 * finger first, and anything that handles it (a link, a button, a card, a text field, a long press, a scroll) consumes
 * it, which leaves the keyboard up. Nothing here consumes an event, so no click below is lost.
 */
fun Modifier.dismissKeyboardOnBlankTap(): Modifier = this then DismissKeyboardElement

/**
 * Keeps taps on a composer's padding from reaching a page behind it or a surrounding page's dismiss listener.
 * Children handle the gesture first; only otherwise unhandled taps are consumed here.
 */
fun Modifier.keepsKeyboardOnTap(): Modifier = pointerInput(Unit) { detectTapGestures {} }

private data object DismissKeyboardElement : ModifierNodeElement<DismissKeyboardNode>() {
    override fun create() = DismissKeyboardNode()
    override fun update(node: DismissKeyboardNode) = Unit
    override fun InspectorInfo.inspectableProperties() { name = "dismissKeyboardOnBlankTap" }
}

private class DismissKeyboardNode : DelegatingNode(), CompositionLocalConsumerModifierNode {
    init {
        delegate(SuspendingPointerInputModifierNode {
            awaitEachGesture {
                // Final: the content under the finger has seen this gesture already and said whether it took it.
                awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Final)
                // Null when the gesture was consumed (a drag that scrolled, a long press) or left the area.
                val up = waitForUpOrCancellation(PointerEventPass.Final) ?: return@awaitEachGesture
                if (!up.isConsumed) {
                    currentValueOf(LocalFocusManager).clearFocus()
                    currentValueOf(LocalSoftwareKeyboardController)?.hide()
                }
            }
        })
    }
}
