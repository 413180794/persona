package persona.core.data

import persona.core.contracts.HubScope
import persona.core.contracts.MeshHub
import java.security.SecureRandom
import java.util.UUID
import kotlinx.serialization.Serializable

/*
 * The phone's hub groups (docs/android/android.md "Multiple hubs"), stored encrypted in `hub-data/groups.enc`.
 *
 * Every paired hub keeps its own pairing scope (`hub-data/<hub>/<generation>/credentials.enc`, `endpoints.enc`).
 * A group's data (databases, caches, preferences) lives in its storage scope: for a group that started with a
 * pairing, that pairing's scope, which is exactly where a single pairing kept everything before groups existed, so
 * the old pairing becomes the first group without moving a file. A pairing scope hosts at most one group's data.
 *
 * Everything below is pure: each change returns the new document and the scopes it leaves unused; the caller writes
 * the document first and erases afterwards, so a crash in between leaves garbage, never a dangling reference.
 */

@Serializable
data class StoredScope(val hubId: String, val generation: String) {
    val scope: HubScope get() = HubScope(hubId, generation)
    val path: String get() = "$hubId/$generation"

    companion object {
        fun of(scope: HubScope) = StoredScope(scope.hubId, scope.pairingGeneration)
    }
}

/** A hub paired from this phone; its credentials are in the pairing scope ([hubId], [generation]). */
@Serializable
data class StoredHub(
    val hubId: String,
    val generation: String,
    /** The hub's own name (`hub_name`) as last seen. */
    val name: String? = null,
    /** The other hubs of its mesh as last seen (hub ids); two paired hubs are in one group when either lists the other. */
    val peers: List<String> = emptyList(),
    val pairedAt: Long = 0,
) {
    /** Where its credentials and route choices are. */
    val pairing: StoredScope get() = StoredScope(hubId, generation)
}

/** A hub of a group's mesh this phone never paired with; reached with a paired hub's credential (plan 2.6). */
@Serializable
data class StoredMeshHub(
    val hubId: String,
    val name: String? = null,
    val endpoints: List<String> = emptyList(),
    val fp: String? = null,
    val tlsPort: Int? = null,
    val online: Boolean? = null,
    val center: Boolean? = null,
)

@Serializable
data class StoredGroup(
    /** `g` and eight hex digits; also the machine id suffix of every group but the primary one. */
    val id: String,
    /** The user's name for the group; null shows the first hub's name. */
    val name: String? = null,
    val storage: StoredScope,
    /** Paired hubs, never empty; the first one is the group's home (its credentials open the group's data). */
    val hubs: List<StoredHub>,
    val mesh: List<StoredMeshHub> = emptyList(),
    val createdAt: Long = 0,
    val center: String? = null,
) {
    val home: StoredHub get() = hubs.first()
    val centerHubId: String? get() = center ?: mesh.firstOrNull { it.center == true }?.hubId
}

/**
 * The data of a group that merged into another one ([into]), kept until what it still owed (messages not yet shown,
 * drafts) has moved into that group (GroupMerge.kt). Only then is [storage] erased; a failed move is retried.
 */
@Serializable
data class PendingMove(val storage: StoredScope, val into: String)

@Serializable
data class GroupsDocument(
    val version: Int = 1,
    val groups: List<StoredGroup> = emptyList(),
    /** The group whose machine ids carry no suffix; null once it is gone (never promoted, plan section 4). */
    val primary: String? = null,
    /** The group hub-level pages show. */
    val selected: String? = null,
    /** Absorbed groups' data still to move (see [PendingMove]). */
    val moves: List<PendingMove> = emptyList(),
) {
    fun group(id: String): StoredGroup? = groups.firstOrNull { it.id == id }
    fun groupOfHub(hubId: String): StoredGroup? = groups.firstOrNull { group -> group.hubs.any { it.hubId == hubId } }
    /** The machine id suffix of [groupId]: none for the primary group. */
    fun namespace(groupId: String): String? = groupId.takeIf { it != primary }
}

enum class Erase {
    /** The whole scope: nothing refers to it any more. */
    All,
    /** Only `credentials.enc` and `endpoints.enc`: the hub is gone, the directory still holds a group's data. */
    Credentials,
    /** Everything but `credentials.enc` and `endpoints.enc`: the group data is gone, the hub is still paired. */
    Data,
}

data class ScopeErasure(val scope: StoredScope, val erase: Erase)

data class RegistryChange(
    val document: GroupsDocument,
    val erase: List<ScopeErasure> = emptyList(),
    /** The group the change was about (where a hub went), when there is one. */
    val group: String? = null,
)

private val random = SecureRandom()

