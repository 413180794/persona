package persona.feature.settings

import android.Manifest
import android.content.ClipboardManager
import android.content.Context
import android.content.pm.PackageManager
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.ui.text.input.KeyboardType
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import java.time.LocalTime
import java.time.format.DateTimeFormatter

@Composable
fun PairingScreen(
    pairing: PairingService,
    sessions: SessionsRepository,
    initialLink: String? = null,
    onReady: () -> Unit,
    onDiagnostics: () -> Unit,
    diagnostics: SettingsDiagnostics? = null,
    onCredentialsSaved: () -> Unit = {},
    onBack: (() -> Unit)? = null,
    footer: @Composable () -> Unit = {},
    /** Start at the connect step instead of the welcome page (adding another hub). */
    skipWelcome: Boolean = false,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val paired by pairing.isPaired.collectAsState()
    val records = remember(context) { ReadinessRecords(context) }
    var readiness by rememberSaveable { mutableStateOf(paired && needsOnboardingReadiness(context)) }
    var welcome by rememberSaveable { mutableStateOf(initialLink == null && !skipWelcome) }
    var credentialsSaved by remember { mutableStateOf(false) }
    var lastRequest by remember { mutableStateOf<PairingRequest?>(null) }
    var awaitingSettings by remember { mutableStateOf(false) }
    var permissionDenied by rememberSaveable { mutableStateOf(false) }
    // Credentials intentionally stay out of saved-instance state and preferences.
    var address by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    var metadata by remember { mutableStateOf<PairingRequest?>(null) }
    var failure by remember { mutableStateOf<PairingFailure?>(null) }
    var running by remember { mutableStateOf(false) }
    var progress by remember { mutableStateOf(listOf<PairingProgress>()) }
    var confirm by remember { mutableStateOf<PairingRequest?>(null) }
    var manual by remember { mutableStateOf(false) }
    var info by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<String?>(null) }
    fun accept(link: String) {
        welcome = false; readiness = false; credentialsSaved = false; lastRequest = null
        try {
            val request = PairingInput.parse(link)
            metadata = request; address = request.url; token = request.token; failure = null
            progress = emptyList(); confirm = request; notice = null
        } catch (error: Exception) { failure = pairingFailure(error) }
    }
    LaunchedEffect(initialLink) { if (!readiness) initialLink?.let(::accept) }
    fun connect(request: PairingRequest) {
        if (running) return
        lastRequest = request
        running = true
        scope.launch {
            failure = null; progress = emptyList()
            fun record(item: PairingProgress) {
                val stamped = item.copy(timestamp = item.timestamp ?: LocalTime.now().format(DateTimeFormatter.ofPattern("HH:mm:ss")))
                progress = progress.filterNot { it.stage == item.stage } + stamped
            }
            try {
                record(PairingProgress(PairingStage.Parse))
                if (PairingLocalNetwork.needsPermission(context, request)) {
                    failure = PairingFailure.LocalPermission
                    return@launch
                }
                if (!credentialsSaved) {
                    pairing.pair(request) { record(it) }
                    credentialsSaved = true
                    records.pending(true)
                    // The link's pairing code is spent; the host must not offer it for confirmation again.
                    onCredentialsSaved()
                }
                record(PairingProgress(PairingStage.LoadMachines))
                try { sessions.refresh() }
                catch (error: CancellationException) { throw error }
                catch (error: Exception) {
                    // The pairing is saved and the shell refreshes on its own; a slow first list is not a failed pairing.
                    android.util.Log.w("JarvisPairing", "machine list after pairing: " + error.javaClass.name)
                    notice = "已经连上 hub，机器列表稍后会自动刷新。"
                }
                record(PairingProgress(PairingStage.Complete))
                token = ""; metadata = null; lastRequest = null; readiness = true
            } catch (error: CancellationException) { throw error }
            catch (error: Exception) {
                // Exception messages may contain pairing links or credentials: log only types and frames.
                android.util.Log.w("JarvisPairing", generateSequence(error as Throwable) { it.cause }.take(4)
                    .joinToString(" <- ") { e -> e.javaClass.name + " @ " + e.stackTrace.take(3).joinToString(", ") })
                failure = pairingFailure(error)
                if (failure == PairingFailure.Timeout && PairingLocalNetwork.needsPermission(context, request)) {
                    failure = PairingFailure.LocalPermission
                }
            }
            finally { running = false }
        }
    }
    val localPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) lastRequest?.let(::connect)
        else { permissionDenied = true; failure = PairingFailure.LocalPermission; notice = "未获得本地网络权限。可以去系统设置授权，再回来重试。" }
    }
    fun authorize() {
        if (!PairingLocalNetwork.missing(context)) lastRequest?.let(::connect)
        else if (permissionDenied) {
            awaitingSettings = PhoneReadiness.open(context, listOf(appDetails))
        } else localPermission.launch(PairingLocalNetwork.Permission)
    }
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        if (awaitingSettings) {
            awaitingSettings = false
            if (!PairingLocalNetwork.missing(context)) { notice = null; lastRequest?.let(::connect) }
        }
    }
    val scanner = rememberLauncherForActivityResult(ScanContract()) { result ->
        result.contents?.let(::accept)
        if (result.originalIntent?.getBooleanExtra(PairingScanActivity.ManualInput, false) == true) {
            credentialsSaved = false; lastRequest = null
            confirm = null; failure = null; progress = emptyList(); manual = true
        }
    }
    fun scan() { scanner.launch(ScanOptions().setCaptureActivity(PairingScanActivity::class.java).setOrientationLocked(false).setDesiredBarcodeFormats(ScanOptions.QR_CODE).setPrompt("").setBeepEnabled(false)) }
    val camera = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) scan() else { credentialsSaved = false; lastRequest = null; confirm = null; failure = null; progress = emptyList(); manual = true; notice = "相机权限未开启，可以手动输入地址和配对码。" }
    }
    fun requestScan() {
        if (context.checkSelfPermission(Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) scan()
        else camera.launch(Manifest.permission.CAMERA)
    }
    fun paste() {
        val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        val value = clipboard.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(context)?.toString()
        if (value.isNullOrBlank()) notice = "剪贴板里没有配对链接。" else accept(value)
    }
    fun edit() { credentialsSaved = false; lastRequest = null; confirm = null; failure = null; progress = emptyList(); manual = true }
    BackHandler(!readiness && !running && (confirm != null || manual || failure != null)) {
        confirm = null; manual = false; failure = null; progress = emptyList(); notice = null
    }
    if (readiness) {
        ReadinessScreen(sessions, diagnostics, notificationsOnly = false,
            onDiagnostics = onDiagnostics, onDone = { records.pending(false); onReady() }, onBack = onBack ?: onReady, footer = footer)
    } else JListPage(title = "配对", onBack = onBack, largeTitle = false) {
        when {
            welcome -> {
                item { PairingHeading("欢迎使用 Jarvis", "把 hub 接到手机上，随时查看电脑上的 Agent，处理需要你确认的事。") }
                item { JGroupItem(GroupPosition.of(0, 3)) { JRow("Agents", leadingIcon = PhIcons.Robot, subtitle = "查看执行过程，回答提问、审批计划和工具操作。", subtitleLines = 3) } }
                item { JGroupItem(GroupPosition.of(1, 3)) { JRow("收件箱", leadingIcon = PhIcons.Tray, subtitle = "集中查看飞书和邮件，让 AI 帮你分拣、草拟回复。", subtitleLines = 3) } }
                item { JGroupItem(GroupPosition.of(2, 3)) { JRow("Jarvis 助手", leadingIcon = PhIcons.Sparkle, subtitle = "连接你的知识库，手机数据会在需要时单独申请授权。", subtitleLines = 3) } }
                item { SettingsCaption("没有 Google 服务也能收通知：手机通过常驻连接接收 hub 的消息。", Modifier.padding(vertical = Tokens.Space.m)) }
                item { JButton("连接 hub", { welcome = false }, Modifier.fillMaxWidth()) }
            }
            failure != null -> {
                val reason = requireNotNull(failure)
                val recovery = reason.recovery()
                item { PairingHeading("连接未完成", "按下面的提示处理后，再试一次。") }
                item { JNotice(reason.explanation(), status = Status.NeedsYou) }
                item {
                    PairingActions {
                        JButton(if (credentialsSaved && recovery == PairingRecovery.Retry) "重新读取机器" else recovery.label, {
                            when (recovery) {
                                PairingRecovery.Permission -> authorize()
                                PairingRecovery.Scan -> requestScan()
                                PairingRecovery.Retry, PairingRecovery.Update -> lastRequest?.let(::connect) ?: edit()
                            }
                        }, Modifier.fillMaxWidth())
                        JButton("连接诊断", onDiagnostics, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary)
                        if (recovery != PairingRecovery.Scan) JButton("重新扫码", ::requestScan, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary, icon = PhIcons.Scan)
                        JButton("修改地址或配对码", ::edit, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary)
                    }
                }
                if (progress.isNotEmpty()) pairingProgress(progress, running, failed = true)
            }
            running || progress.isNotEmpty() -> {
                item { PairingHeading("正在连接 hub", "连接通过身份校验后，才会保存本机凭据。") }
                pairingProgress(progress, running, failed = false)
            }
            confirm != null -> {
                val request = requireNotNull(confirm)
                item { PairingHeading("确认配对", "核对 hub 地址和公钥指纹，确认是你要连接的 hub。") }
                pairingDetails(request)
                if (paired) item { JNotice("确认后，手机将改连这个 hub。", title = "将替换当前配对", status = Status.NeedsYou) }
                item {
                    PairingActions {
                        JButton("确认配对", { confirm = null; connect(request) }, Modifier.fillMaxWidth())
                        JButton("修改输入", ::edit, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary)
                    }
                }
            }
            manual -> {
                item { PairingHeading("手动输入", "填写 hub 地址和配对码，也可以粘贴完整的配对链接。") }
                item {
                    JCard {
                        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                            JTextField(address, { value ->
                                address = value; metadata = metadata?.copy(url = value)
                                if (value.trim().startsWith("jarvis:")) accept(value)
                            }, label = "hub 地址或配对链接", modifier = Modifier.fillMaxWidth(),
                                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri), singleLine = true)
                            JTextField(token, { token = it; metadata = metadata?.copy(token = it) }, label = "配对码",
                                visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                                modifier = Modifier.fillMaxWidth(), singleLine = true)
                            if (address.startsWith("http://")) SettingsCaption("HTTP 不加密，只在内网或 VPN 里使用。在外面请使用加密连接。")
                        }
                    }
                }
                item {
                    PairingActions {
                        JButton("下一步", {
                            try { confirm = metadata ?: PairingInput.manual(address, token) }
                            catch (error: Exception) { failure = pairingFailure(error) }
                        }, Modifier.fillMaxWidth(), enabled = address.isNotBlank() && token.isNotBlank())
                        JButton("粘贴链接", ::paste, Modifier.fillMaxWidth(), kind = JButtonKind.Secondary)
                        JTextButton("返回扫码配对", { manual = false })
                    }
                }
            }
            else -> {
                item { PairingHeading("连接你的 hub", "在 hub 所在的机器上运行下面的命令，再扫描显示的二维码。") }
                item { JCodeBlock("jarvisd hub pair") }
                item {
                    PairingActions {
                        JButton("扫码配对", ::requestScan, Modifier.fillMaxWidth(), icon = PhIcons.Scan)
                        Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                            JButton("粘贴链接", ::paste, Modifier.weight(1f), kind = JButtonKind.Secondary)
                            JButton("手动输入", { manual = true }, Modifier.weight(1f), kind = JButtonKind.Secondary)
                        }
                        SettingsCaption("没有 Google 服务也能收通知：通知走手机和 hub 的常驻连接。")
                    }
                }
            }
        }
        notice?.let { item { JNotice(it, Modifier.padding(top = Tokens.Space.m)) } }
        item { JTextButton("配对会保存什么", { info = true }, Modifier.padding(top = Tokens.Space.m)) }
        item { footer() }
    }
    if (info) JDialog(onDismiss = { info = false }, title = "配对会保存什么",
        text = "手机保存本机凭据、hub 公钥指纹和地址列表，由连接组件加密存储。hub 保存设备凭据的哈希，你可以撤销设备。Android 通知使用常驻连接。",
        confirm = "知道了" to { info = false }, dismiss = null)
}

