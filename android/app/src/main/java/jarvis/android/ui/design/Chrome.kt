package jarvis.android.ui.design

import android.view.HapticFeedbackConstants
import android.view.View
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import jarvis.android.ui.JTheme

/**
 * A search field that stays on the page (iOS's search bar): a capsule on the floating surface, the magnifier, the
 * words, ✕ to clear.
 */
@Composable
fun SearchField(
    value: String, onValueChange: (String) -> Unit, placeholder: String, modifier: Modifier = Modifier,
    focus: FocusRequester? = null, onSubmit: () -> Unit = {},
) {
    val c = JTheme.colors
    Row(
        modifier.fillMaxWidth().height(JSize.tapTarget).surface(CircleShape, floating = true).padding(horizontal = JSpace.m),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        Icon(JIcons.search, null, Modifier.size(JSize.iconXS), tint = c.secondaryText)
        Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
            if (value.isEmpty()) Text(placeholder, style = JTheme.type.body, color = c.tertiaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
            BasicTextField(
                value, onValueChange, singleLine = true,
                textStyle = JTheme.type.body.copy(color = c.label),
                cursorBrush = SolidColor(c.accent),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = androidx.compose.foundation.text.KeyboardActions(onSearch = { onSubmit() }),
                modifier = Modifier.fillMaxWidth().then(if (focus != null) Modifier.focusRequester(focus) else Modifier),
            )
        }
        if (value.isNotEmpty()) IconAction(JIcons.clear, "清空", { onValueChange("") }, tint = c.tertiaryText, size = JSize.iconXS, modifier = Modifier.size(JSize.wellS))
    }
}

/**
 * The header of a pushed page (iOS's navigation bar in this app): a round back button, the title in the middle,
 * round buttons at the end. Under the status bar, on the page's color.
 */