/** A new group id, unlike every id in [taken]. */
fun newGroupId(taken: Set<String> = emptySet()): String {
    while (true) {
        val id = "g%08x".format(random.nextInt().toLong() and 0xffffffffL)
        if (id !in taken) return id
    }
}

/** An empty data scope for a group that has none yet (a group split off another one). */
fun freshStorage(hubId: String): StoredScope = StoredScope(hubId, UUID.randomUUID().toString())

/** What becomes unused between [old] and [new]: a scope survives while it is some group's storage or some hub's pairing. */
fun erasures(old: GroupsDocument, new: GroupsDocument): List<ScopeErasure> {
    // Data waiting to move into another group is still in use.
    val storages = new.groups.map { it.storage }.toSet() + new.moves.map { it.storage }
    val pairings = new.groups.flatMap { group -> group.hubs.map { it.pairing } }.toSet()
    val wasStorage = old.groups.map { it.storage }.toSet() + old.moves.map { it.storage }
    val wasPairing = old.groups.flatMap { group -> group.hubs.map { it.pairing } }.toSet()
    return (wasStorage + wasPairing).mapNotNull { scope ->
        val storage = scope in storages
        val pairing = scope in pairings
        when {
            !storage && !pairing -> ScopeErasure(scope, Erase.All)
            scope in wasStorage && !storage -> ScopeErasure(scope, Erase.Data)
            scope in wasPairing && !pairing -> ScopeErasure(scope, Erase.Credentials)
            else -> null
        }
    }
}

/**
 * The registry for a phone that paired before groups existed: its single pairing becomes the primary (and selected)
 * group, whose storage and only hub are that pairing's scope. Nothing moves; keys stay byte for byte the same.
 */
internal fun migrateRegistry(
    hubId: String?, generation: String?, discarding: Boolean, credentialsPresent: Boolean, now: Long,
    newId: (Set<String>) -> String = ::newGroupId,
): GroupsDocument {
    if (hubId == null || generation == null || discarding || !credentialsPresent) return GroupsDocument()
    val id = newId(emptySet())
    val scope = StoredScope(hubId, generation)
    return GroupsDocument(
        groups = listOf(StoredGroup(id, storage = scope, hubs = listOf(StoredHub(hubId, generation, pairedAt = now)), createdAt = now)),
        primary = id, selected = id,
    )
}

/**
 * A new pairing ([scope] already holds its credentials). Pairing a hub again only replaces its pairing (the old one
 * is erased, the group and its data stay). A hub a group already knows (in its mesh, or listed by one of its hubs)
 * joins that group. Anything else starts a group whose data lives in the new pairing's scope; the first group after
 * none becomes the primary one, otherwise a missing primary is not replaced.
 */
fun GroupsDocument.addPairedHub(
    scope: StoredScope, hubName: String?, now: Long,
    newId: (Set<String>) -> String = ::newGroupId,
): RegistryChange {
    val name = hubName?.takeIf(String::isNotBlank)
    groupOfHub(scope.hubId)?.let { existing ->
        val document = copy(groups = groups.map { group ->
            if (group.id != existing.id) group else group.copy(hubs = group.hubs.map { hub ->
                if (hub.hubId != scope.hubId) hub else hub.copy(generation = scope.generation, name = name ?: hub.name, pairedAt = now)
            })
        })
        return RegistryChange(document, erasures(this, document), existing.id)
    }
    val known = groups.firstOrNull { group -> group.mesh.any { it.hubId == scope.hubId } }
        ?: groups.firstOrNull { group -> group.hubs.any { scope.hubId in it.peers } }
    if (known != null) {
        val entry = known.mesh.firstOrNull { it.hubId == scope.hubId }
        val hub = StoredHub(scope.hubId, scope.generation, name ?: entry?.name,
            peers = (known.hubs.map { it.hubId } + known.mesh.map { it.hubId }).filter { it != scope.hubId }.distinct(),
            pairedAt = now)
        val document = copy(groups = groups.map { group ->
            if (group.id != known.id) group else group.copy(hubs = group.hubs + hub, mesh = group.mesh.filterNot { it.hubId == scope.hubId })
        })
        return RegistryChange(document, erasures(this, document), known.id)
    }
    val id = newId(groups.map { it.id }.toSet())
    val group = StoredGroup(id, storage = scope, hubs = listOf(StoredHub(scope.hubId, scope.generation, name, pairedAt = now)), createdAt = now)
    val document = copy(groups = groups + group, primary = if (groups.isEmpty()) id else primary,
        selected = selected?.takeIf { selected -> groups.any { it.id == selected } } ?: id)
    return RegistryChange(document, erasures(this, document), id)
}

