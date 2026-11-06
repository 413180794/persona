package persona.feature.settings

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.yield
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.ExtensionAuthEvent
import persona.core.contracts.api.*

/** An extension's command-line tools and sign-in (#4507): the words, `next: auth`, the pushes and signing in in steps. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class ExtensionAuthStateTest {
    private val res get() = ApplicationProvider.getApplicationContext<android.content.Context>().resources

    private suspend fun until(check: () -> Boolean) = withTimeout(2_000) { while (!check()) yield() }

    @Test fun theToolsStepInWords() {
        val step = ExtensionOpStep("tools", "running", bytes = 1_048_576, total = 4_194_304,
            tools = listOf(ExtensionOpTool("node", "done", reused = true), ExtensionOpTool("example-cli", "running"), ExtensionOpTool("jq", "pending")))
        assertEquals("安装命令行", OperationText.step(res, "tools", "install"))
        assertEquals("", OperationText.detail(res, step))
        assertNull(OperationText.fraction(step))
        assertEquals("用这台机器上的", OperationText.toolDetail(res, step, step.tools!![0]))
        assertEquals("1.0 MB / 4.0 MB", OperationText.toolDetail(res, step, step.tools!![1]))
        assertEquals(0.25f, OperationText.toolFraction(step, step.tools!![1])!!, 0.001f)
        assertNull(OperationText.toolFraction(step, step.tools!![2]))
        val failed = op("failed", "tools", 9, "tools" to "failed").copy(errorCode = "tools_failed", error = "example-cli：校验不对")
        assertEquals("命令行没装上：example-cli：校验不对", OperationText.error(res, failed))
        assertEquals("原因：x", OperationText.error(res, failed.copy(errorCode = "download_failed", error = "x")))
    }

    @Test fun theSignInInWords() {
        fun line(state: String, account: String? = null, message: String? = null) =
            ExtensionAuthText.state(res, ExtensionAuth("飞书登录", state = state, account = account, message = message))
        assertEquals("未登录", line("signed_out"))
        assertEquals("等待确认", line("pending"))
        assertEquals("已登录：me@example.com", line("signed_in", "me@example.com"))
        assertEquals("已过期", line("expired"))
        assertEquals("出错：连不上", line("error", message = "连不上"))
        assertEquals("扩展没在运行", line("unknown"))
        assertEquals("重新登录", ExtensionAuthText.signInLabel(res, ExtensionAuth(state = "expired")))
        assertEquals("登录", ExtensionAuthText.signInLabel(res, ExtensionAuth(state = "signed_out")))
        val ext = HubExtension("example-cis", "CIS", status = "running", auth = ExtensionAuth(state = "expired"))
        assertEquals("启动 · 未登录", ExtensionText.listLine(res, ext))
        assertEquals("启动", ExtensionText.listLine(res, ext.copy(auth = ExtensionAuth(state = "signed_in"))))
    }

    @Test fun installedAndNotSignedInGoesOnToSigningIn() = runBlocking {
        val api = FakeOperations()
        val model = CatalogExtensionModel(api, "m1", "example-notes", res)
        model.load()
        val signIn = mutableListOf<String>()
        val job = launch(Dispatchers.Unconfined) { model.watch(signIn = { signIn += it }) {} }
        try {
            api.pushes.emit(op("running", "tools", 2, "tools" to "running"))
            until { model.state.value.operation != null }
            api.pushes.emit(op("succeeded", "start", 5, "tools" to "done", "start" to "done").copy(next = "auth"))
            until { signIn.isNotEmpty() }
            assertEquals(listOf("example-notes"), signIn)
        } finally { job.cancelAndJoin() }
    }

    @Test fun thePagesModelFollowsThePushes() = runBlocking {
        val pushes = MutableSharedFlow<ExtensionAuthEvent>(extraBufferCapacity = 4)
        val api = object : ExtensionApiStub() {
            override val authUpdates: Flow<ExtensionAuthEvent> get() = pushes
            override suspend fun extension(id: String) = ExtensionDetail(HubExtension(id, "CIS", status = "running", auth = ExtensionAuth("飞书登录", state = "signed_out")))
        }
        val model = ExtensionModel(api, "example-cis", res)
        model.load()
        val job = launch(Dispatchers.Unconfined) { model.watchAuth() }
        try {
            pushes.emit(ExtensionAuthEvent("other", ExtensionAuth(state = "signed_in")))
            pushes.emit(ExtensionAuthEvent("example-cis", ExtensionAuth("飞书登录", state = "signed_in", account = "me")))
            until { model.state.value.detail?.extension?.auth?.signedIn == true }
            assertEquals("me", model.state.value.detail?.extension?.auth?.account)
        } finally { job.cancelAndJoin() }
    }

    @Test fun signingInAStepAtATime() = runBlocking {
        val asked = mutableListOf<Pair<String, Map<String, String>>>()
        val answers = ArrayDeque(listOf(
            BindPrompt(step = "code", kind = "device_code", url = "https://example.com/device", userCode = "ABCD", interval = 1),
            BindPrompt(step = "code", kind = "device_code", url = "https://example.com/device", userCode = "ABCD", interval = 1),
            BindPrompt(done = true, account = "me"),
        ))
        val model = ConnectorSignInModel({ step, values -> asked += step to values; answers.removeFirst() }, {})
        assertTrue(model.start())
        assertTrue(model.state.value.bind.polled)
        model.poll(wait = {})
        assertEquals(listOf("" to emptyMap<String, String>(), "code" to emptyMap(), "code" to emptyMap()), asked)
        assertEquals("me", model.state.value.signedInAs)
        assertNull(model.state.value.bind.prompt)
    }
}
