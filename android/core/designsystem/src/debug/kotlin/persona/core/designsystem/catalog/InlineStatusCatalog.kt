package persona.core.designsystem.catalog

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.lazy.LazyListScope
import persona.core.designsystem.*

/** The inline status of the chat's "Starting" and a machine's "Redeploy" line, in its three tones. */
internal fun LazyListScope.inlineStatusSamples(onAction: (String) -> Unit) {
    item("inline-status") {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            JSectionHeader("行内状态")
            JInlineStatus("正在启动 Claude，消息会在它就绪后发出")
            JInlineStatus("这台电脑上的 Persona 和 hub 不是同一次部署", tone = InlineTone.Caution, action = {
                JButton("重新部署", { onAction("重新部署") }, kind = JButtonKind.Secondary, size = JButtonSize.Small)
            })
            JInlineStatus("没有启动起来：claude did not become ready within 1m0s", tone = InlineTone.Failed)
        }
    }
}