@Composable
private fun PairingActions(content: @Composable ColumnScope.() -> Unit) {
    Column(Modifier.fillMaxWidth().padding(top = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m), content = content)
}

@Composable
private fun PairingHeading(title: String, description: String) {
    Column(Modifier.padding(vertical = Tokens.Space.l), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        Text(title, style = PersonaTheme.type.largeTitle, color = PersonaTheme.palette.ink)
        Text(description, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
    }
}

private fun LazyListScope.pairingDetails(request: PairingRequest) {
    val details = listOf("hub 地址" to request.url,
        "传输加密" to if (request.url.startsWith("https://")) "HTTPS 加密连接" else "HTTP 地址，未加密",
        "hub 身份" to (request.hub ?: "未提供"), "公钥指纹" to (request.fp ?: "未提供")) +
        listOfNotNull(request.tls?.let { "TLS 端口" to it.toString() })
    itemsIndexed(details) { index, (label, value) ->
        JGroupItem(GroupPosition.of(index, details.size)) { PairingDetail(label, value) }
    }
    if (request.url.startsWith("http://")) item {
        JNotice(if (request.fp != null) "二维码带有公钥指纹，连接时会探测并校验加密地址。"
            else "HTTP 不加密，只在内网或 VPN 里使用。在外面请使用加密连接。", Modifier.padding(top = Tokens.Space.m))
    }
    item { JSectionHeader("候选地址") }
    val addresses = (listOf(request.url) + request.alt).distinct()
    itemsIndexed(addresses) { index, url ->
        JGroupItem(GroupPosition.of(index, addresses.size)) { PairingDetail(if (index == 0) "主地址" else "备用地址 $index", url) }
    }
    item { SettingsCaption("连接时会核对 hub 身份。", Modifier.padding(vertical = Tokens.Space.s)) }
}

@Composable
private fun PairingDetail(label: String, value: String) {
    Column(Modifier.fillMaxWidth().padding(Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
        SettingsCaption(label)
        SelectionContainer { Text(value, style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink) }
    }
}

internal fun pairingSteps(progress: List<PairingProgress>, running: Boolean, failed: Boolean): List<ProgressStep> {
    val current = progress.lastOrNull()?.stage
    return PairingStage.entries.map { stage ->
        val reported = progress.any { it.stage == stage }
        val state = when {
            !reported -> StepState.Pending
            stage == current && failed -> StepState.Failed
            stage == current && running -> StepState.Active
            stage == PairingStage.Complete && current != PairingStage.Complete -> StepState.Pending
            else -> StepState.Complete
        }
        ProgressStep(when (stage) {
            PairingStage.Parse -> "解析二维码"
            PairingStage.Probe -> "探测地址"
            PairingStage.VerifyIdentity -> "校验证书和 hub"
            PairingStage.ExchangeCredentials -> "换本机凭据"
            PairingStage.LoadMachines -> "读机器列表"
            PairingStage.Complete -> "完成"
        }, state)
    }
}

private fun LazyListScope.pairingProgress(progress: List<PairingProgress>, running: Boolean, failed: Boolean) {
    val steps = pairingSteps(progress, running, failed)
    item { JCard { StepProgress(steps) } }
    PairingStage.entries.forEachIndexed { index, stage ->
        val reported = progress.firstOrNull { it.stage == stage }
        val step = steps[index]
        item {
            JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
                JRow(step.title, subtitle = step.state.label, trailing = {
                    Text(reported?.timestamp ?: "—", style = PersonaTheme.type.micro, color = PersonaTheme.palette.ink3)
                })
            }
        }
        itemsIndexed(reported?.candidates.orEmpty()) { candidateIndex, candidate ->
            JGroupItem(GroupPosition.of(candidateIndex, reported?.candidates.orEmpty().size)) {
                PairingDetail(candidate.kind, candidate.url)
                SettingsCaption(candidate.result, Modifier.padding(horizontal = Tokens.Space.m).padding(bottom = Tokens.Space.m))
            }
        }
    }
}