@Composable
fun PageHeader(
    title: String, onBack: (() -> Unit)?, modifier: Modifier = Modifier, subtitle: String? = null,
    background: Color = JTheme.colors.page, actions: @Composable RowScope.() -> Unit = {},
) {
    Box(modifier.fillMaxWidth().background(background).statusBarsPadding().height(JSize.shellBar).padding(horizontal = JSpace.page)) {
        Row(Modifier.align(Alignment.CenterStart), verticalAlignment = Alignment.CenterVertically) {
            if (onBack != null && !LocalPaneRoot.current) CircleButton(JIcons.back, "返回", onBack, tint = JTheme.colors.label)
        }
        Column(Modifier.align(Alignment.Center).padding(horizontal = JSize.tapTarget * 2 + JSpace.s), horizontalAlignment = Alignment.CenterHorizontally) {
            Text(title, style = JTheme.type.heading, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center)
            if (!subtitle.isNullOrBlank()) Text(subtitle, style = JTheme.type.label, color = JTheme.colors.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Row(Modifier.align(Alignment.CenterEnd), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s), content = actions)
    }
}

/**
 * True around the first page of a wide window's right pane (iOS `splitDetailPage`): the list that opened it is beside
 * it, so its [PageHeader] has no back button. Pages pushed over it inside the pane get theirs again.
 */
val LocalPaneRoot = staticCompositionLocalOf { false }

/** A pushed page: its header, then the content on the page's color, to the bottom edge. */
@Composable
fun PageScaffold(
    title: String, onBack: (() -> Unit)?, modifier: Modifier = Modifier, subtitle: String? = null,
    actions: @Composable RowScope.() -> Unit = {}, content: @Composable ColumnScope.() -> Unit,
) {
    Column(modifier.fillMaxSize().background(JTheme.colors.page)) {
        PageHeader(title, onBack, subtitle = subtitle, actions = actions)
        content()
    }
}

/** The fog under a floating top bar: the page's color, fading out below the bar so content scrolls away under it. */
@Composable
fun TopFog(solid: Dp, fade: Dp, modifier: Modifier = Modifier, color: Color = JTheme.colors.page) {
    Column(modifier.fillMaxWidth()) {
        Box(Modifier.fillMaxWidth().height(solid).background(color))
        Box(Modifier.fillMaxWidth().height(fade).background(Brush.verticalGradient(listOf(color, color.copy(alpha = 0f)))))
    }
}

/** The fog over the bottom edge (under the composer), the other way round. */
@Composable
fun BottomFog(height: Dp, modifier: Modifier = Modifier, color: Color = JTheme.colors.page) {
    Box(modifier.fillMaxWidth().height(height).background(Brush.verticalGradient(listOf(color.copy(alpha = 0f), color))))
}

/**
 * A sheet from the bottom (iOS's `.sheet`): the page's color, the panel's corners, a grabber; [title] centered over
 * the content with ✕ at the end when [onClose] is given.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun JSheet(
    onDismiss: () -> Unit, title: String? = null, onClose: (() -> Unit)? = null, halfFirst: Boolean = false,
    content: @Composable ColumnScope.() -> Unit,
) {
    ModalBottomSheet(
        onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = !halfFirst),
        containerColor = JTheme.colors.page, contentColor = JTheme.colors.label,
        shape = RoundedCornerShape(topStart = JRadius.panel, topEnd = JRadius.panel),
        dragHandle = { Box(Modifier.padding(top = JSpace.s, bottom = JSpace.xs).size(width = JSize.wellL, height = JSize.grabber).background(JTheme.colors.fillPressed, CircleShape)) },
    ) {
        Column(Modifier.fillMaxWidth().navigationBarsPadding()) {
            if (title != null) {
                Box(Modifier.fillMaxWidth().padding(horizontal = JSpace.page, vertical = JSpace.s)) {
                    Text(title, style = JTheme.type.heading, color = JTheme.colors.label, maxLines = 1, overflow = TextOverflow.Ellipsis,
                        textAlign = TextAlign.Center, modifier = Modifier.align(Alignment.Center).padding(horizontal = JSize.tapTarget))
                    if (onClose != null) CircleButton(JIcons.close, "关闭", onClose, Modifier.align(Alignment.CenterEnd), size = JSize.wellL)
                }
            }
            content()
            Spacer(Modifier.height(JSpace.l))
        }
    }
}

/** Touch feedback (iOS's haptics): a tap, a success, a warning, an error. */
object Haptics {
    fun tap(view: View) { view.performHapticFeedback(HapticFeedbackConstants.CLOCK_TICK) }
    fun success(view: View) {
        view.performHapticFeedback(if (android.os.Build.VERSION.SDK_INT >= 30) HapticFeedbackConstants.CONFIRM else HapticFeedbackConstants.VIRTUAL_KEY)
    }
    fun warning(view: View) { view.performHapticFeedback(HapticFeedbackConstants.LONG_PRESS) }
    fun error(view: View) {
        view.performHapticFeedback(if (android.os.Build.VERSION.SDK_INT >= 30) HapticFeedbackConstants.REJECT else HapticFeedbackConstants.LONG_PRESS)
    }
}

/** The view to give [Haptics]. */
@Composable
fun hapticView(): View = LocalView.current

/** Nothing over the content: a box that takes the whole page. */
@Composable
fun FullPage(modifier: Modifier = Modifier, content: @Composable BoxScope.() -> Unit) =
    Box(modifier.fillMaxSize().background(JTheme.colors.page), content = content)

/** A gap as wide as [w] in a row. */
@Composable
fun RowScope.Gap(w: Dp) = Spacer(Modifier.width(w))

/** No inset at all (a page that draws its own). */
val NoInsets = WindowInsets(0, 0, 0, 0)

/**
 * A text field on the page (replies, feedback, names): the words on a soft well with the card's corners, the
 * placeholder in the tertiary color; one line or several.
 */
@Composable
fun JTextField(
    value: String, onValueChange: (String) -> Unit, placeholder: String, modifier: Modifier = Modifier,
    enabled: Boolean = true, singleLine: Boolean = false, minLines: Int = 1, maxLines: Int = if (singleLine) 1 else 6,
    focus: FocusRequester? = null, password: Boolean = false, fill: Color? = null,
) {
    val c = JTheme.colors
    BasicTextField(
        value, onValueChange, enabled = enabled, singleLine = singleLine, minLines = minLines, maxLines = maxLines,
        textStyle = JTheme.type.body.copy(color = c.label), cursorBrush = SolidColor(c.accent),
        visualTransformation = if (password) androidx.compose.ui.text.input.PasswordVisualTransformation() else androidx.compose.ui.text.input.VisualTransformation.None,
        keyboardOptions = if (password) KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Password) else KeyboardOptions.Default,
        modifier = modifier.fillMaxWidth().then(if (focus != null) Modifier.focusRequester(focus) else Modifier),
        decorationBox = { inner ->
            Box(
                Modifier.fillMaxWidth().background(fill ?: c.fillWell, RoundedCornerShape(JRadius.card))
                    .padding(horizontal = JSpace.m, vertical = JSpace.m),
                contentAlignment = Alignment.TopStart,
            ) {
                if (value.isEmpty()) Text(placeholder, style = JTheme.type.body, color = c.tertiaryText, maxLines = if (singleLine) 1 else 3, overflow = TextOverflow.Ellipsis)
                inner()
            }
        },
    )
}

/** [JTextField] with its keyboard picked (an address takes the URI keyboard): the same well, words and placeholder. */
@Composable
fun JTextField(
    value: String, onValueChange: (String) -> Unit, placeholder: String, keyboardOptions: KeyboardOptions,
    modifier: Modifier = Modifier, enabled: Boolean = true, singleLine: Boolean = false, fill: Color? = null,
) {
    val c = JTheme.colors
    BasicTextField(
        value, onValueChange, enabled = enabled, singleLine = singleLine, maxLines = if (singleLine) 1 else 6,
        textStyle = JTheme.type.body.copy(color = c.label), cursorBrush = SolidColor(c.accent), keyboardOptions = keyboardOptions,
        modifier = modifier.fillMaxWidth(),
        decorationBox = { inner ->
            Box(
                Modifier.fillMaxWidth().background(fill ?: c.fillWell, RoundedCornerShape(JRadius.card)).padding(JSpace.m),
                contentAlignment = Alignment.TopStart,
            ) {
                if (value.isEmpty()) Text(placeholder, style = JTheme.type.body, color = c.tertiaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
                inner()
            }
        },
    )
}
