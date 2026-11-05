package persona.feature.settings

import android.provider.Settings
import android.Manifest
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import persona.core.contracts.api.*
import persona.core.navigation.LocalDetailKey
import persona.core.navigation.SettingsPage
import persona.core.navigation.SettingsPageRoute
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@Composable
internal fun Action(label: String, enabled: Boolean = true, onClick: () -> Unit) {
    JButton(label, onClick, kind = JButtonKind.Secondary, size = JButtonSize.Small, enabled = enabled)
}

@Composable
internal fun SettingsCaption(text: String, modifier: Modifier = Modifier) {
    Text(text, modifier, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
}

@Composable
fun SettingsScreen(
    services: SettingsServices,
    initialPage: String = "settings",
    onPhoneData: (() -> Unit)? = null,
    onDemo: (() -> Unit)? = null,
    onExitDemo: (() -> Unit)? = null,
    onOpen: ((SettingsPage) -> Unit)? = null,
    onBack: (() -> Unit)? = null,
    /** Opens a page by key (one AI use of 模型配置); without it such pages open inside this one. */
    onNavigate: ((androidx.navigation3.runtime.NavKey) -> Unit)? = null,
) {
    SettingsContent(services.bound(), initialPage, onPhoneData, onDemo, onExitDemo, onOpen, onBack, onNavigate)
}

@Composable
private fun SettingsContent(
    services: SettingsServices,
    initialPage: String,
    onPhoneData: (() -> Unit)?,
    onDemo: (() -> Unit)?,
    onExitDemo: (() -> Unit)?,
    onOpen: ((SettingsPage) -> Unit)?,
    onBack: (() -> Unit)?,
    onNavigate: ((androidx.navigation3.runtime.NavKey) -> Unit)?,
) {
    var page by rememberSaveable(initialPage) { mutableStateOf(initialPage) }
    var purpose by rememberSaveable(initialPage) { mutableStateOf<String?>(null) }
    var extension by rememberSaveable(initialPage) { mutableStateOf<String?>(null) }
    var channel by rememberSaveable(initialPage) { mutableStateOf<String?>(null) }
    // 模型供应商's own steps when they open in place: one provider, adding one, importing from ~/.zshrc.
    var provider by rememberSaveable(initialPage) { mutableStateOf<String?>(null) }
    var providerStep by rememberSaveable(initialPage) { mutableStateOf<String?>(null) }
    val desktops = services.desktops?.collectAsState()?.value.orEmpty()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val connection by services.connection.state.collectAsState()
    val machines by services.sessions.machines.collectAsState()
    val paired by services.pairing.isPaired.collectAsState()
    val deviceController = remember(services) { DeviceSettingsState(services.deviceSettings, services.disconnect) }
    val deviceState by deviceController.state.collectAsState()
    // The signed-in Mac's row says 手机静音 when it is on: read this device's preferences once a desktop shows.
    LaunchedEffect(desktops.isNotEmpty(), page) {
        if (desktops.isNotEmpty() && page == "settings" && paired && services.deviceSettings != null && deviceState.preferences == null) deviceController.loadPreferences()
    }
    var confirmDisconnect by remember { mutableStateOf(false) }
    var update by remember { mutableStateOf<AppUpdate?>(null) }
    var updateMessage by remember { mutableStateOf<Int?>(null) }
    var busy by remember { mutableStateOf(false) }
    var installing by remember { mutableStateOf(false) }
    fun checkUpdate() {
        if (busy) return
        scope.launch {
            busy = true
            try {
                val latest = services.updates.check()
                val installed = context.packageManager.getPackageInfo(context.packageName, 0).longVersionCode
                update = latest?.takeIf { it.versionCode > installed }
                updateMessage = if (update == null) R.string.settings_update_none else null
            } catch (error: CancellationException) { throw error }
            catch (_: Exception) { updateMessage = R.string.settings_update_check_failed }
            finally { busy = false }
        }
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { if (!services.demo) checkUpdate() }
    val extensionService = services.extensions.takeUnless { services.demo }
    val extensionsAvailable by remember(extensionService) { extensionService?.available ?: kotlinx.coroutines.flow.flowOf(false) }.collectAsState(false)
    var extensionSummary by remember { mutableStateOf<String?>(null) }
    val connectorsService = services.connectors.takeUnless { services.demo }
    var mailAccounts by remember { mutableStateOf<List<MailAccount>?>(null) }
    var larkStatus by remember { mutableStateOf<LarkStatus?>(null) }
    val larkChats = LocalLarkFeatures.current.chats
    val translationPrefs = services.translation?.prefs?.collectAsState()?.value
    val translationValue = when {
        services.demo || translationPrefs == null -> null
        translationPrefs.enabled -> TranslationPrefs.languageName(translationPrefs.target)
        else -> stringResource(R.string.settings_off)
    }
    var hubConnectors by remember { mutableStateOf<List<HubConnector>>(emptyList()) }
    val phoneOn = remember(page) {
        listOf(
            Manifest.permission.READ_CONTACTS,
            Manifest.permission.READ_CALENDAR,
            Manifest.permission.ACCESS_FINE_LOCATION,
        ).count {
            androidx.core.content.ContextCompat.checkSelfPermission(context, it) == android.content.pm.PackageManager.PERMISSION_GRANTED
        }
    }
    // The row's count, read again whenever the settings list is shown or the hub reconnects.
    LaunchedEffect(extensionService, extensionsAvailable, page, connection) {
        if (extensionService == null || !extensionsAvailable || page != "settings" || connection != ConnectionState.Online) return@LaunchedEffect
        extensionSummary = try { ExtensionText.listSummary(context.resources, extensionService.extensions()) }
        catch (error: CancellationException) { throw error } catch (_: Exception) { extensionSummary }
    }
    // Extensions' UI slots (docs/extensions.md「UI 插槽」): read again whenever the settings list shows.
    val slots = services.extensionSlots.takeUnless { services.demo }
    val slotsState = slots?.state?.collectAsState()?.value ?: ExtensionSlotsState()
    val slotCards = if (page == "settings" && onNavigate != null) rememberExtensionCards(slots) { onNavigate(it) } else emptyList()
    LaunchedEffect(slots, page, connection) {
        if (slots != null && page == "settings" && connection == ConnectionState.Online) slots.load()
    }
    val channelService = services.channels.takeUnless { services.demo }
    val channelsAvailable by remember(channelService) { channelService?.available ?: kotlinx.coroutines.flow.flowOf(false) }.collectAsState(false)
    var channelList by remember { mutableStateOf<List<HubChannel>?>(null) }
    // Like 扩展's: read again whenever the settings list is shown or the hub reconnects.
    LaunchedEffect(channelService, channelsAvailable, page, connection) {
        if (channelService == null || !channelsAvailable || page != "settings" || connection != ConnectionState.Online) return@LaunchedEffect
        channelList = try { channelService.channels() }
        catch (error: CancellationException) { throw error } catch (_: Exception) { channelList }
    }
    // 设置 › hub 版本's row value (iOS `hubSummary`): read when the list shows or the hub reconnects, then each push.
    val hubUpdates = services.hubUpdates.takeUnless { services.demo }
    var hubUpdateStatus by remember(hubUpdates) { mutableStateOf<persona.core.contracts.api.HubUpdateStatus?>(null) }
    LaunchedEffect(hubUpdates, page, connection) {
        if (hubUpdates == null || page != "settings" || connection != ConnectionState.Online) return@LaunchedEffect
        hubUpdateStatus = try { hubUpdates.status() }
        catch (error: CancellationException) { throw error } catch (_: Exception) { hubUpdateStatus }
    }
    LaunchedEffect(hubUpdates) { hubUpdates?.events?.collect { hubUpdateStatus = it } }
    LaunchedEffect(connectorsService, page, connection, larkChats) {
        if (connectorsService == null || page != "settings" || connection != ConnectionState.Online) return@LaunchedEffect
        try {
            mailAccounts = runCatching { connectorsService.mailAccounts() }.getOrNull()
            // 飞书's status is the lark-connector extension's (`/api/v1/lark/status`): only asked while the hub has it.
            larkStatus = if (larkChats) runCatching { connectorsService.larkStatus() }.getOrNull() else null
            hubConnectors = runCatching { connectorsService.connectors(all = true) }.getOrDefault(emptyList())
        } catch (_: Exception) {}
    }
    val back: () -> Unit = onBack ?: {
        if (page == "mail-add") page = "mail"
        else page = "settings"
    }
    BackHandler(page != "settings" && onBack == null, onBack = back)
    val detailKey = LocalDetailKey.current
    fun open(destination: SettingsPage) {
        if (onOpen != null) onOpen(destination) else page = destination.toInitialPage()
    }
    val deviceFeedback: @Composable () -> Unit = {
        deviceState.message?.let { JNotice(it.text(), status = Status.NeedsYou) }
        if (deviceState.needsCleanup) Action(stringResource(R.string.settings_retry_cleanup), enabled = !deviceState.busy) {
            scope.launch { deviceController.disconnect() }
        }
    }
    when (page) {
        "pair" -> PairingScreen(services.pairing, services.sessions, onReady = back,
            onDiagnostics = { open(SettingsPage.Diagnostics) }, diagnostics = services.diagnostics, onBack = back,
            footer = { onDemo?.let { Action(stringResource(R.string.settings_try_demo), onClick = it) }; deviceFeedback() },
            machines = services.machines.takeUnless { services.demo }, attempts = services.pairingAttempts)
        "capsule" -> CapsuleSettingsPage(services.capsule, back)
        "claude" -> ClaudeSettingsPage(services.sessions, services.machines.takeUnless { services.demo }, back)
        "codex" -> ClaudeSettingsPage(services.sessions, services.machines.takeUnless { services.demo }, back, AgentSettingsKind.Codex)
        "models" -> purpose?.let { id -> ModelPurposePage(services.models.takeUnless { services.demo }, id, onBack = { purpose = null },
                providers = services.providers.takeUnless { services.demo }) }
            ?: ModelConfigurationPage(services.models.takeUnless { services.demo }, back,
                onPurpose = { id -> onNavigate?.invoke(persona.core.navigation.ModelPurposeRoute(id)) ?: run { purpose = id } },
                providers = services.providers.takeUnless { services.demo })
        "plugins" -> extension?.let { id -> ExtensionPage(services.extensions.takeUnless { services.demo }, id, onBack = { extension = null },
                machines = services.sessions.machines,
                onUpgrade = onNavigate?.let { navigate -> { market, ext -> navigate(persona.core.navigation.ExtensionCatalogRoute(market, ext)) } }) }
            ?: ExtensionsPage(services.extensions.takeUnless { services.demo }, back,
                onExtension = { id -> onNavigate?.invoke(persona.core.navigation.ExtensionRoute(id)) ?: run { extension = id } },
                onMarket = onNavigate?.let { navigate -> { navigate(persona.core.navigation.ExtensionMarketRoute) } })
        "channels" -> channel?.let { id -> ChannelPage(channelService, id, onBack = { channel = null }, onOpen = onNavigate) }
            ?: ChannelsPage(channelService, back,
                onChannel = { id -> onNavigate?.invoke(persona.core.navigation.ChannelRoute(id)) ?: run { channel = id } },
                onMarketExtension = onNavigate?.let { navigate -> { market, id -> navigate(persona.core.navigation.ExtensionCatalogRoute(market, id)) } })
        "ready", "notify" -> {
            val preferences: LazyListScope.() -> Unit =
                if (page == "notify") NotificationPreferences(deviceController, paired && services.deviceSettings != null,
                    desktops, onDesktops = { open(SettingsPage.Desktops) }) else ({})
            ReadinessScreen(services.sessions, services.diagnostics,
                notificationsOnly = page == "notify", showTitle = page == "notify",
                onDiagnostics = { open(SettingsPage.Diagnostics) }, onDone = back, onBack = back,
                header = preferences, footer = {
                    if (page == "notify") SettingsCaption(stringResource(R.string.settings_notify_system_footer), Modifier.padding(vertical = Tokens.Space.s))
                    deviceFeedback()
                })
        }
        "desktops" -> DesktopsPage(deviceController, services.desktops.takeUnless { services.demo }, paired && services.deviceSettings != null, back)
        "storage" -> StoragePage(back, services.clearCacheFiles, footer = deviceFeedback)
        "providers" -> {
            val providerService = services.providers.takeUnless { services.demo }
            when {
                providerStep == "add" && providerService != null -> NewProviderPage(providerService, onBack = { providerStep = null }, onSaved = { providerStep = null })
                providerStep == "import" && providerService != null -> ProviderImportPage(providerService, machines, onBack = { providerStep = null }, onDone = { providerStep = null })
                provider != null -> ProviderDetailPage(providerService, provider!!, machines, onBack = { provider = null })
                else -> ProvidersPage(providerService, machines, back,
                    onOpen = { name -> onNavigate?.invoke(persona.core.navigation.ModelProviderRoute(name)) ?: run { provider = name } },
                    onAdd = { providerStep = "add" }, onImport = { providerStep = "import" },
                    selected = { name -> detailKey == persona.core.navigation.ModelProviderRoute(name) })
            }
        }
        "devices" -> DevicesPage(deviceController, paired && services.deviceSettings != null, back, deviceFeedback)
        "hub-update" -> HubUpdatePage(hubUpdates, machines, back, online = connection == ConnectionState.Online, onMachine = onNavigate)
        "hubs" -> HubsScreen(services.hubs ?: HubGroups.None, services.addHub?.let { { page = "hubs-add" } }, back, deviceFeedback)
        // Adding a hub is the pairing page with an adding service; it returns to the hub list.
        "hubs-add" -> PairingScreen(services.addHub ?: services.pairing, services.sessions,
            onReady = { page = "hubs" }, onDiagnostics = { open(SettingsPage.Diagnostics) }, diagnostics = services.diagnostics,
            onBack = { page = "hubs" }, skipWelcome = true, attempts = services.pairingAttempts)
        "diag" -> DiagnosticsScreen(services, onBack = back, footer = deviceFeedback)
        "style" -> JListPage(title = stringResource(R.string.settings_style_title), onBack = back) {
            item { AppearanceSettings() }
            item { deviceFeedback() }
        }
        "language" -> LanguagePage(onBack = back, footer = deviceFeedback)
        "persona" -> JListPage(title = stringResource(R.string.settings_avatar_title), onBack = back) {
            item { PersonaSettings(demo = services.demo) }
            item { deviceFeedback() }
        }
        "about" -> JListPage(title = stringResource(R.string.settings_about_persona), onBack = back) {
            item { AboutSettingsPage() }
            item { deviceFeedback() }
        }
        "phone" -> JListPage(title = stringResource(R.string.settings_phone_data_title), onBack = back) {
            item { JNotice(stringResource(R.string.settings_phone_data_notice)) }
            item { SettingsCaption(stringResource(R.string.settings_phone_data_caption), Modifier.padding(vertical = Tokens.Space.m)) }
            item { Action(stringResource(R.string.settings_phone_data_manage)) { PhoneReadiness.open(context, listOf(appDetails)) } }
            item { deviceFeedback() }
        }
        "translate" -> TranslationSettingsPage(services.translation.takeUnless { services.demo }, services.models.takeUnless { services.demo },
            connectorsService, onBack = back, pickingAtStart = TranslationPreview.take(),
            onModel = { onNavigate?.invoke(persona.core.navigation.ModelPurposeRoute("translate")) ?: run { purpose = "translate"; page = "models" } })
        "triage" -> TriageSettingsPage(services.triage.takeUnless { services.demo }, services.models.takeUnless { services.demo }, onBack = back,
            onModel = { onNavigate?.invoke(persona.core.navigation.ModelPurposeRoute("agent.triage")) ?: run { purpose = "agent.triage"; page = "models" } })
        "mail" -> MailAccountsPage(connectorsService, onBack = back, onAdd = { page = "mail-add" })
        "mail-add" -> AddMailAccountPage(connectorsService, onBack = { page = "mail" }, onAdded = { page = "mail" })
        "feishu" -> FeishuConnectorPage(connectorsService, onBack = back, onAgentModel = {
            onNavigate?.invoke(persona.core.navigation.ModelPurposeRoute("lark.agent")) ?: run { purpose = "lark.agent" }
        })
        else -> if (page.startsWith("connector/")) {
            HubConnectorPage(connectorsService, connectorId = page.removePrefix("connector/"), onBack = back)
        } else JListPage(title = stringResource(R.string.settings_title)) {
            if (services.demo) {
                item { JNotice(stringResource(R.string.settings_demo_notice), title = stringResource(R.string.settings_demo_mode)) }
                onExitDemo?.let { exit ->
                    item { JButton(stringResource(R.string.settings_demo_connect), exit, Modifier.fillMaxWidth().padding(top = Tokens.Space.m)) }
                }
            }
            // The extensions' cards and nav pages head the list; they open by key, so only with a navigation host.
            if (onNavigate != null) extensionSlotItems(slotsState, slotCards,
                selected = { place -> detailKey == persona.core.navigation.ExtensionUiRoute(place.extension, place.id, place.title.ifBlank { place.id }) },
                onOpen = { place -> onNavigate(persona.core.navigation.ExtensionUiRoute(place.extension, place.id, place.title.ifBlank { place.id })) })
            item {
                HubCard(name = "Persona hub", connected = paired && connection == ConnectionState.Online,
                    statusText = if (paired) connection.label() else stringResource(R.string.settings_not_paired),
                    facts = if (paired) machines.count { it.online }.let { online ->
                        listOf(stringResource(R.string.settings_hub_computers_online) to pluralStringResource(R.plurals.settings_hub_computers_count, online, online),
                            stringResource(R.string.settings_hub_address) to (services.hubAddress ?: stringResource(R.string.settings_not_provided)))
                    } else emptyList(),
                    busy = connection in setOf(ConnectionState.Connecting, ConnectionState.Updating),
                    modifier = Modifier.padding(top = Tokens.Space.m), onClick = { open(SettingsPage.Diagnostics) })
            }
            item { JSectionHeader(stringResource(R.string.settings_section_connection)) }
            // The hub list is the one row a single hub group adds to settings (docs/android.md「多 hub」).
            // hub 版本 (docs/hub-update.md「App 里」) follows it while there is a real hub (not 体验模式).
            val hubRows = (if (services.hubs != null) 1 else 0) + (if (hubUpdates != null) 1 else 0)
            if (services.hubs != null) item { JGroupItem(GroupPosition.of(0, 3 + hubRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Hubs), title = stringResource(R.string.settings_row_hubs), leadingIcon = PhIcons.HardDrives, subtitle = stringResource(R.string.settings_row_hubs_subtitle), onClick = { open(SettingsPage.Hubs) }) } }
            if (hubUpdates != null) item { JGroupItem(GroupPosition.of(hubRows - 1, 3 + hubRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.HubUpdate), title = stringResource(R.string.settings_row_hub_update), leadingIcon = PhIcons.ArrowsClockwise, subtitle = stringResource(R.string.settings_row_hub_update_subtitle), value = HubUpdateText.summary(hubUpdateStatus), onClick = { open(SettingsPage.HubUpdate) }) } }
            item { JGroupItem(GroupPosition.of(hubRows, 3 + hubRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Pairing), title = stringResource(R.string.settings_row_pairing), leadingIcon = PhIcons.QrCode, subtitle = stringResource(R.string.settings_row_pairing_subtitle), onClick = { open(SettingsPage.Pairing) }) } }
            item { JGroupItem(GroupPosition.of(hubRows + 1, 3 + hubRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Devices), title = stringResource(R.string.settings_devices_title), leadingIcon = PhIcons.DeviceMobile, subtitle = stringResource(R.string.settings_row_devices_subtitle), onClick = { open(SettingsPage.Devices) }) } }
            item { JGroupItem(GroupPosition.of(hubRows + 2, 3 + hubRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Diagnostics), title = stringResource(R.string.settings_diag_title), leadingIcon = PhIcons.Gauge, subtitle = stringResource(R.string.settings_row_diag_subtitle), onClick = { open(SettingsPage.Diagnostics) }) } }
            item { JSectionHeader(stringResource(R.string.settings_section_notifications)) }
            if (services.capsule != null) item { JGroupItem(GroupPosition.Single) {
                val mode by services.capsule.mode.collectAsState()
                JRow(title = stringResource(R.string.settings_capsule_title), leadingIcon = PhIcons.ChatCircle, value = CapsuleSettingsText.label(mode),
                    subtitle = stringResource(R.string.settings_row_capsule_subtitle), selected = detailKey == SettingsPageRoute(SettingsPage.Capsule),
                    onClick = { open(SettingsPage.Capsule) })
            } }
            // 「Mac 上的 Persona 已登录」 while a desktop app is connected (iOS shows it over the Agents list).
            val notifyRows = if (desktops.isNotEmpty() && !services.demo) 3 else 2
            item { JGroupItem(GroupPosition.of(0, notifyRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Notifications), title = stringResource(R.string.settings_ready_notifications), leadingIcon = PhIcons.BellSimple, subtitle = stringResource(R.string.settings_row_notifications_subtitle), onClick = { open(SettingsPage.Notifications) }) } }
            item { JGroupItem(GroupPosition.of(1, notifyRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Readiness), title = stringResource(R.string.settings_row_background), leadingIcon = PhIcons.BatteryCharging, subtitle = stringResource(R.string.settings_row_background_subtitle), onClick = { open(SettingsPage.Readiness) }) } }
            if (notifyRows == 3) item { DesktopPresenceRow(desktops, deviceState.preferences?.muteWhileDesktop == true,
                selected = detailKey == SettingsPageRoute(SettingsPage.Desktops), GroupPosition.of(2, 3), onClick = { open(SettingsPage.Desktops) }) }
            item { JSectionHeader("AI") }
            // iOS's order: 模型供应商, 模型配置, 助手 (a page of the Persona tab, so only with a navigation host), then the uses.
            val assistantRow = onNavigate != null
            val aiRows = if (assistantRow) 5 else 4
            item { JGroupItem(GroupPosition.of(0, aiRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Providers), title = stringResource(R.string.settings_providers_title), leadingIcon = PhIcons.Cpu, subtitle = stringResource(R.string.settings_row_providers_subtitle), onClick = { open(SettingsPage.Providers) }) } }
            item { JGroupItem(GroupPosition.of(1, aiRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.ModelConfig), title = stringResource(R.string.settings_row_models), leadingIcon = PhIcons.Brain, subtitle = stringResource(R.string.settings_row_models_subtitle), onClick = { open(SettingsPage.ModelConfig) }) } }
            if (assistantRow) item { JGroupItem(GroupPosition.of(2, aiRows)) { JRow(selected = detailKey == persona.core.navigation.AssistantSettingsRoute, title = stringResource(R.string.settings_row_assistant), leadingIcon = PhIcons.Sparkle, subtitle = stringResource(R.string.settings_row_assistant_subtitle), onClick = { onNavigate?.invoke(persona.core.navigation.AssistantSettingsRoute) }) } }
            val translationAt = if (assistantRow) 3 else 2
            item { JGroupItem(GroupPosition.of(translationAt, aiRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Translation), title = stringResource(R.string.settings_translation_title), leadingIcon = PhIcons.Translate, subtitle = stringResource(R.string.settings_row_translation_subtitle), value = translationValue, onClick = { open(SettingsPage.Translation) }) } }
            item { JGroupItem(GroupPosition.of(translationAt + 1, aiRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Triage), title = stringResource(R.string.settings_triage_title), leadingIcon = PhIcons.ListChecks, subtitle = stringResource(R.string.settings_row_triage_subtitle), onClick = { open(SettingsPage.Triage) }) } }
            // Extension management and conversation channels are independent hub capabilities.
            if (extensionsAvailable) {
                item { JSectionHeader(stringResource(R.string.settings_section_extensions)) }
                item { JGroupItem(GroupPosition.Single) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Extensions), title = stringResource(R.string.settings_section_extensions), leadingIcon = PhIcons.PuzzlePiece, subtitle = stringResource(R.string.settings_row_extensions_subtitle), value = extensionSummary, onClick = { open(SettingsPage.Extensions) }) } }
            }
            item { JSectionHeader(stringResource(R.string.settings_section_computers)) }
            item { JGroupItem(GroupPosition.of(0, 2)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.ClaudeCode), title = stringResource(R.string.settings_row_claude), leadingIcon = PhIcons.TerminalWindow, subtitle = stringResource(R.string.settings_row_claude_subtitle), onClick = { open(SettingsPage.ClaudeCode) }) } }
            item { JGroupItem(GroupPosition.of(1, 2)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Codex), title = stringResource(R.string.settings_row_codex), leadingIcon = PhIcons.Code, subtitle = stringResource(R.string.settings_row_codex_subtitle), onClick = { open(SettingsPage.Codex) }) } }
            if (channelsAvailable) {
                item { JSectionHeader(stringResource(R.string.settings_section_channels)) }
                item {
                    ChannelsSettingsRow(channelList, GroupPosition.Single,
                        selected = detailKey == SettingsPageRoute(SettingsPage.Channels), onClick = { open(SettingsPage.Channels) })
                }
            }
            item { JSectionHeader(stringResource(R.string.settings_section_connectors)) }
            val connectorRows = connectorRows(hubConnectors, lark = larkChats)
            fun connectorPosition(row: ConnectorRow) = GroupPosition.of(connectorRows.indexOf(row), connectorRows.size)
            item {
                JGroupItem(connectorPosition(ConnectorRow.Phone)) {
                    JRow(
                        selected = detailKey == SettingsPageRoute(SettingsPage.PhoneData),
                        title = stringResource(R.string.settings_diag_phone),
                        leadingIcon = PhIcons.DeviceMobile,
                        subtitle = stringResource(R.string.settings_row_phone_subtitle),
                        value = ConnectorText.device(phoneOn),
                        onClick = { if (onOpen == null && onPhoneData != null) onPhoneData() else open(SettingsPage.PhoneData) },
                    )
                }
            }
            item {
                JGroupItem(connectorPosition(ConnectorRow.Mail)) {
                    JRow(
                        selected = detailKey == SettingsPageRoute(SettingsPage.Mail),
                        title = stringResource(R.string.settings_row_mail),
                        leadingIcon = PhIcons.EnvelopeSimple,
                        subtitle = stringResource(R.string.settings_row_mail_subtitle),
                        value = ConnectorText.mail(mailAccounts),
                        onClick = { open(SettingsPage.Mail) },
                    )
                }
            }
            if (ConnectorRow.Lark in connectorRows) item {
                JGroupItem(connectorPosition(ConnectorRow.Lark)) {
                    JRow(
                        selected = detailKey == SettingsPageRoute(SettingsPage.Feishu),
                        title = stringResource(R.string.settings_row_lark),
                        leadingIcon = PhIcons.ChatCircleDots,
                        subtitle = stringResource(R.string.settings_row_lark_subtitle),
                        value = ConnectorText.lark(larkStatus),
                        onClick = { open(SettingsPage.Feishu) },
                    )
                }
            }
            connectorRows.filterIsInstance<ConnectorRow.Hub>().forEach { row ->
                val c = row.connector
                item {
                    JGroupItem(connectorPosition(row)) {
                        JRow(
                            title = c.name,
                            leadingIcon = PhIcons.PlugsConnected,
                            subtitle = listOfNotNull(c.account, c.via).joinToString(" · ").ifEmpty { ConnectorText.state(c) },
                            value = ConnectorText.summary(c),
                            onClick = { page = "connector/${c.id}" },
                        )
                    }
                }
            }
            item {
                SettingsCaption(
                    stringResource(R.string.settings_connectors_caption),
                    Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s),
                )
            }
            item { JSectionHeader(stringResource(R.string.settings_section_appearance)) }
            item { JGroupItem(GroupPosition.of(0, 3)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Persona), title = stringResource(R.string.settings_avatar_title), leadingIcon = PhIcons.Robot, value = LocalPersonaDesign.current.title, onClick = { open(SettingsPage.Persona) }) } }
            item { JGroupItem(GroupPosition.of(1, 3)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Appearance), title = stringResource(R.string.settings_style_title), leadingIcon = PhIcons.SquaresFour, value = PersonaTheme.style.label, onClick = { open(SettingsPage.Appearance) }) } }
            item { JGroupItem(GroupPosition.of(2, 3)) { LanguageRow(selected = detailKey == SettingsPageRoute(SettingsPage.Language), onClick = { open(SettingsPage.Language) }) } }
            item { JSectionHeader(stringResource(R.string.settings_section_about)) }
            item { JGroupItem(GroupPosition.of(0, 3)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.About), title = stringResource(R.string.settings_section_about), leadingIcon = PhIcons.Info, subtitle = stringResource(R.string.settings_row_about_subtitle), onClick = { open(SettingsPage.About) }) } }
            item { JGroupItem(GroupPosition.of(1, 3)) { StorageRow(selected = detailKey == SettingsPageRoute(SettingsPage.Storage), onClick = { open(SettingsPage.Storage) }) } }
            item { JGroupItem(GroupPosition.of(2, 3)) {
                JRow(stringResource(R.string.settings_update_check), value = when {
                    busy -> stringResource(R.string.settings_update_checking)
                    installing -> stringResource(R.string.settings_update_installing)
                    update != null -> stringResource(R.string.settings_update_found)
                    services.demo -> stringResource(R.string.settings_demo_mode)
                    else -> updateMessage?.let { stringResource(it).trimEnd('。', '.') } ?: stringResource(R.string.settings_update_check_hint)
                },
                    enabled = !services.demo && !busy && !installing, onClick = ::checkUpdate)
            } }
            if (update != null || updateMessage != null) item {
                JCard(Modifier.padding(top = Tokens.Space.m)) {
                    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                        update?.let { latest ->
                            Text(stringResource(R.string.settings_update_new_version, latest.versionName), style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                            if (latest.releaseNotes.isNotBlank()) Text(latest.releaseNotes, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
                            JButton(stringResource(if (installing) R.string.settings_update_downloading else R.string.settings_update_install), enabled = !installing, onClick = {
                                if (!context.packageManager.canRequestPackageInstalls()) {
                                    PhoneReadiness.open(context, listOf(SettingsTarget(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, packageUri = true)))
                                    updateMessage = R.string.settings_update_allow_unknown
                                } else scope.launch {
                                    installing = true
                                    try {
                                        AppInstaller.install(context, services.updates, latest)
                                        updateMessage = R.string.settings_update_verified
                                    } catch (error: CancellationException) { throw error }
                                    catch (_: Exception) { updateMessage = R.string.settings_update_failed }
                                    finally { installing = false }
                                }
                            })
                        }
                        updateMessage?.let { SettingsCaption(stringResource(it)) }
                    }
                }
            }
            if (paired) item {
                JButton(stringResource(R.string.settings_disconnect_forget), { confirmDisconnect = true },
                    Modifier.fillMaxWidth().padding(top = Tokens.Space.xl), kind = JButtonKind.Destructive, enabled = !deviceState.busy)
            }
            item { deviceFeedback() }
        }
    }
    // Register after child pages so routed back also takes precedence over the pairing wizard's local steps.
    if (onBack != null) BackHandler(page != "settings", onBack = onBack)
    // An AI use opened inside 模型配置 (no navigation host) closes before the page does.
    BackHandler(page == "models" && purpose != null) { purpose = null }
    BackHandler(page == "plugins" && extension != null) { extension = null }
    BackHandler(page == "channels" && channel != null) { channel = null }
    BackHandler(page == "providers" && (provider != null || providerStep != null)) { if (providerStep != null) providerStep = null else provider = null }
    BackHandler(page == "hubs-add") { page = "hubs" }
    if (confirmDisconnect) JDialog(
        onDismiss = { confirmDisconnect = false }, title = stringResource(R.string.settings_disconnect_title),
        text = stringResource(R.string.settings_disconnect_text),
        destructive = true, confirmEnabled = !deviceState.busy,
        confirm = stringResource(R.string.settings_disconnect) to { confirmDisconnect = false; scope.launch { deviceController.disconnect() }; Unit },
    )
}

