package persona.feature.settings

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.ExtensionUiContext
import persona.core.contracts.api.ExtensionUiResult
import persona.core.contracts.api.ExtensionView
import java.io.File

/**
 * quota-relay's sidebar page (#4576) as the extension draws it, from made-up data (the extension's
 * `testdata/relay-accounts-view.json`): every item decodes and is drawn, the account rows push the account's page,
 * and the type / status / search controls send what the extension reads back.
 */
internal object RelayFixture {
    private val raw: JsonObject by lazy { JsonCodec.parseToJsonElement(File("src/test/fixtures/relay-accounts-view.json").readText()).jsonObject }

    fun view(name: String): ExtensionView = JsonCodec.decodeFromJsonElement(ExtensionView.serializer(), raw.getValue(name))

    /** How many items the extension sent in [name], before decoding. */
    fun rawItems(name: String): Int = raw.getValue(name).jsonObject.getValue("sections").jsonArray
        .sumOf { (it.jsonObject["items"] as? JsonArray)?.size ?: 0 }
}

private class RelayUi : ExtensionApiStub() {
    val views = mapOf("all" to RelayFixture.view("default"), "kiro" to RelayFixture.view("kiro"))
    val actions = mutableListOf<Triple<String, String?, Map<String, String>?>>()
    /** The type the extension keeps in its state: a filter or a search sends no type and keeps it. */
    private var type = "all"
    private var status = "all"

    override suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext?, state: String?): ExtensionView = views.getValue(type)

    override suspend fun uiAction(extension: String, ui: String, action: String, context: ExtensionUiContext?, state: String?,
                                  values: Map<String, String>?): ExtensionUiResult {
        actions += Triple(action, state, values)
        values?.get("type")?.let { type = it }
        values?.get("status")?.let { status = it }
        // the extension's answer says which status is chosen (the fixture's views are all drawn with `all`)
        val view = views.getValue(type)
        return ExtensionUiResult(view = view.copy(filter = view.filter?.copy(value = status)))
    }
}

class RelayAccountsTest {
    private val pages = listOf("default", "kiro", "detail")

    @Test fun everyItemDecodesAndIsDrawn() {
        for (name in pages) {
            val view = RelayFixture.view(name)
            val items = view.sections.flatMap { it.items }
            assertEquals("$name: no item left out when read", RelayFixture.rawItems(name), items.size)
            assertEquals("$name: every item drawn", emptyList<String>(), items.filterNot(ExtensionViewText::drawable).map { it.type + ":" + it.key })
            assertTrue("$name: the list's keys are its own", ExtensionViewText.itemKeys(items).filterNotNull().let { it.size == it.toSet().size })
        }
        val default = RelayFixture.view("default")
        // all 73 accounts (71 on the gateway and 3 seats, a link with seats drawn as their section) as rows
        assertEquals(73, default.sections.flatMap { it.items }.count { it.type == "row" })
        assertTrue("well inside the 300-item cap", RelayFixture.rawItems("default") <= 280)
        // the status filter has more than 5 choices: this app (UI version 3+) keeps it and the search
        assertEquals(8, default.filter!!.choices.size)
        assertEquals("all", default.filter!!.value)
        assertNotNull(default.search)
        assertEquals("all", ExtensionViewText.fields(default)["type"])
    }

    @Test fun noLauncherOrKeyOnThePage() {
        for (name in pages) {
            val text = JsonCodec.encodeToString(ExtensionView.serializer(), RelayFixture.view(name))
            assertFalse("$name names a launcher", "启动器" in text)
            assertFalse("$name names a key", Regex("""Key[：:]""").containsMatchIn(text))
        }
    }

    @Test fun anAccountRowPushesItsPage() = runBlocking {
        val api = RelayUi()
        val model = ExtensionViewModel(api, "quota-relay", "sidebar")
        model.load()
        val rows = model.state.value.view!!.sections.flatMap { it.items }.filter { it.type == "row" && it.action != null }
        assertEquals(73, rows.size)
        for (row in rows) {
            val push = row.action!!.push!!
            assertEquals("sidebar", push.ui)
            assertTrue(push.state!!.startsWith("{\"v\":1,\"a\":"))
            assertEquals(row.title, push.title)
        }
        val outcome = model.press(rows.first().action!!)
        assertEquals(rows.first().action!!.push, outcome.push)
        assertTrue("a push never reaches the extension", api.actions.isEmpty())
        val route = ExtensionViewText.pushed("quota-relay", "sidebar", "中转账号", 0, outcome.push!!)!!
        assertEquals("sidebar", route.uiId)
        assertEquals(rows.first().action!!.push!!.state, route.state)
    }

    @Test fun typeStatusAndSearchGoBackWithTheState() = runBlocking {
        val api = RelayUi()
        val model = ExtensionViewModel(api, "quota-relay", "sidebar")
        model.load()
        val state = model.state.value.view!!.state
        val type = model.state.value.view!!.sections[0].items.first { it.type == "segmented" }
        model.choose(type, "kiro")
        assertEquals(Triple("type", state, mapOf("type" to "kiro")), api.actions.last())
        val kiro = model.state.value.view!!
        assertEquals("{\"v\":1,\"type\":\"kiro\",\"status\":\"all\"}", kiro.state)
        assertEquals("kiro", model.state.value.values["type"])
        model.filter("cooldown")
        assertEquals(Triple(kiro.filter!!.action.id, kiro.state, mapOf("status" to "cooldown")), api.actions.last())
        model.search("kiro-4*")
        assertEquals(Triple(kiro.search!!.action.id, kiro.state, mapOf("q" to "kiro-4*", "status" to "cooldown")), api.actions.last())
        assertFalse(model.state.value.busy)
    }
}
