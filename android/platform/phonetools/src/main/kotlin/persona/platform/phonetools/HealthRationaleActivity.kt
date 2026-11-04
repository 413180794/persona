package persona.platform.phonetools

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import persona.core.designsystem.*

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
            PersonaTheme {
                JListPage(title = "健康数据", onBack = { finish() }) {
                    item {
                        Paragraphs("Persona 怎么用你的健康数据", listOf(
                            "只有你在电脑上问 Claude 健康相关的问题时，Claude 才通过 Persona 读一次每天的汇总：步数、距离、活动能量、心率、静息心率、睡眠时长、体重。",
                            "Persona 只读不写，也不在手机上另存这些数据。读到的汇总经你自己的 hub 交给 Claude，进入那次对话的上下文。",
                        ))
                    }
                    item {
                        Paragraphs("随时可以关掉", listOf(
                            "在 Persona › 设置 › 手机数据 关掉「健康」，Claude 就看不到这个工具；也可以在 Health Connect 里撤销 Persona 的权限。",
                        ))
                    }
                }
            }
        }
    }
}

@Composable
private fun Paragraphs(header: String, lines: List<String>) {
    JGroupItem(GroupPosition.Single, Modifier.padding(bottom = 12.dp)) {
        Text(header, style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 12.dp))
        lines.forEach { t ->
            Text(t, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink2,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
        }
    }
}
