package persona.core.contracts

import kotlinx.serialization.json.Json
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.ExtensionCatalog
import persona.core.contracts.api.ExtensionMarket
import persona.core.contracts.api.ExtensionUiPlace

// docs/extensions.md「UI 插槽」「角标」: the hub pushes a nav page's badge as an extension_ui event (plugin_ui before #1844,
// both since); null clears it.
class ExtensionUiContractTest {
    @Test fun aBadgeEventDecodes() {
        val event = ServerEvent.decode("""{"type":"extension_ui","extension":"example-gateway","id":"usage","badge":{"text":"3","tone":"danger"}}""")
        assertEquals(ExtensionUiEvent("example-gateway", "usage", persona.core.contracts.api.ExtensionViewBadge("3", "danger")), event)
        val legacy = ServerEvent.decode("""{"type":"plugin_ui","plugin":"example-gateway","id":"usage","badge":{"text":"3","tone":"danger"}}""")
        assertEquals(event, legacy)
        val cleared = ServerEvent.decode("""{"type":"plugin_ui","plugin":"example-gateway","id":"usage","badge":null}""") as ExtensionUiEvent
        assertNull(cleared.badge)
        assertNull((ServerEvent.decode("""{"type":"extension_ui","extension":"example-gateway","id":"usage"}""") as ExtensionUiEvent).badge)
        // Without the extension or the id it is not one.
        assertNull(ServerEvent.decode("""{"type":"plugin_ui","id":"usage"}"""))
        assertNull(ServerEvent.decode("""{"type":"extension_ui","id":"usage"}"""))
    }

    // The new routes answer extension / extension_name / extensions, the old ones plugin / plugin_name / plugins.
    @Test fun bothKeysDecode() {
        val json = Json(JsonCodec) {}
        val current = json.decodeFromString(ExtensionUiPlace.serializer(),
            """{"extension":"removed","extension_name":"removed","id":"usage","title":"用量","placement":"nav_page"}""")
        val legacy = json.decodeFromString(ExtensionUiPlace.serializer(),
            """{"plugin":"removed","plugin_name":"removed","id":"usage","title":"用量","placement":"nav_page"}""")
        assertEquals("removed", current.extension)
        assertEquals("removed", current.extensionName)
        assertEquals(current, legacy)
        assertEquals(3, json.decodeFromString(ExtensionMarket.serializer(), """{"id":"m","extensions":3}""").extensions)
        assertEquals(3, json.decodeFromString(ExtensionMarket.serializer(), """{"id":"m","plugins":3}""").extensions)
        val catalog = """{"markets":[],"plugins":[{"market":"m","id":"x","name":"X","version":"1.0.0"}]}"""
        assertEquals(1, json.decodeFromString(ExtensionCatalog.serializer(), catalog).extensions.size)
        val signed = json.decodeFromString(ExtensionMarket.serializer(), """{"id":"m","signature":{"state":"verified","key_id":"32ed46cdbe349656"}}""")
        assertTrue(signed.signature!!.verified)
        assertTrue(json.decodeFromString(ExtensionMarket.serializer(), """{"id":"m","signature":{"state":"key_changed"}}""").signature!!.failed)
    }
}
