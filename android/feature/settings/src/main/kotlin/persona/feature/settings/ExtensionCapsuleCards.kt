package persona.feature.settings

import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalUriHandler
import kotlinx.coroutines.flow.collect
import persona.core.contracts.api.ExtensionViewAction
import persona.core.contracts.api.ExtensionViewItem
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import kotlinx.coroutines.launch
import persona.core.contracts.api.ExtensionManagement
import persona.core.contracts.api.ExtensionUiContext
import persona.core.contracts.api.ExtensionUiPlace
import persona.core.designsystem.InlineTone
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JButtonSize
import persona.core.designsystem.JCard
import persona.core.designsystem.JInlineStatus
import persona.core.designsystem.JLoading
import persona.core.designsystem.JSectionHeader
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Tokens
import persona.core.navigation.ExtensionUiRoute

/** The Android overlay's compact, declarative extension slot. The extension runs only on its hub. */
internal object ExtensionCapsuleSlot {
    const val Placement = "capsule_card"
    fun accepts(place: ExtensionUiPlace): Boolean =
        place.placement == Placement && place.extension.isNotBlank() && place.id.isNotBlank()
}

/**
 * Mounted only while the capsule card is expanded. Removing it cancels refreshes and actions; the overlay supplies
 * its own resumed LifecycleOwner, AppFeedback and URI handler, as a normal screen does.
 */
@Composable
fun ExtensionCapsuleCards(api: ExtensionManagement?, onOpen: (ExtensionUiRoute) -> Unit = {}) {
    api ?: return
    val available by api.slotsAvailable.collectAsState(false)
    if (!available) return
    val places = remember(api) { ExtensionPlacesModel(api, ExtensionCapsuleSlot.Placement) }
    val state by places.state.collectAsState()
    val scope = rememberCoroutineScope()
    LaunchedEffect(places) { places.load() }
    // Install, enable or invalidate while the card is open: the same list is read again, without a new connection.
    LaunchedEffect(api, places) { api.uiEvents.collect { places.load() } }
    state.error?.let { error ->
        JInlineStatus(error.text(), tone = InlineTone.Failed)
        JButton(stringResource(R.string.settings_ext_retry), { scope.launch { places.load() } }, kind = JButtonKind.Secondary,
            size = JButtonSize.Small, enabled = !state.loading)
    }
    val cards = state.places.orEmpty().filter(ExtensionCapsuleSlot::accepts)
    if (cards.isEmpty()) return
    JSectionHeader(stringResource(R.string.settings_ext_title))
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        cards.forEach { place -> key(place.extension, place.id) {
            CapsuleExtensionCard(api, place, onOpen)
        } }
    }
}

@Composable
private fun CapsuleExtensionCard(api: ExtensionManagement, place: ExtensionUiPlace, onOpen: (ExtensionUiRoute) -> Unit) {
    val context = remember { ExtensionUiContext(size = "compact", platform = "android") }
    val model = remember(api, place.extension, place.id) { ExtensionViewModel(api, place, context) }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    val uri = LocalUriHandler.current
    val res = LocalResources.current
    var confirming by remember(model) { mutableStateOf<CapsuleConfirmation?>(null) }
    var message by remember(model) { mutableStateOf<String?>(null) }
    fun outcome(result: ExtensionActionOutcome) {
        message = result.error?.text(res) ?: result.toast
        result.push?.let { onOpen(ExtensionViewText.opened(place, it)) }
        result.openUrl?.let { if (ExtensionViewText.isHttps(it)) uri.openUri(it) }
    }
    fun press(action: ExtensionViewAction) {
        scope.launch { outcome(model.press(action)) }
    }
    val handle = ExtensionViewHandle(
        press = { item, action ->
            if (!action.confirm.isNullOrBlank()) confirming = CapsuleConfirmation(item, action) else press(action)
        },
        edit = model::edit,
        choose = {},
        open = { if (ExtensionViewText.isHttps(it)) uri.openUri(it) },
        segment = { item, value ->
            val action = item.action
            if (action != null && !action.confirm.isNullOrBlank()) confirming = CapsuleConfirmation(item, action, value)
            else scope.launch { outcome(model.choose(item, value)) }
        },
    )
    LaunchedEffect(model) { model.load() }
    KeepFresh(model, api.uiEvents)
    if (state.unsupported) return
    JCard {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            Text(state.view?.title?.takeIf { it.isNotBlank() } ?: place.title.ifBlank { place.id },
                style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
            place.extensionName?.takeIf { it.isNotBlank() }?.let {
                Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            when {
                state.view != null && confirming == null -> ExtensionCardContent(state, handle)
                state.view == null && state.error == null -> JLoading()
            }
            state.error?.let { error ->
                JInlineStatus(error.text(), tone = InlineTone.Failed)
                JButton(stringResource(R.string.settings_ext_retry), { scope.launch { model.load() } }, kind = JButtonKind.Secondary,
                    size = JButtonSize.Small, enabled = !state.loading)
            }
            confirming?.let { pending ->
                val item = pending.item
                val action = pending.action
                Text(action.confirm.orEmpty(), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink)
                JButton(ExtensionViewText.confirmLabel(res, item), {
                    confirming = null
                    if (pending.choice == null) press(action)
                    else scope.launch { outcome(model.choose(item, pending.choice)) }
                },
                    kind = if (item.role == "destructive") JButtonKind.Destructive else JButtonKind.Secondary,
                    size = JButtonSize.Small, enabled = !state.busy)
                JButton(stringResource(R.string.settings_ext_cancel), { confirming = null }, kind = JButtonKind.Secondary, size = JButtonSize.Small)
            }
            message?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
            place.opens?.takeIf { it.isNotBlank() }?.let { page ->
                JButton(stringResource(R.string.settings_ext_open_full_page),
 { onOpen(ExtensionUiRoute(place.extension, page, place.title)) },
                    kind = JButtonKind.Secondary, size = JButtonSize.Small)
            }
        }
    }
}

private data class CapsuleConfirmation(val item: ExtensionViewItem, val action: ExtensionViewAction, val choice: String? = null)
