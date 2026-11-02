package jarvis.android.ui

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.withStyle
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import jarvis.android.JarvisApp
import jarvis.android.MainActivity
import jarvis.android.data.ApiClient
import jarvis.android.data.HubException
import jarvis.android.data.Engines
import jarvis.android.data.Pairing
import jarvis.android.data.PairingEngine
import jarvis.android.data.PairingStore
import jarvis.android.ui.design.ButtonKind
import jarvis.android.ui.design.ErrorLine
import jarvis.android.ui.design.GroupCard
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JTextField
import jarvis.android.ui.design.JarvisMark
import jarvis.android.ui.design.PillButton
import jarvis.android.ui.design.RowDivider
import jarvis.android.ui.design.SectionHeader
import jarvis.android.ui.settings.KitNoticeRow
import kotlinx.coroutines.launch

/**
 * First run (iOS `WelcomeView` and `PairView` on one page): Jarvis' mark and what it is, 扫码配对 for the QR code
 * `jarvisd hub pair` prints, or the address and token typed into the card below.
 */
@Composable
fun PairScreen(reason: String? = null) {
    var url by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    fun pair(p: Pairing, fromLink: Boolean = false) {
        busy = true; error = null
        scope.launch {
            error = try {
                val api = ApiClient(p.url, p.token)
                val machines = api.machines() // checks address and token together
                // Trade the pairing token for this phone's own, revocable in 设置 › 设备 (an older hub has no such call).
                val own = try { api.createToken(JarvisApp.instance.deviceName).token } catch (e: HubException) {
                    if (e.status == 404) p.token else throw e
                }
                val paired = Pairing(p.url.trimEnd('/'), own)
                JarvisApp.instance.pairings.save(paired)
                // Next: which engine the hub's own machine uses (docs/engines.md), asked once right after pairing; only
                // when the hub reports engines (an older hub cannot save one and the step must not trap).
                PairingEngine.pending.value = Engines.pairingMachine(machines)?.takeIf { it.engines.isNotEmpty() }
                JarvisApp.instance.connect(paired)
                null
            } catch (e: HubException) {
                if (e.status == 401) "token 不对" else e.message
            } catch (e: Exception) {
                "连不上 ${p.url}：${e.message ?: e.javaClass.simpleName}。手机和 hub 要在同一个网络或 VPN 里"
            }
            busy = false
            // Only now: while a link is pending, Root keeps this screen up. Clearing it earlier took the screen
            // (and this coroutine) away mid-pairing when the phone was already paired, so the link did nothing.
            if (fromLink) MainActivity.pendingPair.value = null
        }
    }

    val scan = rememberLauncherForActivityResult(ScanContract()) { r ->
        val p = r.contents?.let { PairingStore.parse(it) }
        if (p != null) { url = p.url; token = p.token; pair(p) } else if (r.contents != null) error = "不是 Jarvis 的配对二维码"
    }
    val link = MainActivity.pendingPair.value
    LaunchedEffect(link) {
        if (link != null) { url = link.url; token = link.token; pair(link, fromLink = true) }
    }

    val c = JTheme.colors
    BoxWithConstraints(Modifier.fillMaxSize().background(c.page), contentAlignment = Alignment.TopCenter) {
        // At least the screen's height, so a short page sits in the middle; a keyboard makes it scroll.
        Column(
            Modifier.widthIn(max = JSize.readable).fillMaxWidth().verticalScroll(rememberScrollState()).heightIn(min = maxHeight)
                .safeDrawingPadding().padding(vertical = JSpace.xxl),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            JarvisMark(JSize.box96)
            Spacer(Modifier.height(JSpace.xl))
            Text("Jarvis", style = JTheme.type.display, color = c.label, maxLines = 1)
            Spacer(Modifier.height(JSpace.sm))
            Text(
                "把每台电脑上的 Claude 装进口袋", style = JTheme.type.title, color = c.secondaryText, textAlign = TextAlign.Center,
                modifier = Modifier.padding(horizontal = JSpace.xl),
            )
            // Why this page is back (the hub refused this phone's token): a notice above the ways to pair again.
            reason?.let { GroupCard(Modifier.fillMaxWidth().padding(top = JSpace.l)) { KitNoticeRow("需要重新配对", it, tint = c.danger) } }

            Spacer(Modifier.height(JSpace.xxl))
            PillButton(
                "扫码配对",
                onClick = {
                    scan.launch(ScanOptions().setDesiredBarcodeFormats(ScanOptions.QR_CODE).setPrompt("扫描配对二维码").setBeepEnabled(false).setOrientationLocked(false))
                },
                modifier = Modifier.fillMaxWidth().padding(horizontal = JSpace.page),
                kind = ButtonKind.Prominent, icon = JIcons.qrScan, enabled = !busy, large = true,
            )
            Text(
                // The command on a line of its own, so it never breaks in the middle.
                buildAnnotatedString {
                    append("在 hub 所在的机器上运行这条命令，扫它打印的二维码\n")
                    withStyle(SpanStyle(fontFamily = JTheme.type.code.fontFamily, color = c.label)) { append("~/.jarvis/bin/jarvisd hub pair") }
                },
                style = JTheme.type.caption, color = c.secondaryText, textAlign = TextAlign.Center,
                modifier = Modifier.padding(horizontal = JSpace.xl).padding(top = JSpace.m),
            )

            Spacer(Modifier.height(JSpace.l))
            SectionHeader("或者手动填写")
            GroupCard {
                FieldRow("地址") {
                    JTextField(
                        url, { url = it }, "http://<hub 地址>:18660", KeyboardOptions(keyboardType = KeyboardType.Uri),
                        Modifier.weight(1f), singleLine = true, fill = Color.Transparent,
                    )
                }
                RowDivider()
                FieldRow("token") {
                    JTextField(token, { token = it }, "配对 token", Modifier.weight(1f), singleLine = true, password = true, fill = Color.Transparent)
                }
            }
            PillButton(
                "连接", onClick = { pair(Pairing(url.trim(), token.trim())) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = JSpace.page).padding(top = JSpace.m),
                kind = ButtonKind.Tinted, large = true, enabled = !busy && url.startsWith("http") && token.isNotBlank(),
            )
            if (busy) {
                Row(Modifier.padding(top = JSpace.l), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
                    CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.rule, color = c.secondaryText)
                    Text("正在连接…", style = JTheme.type.secondary, color = c.secondaryText, maxLines = 1)
                }
            }
            error?.let { ErrorLine(it, Modifier.fillMaxWidth().padding(horizontal = JSpace.page + JSpace.xs).padding(top = JSpace.m)) }
        }
    }
}

/** One line of the manual card: what it is on the left, the field taking the rest. */
@Composable
private fun FieldRow(label: String, field: @Composable RowScope.() -> Unit) {
    Row(Modifier.fillMaxWidth().heightIn(min = JSize.box50).padding(start = JSpace.l, end = JSpace.xs), verticalAlignment = Alignment.CenterVertically) {
        Text(label, style = JTheme.type.body, color = JTheme.colors.label, maxLines = 1, modifier = Modifier.width(JSize.box56))
        field()
    }
}