@Composable
fun ReadinessScreen(
    sessions: SessionsRepository,
    diagnostics: SettingsDiagnostics? = null,
    notificationsOnly: Boolean = false,
    showTitle: Boolean = true,
    onDiagnostics: () -> Unit,
    onDone: () -> Unit,
    onBack: (() -> Unit)? = onDone,
    header: LazyListScope.() -> Unit = {},
    footer: @Composable () -> Unit = {},
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val preferences = remember { context.getSharedPreferences("settings-readiness", android.content.Context.MODE_PRIVATE) }
    val records = remember(context) { ReadinessRecords(context) }
    var system by remember { mutableStateOf<SystemKind?>(null) }
    var items by remember { mutableStateOf(emptyList<ReadyItem>()) }
    var revision by remember { mutableIntStateOf(0) }
    var testMessage by remember { mutableStateOf<Int?>(null) }
    val machines by sessions.machines.collectAsState()
    fun refresh() { scope.launch {
        val detected = system ?: withContext(Dispatchers.IO) { PhoneReadiness.system() }
        system = detected
        items = PhoneReadiness.items(context, detected)
        if (detected.name == PhoneReadiness.HyperOS) { delay(1_000); items = PhoneReadiness.items(context, detected) }
    } }
    val notifications = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { refresh() }
    val localNetwork = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { refresh() }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { refresh() }
    LaunchedEffect(Unit) {
        refresh()
        try { sessions.refresh() } catch (error: CancellationException) { throw error } catch (_: Exception) { }
    }
    fun finish() {
        items.filter { !notificationsOnly || it.id == "notifications" || it.id.startsWith("channel") }.forEach { item ->
            val decision = when {
                item.satisfied == true -> "verified"
                item.satisfied == null && preferences.getBoolean(item.id, false) -> "confirmed"
                else -> "skipped"
            }
            records.record(item.id, decision)
        }
        if (!notificationsOnly) {
            listOf("machines", "lock-test", "cellular-test").forEach { id ->
                if (records.decision(id) == null) records.record(id, "skipped")
            }
            records.pending(false)
        }
        onDone()
    }
    JListPage(title = stringResource(if (notificationsOnly) R.string.settings_ready_notifications else R.string.settings_ready_title), onBack = onBack, largeTitle = showTitle && onBack == null) {
        header()
        if (notificationsOnly) item { JSectionHeader(stringResource(R.string.settings_ready_channels_header)) }
        item {
            Column(Modifier.padding(vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                SettingsCaption(system?.let { stringResource(R.string.settings_ready_system, PhoneReadiness.systemName(context, it.name), it.version) }
                    ?: stringResource(R.string.settings_ready_detecting))
                SettingsCaption(stringResource(R.string.settings_ready_about))
            }
        }
        items(items.filter { !notificationsOnly || it.id == "notifications" || it.id.startsWith("channel") }, key = { it.id }) { item ->
            val skipped = remember(item.id, revision) { records.skipped(item.id) }
            val confirmed = remember(item.id, revision) { preferences.getBoolean(item.id, false) }
            val status = when (item.satisfied) { true -> Status.Unread; false -> Status.NeedsYou; null -> Status.Unknown }
            JCard(Modifier.padding(bottom = Tokens.Space.m), status = status) {
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    Text(item.title, style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                    StatusLabel(status, text = stringResource(when (item.satisfied) {
                        true -> R.string.settings_ready_satisfied
                        false -> R.string.settings_ready_needs_setup
                        null -> if (confirmed) R.string.settings_ready_confirmed else R.string.settings_ready_needs_confirm
                    }))
                    if (skipped && item.satisfied != true && !confirmed) SettingsCaption(stringResource(R.string.settings_ready_skipped_note))
                    SettingsCaption(item.detail)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        Action(stringResource(R.string.settings_ready_open)) {
                            when {
                                item.id == "notifications" && Build.VERSION.SDK_INT >= 33 &&
                                    !preferences.getBoolean("notification-requested", false) -> {
                                    preferences.edit().putBoolean("notification-requested", true).apply()
                                    notifications.launch(Manifest.permission.POST_NOTIFICATIONS)
                                }
                                item.id == "local-network" && PairingLocalNetwork.missing(context) &&
                                    !preferences.getBoolean("local-network-requested", false) -> {
                                    preferences.edit().putBoolean("local-network-requested", true).apply()
                                    localNetwork.launch(PairingLocalNetwork.Permission)
                                }
                                else -> if (!PhoneReadiness.open(context, item.targets)) {
                                    testMessage = R.string.settings_ready_open_failed
                                }
                            }
                        }
                        if (item.satisfied == null) Action(stringResource(if (confirmed) R.string.settings_ready_undo_confirm else R.string.settings_ready_mark_done)) {
                            preferences.edit().putBoolean(item.id, !confirmed).apply()
                            records.record(item.id, if (confirmed) "pending" else "confirmed"); revision++
                        }
                        if (item.satisfied != true && !confirmed) Action(stringResource(if (skipped) R.string.settings_ready_unskip else R.string.settings_ready_skip)) {
                            records.record(item.id, if (skipped) "pending" else "skipped"); revision++
                        }
                    }
                }
            }
        }
        if (!notificationsOnly) {
            item { JSectionHeader(stringResource(R.string.settings_section_computers), count = machines.size) }
            if (machines.isEmpty()) item { JEmptyState(stringResource(R.string.settings_ready_no_machines), stringResource(R.string.settings_ready_no_machines_detail)) }
            itemsIndexed(machines) { index, machine ->
                JGroupItem(GroupPosition.of(index, machines.size)) {
                    val missing = stringResource(R.string.settings_not_provided)
                    JRow(machine.displayName, value = stringResource(if (machine.online) R.string.settings_online else R.string.settings_offline))
                    JRow("node", value = machine.nodeVersion ?: missing)
                    JRow("herdr", value = machine.herdrVersion ?: missing)
                    JRow("Claude", value = machine.claudeVersion ?: missing)
                    if (machine.nodeMismatch == true) SettingsCaption(stringResource(R.string.settings_ready_node_mismatch), Modifier.padding(Tokens.Space.m))
                }
            }
            item { Action(stringResource(R.string.settings_ready_skip_machines)) { records.record("machines", "skipped"); revision++; testMessage = R.string.settings_ready_skipped_machines } }
            item { LockScreenTestSection(diagnostics, onCheckSettings = ::refresh) }
            item { Action(stringResource(R.string.settings_ready_skip_lock)) { records.record("lock-test", "skipped"); revision++; testMessage = R.string.settings_ready_skipped_lock } }
            item {
                JCard(Modifier.padding(vertical = Tokens.Space.m), status = Status.Unknown) {
                    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        Text(stringResource(R.string.settings_ready_cellular), style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                        SettingsCaption(stringResource(R.string.settings_ready_cellular_about))
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                            Action(stringResource(R.string.settings_ready_cellular_works)) { records.record("cellular-test", "confirmed"); revision++; preferences.edit().putString("cellular-test", CellularTest.Confirmed).apply(); testMessage = R.string.settings_ready_cellular_recorded }
                            Action(stringResource(R.string.settings_ready_skip)) { records.record("cellular-test", "skipped"); revision++; testMessage = R.string.settings_ready_skipped_cellular }
                        }
                    }
                }
            }
            items(listOf("machines" to R.string.settings_ready_machines_check, "lock-test" to R.string.settings_lock_test_title, "cellular-test" to R.string.settings_ready_cellular_test)) { (id, title) ->
                val skipped = remember(id, revision) { records.skipped(id) }
                if (skipped) SettingsCaption(stringResource(R.string.settings_ready_skipped_item, stringResource(title)))
            }
            item { Action(stringResource(R.string.settings_diag_title), onClick = onDiagnostics) }
        }
        item {
            Column(Modifier.padding(vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                testMessage?.let { JNotice(stringResource(it)) }
                SettingsCaption(stringResource(R.string.settings_ready_skip_note))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    Action(stringResource(R.string.settings_ready_skip_rest), enabled = items.isNotEmpty(), onClick = ::finish)
                    JButton(stringResource(R.string.settings_done), ::finish, size = JButtonSize.Small, enabled = items.isNotEmpty())
                }
                footer()
            }
        }
    }
}

/** Reuses the settings checklist for the first successful pairing. */
@Composable
fun OnboardingScreen(services: SettingsServices, onDone: () -> Unit) {
    OnboardingContent(services.bound(), onDone)
}

@Composable
private fun OnboardingContent(services: SettingsServices, onDone: () -> Unit) {
    val context = LocalContext.current
    val records = remember(context) { ReadinessRecords(context) }
    // The app stopped during the engine step after pairing: that step comes back before the checklist.
    var engine by rememberSaveable { mutableStateOf(records.enginePending()?.takeIf { services.machines != null }) }
    engine?.let { machine ->
        EngineSelectionScreen(machine, services.sessions, services.machines, pairing = true, onDone = {
            records.enginePending(null); engine = null
        })
        return
    }
    var diagnostics by rememberSaveable { mutableStateOf(false) }
    BackHandler { if (diagnostics) diagnostics = false else onDone() }
    if (diagnostics) {
        DiagnosticsScreen(services, onBack = { diagnostics = false })
    } else ReadinessScreen(services.sessions, services.diagnostics, notificationsOnly = false, showTitle = false,
        onDiagnostics = { diagnostics = true }, onDone = onDone, onBack = onDone)
}