/** Forgets one paired hub; removing a group's last hub removes the group. */
fun GroupsDocument.removeHub(groupId: String, hubId: String): RegistryChange {
    val group = group(groupId) ?: return RegistryChange(this)
    if (group.hubs.none { it.hubId == hubId }) return RegistryChange(this, group = groupId)
    if (group.hubs.size == 1) return removeGroup(groupId)
    val document = copy(groups = groups.map { if (it.id == groupId) it.copy(hubs = it.hubs.filterNot { hub -> hub.hubId == hubId }) else it })
    return RegistryChange(document, erasures(this, document), groupId)
}

/** Forgets every hub of a group and its data. The primary group is not replaced; the selection moves to the first group. */
fun GroupsDocument.removeGroup(groupId: String): RegistryChange {
    if (group(groupId) == null) return RegistryChange(this)
    val remaining = groups.filterNot { it.id == groupId }
    // The user forgets the group with its data, and with it what was still to move into it.
    val document = copy(groups = remaining, primary = primary.takeIf { it != groupId && remaining.isNotEmpty() },
        selected = selected.takeIf { it != groupId && remaining.any { group -> group.id == it } } ?: remaining.firstOrNull()?.id,
        moves = moves.filterNot { it.into == groupId })
    return RegistryChange(document, erasures(this, document))
}

/**
 * What one authenticated hello from [from] teaches. [mesh] lists every hub of [from]'s mesh component (itself
 * included); null means the hub has no mesh support, which only updates names. Hubs in the list now list each other;
 * hubs outside it drop whatever they listed of it (the list is the whole component, so they are not linked to it any
 * more). Unpaired hubs of the list become the group's mesh hubs. Then the groups are worked out again ([regroup]).
 */
fun GroupsDocument.learn(
    from: String, hubName: String?, mesh: List<MeshHub>?, now: Long,
    newId: (Set<String>) -> String = ::newGroupId,
    newStorage: (String) -> StoredScope = ::freshStorage,
): RegistryChange {
    val entries = mesh.orEmpty().filter { it.hubId.isNotBlank() }.associateBy { it.hubId }
    val centerId = mesh?.firstOrNull { it.center == true }?.hubId
    fun nameOf(id: String) = (if (id == from) hubName?.takeIf(String::isNotBlank) else null)
        ?: entries[id]?.name?.takeIf(String::isNotBlank)
    val order = mesh?.let { list -> (list.map { it.hubId }.filter(String::isNotBlank) + from).distinct() }
    val members = order?.toSet()
    val learned = copy(groups = groups.map { group ->
        group.copy(
            hubs = group.hubs.map { hub ->
                val peers = when {
                    members == null -> hub.peers
                    hub.hubId in members -> order.filter { it != hub.hubId }
                    hub.peers.any { it in members } -> hub.peers.filterNot { it in members }
                    else -> hub.peers
                }
                hub.copy(name = nameOf(hub.hubId) ?: hub.name, peers = peers)
            },
            mesh = group.mesh.map { entry -> nameOf(entry.hubId)?.let { entry.copy(name = it) } ?: entry },
        )
    })
    if (members == null) return RegistryChange(learned, erasures(this, learned), learned.groupOfHub(from)?.id)
    val regrouped = learned.regroup(now, newId, newStorage).document
    val paired = regrouped.groups.flatMap { group -> group.hubs.map { it.hubId } }.toSet()
    val home = regrouped.groupOfHub(from)
        ?: regrouped.groups.firstOrNull { group -> group.hubs.any { it.hubId in members } }
    val document = regrouped.copy(groups = regrouped.groups.map { group ->
        if (group.id == home?.id) group.copy(
            center = centerId ?: group.center,
            mesh = order.filter { it !in paired }.map { id ->
                entries[id]?.let { StoredMeshHub(id, it.name.takeIf(String::isNotBlank) ?: nameOf(id), it.endpoints, it.fp, it.tlsPort, it.online, it.center) }
                    ?: StoredMeshHub(id, nameOf(id))
            }
        )
        else group.copy(mesh = group.mesh.filterNot { it.hubId in members })
    })
    return RegistryChange(document, erasures(this, document), home?.id)
}

/**
 * Works the groups out from the paired hubs' peers: two hubs are linked when either lists the other, a group is a
 * connected component. Ids are stable: components are served in order (the one with a primary-group hub first, then
 * by their earliest hub: the group's creation, its place, the hub's place), and each takes the primary id if it has a
 * hub of the primary group, else the earliest-created id among its hubs' groups that no earlier component took.
 * So a merge keeps the oldest id and a split leaves the id with the side holding the group's first hub; a component
 * left without an id gets a new one and a new empty data scope. A group no component kept is absorbed: its data
 * scope is erased (its paired hubs keep their credentials). The primary id survives or becomes null; the selection
 * follows the group it was on.
 */
