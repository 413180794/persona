package persona.core.designsystem

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.MutableIntState
import androidx.compose.runtime.staticCompositionLocalOf

/**
 * How many web pages (the link card's WebView) are shown over the page around them. Glass must never take a
 * WebView in: Chromium draws through a GL functor that a blur's offscreen layer cannot hold, so the page went blank
 * under glass after a scroll, and opening a link in Persona's conversation crashed the renderer on the Android 15
 * emulator (2026-11-02); a web page also redraws on every scroll, like the terminal. The shell gives each page its
 * own count: while it is above zero that page's glass is opaque ([GlassLevel.Solid]) and the dock makes way for the
 * card. A card also keeps the glass inside itself opaque, for hosts that keep no count (the shell's own card).
 */
val LocalWebSheets = staticCompositionLocalOf<MutableIntState?> { null }

/** Counts a web page over the page around it while [shown] ([LocalWebSheets]); nothing without a count. */
@Composable
fun WebSheetShown(shown: Boolean) {
    val sheets = LocalWebSheets.current ?: return
    DisposableEffect(sheets, shown) {
        if (shown) sheets.intValue += 1
        onDispose { if (shown) sheets.intValue -= 1 }
    }
}
