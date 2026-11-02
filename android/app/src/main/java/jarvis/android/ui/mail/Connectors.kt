package jarvis.android.ui.mail

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextOverflow
import jarvis.android.data.ApiClient
import jarvis.android.mail.ConnectorText
import jarvis.android.mail.MailStore
import jarvis.android.ui.JTheme
import jarvis.android.ui.ago
import jarvis.android.ui.design.IconTile
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.settings.KitGroup
import jarvis.android.ui.settings.KitPage
import jarvis.android.ui.settings.KitRow
import jarvis.android.ui.settings.KitValueRow
import kotlinx.coroutines.CancellationException
import java.time.Instant

/**
 * 设置's 连接器 group (iOS `ConnectorsSection`, 2026-11-01): where Jarvis' data comes from and acts — this phone's data
 * (the phone_* tools), the mail accounts, 飞书 (the hub's lark-connector) — one row each with what it has now.
 */
@Composable
fun ConnectorsGroup(api: ApiClient, onPhone: () -> Unit, onMail: () -> Unit, onFeishu: () -> Unit, reloadKey: Any? = null) {
    val accounts by MailStore.accounts.collectAsState()
    val lark by MailStore.lark.collectAsState()
    val runner = runCatching { jarvis.android.JarvisApp.instance.phoneTools }.getOrNull()
    val phoneOn = runner?.enabled?.collectAsState()?.value?.size ?: 0
    // Again when a page closes: it may have changed them.
    LaunchedEffect(reloadKey) {
        runCatching { MailStore.reloadAccounts(api) }
        runCatching { MailStore.reloadLark(api) }
    }
    val c = JTheme.colors
    KitGroup(
        header = "连接器",
        footer = "Jarvis 能读、能替你操作的数据来源：助手、AI 分身和 Agent 通过它们查日程、读消息、发邮件；替你出面的操作都会先问你。",
    ) {
        row { KitRow("这台手机", onClick = onPhone, icon = JIcons.phone, iconColor = JTheme.hue.green, value = ConnectorText.device(phoneOn)) }
        row { KitRow("邮箱", onClick = onMail, icon = JIcons.mail, iconColor = c.accent, value = ConnectorText.mail(accounts)) }
        row { KitRow("飞书", onClick = onFeishu, icon = JIcons.feishuChats, iconColor = JTheme.brand.feishu, value = ConnectorText.lark(lark)) }
    }
}

/**
 * 设置 › 连接器 › 飞书 (iOS `FeishuConnectorView`): whether the hub reaches lark-connector, who is signed in, the last
 * real-time event, and what Jarvis does with it. The account itself is signed in on the hub's machine (docs/feishu.md).
 */
@Composable
fun FeishuConnectorScreen(api: ApiClient, onBack: () -> Unit, onAgentModel: () -> Unit, preloaded: Boolean = false) {
    val c = JTheme.colors
    val status by MailStore.lark.collectAsState()
    var loading by remember { mutableStateOf(!preloaded) }
    var failure by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        if (preloaded) return@LaunchedEffect
        try {
            MailStore.reloadLark(api)
            failure = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            failure = "读不到飞书的状态：${e.message}"
        }
        loading = false
    }
    val st = status
    val dot = when {
        st == null -> c.secondaryText
        st.connected -> c.positive
        st.configured -> c.danger
        else -> c.secondaryText
    }
    val state = when {
        st == null -> if (loading) "正在检查…" else "读不到状态"
        !st.configured -> "未配置"
        st.connected -> "已连接"
        else -> "连不上 lark-connector"
    }

    KitPage("飞书", onBack) {
        item(key = "status") {
            KitGroup(
                footer = if (st?.configured == false) "hub 还没接飞书：在 hub 所在的机器上运行 lark-connector，并在 hub.json 的 lark 里填上它（docs/feishu.md）。"
                else "飞书账号在 hub 所在机器的 lark-connector 里登录，这里只看连接状态。",
            ) {
                row {
                    Row(
                        Modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
                    ) {
                        IconTile(JIcons.feishuChats, JTheme.brand.feishu, size = JSize.wellM)
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
                            Text(st?.name?.takeIf { it.isNotBlank() } ?: "飞书", style = JTheme.type.heading, color = c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                                Box(Modifier.size(JSize.dotS).background(dot, CircleShape))
                                Text(state, style = JTheme.type.label, color = c.secondaryText, maxLines = 2)
                            }
                        }
                        if (loading) CircularProgressIndicator(Modifier.size(JSize.iconXS), strokeWidth = JSize.rule, color = c.secondaryText)
                    }
                }
                st?.lastEventAt?.takeIf { it > 0 }?.let { at -> row { KitValueRow("最近一条实时消息", ago(Instant.ofEpochSecond(at))) } }
                (st?.error?.takeIf { it.isNotBlank() } ?: failure)?.let { e ->
                    row { Text(e, style = JTheme.type.label, color = c.danger, modifier = Modifier.padding(horizontal = JSpace.l, vertical = JSpace.m)) }
                }
            }
        }
        item(key = "uses") {
            KitGroup(header = "Jarvis 用飞书做什么", separatorStart = JSpace.l + JSize.iconS + JSpace.m) {
                row { Use(JIcons.inbox, "收件箱", "私聊、@我、群消息和云文档里提到你的地方，AI 先分拣、起草回复") }
                row { Use(JIcons.knowledge, "知识库", "会话、人、群、项目归档下来，给助手和 AI 分身做上下文") }
                row { Use(JIcons.ai, "助手", "Jarvis 能搜消息、读文档；发消息等写操作先在手机上问你") }
                row { Use(JIcons.agents, "/agent", "在飞书任何地方以 /agent 开头说一句，就起一个 Agent 处理并替你回复") }
                row { KitRow("/agent 模型", onClick = onAgentModel, icon = JIcons.model, iconColor = c.ai) }
            }
        }
    }
}

@Composable
private fun Use(icon: ImageVector, title: String, detail: String) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().padding(horizontal = JSpace.l, vertical = JSpace.m),
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Icon(icon, null, Modifier.padding(top = JSpace.xxs).size(JSize.iconS), tint = c.accent)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(title, style = JTheme.type.body, color = c.label)
            Text(detail, style = JTheme.type.caption, color = c.secondaryText)
        }
    }
}

/** 设置 › 连接器 › 这台手机: the 手机数据 switches (the phone_* tools), as a page of their own. */
@Composable
fun PhoneDataScreen(onBack: () -> Unit) {
    KitPage("这台手机", onBack) {
        item(key = "phone") { jarvis.android.ui.PhoneDataSection() }
    }
}
