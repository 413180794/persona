package jarvis.core.designsystem

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier

/** The older button names, drawn by [JButton] in the active style. */
@Composable
fun PrimaryButton(
    text: String, onClick: () -> Unit, modifier: Modifier = Modifier,
    enabled: Boolean = true, loading: Boolean = false, loadingText: String = "正在处理…",
) = JButton(text, onClick, modifier, JButtonKind.Primary, enabled = enabled, loading = loading, loadingText = loadingText)

@Composable
fun SecondaryButton(
    text: String, onClick: () -> Unit, modifier: Modifier = Modifier,
    enabled: Boolean = true, loading: Boolean = false, loadingText: String = "正在处理…",
) = JButton(text, onClick, modifier, JButtonKind.Secondary, enabled = enabled, loading = loading, loadingText = loadingText)

@Composable
fun DestructiveButton(
    text: String, onClick: () -> Unit, modifier: Modifier = Modifier,
    enabled: Boolean = true, loading: Boolean = false, loadingText: String = "正在处理…",
) = JButton(text, onClick, modifier, JButtonKind.Destructive, enabled = enabled, loading = loading, loadingText = loadingText)
