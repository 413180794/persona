package persona.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.api.PluginViewAction
import persona.core.contracts.api.PluginViewItem
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.launch

// Draws a plugin's View (docs/plugins.md「声明式 UI」) with this app's design system: each of the eight kinds of item
// as a component of the style in use, tones as role colours. The plugin gives no colour, size or icon.

/** What the page does for a view: press a button (after its confirm), choose for a choice field, open a link. */
internal class PluginViewHandle(
    val press: (PluginViewItem, PluginViewAction) -> Unit,
    val edit: (String, String) -> Unit,
    val choose: (PluginViewItem) -> Unit,
    val open: (String) -> Unit,
)

/**
 * The view's dialogs (a button's confirm, a choice field's choices) and the way its buttons go: an action's toast
 * shows as the app's notice, its link opens in the app, a new view replaces the page.
 */
@Composable
internal fun rememberPluginView(model: PluginViewModel): PluginViewHandle {
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    val uri = LocalUriHandler.current
    val state by model.state.collectAsState()
    var confirming by remember(model) { mutableStateOf<Pair<PluginViewItem, PluginViewAction>?>(null) }
    var choosing by remember(model) { mutableStateOf<PluginViewItem?>(null) }
    val open: (String) -> Unit = { url -> if (PluginViewText.isHttps(url)) runCatching { uri.openUri(url) } }
    fun run(action: PluginViewAction) = scope.launch {
        val outcome = model.press(action)
        outcome.toast?.let { feedback?.notice(it) }
        outcome.error?.let { feedback?.notice(it) }
        outcome.openUrl?.let(open)
    }
    val handle = remember(model) {
        PluginViewHandle(
            press = { item, action -> if (!action.confirm.isNullOrBlank()) confirming = item to action else run(action) },
            edit = model::edit,
            choose = { choosing = it },
            open = open,
        )
    }
    confirming?.let { (item, action) ->
        JDialog({ confirming = null }, title = PluginViewText.confirmTitle(item), text = action.confirm,
            confirm = PluginViewText.confirmLabel(item) to { confirming = null; run(action) },
            destructive = item.role == "destructive")
    }
    choosing?.let { item ->
        val name = item.name.orEmpty()
        val current = state.values[name]
        JActionSheet(item.choices.orEmpty().map { choice ->
            JAction(choice.label, checked = current == choice.value, onClick = { model.edit(name, choice.value) })
        }, { choosing = null }, title = item.label ?: name)
    }
    return handle
}

/**
 * The view's sections and items, into a page's list: [key] keeps two views on one page apart. Rows, links, switches
 * and choices that follow each other form one group; the rest stand on their own.
 */
internal fun LazyListScope.pluginViewItems(key: String, state: PluginViewState, handle: PluginViewHandle, showTitles: Boolean = true) {
    val view = state.view ?: return
    view.sections.forEachIndexed { s, section ->
        val title = section.title?.takeIf { it.isNotBlank() }
        if (title != null && showTitles) item("$key-s$s-title") { JSectionHeader(title) }
        else if (s > 0) item("$key-s$s-gap") { Spacer(Modifier.padding(top = Tokens.Space.l)) }
        val items = section.items.filter(PluginViewText::drawable)
        var i = 0
        while (i < items.size) {
            val start = i
            if (PluginViewText.grouped(items[i])) while (i < items.size && PluginViewText.grouped(items[i])) i++ else i++
            val run = items.subList(start, i)
            run.forEachIndexed { index, item ->
                item("$key-s$s-i${start + index}") {
                    if (PluginViewText.grouped(item)) {
                        JGroupItem(GroupPosition.of(index, run.size), if (index == 0 && start > 0) Modifier.padding(top = Tokens.Space.s) else Modifier) {
                            GroupedItem(item, state, handle)
                        }
                    } else StandaloneItem(item, state, handle, first = start == 0)
                }
            }
        }
        section.footer?.takeIf { it.isNotBlank() }?.let { footer ->
            item("$key-s$s-footer") { SettingsCaption(footer, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s)) }
        }
    }
}

