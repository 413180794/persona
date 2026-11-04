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
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val connection by services.connection.state.collectAsState()
    val machines by services.sessions.machines.collectAsState()
    val paired by services.pairing.isPaired.collectAsState()
    val deviceController = remember(services) { DeviceSettingsState(services.deviceSettings, services.disconnect) }
    val deviceState by deviceController.state.collectAsState()
    var confirmDisconnect by remember { mutableStateOf(false) }
    var update by remember { mutableStateOf<AppUpdate?>(null) }
    var updateMessage by remember { mutableStateOf<String?>(null) }
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
                updateMessage = if (update == null) "暂未发现可用更新。" else null
            } catch (error: CancellationException) { throw error }
            catch (_: Exception) { updateMessage = "检查更新失败，请稍后重试。" }
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
        else -> "关闭"
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
        extensionSummary = try { ExtensionText.listSummary(extensionService.extensions()) }
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
        deviceState.message?.let { JNotice(it, status = Status.NeedsYou) }
        if (deviceState.needsCleanup) Action("重试清理", enabled = !deviceState.busy) {
            scope.launch { deviceController.disconnect() }
        }
    }
    when (page) {
        "pair" -> PairingScreen(services.pairing, services.sessions, onReady = back,
            onDiagnostics = { open(SettingsPage.Diagnostics) }, diagnostics = services.diagnostics, onBack = back,
            footer = { onDemo?.let { Action("没有 hub？先体验一下", onClick = it) }; deviceFeedback() },
            machines = services.machines.takeUnless { services.demo })
        "claude" -> ClaudeSettingsPage(services.sessions, services.machines.takeUnless { services.demo }, back)
        "models" -> purpose?.let { id -> ModelPurposePage(services.models.takeUnless { services.demo }, id, onBack = { purpose = null },
                providers = services.providers.takeUnless { services.demo }) }
            ?: ModelConfigurationPage(services.models.takeUnless { services.demo }, back,
                onPurpose = { id -> onNavigate?.invoke(persona.core.navigation.ModelPurposeRoute(id)) ?: run { purpose = id } },
                providers = services.providers.takeUnless { services.demo })
        "plugins" -> extension?.let { id -> ExtensionPage(services.extensions.takeUnless { services.demo }, id, onBack = { extension = null }) }
            ?: ExtensionsPage(services.extensions.takeUnless { services.demo }, back,
                onExtension = { id -> onNavigate?.invoke(persona.core.navigation.ExtensionRoute(id)) ?: run { extension = id } },
                onMarket = onNavigate?.let { navigate -> { navigate(persona.core.navigation.ExtensionMarketRoute) } })
        "channels" -> channel?.let { id -> ChannelPage(channelService, id, onBack = { channel = null }, onOpen = onNavigate) }
            ?: ChannelsPage(channelService, back,
                onChannel = { id -> onNavigate?.invoke(persona.core.navigation.ChannelRoute(id)) ?: run { channel = id } },
                onMarketExtension = onNavigate?.let { navigate -> { market, id -> navigate(persona.core.navigation.ExtensionCatalogRoute(market, id)) } })
        "ready", "notify" -> {
            val preferences: LazyListScope.() -> Unit =
                if (page == "notify") NotificationPreferences(deviceController, paired && services.deviceSettings != null) else ({})
            ReadinessScreen(services.sessions, services.diagnostics,
                notificationsOnly = page == "notify", showTitle = page == "notify",
                onDiagnostics = { open(SettingsPage.Diagnostics) }, onDone = back, onBack = back,
                header = preferences, footer = deviceFeedback)
        }
        "devices" -> DevicesPage(deviceController, paired && services.deviceSettings != null, back, deviceFeedback)
        "hub-update" -> HubUpdatePage(hubUpdates, machines, back, online = connection == ConnectionState.Online, onMachine = onNavigate)
        "hubs" -> HubsScreen(services.hubs ?: HubGroups.None, services.addHub?.let { { page = "hubs-add" } }, back, deviceFeedback)
        // Adding a hub is the pairing page with an adding service; it returns to the hub list.
        "hubs-add" -> PairingScreen(services.addHub ?: services.pairing, services.sessions,
            onReady = { page = "hubs" }, onDiagnostics = { open(SettingsPage.Diagnostics) }, diagnostics = services.diagnostics,
            onBack = { page = "hubs" }, skipWelcome = true)
        "diag" -> DiagnosticsScreen(services, onBack = back, footer = deviceFeedback)
        "style" -> JListPage(title = "界面风格", onBack = back) {
            item { AppearanceSettings() }
            item { deviceFeedback() }
        }
        "persona" -> JListPage(title = "形象", onBack = back) {
            item { PersonaSettings(demo = services.demo) }
            item { deviceFeedback() }
        }
        "about" -> JListPage(title = "关于 Persona", onBack = back) {
            item { AboutSettingsPage() }
            item { deviceFeedback() }
        }
        "phone" -> JListPage(title = "手机数据", onBack = back) {
            item { JNotice("通讯录、日历、照片和位置由手机工具在使用时申请权限。你可以随时到应用权限里关闭。") }
            item { SettingsCaption("剪贴板只能在 App 打开时读取。Android 不提供提醒事项数据源。", Modifier.padding(vertical = Tokens.Space.m)) }
            item { Action("管理权限") { PhoneReadiness.open(context, listOf(appDetails)) } }
            item { deviceFeedback() }
        }
        "translate" -> TranslationSettingsPage(services.translation.takeUnless { services.demo }, services.models.takeUnless { services.demo },
            connectorsService, onBack = back, pickingAtStart = TranslationPreview.take(),
            onModel = { onNavigate?.invoke(persona.core.navigation.ModelPurposeRoute("translate")) ?: run { purpose = "translate"; page = "models" } })
        "mail" -> MailAccountsPage(connectorsService, onBack = back, onAdd = { page = "mail-add" })
        "mail-add" -> AddMailAccountPage(connectorsService, onBack = { page = "mail" }, onAdded = { page = "mail" })
        "feishu" -> FeishuConnectorPage(connectorsService, onBack = back, onAgentModel = {
            onNavigate?.invoke(persona.core.navigation.ModelPurposeRoute("lark.agent")) ?: run { purpose = "lark.agent" }
        })
        else -> if (page.startsWith("connector/")) {
            HubConnectorPage(connectorsService, connectorId = page.removePrefix("connector/"), onBack = back)
        } else JListPage(title = "设置") {
            if (services.demo) {
                item { JNotice("示例数据，不会连接 hub 或安装更新", title = "体验模式") }
                onExitDemo?.let { exit ->
                    item { JButton("连接我的 hub", exit, Modifier.fillMaxWidth().padding(top = Tokens.Space.m)) }
                }
            }
            // The extensions' cards and nav pages head the list; they open by key, so only with a navigation host.
            if (onNavigate != null) extensionSlotItems(slotsState, slotCards,
                selected = { place -> detailKey == persona.core.navigation.ExtensionUiRoute(place.extension, place.id, place.title.ifBlank { place.id }) },
                onOpen = { place -> onNavigate(persona.core.navigation.ExtensionUiRoute(place.extension, place.id, place.title.ifBlank { place.id })) })
            item {
                HubCard(name = "Persona hub", connected = paired && connection == ConnectionState.Online,
                    statusText = if (paired) connection.label() else "尚未配对",
                    facts = if (paired) listOf("电脑在线" to "${machines.count { it.online }} 台", "hub 地址" to (services.hubAddress ?: "未提供")) else emptyList(),
                    busy = connection in setOf(ConnectionState.Connecting, ConnectionState.Updating),
                    modifier = Modifier.padding(top = Tokens.Space.m), onClick = { open(SettingsPage.Diagnostics) })
            }
            item { JSectionHeader("连接") }
            // The hub list is the one row a single hub group adds to settings (docs/android.md「多 hub」).
            // hub 版本 (docs/hub-update.md「App 里」) follows it while there is a real hub (not 体验模式).
            val hubRows = (if (services.hubs != null) 1 else 0) + (if (hubUpdates != null) 1 else 0)
            if (services.hubs != null) item { JGroupItem(GroupPosition.of(0, 3 + hubRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Hubs), title = "hub", leadingIcon = PhIcons.HardDrives, subtitle = "添加、改名和断开 hub", onClick = { open(SettingsPage.Hubs) }) } }
            if (hubUpdates != null) item { JGroupItem(GroupPosition.of(hubRows - 1, 3 + hubRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.HubUpdate), title = "hub 版本", leadingIcon = PhIcons.ArrowsClockwise, subtitle = "版本、检查和安装更新、自动更新", value = HubUpdateText.summary(hubUpdateStatus), onClick = { open(SettingsPage.HubUpdate) }) } }
            item { JGroupItem(GroupPosition.of(hubRows, 3 + hubRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Pairing), title = "配对", leadingIcon = PhIcons.QrCode, subtitle = "扫码、粘贴链接或手动输入", onClick = { open(SettingsPage.Pairing) }) } }
            item { JGroupItem(GroupPosition.of(hubRows + 1, 3 + hubRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Devices), title = "设备", leadingIcon = PhIcons.DeviceMobile, subtitle = "已配对设备和访问权限", onClick = { open(SettingsPage.Devices) }) } }
            item { JGroupItem(GroupPosition.of(hubRows + 2, 3 + hubRows)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Diagnostics), title = "连接诊断", leadingIcon = PhIcons.Gauge, subtitle = "连接路径、探测结果和记录", onClick = { open(SettingsPage.Diagnostics) }) } }
            item { JSectionHeader("通知与后台") }
            item { JGroupItem(GroupPosition.of(0, 2)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Notifications), title = "通知", leadingIcon = PhIcons.BellSimple, subtitle = "需要你、做完了和系统通知渠道", onClick = { open(SettingsPage.Notifications) }) } }
            item { JGroupItem(GroupPosition.of(1, 2)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Readiness), title = "后台保活", leadingIcon = PhIcons.BatteryCharging, subtitle = "通知、电池和系统后台设置", onClick = { open(SettingsPage.Readiness) }) } }
            // The assistant page lives in the assistant module; only a navigation host can open it.
            if (onOpen != null) {
                item { JSectionHeader("助手") }
                item { JGroupItem(GroupPosition.Single) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Assistant), title = "Persona", leadingIcon = PhIcons.Sparkle, subtitle = "在哪台机器上运行、关于我、记忆和时区", onClick = { open(SettingsPage.Assistant) }) } }
            }
            item { JSectionHeader("AI") }
            item { JGroupItem(GroupPosition.of(0, 2)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.ModelConfig), title = "模型配置", leadingIcon = PhIcons.Brain, subtitle = "对话、分拣、翻译和各项 AI 功能统一选择模型", onClick = { open(SettingsPage.ModelConfig) }) } }
            item { JGroupItem(GroupPosition.of(1, 2)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Translation), title = "翻译", leadingIcon = PhIcons.Translate, subtitle = "邮件和选中的飞书会话自动翻译", value = translationValue, onClick = { open(SettingsPage.Translation) }) } }
            // 扩展 shows only when the hub manages extensions (docs/extensions.md); its value says how many need the user.
            // 渠道 right under it while the hub has channels (docs/channels.md「界面」): how many are connected, red when one went wrong.
            val extensions = listOfNotNull("plugins".takeIf { extensionsAvailable }, "channels".takeIf { channelsAvailable })
            if (extensions.isNotEmpty()) item { JSectionHeader("扩展") }
            if (extensionsAvailable) {
                item { JGroupItem(GroupPosition.of(extensions.indexOf("plugins"), extensions.size)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Extensions), title = "扩展", leadingIcon = PhIcons.PuzzlePiece, subtitle = "开关、设置和密钥", value = extensionSummary, onClick = { open(SettingsPage.Extensions) }) } }
            }
            if (channelsAvailable) item {
                ChannelsSettingsRow(channelList, GroupPosition.of(extensions.indexOf("channels"), extensions.size),
                    selected = detailKey == SettingsPageRoute(SettingsPage.Channels), onClick = { open(SettingsPage.Channels) })
            }
            item { JSectionHeader("电脑") }
            item { JGroupItem(GroupPosition.Single) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.ClaudeCode), title = "Claude Code 设置", leadingIcon = PhIcons.TerminalWindow, subtitle = "各台电脑上的常用选项，可以一起改", onClick = { open(SettingsPage.ClaudeCode) }) } }
            item { JSectionHeader("连接器") }
            val connectorRows = connectorRows(hubConnectors, lark = larkChats)
            fun connectorPosition(row: ConnectorRow) = GroupPosition.of(connectorRows.indexOf(row), connectorRows.size)
            item {
                JGroupItem(connectorPosition(ConnectorRow.Phone)) {
                    JRow(
                        selected = detailKey == SettingsPageRoute(SettingsPage.PhoneData),
                        title = "这台手机",
                        leadingIcon = PhIcons.DeviceMobile,
                        subtitle = "通讯录、日历、照片、位置和剪贴板",
                        value = ConnectorText.device(phoneOn),
                        onClick = { if (onOpen == null && onPhoneData != null) onPhoneData() else open(SettingsPage.PhoneData) },
                    )
                }
            }
            item {
                JGroupItem(connectorPosition(ConnectorRow.Mail)) {
                    JRow(
                        selected = detailKey == SettingsPageRoute(SettingsPage.Mail),
                        title = "邮箱",
                        leadingIcon = PhIcons.EnvelopeSimple,
                        subtitle = "Gmail、飞书邮箱和其他 IMAP",
                        value = ConnectorText.mail(mailAccounts),
                        onClick = { open(SettingsPage.Mail) },
                    )
                }
            }
            if (ConnectorRow.Lark in connectorRows) item {
                JGroupItem(connectorPosition(ConnectorRow.Lark)) {
                    JRow(
                        selected = detailKey == SettingsPageRoute(SettingsPage.Feishu),
                        title = "飞书",
                        leadingIcon = PhIcons.ChatCircleDots,
                        subtitle = "lark-connector 连接状态与账号",
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
                    "Persona 能读、能替你操作的数据来源：助手、AI 分身和 Agent 通过它们查日程、读消息、发邮件；替你出面的操作都会先问你。",
                    Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s),
                )
            }
            item { JSectionHeader("外观") }
            item { JGroupItem(GroupPosition.of(0, 2)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Persona), title = "形象", leadingIcon = PhIcons.Robot, value = LocalPersonaDesign.current.title, onClick = { open(SettingsPage.Persona) }) } }
            item { JGroupItem(GroupPosition.of(1, 2)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.Appearance), title = "界面风格", leadingIcon = PhIcons.SquaresFour, value = PersonaTheme.style.label, onClick = { open(SettingsPage.Appearance) }) } }
            item { JSectionHeader("关于") }
            item { JGroupItem(GroupPosition.of(0, 2)) { JRow(selected = detailKey == SettingsPageRoute(SettingsPage.About), title = "关于", leadingIcon = PhIcons.Info, subtitle = "当前版本和构建号", onClick = { open(SettingsPage.About) }) } }
            item { JGroupItem(GroupPosition.of(1, 2)) {
                JRow("检查更新", value = when { busy -> "正在检查"; installing -> "正在安装"; update != null -> "发现新版本"; services.demo -> "体验模式"; else -> updateMessage?.removeSuffix("。") ?: "检查可用版本" },
                    enabled = !services.demo && !busy && !installing, onClick = ::checkUpdate)
            } }
            if (update != null || updateMessage != null) item {
                JCard(Modifier.padding(top = Tokens.Space.m)) {
                    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                        update?.let { latest ->
                            Text("有新版本 ${latest.versionName}", style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                            if (latest.releaseNotes.isNotBlank()) Text(latest.releaseNotes, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
                            JButton(if (installing) "正在下载和校验" else "下载并安装", enabled = !installing, onClick = {
                                if (!context.packageManager.canRequestPackageInstalls()) {
                                    PhoneReadiness.open(context, listOf(SettingsTarget(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, packageUri = true)))
                                    updateMessage = "请允许安装未知应用，返回后再次点“下载并安装”。"
                                } else scope.launch {
                                    installing = true
                                    try {
                                        AppInstaller.install(context, services.updates, latest)
                                        updateMessage = "下载和校验完成，请在系统页面确认安装。"
                                    } catch (error: CancellationException) { throw error }
                                    catch (_: Exception) { updateMessage = "更新未完成。请检查下载连接、校验信息和安装权限后重试。" }
                                    finally { installing = false }
                                }
                            })
                        }
                        updateMessage?.let { SettingsCaption(it) }
                    }
                }
            }
            if (paired) item {
                JButton("断开并忘记服务器", { confirmDisconnect = true },
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
    BackHandler(page == "hubs-add") { page = "hubs" }
    if (confirmDisconnect) JDialog(
        onDismiss = { confirmDisconnect = false }, title = "断开配对？",
        text = "将断开与 hub 的连接，清除本机凭据和这个 hub 的本地数据。再次使用需要重新配对。",
        destructive = true, confirmEnabled = !deviceState.busy,
        confirm = "断开" to { confirmDisconnect = false; scope.launch { deviceController.disconnect() }; Unit },
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
    var testMessage by remember { mutableStateOf<String?>(null) }
    val machines by sessions.machines.collectAsState()
    fun refresh() { scope.launch {
        val detected = system ?: withContext(Dispatchers.IO) { PhoneReadiness.system() }
        system = detected
        items = PhoneReadiness.items(context, detected)
        if (detected.name == "澎湃 OS") { delay(1_000); items = PhoneReadiness.items(context, detected) }
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
    JListPage(title = if (notificationsOnly) "通知" else "准备清单", onBack = onBack, largeTitle = showTitle && onBack == null) {
        header()
        if (notificationsOnly) item { JSectionHeader("系统通知渠道") }
        item {
            Column(Modifier.padding(vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                SettingsCaption(system?.let { "${it.name} ${it.version}" } ?: "正在识别系统")
                SettingsCaption("能检查的项目显示当前状态。系统不允许查询的项目，需要你设置后确认。")
            }
        }
        items(items.filter { !notificationsOnly || it.id == "notifications" || it.id.startsWith("channel") }, key = { it.id }) { item ->
            val skipped = remember(item.id, revision) { records.skipped(item.id) }
            val confirmed = remember(item.id, revision) { preferences.getBoolean(item.id, false) }
            val status = when (item.satisfied) { true -> Status.Unread; false -> Status.NeedsYou; null -> Status.Unknown }
            JCard(Modifier.padding(bottom = Tokens.Space.m), status = status) {
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    Text(item.title, style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                    StatusLabel(status, text = when (item.satisfied) { true -> "已满足"; false -> "需要设置"; null -> if (confirmed) "你已确认完成，系统无法核验" else "需要手动确认" })
                    if (skipped && item.satisfied != true && !confirmed) SettingsCaption("已跳过，可随时回来设置")
                    SettingsCaption(item.detail)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        Action("去设置") {
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
                                    testMessage = "系统设置打不开，请从手机设置进入 Persona 应用详情。"
                                }
                            }
                        }
                        if (item.satisfied == null) Action(if (confirmed) "撤销确认" else "已完成") {
                            preferences.edit().putBoolean(item.id, !confirmed).apply()
                            records.record(item.id, if (confirmed) "pending" else "confirmed"); revision++
                        }
                        if (item.satisfied != true && !confirmed) Action(if (skipped) "取消跳过" else "跳过") {
                            records.record(item.id, if (skipped) "pending" else "skipped"); revision++
                        }
                    }
                }
            }
        }
        if (!notificationsOnly) {
            item { JSectionHeader("电脑", count = machines.size) }
            if (machines.isEmpty()) item { JEmptyState("还没有机器信息", "可稍后在 Agents 中检查。") }
            itemsIndexed(machines) { index, machine ->
                JGroupItem(GroupPosition.of(index, machines.size)) {
                    JRow(machine.displayName, value = if (machine.online) "在线" else "离线")
                    JRow("node", value = machine.nodeVersion ?: "未提供")
                    JRow("herdr", value = machine.herdrVersion ?: "未提供")
                    JRow("Claude", value = machine.claudeVersion ?: "未提供")
                    if (machine.nodeMismatch == true) SettingsCaption("node 与 hub 部署版本不一致", Modifier.padding(Tokens.Space.m))
                }
            }
            item { Action("跳过电脑检查") { records.record("machines", "skipped"); revision++; testMessage = "已跳过电脑检查，稍后可在 Agents 查看在线状态和版本。" } }
            item { LockScreenTestSection(diagnostics, onCheckSettings = ::refresh) }
            item { Action("跳过锁屏测试") { records.record("lock-test", "skipped"); revision++; testMessage = "已跳过锁屏测试，暂时无法确认锁屏通知是否正常。" } }
            item {
                JCard(Modifier.padding(vertical = Tokens.Space.m), status = Status.Unknown) {
                    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                        Text("蜂窝网络", style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
                        SettingsCaption("关掉 Wi-Fi，用蜂窝网络再试一次，确认在外面也能连上。")
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                            Action("已试通") { records.record("cellular-test", "confirmed"); revision++; preferences.edit().putString("cellular-test", "用户确认已试通").apply(); testMessage = "已记录到连接诊断。" }
                            Action("跳过") { records.record("cellular-test", "skipped"); revision++; testMessage = "已跳过蜂窝测试，暂时无法确认外出时能否连接。" }
                        }
                    }
                }
            }
            items(listOf("machines" to "电脑检查", "lock-test" to "锁屏测试", "cellular-test" to "蜂窝测试")) { (id, title) ->
                val skipped = remember(id, revision) { records.skipped(id) }
                if (skipped) SettingsCaption("$title：已跳过")
            }
            item { Action("连接诊断", onClick = onDiagnostics) }
        }
        item {
            Column(Modifier.padding(vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                testMessage?.let { JNotice(it) }
                SettingsCaption("可以先跳过。未设置的项目可能影响锁屏后的通知和后台连接，之后可回到这里补。")
                FlowRow(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    Action("跳过未完成项", enabled = items.isNotEmpty(), onClick = ::finish)
                    JButton("完成", ::finish, size = JButtonSize.Small, enabled = items.isNotEmpty())
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
