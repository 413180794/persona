package jarvis.android.phone

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.ui.Modifier
import jarvis.android.ui.JTheme
import jarvis.android.ui.JarvisTheme
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.settings.KitGroup
import jarvis.android.ui.settings.KitPage

/**
 * Health Connect's 「why does this app want health data」 page. Its permission dialog does not open at all for an app
 * without one (`androidx.health.ACTION_SHOW_PERMISSIONS_RATIONALE`; Android 14+ looks for the
 * `VIEW_PERMISSION_USAGE` alias in the manifest instead).
 */
class HealthRationaleActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            JarvisTheme {
                KitPage("健康数据", onBack = { finish() }) {
                    item { Paragraphs("Jarvis 怎么用你的健康数据", listOf(
                        "只有你在电脑上问 Claude 健康相关的问题时，Claude 才通过 Jarvis 读一次每天的汇总：步数、距离、活动能量、心率、静息心率、睡眠时长、体重。",
                        "Jarvis 只读不写，也不在手机上另存这些数据。读到的汇总经你自己的 hub 交给 Claude，进入那次对话的上下文。",
                    )) }
                    item { Paragraphs("随时可以关掉", listOf(
                        "在 Jarvis › 设置 › 手机数据 关掉「健康」，Claude 就看不到这个工具；也可以在 Health Connect 里撤销 Jarvis 的权限。",
                    )) }
                }
            }
        }
    }
}

@androidx.compose.runtime.Composable
private fun Paragraphs(header: String, lines: List<String>) {
    KitGroup(header = header) {
        lines.forEach { t ->
            row { Text(t, style = JTheme.type.body, color = JTheme.colors.label, modifier = Modifier.padding(horizontal = JSpace.l, vertical = JSpace.m)) }
        }
    }
}
