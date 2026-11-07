package persona.core.contracts.api

import persona.core.contracts.GroupKeys
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/** One paired or mesh-learned hub of a group (docs/android/android.md "Multiple hubs"). */
data class HubMemberInfo(
    val hubId: String,
    /** The hub's own name (`hub_name`), else a short form of its id. */
    val name: String,
    /** Paired from this phone (own credentials); false for hubs learned from the mesh, reached with a group credential. */
    val paired: Boolean,
    /** The group's connection currently runs through this hub. */
    val connected: Boolean = false,
    /** What the mesh last said; null before any hello listed it. */
    val online: Boolean? = null,
)

data class HubGroupInfo(
    val id: String,
    /** The user's name for the group, else the first hub's name. */
    val name: String,
    /** The machine id suffix ([GroupKeys]); null for the primary group, whose ids carry none. */
    val namespace: String?,
    /** Hub-level pages (Persona, inbox, hub settings) show this group. */
    val selected: Boolean,
    val state: ConnectionState,
    val hubs: List<HubMemberInfo>,
)

/** The phone's hub groups: merged lists label and filter by these; settings manages them. */
interface HubGroups {
    val groups: StateFlow<List<HubGroupInfo>>

    /** The group a merged-list machine id belongs to; null while unknown. */
    fun groupOf(machineId: String): HubGroupInfo? =
        GroupKeys.namespaceOf(machineId).let { namespace -> groups.value.firstOrNull { it.namespace == namespace } }

    /** Shows [groupId] in the hub-level pages. */
    suspend fun select(groupId: String)
    suspend fun rename(groupId: String, name: String)
    /** Forgets every hub of the group and erases its local data; other groups are untouched. */
    suspend fun removeGroup(groupId: String)
    /** Forgets one paired hub; the group keeps its data while another hub remains. */
    suspend fun removeHub(groupId: String, hubId: String)

    companion object {
        val None: HubGroups = object : HubGroups {
            override val groups = MutableStateFlow(emptyList<HubGroupInfo>())
            override suspend fun select(groupId: String) = Unit
            override suspend fun rename(groupId: String, name: String) = Unit
            override suspend fun removeGroup(groupId: String) = Unit
            override suspend fun removeHub(groupId: String, hubId: String) = Unit
        }
    }
}

/** Labels in merged lists only appear with more than one group, so a single group looks exactly as before. */
fun List<HubGroupInfo>.labelled(): Boolean = size > 1
