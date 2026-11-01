package jarvis.android.ui

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.QrCodeScanner
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
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
import kotlinx.coroutines.launch

/** First run: scan the QR code `jarvisd hub pair` prints, or type the address and token. */
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
                // Next: which engine the hub's own machine uses (docs/engines.md), asked once right after pairing.
                PairingEngine.pending.value = Engines.pairingMachine(machines)
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

    Surface(Modifier.fillMaxSize()) {
        Column(
            Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Spacer(Modifier.height(32.dp))
            Text("连接 hub", style = MaterialTheme.typography.headlineMedium)
            reason?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium) }
            Text(
                "在 hub 所在的机器上运行 ~/.jarvis/bin/jarvisd hub pair，扫它打印的二维码。",
                style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Button(
                onClick = {
                    scan.launch(ScanOptions().setDesiredBarcodeFormats(ScanOptions.QR_CODE).setPrompt("扫描配对二维码").setBeepEnabled(false).setOrientationLocked(false))
                },
                enabled = !busy, modifier = Modifier.fillMaxWidth(),
            ) {
                Icon(Icons.Default.QrCodeScanner, null)
                Text("  扫码配对", maxLines = 1)
            }
            Spacer(Modifier.height(8.dp))
            Text("或者手动填写", style = MaterialTheme.typography.titleSmall)
            OutlinedTextField(
                url, { url = it }, label = { Text("hub 地址") }, placeholder = { Text("http://<hub 地址>:18660") },
                singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri), modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                token, { token = it }, label = { Text("token") }, singleLine = true,
                visualTransformation = PasswordVisualTransformation(), modifier = Modifier.fillMaxWidth(),
            )
            OutlinedButton(
                onClick = { pair(Pairing(url.trim(), token.trim())) },
                enabled = !busy && url.startsWith("http") && token.isNotBlank(), modifier = Modifier.fillMaxWidth(),
            ) { Text("连接", maxLines = 1) }
            if (busy) CircularProgressIndicator()
            error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        }
    }
}
