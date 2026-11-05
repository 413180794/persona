package persona.feature.settings

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.BindField
import persona.core.contracts.api.BindPrompt
import persona.core.contracts.api.HubConnector

/** A plugin connector's sign-in, a step at a time, against a fake hub (what each step asks for is the hub's). */
class ConnectorSignInModelTest {
    private class Hub(vararg answers: BindPrompt) : ConnectorsApiStub() {
        val queue = ArrayDeque(answers.toList())
        val steps = mutableListOf<Pair<String, Map<String, String>>>()
        var loggedOut = false
        override suspend fun connectorLogin(id: String, step: String, values: Map<String, String>): BindPrompt {
            steps += step to values
            return queue.removeFirstOrNull() ?: throw java.io.IOException("offline")
        }
        override suspend fun connectorLogout(id: String) { loggedOut = true }
    }

    @Test fun fieldsThenCodeThenDone() = runBlocking {
        val hub = Hub(
            BindPrompt(step = "server", fields = listOf(BindField("server", "Server"), BindField("note", "Note"))),
            BindPrompt(step = "scan", kind = "qr", interval = 1),
            BindPrompt(step = "scan", kind = "qr", interval = 1),
            BindPrompt(done = true, account = "wxid_example_a"),
        )
        val model = ConnectorSignInModel(hub, "example")
        assertTrue(model.start())
        assertEquals("" to emptyMap<String, String>(), hub.steps[0])
        assertFalse(model.state.value.bind.canSubmit)
        model.setValue("server", " https://wechat-connector.example.com ")
        assertTrue(model.submit())
        // Trimmed, and the empty field left out.
        assertEquals("server" to mapOf("server" to "https://wechat-connector.example.com"), hub.steps[1])
        assertTrue(model.state.value.bind.polled)
        var waits = 0
        model.poll { waits++ }
        assertEquals(2, waits)
        assertEquals(listOf("scan", "scan"), hub.steps.drop(2).map { it.first })
        assertNull(model.state.value.bind.prompt)
        assertEquals("wxid_example_a", model.state.value.signedInAs)
        model.signedInShown()
        assertNull(model.state.value.signedInAs)
    }

    @Test fun aFailedStepSaysWhyAndKeepsTheStep() = runBlocking {
        val model = ConnectorSignInModel(Hub(BindPrompt(step = "token", fields = listOf(BindField("token", "Token", secret = true)))), "example")
        model.start()
        model.setValue("token", "example")
        assertFalse(model.submit())
        val bind = model.state.value.bind
        assertNotNull(bind.error)
        assertFalse(bind.sending)
        assertEquals("token", bind.prompt?.step)
        model.cancel()
        assertNull(model.state.value.bind.prompt)
    }

    @Test fun logoutAndCapabilities() = runBlocking {
        val hub = Hub()
        assertNull(ConnectorSignInModel(hub, "example").logout())
        assertTrue(hub.loggedOut)
        val c = HubConnector(id = "example", name = "Example", status = "rate_limited", capabilities = listOf("login", "threads"))
        assertTrue(ConnectorText.signsInHere(c))
        assertTrue(ConnectorText.isSignedIn(c))
        assertFalse(ConnectorText.signsInHere(c.copy(capabilities = listOf("threads"))))
        assertFalse(ConnectorText.isSignedIn(c.copy(status = "not_configured")))
    }
}
