package persona.feature.agents

import persona.core.contracts.api.AgentCreationService
import persona.core.contracts.api.MainConversationSender
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/** Pairing-owned dependencies, supplied by the app without coupling entries to its shell. */
interface AgentsWiring {
    val state: StateFlow<AgentsWiringState>

    companion object {
        val Empty = object : AgentsWiring {
            override val state = MutableStateFlow(AgentsWiringState())
        }
    }
}

data class AgentsWiringState(
    val scope: String = "",
    val operations: AgentsOperationsBindings? = null,
    val creation: AgentCreationService? = null,
    val drafts: AgentsDraftSource? = null,
    val mainSender: MainConversationSender? = null,
    val creationPreferences: DataStore<Preferences>? = null,
    val tasks: persona.core.domain.tasks.TasksStore? = null,
    val archives: persona.core.contracts.api.AgentArchives? = null,
)
