package persona.feature.chat

import persona.core.designsystem.text
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import persona.core.contracts.Interaction
import persona.core.designsystem.*
import persona.core.contracts.mcpServerLabel
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
        Text(stringResource(R.string.chat_elicit_server, request.mcpServerName?.takeIf(String::isNotBlank)?.let(::mcpServerLabel) ?: stringResource(R.string.chat_elicit_not_given)), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        Text(stringResource(R.string.chat_elicit_host, ElicitationUrl.host(form.url) ?: stringResource(R.string.chat_elicit_invalid_link)), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        Text(form.url.orEmpty(), style = PersonaTheme.type.callout)
        Text(stringResource(R.string.chat_elicit_url_note), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        JButton(stringResource(R.string.chat_elicit_open_link), kind = JButtonKind.Secondary, onClick = {
            if (url != null) {
                failed = runCatching { handler.openUri(url) }.isFailure
                if (!failed) update(draft.copy(openedElicitationUrl = url))
            }
        }, enabled = enabled && url != null)
        if (failed) Text(stringResource(R.string.chat_elicit_open_failed), color = PersonaTheme.palette.danger.text)
        return
    }
    val validation = ElicitationForm.validate(form?.requestedSchema, draft.fields)
    if (form?.mode != "form" || !validation.supported) {
        Text(stringResource(R.string.chat_elicit_unsupported), style = PersonaTheme.type.callout)
        return
    }
    ElicitationForm.fields(form.requestedSchema).forEach { field ->
        val value = draft.fields[field.key]
        fun set(value: JsonElement?) = update(draft.copy(fields = JsonObject(
            if (value == null) draft.fields - field.key else draft.fields + (field.key to value))))
        Column(Modifier.fillMaxWidth().padding(vertical = Tokens.Space.s), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            Text(stringResource(if (field.required) R.string.chat_elicit_required else R.string.chat_elicit_optional, field.title), style = PersonaTheme.type.headline)
            field.description?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2) }
            when {
                field.type == "boolean" -> Row(horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    listOf(true to stringResource(R.string.chat_elicit_yes), false to stringResource(R.string.chat_elicit_no)).forEach { (choice, label) ->
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
                    }, enabled = enabled, multiSelect = field.type == "array", position = index + 1 to field.choices.size)
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
            if (!field.required && value != null) JTextButton(stringResource(R.string.chat_elicit_clear), { set(null) }, enabled = enabled)
            validation.errors[field.key]?.let { Text(it.text(), style = PersonaTheme.type.caption, color = PersonaTheme.palette.danger.text) }
            if (field.secret) Text(stringResource(R.string.chat_elicit_secret_note), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
        }
    }
}

@Composable
internal fun ElicitationActions(request: Interaction, draft: AnswerDraft, enabled: Boolean, act: (AnswerAction, AnswerDraft) -> Unit) {
    DecisionButtons(
        primary = JAction(stringResource(if (request.elicitation?.mode == "url") R.string.chat_elicit_opened else R.string.chat_elicit_submit),
            enabled = enabled && AnswerBuilder.build(request, AnswerAction.SubmitElicitation, draft).allowed) {
            act(AnswerAction.SubmitElicitation, draft)
        },
        secondary = JAction(stringResource(R.string.chat_elicit_decline), enabled = enabled) { act(AnswerAction.DeclineElicitation, draft) },
        more = listOf(JAction(stringResource(R.string.chat_elicit_cancel), enabled = enabled) { act(AnswerAction.CancelElicitation, draft) }),
    )
}