/** A row, a link, a switch or a choice: one line of a group. */
@Composable
private fun GroupedItem(item: PluginViewItem, state: PluginViewState, handle: PluginViewHandle) {
    val enabled = !state.busy
    when (item.type) {
        "row" -> {
            val action = item.action?.takeIf { it.id.isNotEmpty() }
            val badge = item.badge?.takeIf { it.text.isNotBlank() }
            JRow(item.title.orEmpty(), subtitle = item.subtitle?.takeIf { it.isNotBlank() }, value = item.value?.takeIf { it.isNotBlank() },
                trailing = when {
                    action != null && state.acting == action.id -> ({ JSpinner() })
                    badge != null -> ({ JTag(badge.text, PluginViewText.tone(badge.tone)) })
                    else -> null
                },
                chevron = action != null, enabled = enabled || action == null,
                onClick = action?.let { a -> { handle.press(item, a) } })
        }
        "link" -> JRow(item.title.orEmpty(), leadingIcon = PhIcons.ArrowSquareOut, chevron = false, onClick = { handle.open(item.url.orEmpty()) })
        "field" -> {
            val name = item.name.orEmpty()
            val value = state.values[name].orEmpty()
            if (item.kind == "toggle") {
                JSwitchRow(item.label.orEmpty(), value == "true", { on -> handle.edit(name, if (on) "true" else "false") }, enabled = enabled)
            } else {
                val label = item.choices.orEmpty().firstOrNull { it.value == value }?.label ?: value.ifEmpty { item.placeholder ?: "未选" }
                JRow(item.label.orEmpty(), value = label, enabled = enabled, onClick = { handle.choose(item) })
            }
        }
    }
}

/** Text, a number, a progress bar, a button, a text field, a divider: each on its own. */
@Composable
private fun StandaloneItem(item: PluginViewItem, state: PluginViewState, handle: PluginViewHandle, first: Boolean) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val top = if (first) Modifier else Modifier.padding(top = Tokens.Space.s)
    when (item.type) {
        "text" -> {
            val text = item.text.orEmpty()
            when (item.style) {
                "code" -> JCodeBlock(text, top, small = true)
                "caption" -> Text(text, top.padding(horizontal = rowPadding), style = type.caption, color = palette.ink3)
                "secondary" -> Text(text, top.padding(horizontal = rowPadding), style = type.callout, color = palette.ink2)
                else -> Text(text, top.padding(horizontal = rowPadding), style = type.body, color = palette.ink)
            }
        }
        "metric" -> JGroupItem(GroupPosition.Single, top) {
            Column(Modifier.fillMaxWidth().padding(horizontal = rowPadding, vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
                Text(item.label.orEmpty(), style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val tone = PluginViewText.tone(item.tone)
                Text(item.value.orEmpty(), style = type.number, color = if (tone == Tone.Neutral) palette.ink else palette.tone(tone).text, maxLines = 1,
                    overflow = TextOverflow.Ellipsis)
                item.detail?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.caption, color = palette.ink3) }
            }
        }
        "progress" -> JGroupItem(GroupPosition.Single, top) {
            Column(Modifier.fillMaxWidth().padding(horizontal = rowPadding, vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(item.label.orEmpty(), Modifier.weight(1f), style = type.body, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Spacer(Modifier.width(Tokens.Space.s))
                    Text(PluginViewText.percent(item), style = type.micro, color = palette.ink2, maxLines = 1)
                }
                JLinearProgress(PluginViewText.ratio(item), tone = PluginViewText.tone(item.tone))
                item.detail?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.caption, color = palette.ink3) }
            }
        }
        "button" -> {
            val action = item.action ?: return
            JButton(item.title.orEmpty(), { handle.press(item, action) }, top.fillMaxWidth(),
                kind = when {
                    item.role == "destructive" -> JButtonKind.Destructive
                    action.submit -> JButtonKind.Primary
                    else -> JButtonKind.Secondary
                },
                loading = state.acting == action.id, enabled = !state.busy)
        }
        "field" -> FieldBox(item, state, handle, top)
        "divider" -> JDivider(top.padding(vertical = Tokens.Space.s))
    }
}

/** A text, secret or number field; a secret is never filled in by the plugin and never shown. */
@Composable
private fun FieldBox(item: PluginViewItem, state: PluginViewState, handle: PluginViewHandle, modifier: Modifier) {
    val name = item.name.orEmpty()
    val value = state.values[name].orEmpty()
    val secret = item.kind == "secret"
    val number = item.kind == "number"
    JTextField(value, { text -> if (!number || PluginViewText.isNumber(text)) handle.edit(name, text) }, modifier.fillMaxWidth(),
        label = item.label, placeholder = item.placeholder?.takeIf { it.isNotBlank() }, singleLine = true, enabled = !state.busy,
        visualTransformation = if (secret) PasswordVisualTransformation() else androidx.compose.ui.text.input.VisualTransformation.None,
        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, keyboardType = when {
            secret -> KeyboardType.Password
            number -> KeyboardType.Decimal
            else -> KeyboardType.Text
        }))
}
