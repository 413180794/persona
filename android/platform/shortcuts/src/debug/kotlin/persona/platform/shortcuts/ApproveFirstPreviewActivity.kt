package persona.platform.shortcuts

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import persona.core.contracts.Interaction
import persona.core.contracts.Question
import persona.core.designsystem.AppStyle
import persona.core.designsystem.AppStyleStore

/**
 * Debug builds only: the 「批准第一个待处理」 dialog in one fixed state with made-up data, for captures of every state ×
 * style × light / dark without a hub (the real one is not exported):
 *
 *     adb shell am start -n <包名>/persona.platform.shortcuts.ApproveFirstPreviewActivity \
 *       --es debug.approveState plan --es debug.style board --es debug.theme dark
 *
 * States: loading, nothing, unpaired, question, screen, permission, plan, sending, error. Buttons only close it.
 */
class ApproveFirstPreviewActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val state = state(intent.getStringExtra("debug.approveState"))
        val style = intent.getStringExtra("debug.style")?.let(AppStyle::fromKey) ?: AppStyleStore(this).style.value
        val dark = when (intent.getStringExtra("debug.theme")) { "dark" -> true; "light" -> false; else -> null }
        setContent {
            ShortcutTheme(style, dark) { ApproveFirstDialog(state, onApprove = ::finish, onOpen = { finish() }, onDismiss = ::finish) }
        }
    }

    private fun state(name: String?): ApproveState {
        val where = "补齐网络层的单元测试 · MacBook"
        val permission = Interaction("preview-permission", "macbook", "tests", kind = "permission", toolName = "Bash",
            summary = "Bash: swift test --filter NetworkTests", sessionTitle = "补齐网络层的单元测试")
        val plan = Interaction("preview-plan", "studio", "photos", kind = "plan", sessionTitle = "照片同步脚本支持 HEIC",
            plan = "## 支持 HEIC\n\n1. 用 sips 把 HEIC 转成 JPEG，质量 90，保留 EXIF\n2. 转换放到导出前，原文件不动\n3. 已经转过的按内容哈希跳过")
        return when (name) {
            "nothing" -> ApproveState.Nothing
            "unpaired" -> ApproveState.NotPaired
            "question" -> ApproveState.NeedsApp(permission.copy(kind = "question",
                questions = listOf(Question("网络层的测试用哪个框架？"))), where)
            "screen" -> ApproveState.NeedsApp(permission.copy(kind = "screen"), where)
            "permission" -> ApproveState.Confirm(permission, where)
            "plan" -> ApproveState.Confirm(plan, "照片同步脚本支持 HEIC · Mac Studio")
            "sending" -> ApproveState.Confirm(permission, where, sending = true)
            "error" -> ApproveState.Failed(ShortcutText.loadFailure(java.net.ConnectException("refused")))
            else -> ApproveState.Loading
        }
    }
}
