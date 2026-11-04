package persona.core.designsystem

import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf

/**
 * Fenced blocks in an assistant's reply that are drawn as an extension's view (docs/extensions.md「声明式 UI」, #1850):
 * ```persona-view (a view, or one component) and the older ```persona-card. [parse] reads a block (null: not one, or
 * not whole yet while it streams in, and it stays the code it is); [draw] draws what it read. The app provides them
 * (they are the extensions' renderer); without them every fence is code.
 */
class ViewBlocks(val parse: (language: String, code: String) -> Any?, val draw: @Composable (Any) -> Unit)

val LocalViewBlocks = staticCompositionLocalOf<ViewBlocks?> { null }
