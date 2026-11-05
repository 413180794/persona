package persona.feature.chat

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import persona.core.contracts.Interaction
import persona.core.designsystem.*
import persona.core.domain.mcpServerLabel
import persona.core.domain.answer.*
import kotlinx.serialization.json.*

@Composable
internal fun ElicitationFields(request: Interaction, draft: AnswerDraft, update: (AnswerDraft) -> Unit, enabled: Boolean) {
    val form = request.elicitation
    form?.message?.takeIf(String::isNotBlank)?.let { ChatLinkText(it) }
    if (form?.mode == "url") {
        val url = ElicitationUrl.target(form.url)
        val handler = LocalUriHandler.current
        var failed by remember(form.url) { mutableStateOf(false) }
        Text("MCP 服务器：${request.mcpServerName?.takeIf(String::isNotBlank)?.let(::mcpServerLabel) ?: "未提供"}", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        Text("目标主机：${ElicitationUrl.host(form.url) ?: "无效链接"}", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        Text(form.url.orEmpty(), style = PersonaTheme.type.callout)
        Text("打开链接不代表授权成功。完成网页操作后，回到这里点「已打开，继续」。", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        JButton("打开链接", kind = JButtonKind.Secondary, onClick = {
            if (url != null) {
                failed = runCatching { handler.openUri(url) }.isFailure
                if (!failed) update(draft.copy(openedElicitationUrl = url))
            }
        }, enabled = enabled && url != null)
        if (failed) Text("链接未能打开，请重试或到终端处理", color = PersonaTheme.palette.danger.text)
        return
    }
    val validation = ElicitationForm.validate(form?.requestedSchema, draft.fields)
    if (form?.mode != "form" || !validation.supported) {
        Text("这个 MCP 请求暂不支持在手机上填写，请到终端核对并处理。", style = PersonaTheme.type.callout)
        return
    }
    ElicitationForm.fields(form.requestedSchema).forEach { field ->
        val value = draft.fields[field.key]
        fun set(value: JsonElement?) = update(draft.copy(fields = JsonObject(
            if (value == null) draft.fields - field.key else draft.fields + (field.key to value))))
        Column(Modifier.fillMaxWidth().padding(vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            Text(field.title + if (field.required) "（必填）" else "（选填）", style = PersonaTheme.type.headline)
            field.description?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
            when {
                field.type == "boolean" -> Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    listOf(true to "是", false to "否").forEach { (choice, label) ->
                        JChip(label, selected = value == JsonPrimitive(choice), onClick = if (enabled) ({ set(JsonPrimitive(choice)) }) else null)
                    }
                }
                field.choices.isNotEmpty() -> field.choices.forEachIndexed { index, choice ->
                    val selected = if (field.type == "array") choice in (value as? JsonArray).orEmpty() else value == choice
                    DecisionOption(field.choiceLabels.getOrNull(index) ?: choice.content, selected = selected, onClick = {
                        if (field.type == "array") {
                            val values = (value as? JsonArray).orEmpty()
                            set(JsonArray(if (selected) values - choice else values + choice))
                        } else set(choice)
                    }, enabled = enabled)
                }
                else -> JTextField((value as? JsonPrimitive)?.content.orEmpty(), { set(JsonPrimitive(it)) },
                    modifier = Modifier.fillMaxWidth(), enabled = enabled,
                    visualTransformation = if (field.secret) PasswordVisualTransformation() else VisualTransformation.None,
                    keyboardOptions = KeyboardOptions(keyboardType = when {
                        field.secret -> KeyboardType.Password
                        field.type in setOf("number", "integer") -> KeyboardType.Decimal
                        else -> KeyboardType.Text
                    }, autoCorrectEnabled = !field.secret), isError = value != null && field.key in validation.errors)
            }
            if (!field.required && value != null) JTextButton("清空此项", { set(null) }, enabled = enabled)
            validation.errors[field.key]?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.danger.text) }
            if (field.secret) Text("此项不会保存，离开后需重新填写", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        }
    }
}

@Composable
internal fun ElicitationActions(request: Interaction, draft: AnswerDraft, enabled: Boolean, act: (AnswerAction, AnswerDraft) -> Unit) {
    DecisionButtons(
        primary = JAction(if (request.elicitation?.mode == "url") "已打开，继续" else "提交",
            enabled = enabled && AnswerBuilder.build(request, AnswerAction.SubmitElicitation, draft).allowed) {
            act(AnswerAction.SubmitElicitation, draft)
        },
        secondary = JAction("拒绝", enabled = enabled) { act(AnswerAction.DeclineElicitation, draft) },
        more = listOf(JAction("取消", enabled = enabled) { act(AnswerAction.CancelElicitation, draft) }),
    )
}
