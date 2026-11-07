package persona.feature.agents

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.Session
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

@Composable
internal fun AgentArchivesScreen(
    model: AgentArchivesState,
    machineNames: Map<String, String>,
    onBack: () -> Unit,
    onOpen: (Session) -> Unit,
) {
    val state by model.state.collectAsStateWithLifecycle()
    LaunchedEffect(model) { model.refresh() }
    var query by rememberSaveable { mutableStateOf("") }
    val rows = remember(state.sessions, query, machineNames) {
        val needle = query.trim()
        state.sessions.filter { session ->
            listOf(session.displayTitle, session.cwd, session.agentLabel, machineNames[session.machineId].orEmpty(),
                session.lastText.orEmpty()).any { it.contains(needle, ignoreCase = true) }
        }.sortedByDescending { it.updatedAt.orEmpty() }
    }
    JPage(stringResource(R.string.agents_archives_title), onBack = onBack, titleBusy = state.loading,
        actions = {
            JIconButton(PhIcons.ArrowClockwise, stringResource(R.string.agents_archives_refresh), model::refresh,
                enabled = !state.loading && state.busy == null)
        }) { padding ->
        LazyColumn(Modifier.fillMaxSize().testTag("agent-archives"), contentPadding = padding) {
            item("search") {
                JSearchField(query, { query = it }, stringResource(R.string.agents_archives_search),
                    Modifier.padding(horizontal = Tokens.Space.m))
            }
            item("description") {
                Text(stringResource(R.string.agents_archives_description),
                    Modifier.padding(Tokens.Space.m), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            }
            state.error?.let { error -> item("error") {
                JInlineStatus(operationFeedback(error).text.resolve(agentResources()), Modifier.padding(Tokens.Space.m),
                    tone = InlineTone.Failed, action = {
                        JTextButton(stringResource(R.string.agents_archives_refresh), model::refresh,
                            enabled = !state.loading && state.busy == null)
                    })
            } }
            if (!state.loaded && state.loading) item("loading") { JLoading() }
            else if (state.loaded && rows.isEmpty()) item("empty") {
                JEmptyState(stringResource(if (query.isBlank()) R.string.agents_archives_empty else R.string.agents_archives_no_matches),
                    stringResource(R.string.agents_archives_empty_hint), icon = PhIcons.Archive)
            }
            items(rows, key = { it.key.path }) { session ->
                JRow(session.displayTitle, modifier = Modifier.padding(horizontal = Tokens.Space.m), subtitle = listOfNotNull(session.agentLabel,
                    machineNames[session.machineId] ?: session.machineId, session.cwd.takeIf { it.isNotBlank() }).joinToString(" · "),
                    onClick = { onOpen(session) }, trailing = {
                        JButton(stringResource(R.string.agents_archives_restore), { model.restore(session) },
                            kind = JButtonKind.Quiet, size = JButtonSize.Small,
                            loading = state.busy == session.key,
                            enabled = state.busy == null && !state.loading && model.api.supports(session.key),
                            modifier = Modifier.testTag("restore:${session.key.path}"))
                    })
            }
        }
    }
}
