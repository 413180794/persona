package persona.feature.agents

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.*
import persona.core.contracts.SessionKey
import persona.core.contracts.api.HubPreferences
import persona.core.contracts.api.HubPreferencesSnapshot
import persona.core.domain.sort.AgentGrouping
import persona.core.domain.sort.AgentSort
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class AgentsPreferencesTest {
    @Test fun storedLegacyValuesReadAsTheNewDefaultAndStateRemainsSelectable() = runTest {
        val store = object : DataStore<Preferences> {
            override val data = MutableStateFlow<Preferences>(emptyPreferences())
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                transform(data.value).also { data.value = it }
        }
        val hub = object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }
        val preferences = AgentsPreferences(store, hub)
        assertEquals("a phone that never chose a grouping gets 按仓库", AgentGrouping.Repo, preferences.snapshots.first().grouping)
        for (value in listOf("Space", "Machine", "removed-value")) {
            store.data.value = preferencesOf(stringPreferencesKey("grouping") to value)
            assertEquals(AgentGrouping.MachineSpace, preferences.snapshots.first().grouping)
        }
        preferences.grouping(AgentGrouping.State)
        assertEquals(AgentGrouping.State, preferences.snapshots.first().grouping)
        preferences.grouping(AgentGrouping.MachineSpace)
        assertEquals("MachineSpace", store.data.value[stringPreferencesKey("grouping")])
        assertEquals(AgentGrouping.MachineSpace, preferences.snapshots.first().grouping)
        // "Status first" until "Recently updated" is chosen (iOS dev.sort); botmux shown until hidden (#2786)
        assertEquals(AgentSort.State, preferences.snapshots.first().sort)
        assertEquals(false, preferences.snapshots.first().hideBotmux)
        preferences.sort(AgentSort.Updated); preferences.hideBotmux(true)
        assertEquals(AgentSort.Updated, preferences.snapshots.first().sort)
        assertEquals(true, preferences.snapshots.first().hideBotmux)
        store.data.value = preferencesOf(stringPreferencesKey("sort") to "removed-value")
        assertEquals(AgentSort.State, preferences.snapshots.first().sort)
    }
}
