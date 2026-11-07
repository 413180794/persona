package persona.core.designsystem

import androidx.compose.ui.unit.dp

/** Layout recipes used only by the opt-in native extension stream. Legacy surfaces are untouched. */
object NativeStreamLayout {
    val tileMinWidth = 140.dp
    val tileRegularWidth = 200.dp
    const val tileWideFraction = 0.8f
    const val gridDefaultColumns = 4
    const val squareAspect = 1f
    const val landscapeAspect = 4f / 3f
    const val wideAspect = 16f / 9f
    const val portraitAspect = 3f / 4f
    const val stepperPauseMillis = 600L
}
