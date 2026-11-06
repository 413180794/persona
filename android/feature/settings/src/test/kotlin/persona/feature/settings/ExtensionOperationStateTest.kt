package persona.feature.settings

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.yield
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.*

internal val opPreview = ExtensionPreview("pv1", PreviewManifest("example-notes", "Example Notes", "1.2.0"),
    listOf(ExtensionPermission("runs_code", "在 hub 机器上运行程序")), "d-1")

internal fun op(state: String, step: String, at: Int, vararg steps: Pair<String, String>, kind: String = "install", error: String? = null,
    retryable: Boolean = false, id: String = "op1") = ExtensionOperation(id, kind, "example-notes", name = "Example Notes", market = "m1",
    version = "1.2.0", state = state, step = step, steps = steps.map { (s, st) -> ExtensionOpStep(s, st) }, error = error, retryable = retryable,
    startedAt = "2026-11-06T08:00:00Z", updatedAt = "2026-11-06T08:00:%02dZ".format(at))

/** A hub with extension_operations: the operations it starts and the pushes a test sends. */
internal open class FakeOperations(var stepped: Boolean = true) : ExtensionApiStub() {
    val calls = mutableListOf<String>()
    val pushes = MutableSharedFlow<ExtensionOperation?>(extraBufferCapacity = 16)
    var listed = emptyList<ExtensionOperation>()
    var started = op("running", "download", 1, "download" to "running", "verify" to "pending", "consent" to "pending", "install" to "pending")
    var catalog = listOf(CatalogExtension("m1", id = "example-notes", name = "Example Notes", version = "1.2.0"))
    override val operationsAvailable: Flow<Boolean> get() = flowOf(stepped)
    override val operationUpdates: Flow<ExtensionOperation?> get() = pushes
    override suspend fun catalog(query: String, market: String?, refresh: Boolean) = ExtensionCatalog(listOf(ExtensionMarket("m1", name = "Example")), catalog)
    override suspend fun previewFromMarket(market: String, extension: String): ExtensionPreview { calls += "preview"; return opPreview }
    override suspend fun install(preview: ExtensionPreview): ExtensionDetail { calls += "install"; return ExtensionDetail(HubExtension("example-notes", "Example Notes", "ui", "running")) }
    override suspend fun uninstall(id: String, keepData: Boolean, cascade: Boolean) { calls += "uninstall" }
    override suspend fun startFromMarket(market: String, extension: String): ExtensionOperation { calls += "start $market/$extension"; return started }
    override suspend fun agree(preview: ExtensionPreview, upgradeOf: String?): ExtensionOperation {
        calls += "agree ${preview.previewId} $upgradeOf"
        return op("running", "install", 6, "download" to "done", "consent" to "done", "install" to "running")
    }
    override suspend fun startUninstall(id: String, keepData: Boolean): ExtensionOperation {
        calls += "uninstall-async $id keep=$keepData"; return op("running", "stop", 1, "stop" to "running", "remove" to "pending", kind = "uninstall", id = "op2")
    }
    override suspend fun operations(): List<ExtensionOperation> { calls += "list"; return listed }
    override suspend fun operation(id: String): ExtensionOperation { calls += "get $id"; return listed.firstOrNull { it.id == id }?.copy(preview = opPreview) ?: started.copy(preview = opPreview) }
    override suspend fun retryOperation(id: String): ExtensionOperation { calls += "retry $id"; return started.copy(updatedAt = "2026-11-06T08:01:00Z") }
    override suspend fun cancelOperation(id: String) { calls += "cancel $id" }
}

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ExtensionOperationStateTest {
    private val res get() = ApplicationProvider.getApplicationContext<android.content.Context>().resources

    /** Runs the model's watch beside block, as the page does while it is open. */
    private fun watching(api: FakeOperations, block: suspend (CatalogExtensionModel, MutableList<String>) -> Unit) = runBlocking {
        val model = CatalogExtensionModel(api, "m1", "example-notes", res)
        val notices = mutableListOf<String>()
        model.load()
        val job = launch(Dispatchers.Unconfined) { model.watch { notices += it } }
        try { block(model, notices) } finally { job.cancelAndJoin() }
    }

    private suspend fun until(check: () -> Boolean) = withTimeout(2_000) { while (!check()) yield() }

    @Test fun theStepsSayWhatTheyAreAt() {
        val o = op("running", "download", 1, "download" to "running", "verify" to "pending", "unpack" to "skipped", "teleport" to "pending")
            .let { it.copy(steps = it.steps.map { s -> if (s.id == "download") s.copy(bytes = 3L shl 20, total = 16L shl 20) else s }) }
        assertEquals(listOf("download", "verify", "teleport"), OperationText.shown(o).map { it.id })
        assertEquals("下载…", OperationText.line(res, o))
        assertEquals("3.0 MB / 16 MB", OperationText.detail(res, o.steps[0]))
        assertEquals(3f / 16f, OperationText.fraction(o.steps[0])!!, 0.001f)
        // A step this app does not know shows its id.
        assertEquals("teleport", OperationText.step(res, "teleport", o.kind))
        assertEquals("升级", OperationText.step(res, "install", "upgrade"))
        assertEquals("用本机缓存", OperationText.detail(res, ExtensionOpStep("download", "done", cached = true)))
        assertEquals("1 / 2 台", OperationText.detail(res, ExtensionOpStep("nodes", "running", machines = ExtensionOpMachines(1, 2))))
        assertEquals("卡在「下载」", OperationText.line(res, o.copy(state = "failed")))
        assertEquals("等你确认权限", OperationText.line(res, o.copy(state = "needs_consent")))
        assertEquals(0 to 3, OperationText.progress(o))
        assertTrue(OperationText.cancelable(o))
        assertFalse(OperationText.cancelable(o.copy(steps = o.steps + ExtensionOpStep("install", "running"))))
    }

    @Test fun theWholeWay() {
        val api = FakeOperations()
        watching(api) { model, notices ->
            until { "list" in api.calls }
            assertTrue(model.preview())
            assertTrue("start m1/example-notes" in api.calls)
            // Older news that comes late does not undo it.
            api.pushes.emit(op("running", "download", 0, "download" to "pending"))
            assertEquals("running", model.state.value.operation?.steps?.first()?.state)
            // Checked: the hub waits for the yes, the sheet shows its preview.
            api.pushes.emit(op("needs_consent", "consent", 3, "download" to "done", "consent" to "pending"))
            until { model.state.value.preview != null }
            assertTrue("get op1" in api.calls)
            assertEquals("op1", model.state.value.consentFor)
            assertNull(model.confirm())
            assertTrue("agree pv1 null" in api.calls)
            assertNull(model.state.value.preview)
            assertEquals("install", model.state.value.operation?.step)
            api.pushes.emit(op("succeeded", "nodes", 9, "install" to "done"))
            until { notices.isNotEmpty() }
            assertEquals(listOf("已安装 Example Notes"), notices)
            assertNull(model.state.value.operation)
            assertFalse("install" in api.calls)
        }
    }

    @Test fun closingTheSheetCancels() {
        val api = FakeOperations()
        api.started = op("needs_consent", "consent", 2, "download" to "done", "consent" to "pending")
        watching(api) { model, _ ->
            model.preview()
            until { model.state.value.preview != null }
            model.dismissPreview()
            assertTrue("cancel op1" in api.calls)
            assertNull(model.state.value.preview)
            assertNull(model.state.value.operation)
            // The hub's canceled push changes nothing more.
            api.pushes.emit(op("canceled", "consent", 4))
            assertNull(model.state.value.operation)
        }
    }

    @Test fun reopeningShowsTheOneStuckAndRetries() {
        val api = FakeOperations()
        val stuck = op("failed", "download", 5, "download" to "failed", "verify" to "pending", error = "下载 x：30 秒没有收到数据", retryable = true)
        api.listed = listOf(stuck, op("succeeded", "nodes", 1, id = "old").copy(startedAt = "2026-11-06T07:00:00Z"),
            op("running", "download", 1, id = "other").copy(extension = "someone-else"))
        watching(api) { model, notices ->
            until { model.state.value.operation != null }
            assertEquals(stuck.id, model.state.value.operation?.id)
            assertFalse(model.state.value.busy)
            assertTrue(model.retry())
            assertTrue("retry op1" in api.calls)
            assertEquals("running", model.state.value.operation?.state)
            // After a reconnect it is listed again; what was missed shows.
            api.listed = listOf(op("failed", "start", 0, "start" to "failed", error = "启动失败", retryable = false).copy(updatedAt = "2026-11-06T08:02:00Z"))
            api.pushes.emit(null)
            until { model.state.value.operation?.step == "start" }
            assertFalse(model.retry())
            model.closeOperation()
            assertNull(model.state.value.operation)
            assertTrue("cancel op1" in api.calls)
            assertTrue(notices.isEmpty())
        }
    }

    @Test fun removingInSteps() {
        val api = FakeOperations()
        watching(api) { model, notices ->
            assertFalse(model.uninstall(keepData = true))
            assertTrue("uninstall-async example-notes keep=true" in api.calls)
            assertEquals("uninstall", model.state.value.operation?.kind)
            api.pushes.emit(op("succeeded", "nodes", 9, kind = "uninstall", id = "op2"))
            until { notices.isNotEmpty() }
            assertEquals("已卸载 Example Notes", notices.single())
        }
    }

    @Test fun anOlderHubWaitsForEachCall() {
        val api = FakeOperations(stepped = false)
        watching(api) { model, _ ->
            assertTrue(model.preview())
            assertEquals(listOf("preview"), api.calls)
            assertEquals("pv1", model.state.value.preview?.previewId)
            assertNull(model.state.value.operation)
            assertEquals("已安装", model.confirm())
            assertTrue(model.uninstall(keepData = true))
            assertEquals(listOf("preview", "install", "uninstall"), api.calls)
        }
    }
}
