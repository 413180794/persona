package persona.core.data

import persona.core.contracts.HubScope
import persona.core.contracts.MeshHub
import java.io.File
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class HubGroupRegistryTest {
    @get:Rule val temporary = TemporaryFolder()

    private var ids = 0
    private val newId: (Set<String>) -> String = { taken -> generateSequence { "g%08x".format(++ids) }.first { it !in taken } }
    private var storages = 0
    private val newStorage: (String) -> StoredScope = { hub -> StoredScope(hub, "fresh${++storages}") }

    private fun scope(hub: String, generation: String = "${hub}gen") = StoredScope(hub, generation)
    private fun mesh(vararg hubs: String) = hubs.map { MeshHub(hubId = it, name = "名字$it") }

    /** Pairs [hubs] one after another (each into its own group unless something links it). */
    private fun paired(vararg hubs: String, at: Long = 1): GroupsDocument =
        hubs.fold(GroupsDocument()) { document, hub -> document.addPairedHub(scope(hub), null, at + hubs.indexOf(hub), newId).document }

    private fun GroupsDocument.ids() = groups.map { group -> group.id to group.hubs.map { it.hubId } }

    @Test fun aSinglePairingBecomesThePrimaryGroupInPlace() {
        val document = migrateRegistry("hub-a", "gen-1", discarding = false, credentialsPresent = true, now = 5, newId = newId)
        val group = document.groups.single()
        assertEquals(StoredScope("hub-a", "gen-1"), group.storage)
        assertEquals(listOf(StoredHub("hub-a", "gen-1", pairedAt = 5)), group.hubs)
        assertEquals(group.id, document.primary)
        assertEquals(group.id, document.selected)
        // The primary group's machine ids carry no suffix: keys stay byte for byte what they were.
        assertNull(document.namespace(group.id))
        // The storage is the old pairing scope, so its directories and rows keep their scope path.
        assertEquals(HubScope("hub-a", "gen-1").path, group.storage.path)
        assertEquals(GroupsDocument(), migrateRegistry(null, null, false, false, 5, newId))
        assertEquals(GroupsDocument(), migrateRegistry("hub-a", "gen-1", discarding = true, credentialsPresent = false, now = 5, newId = newId))
    }

    @Test fun groupIdsLookLikeGroupIdsAndNeverRepeat() {
        val taken = mutableSetOf<String>()
        repeat(200) { taken += newGroupId(taken) }
        assertEquals(200, taken.size)
        assertTrue(taken.all { Regex("g[0-9a-f]{8}").matches(it) })
    }

    @Test fun separatePairingsAreSeparateGroupsAndOnlyTheFirstIsPrimary() {
        val document = paired("a", "b")
        assertEquals(2, document.groups.size)
        val (first, second) = document.groups
        assertEquals(first.id, document.primary)
        assertEquals(first.id, document.selected)
        assertNull(document.namespace(first.id))
        assertEquals(second.id, document.namespace(second.id))
        // A new group's data lives in its pairing's scope, like a single pairing always did.
        assertEquals(scope("b"), second.storage)
    }

    @Test fun aHelloThatListsAnotherPairedHubMergesTheGroupsAndKeepsTheOldestId() {
        val document = paired("a", "b")
        val (first, second) = document.groups
        val change = document.learn("b", "公司", mesh("a", "b"), now = 10, newId = newId, newStorage = newStorage)
        val merged = change.document.groups.single()
        assertEquals(first.id, merged.id)
        assertEquals(listOf("a", "b"), merged.hubs.map { it.hubId })
        assertEquals(scope("a"), merged.storage)
        assertEquals(first.id, change.document.primary)
        assertEquals("公司", merged.hubs[1].name)
        assertEquals("名字a", merged.hubs[0].name)
        // The absorbed group's data waits to move into the merged group: nothing is erased yet.
        assertTrue(change.erase.isEmpty())
        assertEquals(listOf(PendingMove(second.storage, first.id)), change.document.moves)
        assertEquals(first.id, change.group)
        // Once moved it goes; hub b's credentials live in that directory and stay.
        val done = change.document.completeMove(second.storage)
        assertTrue(done.document.moves.isEmpty())
        assertEquals(listOf(ScopeErasure(second.storage, Erase.Data)), done.erase)
    }

    @Test fun aPendingMoveFollowsItsGroupAndDiesWithIt() {
        val merged = paired("a", "b", "c").learn("b", null, mesh("a", "b"), now = 10, newId = newId, newStorage = newStorage).document
        val (ab, c) = merged.groups
        val bStorage = merged.moves.single().storage
        // The group b moved into merges again: the move goes with it.
        val again = merged.learn("c", null, mesh("a", "b", "c"), now = 11, newId = newId, newStorage = newStorage)
        assertEquals(setOf(PendingMove(bStorage, ab.id), PendingMove(c.storage, ab.id)), again.document.moves.toSet())
        assertTrue(again.erase.isEmpty())
        // Forgetting the group forgets what was to move into it, and erases it all (the user confirmed that).
        val removed = again.document.removeGroup(ab.id)
        assertTrue(removed.document.moves.isEmpty())
        assertTrue(ScopeErasure(bStorage, Erase.All) in removed.erase)
        assertTrue(ScopeErasure(c.storage, Erase.All) in removed.erase)
    }

    @Test fun unsentMessagesAndDraftsMoveWithTheirIdsAndNothingIsOverwritten() {
        fun message(id: String, state: OutboxState) = OutboxRow(position = 7, scope = "hub-b/gen", machine = "mac", session = "s1",
            client_msg_id = id, text = "text $id", clear_draft = null, state = state.wire, error = null, created_at = 42)
        val messages = listOf(message("queued", OutboxState.Queued), message("sending", OutboxState.Sending),
            message("waiting", OutboxState.WaitingForNetwork), message("unknown", OutboxState.Unknown),
            message("review", OutboxState.NeedsConfirmation), message("refused", OutboxState.Refused),
            message("sent", OutboxState.Sent), message("shown", OutboxState.Shown), message("cancelled", OutboxState.Cancelled))
        val drafts = listOf(DraftRow("hub-b/gen", "mac", "s1", "half a sentence", 3),
            DraftRow("hub-b/gen", "mac", "s2", "", 5),
            DraftRow("hub-b/gen", "mac", "s3", "", 1, attachments = "[{\"id\":\"a\"}]"),
            DraftRow("hub-b/gen", "mac", "s4", "theirs", 2))
        val unsent = unsentOf(messages, drafts)
        // Shown and cancelled ones are settled; an empty draft is nothing.
        assertEquals(listOf("queued", "sending", "waiting", "unknown", "review", "refused", "sent"), unsent.messages.map { it.client_msg_id })
        assertEquals(listOf("s1", "s3", "s4"), unsent.drafts.map { it.session })
        val (rows, moved) = adopted(unsent, "hub-a/gen", draftsWithContent = setOf("mac" to "s4"))
        assertTrue(rows.all { it.scope == "hub-a/gen" && it.position == 0L && it.machine == "mac" && it.created_at == 42L })
        assertEquals(unsent.messages.map { it.client_msg_id }, rows.map { it.client_msg_id })
        // An interrupted send is checked with the hub before anything is sent again.
        assertEquals(OutboxState.Unknown.wire, rows.single { it.client_msg_id == "sending" }.state)
        assertEquals(OutboxState.Queued.wire, rows.single { it.client_msg_id == "queued" }.state)
        // The group's own draft with something in it stays.
        assertEquals(listOf("s1", "s3"), moved.map { it.session })
        assertTrue(moved.all { it.scope == "hub-a/gen" })
        assertEquals(3L, moved.first().version)
    }

    @Test fun aMergeWithThePrimaryGroupKeepsThePrimaryIdEvenWhenAnotherGroupIsOlder() {
        val document = paired("a", "b")
        val older = document.groups[1].copy(createdAt = 0)
        val reordered = document.copy(groups = listOf(document.groups[0], older))
        val merged = reordered.learn("a", null, mesh("a", "b"), now = 10, newId = newId, newStorage = newStorage).document
        assertEquals(document.primary, merged.groups.single().id)
    }

    @Test fun aSplitLeavesTheIdWithTheGroupsFirstHubAndGivesTheRestANewGroupAndEmptyData() {
        val joined = paired("a", "b").learn("a", null, mesh("a", "b"), 10, newId, newStorage).document
        val group = joined.groups.single()
        // a's mesh no longer lists b: they are not linked any more.
        val change = joined.learn("a", null, mesh("a"), 20, newId, newStorage)
        val document = change.document
        assertEquals(listOf(group.id to listOf("a"), document.groups[1].id to listOf("b")), document.ids())
        assertNotEquals(group.id, document.groups[1].id)
        assertEquals(StoredScope("b", "fresh1"), document.groups[1].storage)
        assertEquals(20L, document.groups[1].createdAt)
        assertEquals(group.id, document.primary)
        assertTrue(change.erase.isEmpty())
        // b's stale view (it still lists a) no longer counts once a's fresh hello says otherwise.
        assertFalse(document.groups[1].hubs.single().peers.contains("a"))
    }

    @Test fun learningTwiceChangesNothing() {
        val once = paired("a", "b", "c").learn("a", null, mesh("a", "b"), 10, newId, newStorage).document
        val twice = once.learn("a", null, mesh("a", "b"), 11, newId, newStorage)
        assertEquals(once, twice.document)
        assertTrue(twice.erase.isEmpty())
        assertEquals(once, once.regroup(12, newId, newStorage).document)
    }

    @Test fun aHubWithoutMeshSupportOnlyUpdatesItsName() {
        val document = paired("a", "b")
        val change = document.learn("b", "家里", null, 10, newId, newStorage)
        assertEquals(document.ids(), change.document.ids())
        assertEquals("家里", change.document.groupOfHub("b")!!.hubs.single().name)
        assertTrue(change.erase.isEmpty())
    }

    @Test fun unpairedMeshHubsBecomeTheGroupsMeshHubsAndPairingOneJoinsThatGroup() {
        val document = paired("a", "x").learn("a", null, listOf(
            MeshHub("a", "家里", online = true, self = true),
            MeshHub("b", "书房", online = false, endpoints = listOf("https://b.example.com:18662"), tlsPort = 18662, fp = "pin-b"),
        ), 10, newId, newStorage).document
        val home = document.groupOfHub("a")!!
        assertEquals(listOf(StoredMeshHub("b", "书房", listOf("https://b.example.com:18662"), "pin-b", 18662, false)), home.mesh)
        assertTrue(document.groupOfHub("x")!!.mesh.isEmpty())
        val change = document.addPairedHub(scope("b"), null, 20, newId)
        assertEquals(home.id, change.group)
        val joined = change.document.group(home.id)!!
        assertEquals(listOf("a", "b"), joined.hubs.map { it.hubId })
        assertEquals("书房", joined.hubs[1].name)
        assertTrue(joined.mesh.isEmpty())
        assertTrue(change.erase.isEmpty())
        // Still one group after the next hello from either side.
        val again = change.document.learn("b", null, mesh("a", "b"), 30, newId, newStorage).document
        assertEquals(change.document.ids(), again.ids())
    }

    @Test fun aHubListedByAPairedHubJoinsItsGroup() {
        val document = paired("a").let { it.copy(groups = it.groups.map { group -> group.copy(hubs = group.hubs.map { hub -> hub.copy(peers = listOf("b")) }) }) }
        val change = document.addPairedHub(scope("b"), "公司", 5, newId)
        assertEquals(document.groups.single().id, change.group)
        assertEquals(listOf("a", "b"), change.document.groups.single().hubs.map { it.hubId })
    }

    @Test fun pairingTheSameHubAgainOnlyReplacesItsPairing() {
        val document = paired("a")
        val group = document.groups.single()
        val change = document.addPairedHub(StoredScope("a", "regen"), "家里", 9, newId)
        val updated = change.document.groups.single()
        assertEquals(group.id, updated.id)
        assertEquals(group.storage, updated.storage)
        assertEquals(StoredScope("a", "regen"), updated.hubs.single().pairing)
        assertEquals(9L, updated.hubs.single().pairedAt)
        // The old pairing directory still holds the group's data: only its credentials go.
        assertEquals(listOf(ScopeErasure(scope("a"), Erase.Credentials)), change.erase)
    }

    @Test fun removingAHubKeepsTheGroupAndItsDataUntilTheLastHubGoes() {
        val document = paired("a", "b").learn("a", null, mesh("a", "b"), 10, newId, newStorage).document
        val group = document.groups.single()
        val first = document.removeHub(group.id, "a")
        assertEquals(listOf(group.id to listOf("b")), first.document.ids())
        assertEquals(group.storage, first.document.groups.single().storage)
        assertEquals(listOf(ScopeErasure(scope("a"), Erase.Credentials)), first.erase)
        assertEquals(group.id, first.document.primary)
        val last = first.document.removeHub(group.id, "b")
        assertTrue(last.document.groups.isEmpty())
        assertEquals(setOf(ScopeErasure(scope("a"), Erase.All), ScopeErasure(scope("b"), Erase.All)), last.erase.toSet())
        assertNull(last.document.primary)
        assertNull(last.document.selected)
    }

    @Test fun removingThePrimaryGroupPromotesNobodyUntilEveryGroupIsGone() {
        val document = paired("a", "b")
        val (first, second) = document.groups
        val change = document.copy(selected = first.id).removeGroup(first.id)
        assertEquals(listOf(ScopeErasure(scope("a"), Erase.All)), change.erase)
        assertNull(change.document.primary)
        assertEquals(second.id, change.document.selected)
        // The remaining group keeps its suffix.
        assertEquals(second.id, change.document.namespace(second.id))
        // A pairing while other groups exist does not become primary either.
        val third = change.document.addPairedHub(scope("c"), null, 30, newId)
        assertNull(third.document.primary)
        assertEquals(second.id, third.document.selected)
        // With every group gone, the next pairing is the primary group again.
        val empty = third.document.removeGroup(second.id).document.removeGroup(third.group!!).document
        assertEquals(GroupsDocument(), empty)
        val fresh = empty.addPairedHub(scope("d"), null, 40, newId)
        assertEquals(fresh.group, fresh.document.primary)
        assertEquals(fresh.group, fresh.document.selected)
    }

    @Test fun theSelectionFollowsTheGroupItWasOn() {
        val document = paired("a", "b", "c")
        val third = document.groups[2]
        val selected = document.copy(selected = third.id)
        // c joins b's group: c's group is absorbed, the selection moves to where c went.
        val merged = selected.learn("c", null, mesh("b", "c"), 10, newId, newStorage).document
        assertEquals(merged.groupOfHub("c")!!.id, merged.selected)
        assertEquals(document.groups[1].id, merged.selected)
        // A selected group that keeps its id keeps the selection.
        val kept = document.copy(selected = document.groups[1].id).learn("c", null, mesh("b", "c"), 10, newId, newStorage).document
        assertEquals(document.groups[1].id, kept.selected)
    }

    @Test fun aSplitOfThePrimaryGroupKeepsPrimaryWithTheFirstHub() {
        val joined = paired("a", "b").learn("b", null, mesh("a", "b"), 10, newId, newStorage).document
        val primary = joined.primary
        val split = joined.learn("b", null, mesh("b"), 20, newId, newStorage).document
        assertEquals(primary, split.groupOfHub("a")!!.id)
        assertEquals(primary, split.primary)
        assertNotEquals(primary, split.groupOfHub("b")!!.id)
    }

    @Test fun erasuresTellDataFromCredentials() {
        val old = GroupsDocument(groups = listOf(
            StoredGroup("g00000001", storage = scope("a"), hubs = listOf(StoredHub("a", "agen"), StoredHub("b", "bgen"))),
            StoredGroup("g00000002", storage = scope("c"), hubs = listOf(StoredHub("c", "cgen"))),
        ))
        val new = GroupsDocument(groups = listOf(
            StoredGroup("g00000001", storage = scope("a"), hubs = listOf(StoredHub("b", "bgen"), StoredHub("c", "cgen"))),
        ))
        assertEquals(setOf(ScopeErasure(scope("a"), Erase.Credentials), ScopeErasure(scope("c"), Erase.Data)), erasures(old, new).toSet())
        assertEquals(setOf(ScopeErasure(scope("a"), Erase.All), ScopeErasure(scope("b"), Erase.All), ScopeErasure(scope("c"), Erase.All)),
            erasures(old, GroupsDocument()).toSet())
    }

    @Test fun partialErasuresKeepTheOtherHalf() {
        val roots = listOf(temporary.newFolder("files"), temporary.newFolder("cache"), temporary.newFolder("no-backup"))
        val noBackup = roots[2]
        val scope = HubScope("hub-a", "gen-1")
        fun seed() = roots.forEach { root ->
            File(root, "hub-data/${scope.path}/preferences").mkdirs()
            File(root, "hub-data/${scope.path}/data.db").writeText("data")
            if (root == noBackup) pairingFiles.forEach { File(root, "hub-data/${scope.path}/$it").writeText(it) }
        }
        seed()
        eraseData(roots, noBackup, scope)
        assertFalse(File(roots[0], "hub-data/${scope.path}").exists())
        assertFalse(File(roots[1], "hub-data/${scope.path}").exists())
        assertEquals(pairingFiles, File(noBackup, "hub-data/${scope.path}").list()!!.toSet())
        roots.forEach { File(it, "hub-data/${scope.path}").deleteRecursively() }
        seed()
        eraseCredentials(noBackup, scope)
        assertEquals(setOf("preferences", "data.db"), File(noBackup, "hub-data/${scope.path}").list()!!.toSet())
        assertTrue(File(roots[0], "hub-data/${scope.path}/data.db").exists())
        assertEquals(listOf(scope), scopeDirectories(roots))
        eraseScope(roots, scope)
        assertTrue(scopeDirectories(roots).isEmpty())
        roots.forEach { assertFalse(File(it, "hub-data/hub-a").exists()) }
    }
}
