package persona.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import persona.core.contracts.api.ExtensionDependency
import persona.core.contracts.api.ExtensionPreview
import persona.core.contracts.api.ExtensionSandbox
import persona.core.designsystem.*

internal object ExtensionModelText {
    fun dependency(d: ExtensionDependency): String = listOfNotNull(
        d.name?.ifBlank { null } ?: d.id,
        d.version?.ifBlank { null },
        when (d.state) {
            "ok" -> "已满足"
            "missing" -> "未安装"
            "disabled" -> "需要先启用"
            "not_ready" -> "依赖未就绪"
            "old" -> "版本不足（现在是 ${d.installed.orEmpty()}）"
            else -> "状态未知，重新预览"
        },
    ).joinToString(" · ")

    fun blockedPreview(preview: ExtensionPreview): String = if (
        preview.depends.any { it.state == "not_ready" } || preview.bundle.any { member -> member.depends.any { it.state == "not_ready" } }
    ) "还有依赖未就绪，暂时不能安装；先完成依赖扩展的设置，确认其依赖也已就绪，再重新预览。"
    else "还有依赖没有满足，暂时不能安装；先启用所需扩展，或补齐市场后重新预览。"

    fun sandbox(s: ExtensionSandbox): String = when {
        s.active -> "沙箱正在生效"
        s.enabled -> "已要求沙箱，但当前未生效"
        else -> "未启用沙箱"
    }
}

internal fun LazyListScope.sandbox(s: ExtensionSandbox?) {
    s ?: return
    item("sandbox-title") { JSectionHeader("运行隔离") }
    item("sandbox-state") {
        JGroupItem(GroupPosition.Single) {
            JRow(ExtensionModelText.sandbox(s), subtitle = s.why?.ifBlank { null }, subtitleLines = 4)
        }
    }
    item("sandbox-note") {
        SettingsCaption("沙箱只隔离 hub 运行的已安装扩展；是否生效以上面实际状态为准。只能在 hub 所在的机器或管理界面修改。",
            Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
    }
    s.egress?.let { e -> item("sandbox-egress") {
        SettingsCaption("经出网代理：放行 ${e.allowed} 次 · 拒绝 ${e.denied} 次" +
            e.lastDenied?.let { "\n最近拒绝：$it" }.orEmpty(), Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
    } }
}

@Composable
internal fun BundlePreview(preview: ExtensionPreview) {
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        if (preview.depends.isNotEmpty()) {
            Text("依赖", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            preview.depends.forEach { JInlineStatus(ExtensionModelText.dependency(it),
                tone = if (it.state == "ok") InlineTone.Done else InlineTone.Caution) }
        }
        preview.manifest?.extensionPack?.takeIf { it.isNotEmpty() }?.let { members ->
            Text("合集成员", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            members.forEach { JRow(it.id, subtitle = listOfNotNull(it.version, it.market).joinToString(" · ").ifBlank { null }) }
            SettingsCaption("已安装且版本够用的成员不重复安装；下面列出这次要一起安装或升级的扩展。")
        }
        if (preview.bundle.isNotEmpty()) {
            Text("一起安装或升级（${preview.bundle.size} 个）", style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
            preview.bundle.forEach { member ->
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    JRow(member.manifest?.name?.ifBlank { null } ?: member.manifest?.id.orEmpty(),
                        subtitle = member.manifest?.version, value = if (member.upgrade) "升级" else "安装")
                    PermissionList("它可以", member.permissions)
                    if (member.permissions.isEmpty()) SettingsCaption("没有申请额外权限")
                    if (!member.addedPermissions.isNullOrEmpty()) PermissionList("新增权限", member.addedPermissions.orEmpty())
                    member.depends.forEach { SettingsCaption(ExtensionModelText.dependency(it)) }
                    member.warnings.orEmpty().forEach { JInlineStatus(it, tone = InlineTone.Caution) }
                }
            }
        }
        preview.unresolved.forEach { JInlineStatus("找不到：$it", tone = InlineTone.Failed) }
        if (!preview.canConfirm) JInlineStatus(ExtensionModelText.blockedPreview(preview), tone = InlineTone.Caution)
    }
}