fun GroupsDocument.regroup(
    now: Long,
    newId: (Set<String>) -> String = ::newGroupId,
    newStorage: (String) -> StoredScope = ::freshStorage,
): RegistryChange {
    data class Member(val hub: StoredHub, val group: StoredGroup, val groupIndex: Int, val hubIndex: Int)
    val members = groups.flatMapIndexed { groupIndex, group -> group.hubs.mapIndexed { hubIndex, hub -> Member(hub, group, groupIndex, hubIndex) } }
        .distinctBy { it.hub.hubId }
    val position = members.withIndex().associate { it.value.hub.hubId to it.index }
    val parent = IntArray(members.size) { it }
    fun find(i: Int): Int { var x = i; while (parent[x] != x) { parent[x] = parent[parent[x]]; x = parent[x] }; return x }
    members.forEachIndexed { i, member ->
        member.hub.peers.forEach { peer -> position[peer]?.let { j -> parent[find(i)] = find(j) } }
    }
    val groupOrder = compareBy<StoredGroup>({ it.createdAt }, { group -> groups.indexOfFirst { it.id == group.id } })
    val memberOrder = compareBy<Member>({ it.group.createdAt }, { it.groupIndex }, { it.hubIndex })
    val components = members.indices.groupBy(::find).values.map { indices -> indices.map { members[it] } }
        .sortedWith(compareBy<List<Member>>({ component -> if (component.any { it.group.id == primary }) 0 else 1 })
            .thenComparator { a, b -> memberOrder.compare(a.minWith(memberOrder), b.minWith(memberOrder)) })
    val claimed = mutableMapOf<String, Int>()
    val taken = groups.map { it.id }.toMutableSet()
    val ids = components.mapIndexed { index, component ->
        val candidates = component.map { it.group }.distinctBy { it.id }.sortedWith(groupOrder)
        val id = candidates.firstOrNull { it.id == primary && it.id !in claimed }?.id
            ?: candidates.firstOrNull { it.id !in claimed }?.id
            ?: newId(taken).also { taken += it }
        claimed.putIfAbsent(id, index)
        id
    }
    // Where each old group ends up: the component that kept its id, else the first one holding one of its hubs.
    val landing = groups.associate { group ->
        group.id to (claimed[group.id] ?: components.indexOfFirst { component -> component.any { it.group.id == group.id } })
    }
    val pairedIds = members.map { it.hub.hubId }.toSet()
    val built = components.mapIndexed { index, component ->
        val id = ids[index]
        val kept = groups.firstOrNull { it.id == id }
        val hubs = component.sortedWith(compareBy<Member>({ if (it.group.id == id) 0 else 1 }).then(memberOrder)).map { it.hub }
        val mesh = groups.filter { landing[it.id] == index }.flatMap { it.mesh }
            .distinctBy { it.hubId }.filterNot { it.hubId in pairedIds }
        StoredGroup(id, kept?.name, kept?.storage ?: newStorage(hubs.first().hubId), hubs, mesh, kept?.createdAt ?: now, kept?.center)
    }
    val ordered = built.filter { group -> groups.any { it.id == group.id } }.sortedBy { group -> groups.indexOfFirst { it.id == group.id } } +
        built.filter { group -> groups.none { it.id == group.id } }
    val selection = selected?.let { selected -> landing[selected]?.takeIf { it >= 0 }?.let { ids[it] } }
    // A group no component kept was absorbed: its data waits to move into the group its hubs landed in instead of
    // being erased (unsent messages and drafts must survive a merge). Moves into an absorbed group follow it.
    fun target(id: String): String? = if (ordered.any { it.id == id }) id else landing[id]?.takeIf { it >= 0 }?.let(ids::get)
    val absorbed = groups.filter { group -> ordered.none { it.id == group.id } }.mapNotNull { group ->
        target(group.id)?.let { PendingMove(group.storage, it) }
    }
    val document = copy(
        groups = ordered,
        primary = primary.takeIf { primary -> ordered.any { it.id == primary } },
        selected = selection ?: ordered.firstOrNull()?.id,
        moves = (moves.mapNotNull { move -> target(move.into)?.let { move.copy(into = it) } } + absorbed)
            .filterNot { move -> ordered.any { it.storage == move.storage } }.distinctBy { it.storage },
    )
    return RegistryChange(document, erasures(this, document))
}

/** [storage]'s data has moved into its group (or there was nothing to move): it can go now. */
fun GroupsDocument.completeMove(storage: StoredScope): RegistryChange {
    val document = copy(moves = moves.filterNot { it.storage == storage })
    return RegistryChange(document, erasures(this, document))
}
