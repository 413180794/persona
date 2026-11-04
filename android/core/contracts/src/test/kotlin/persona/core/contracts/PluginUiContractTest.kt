package persona.core.contracts

import org.junit.Assert.*
import org.junit.Test

// docs/plugins.md「UI 插槽」「角标」: the hub pushes a nav page's badge as a plugin_ui event; null clears it.
class PluginUiContractTest {
    @Test fun aBadgeEventDecodes() {
        val event = ServerEvent.decode("""{"type":"plugin_ui","plugin":"example-gateway","id":"usage","badge":{"text":"3","tone":"danger"}}""")
        assertEquals(PluginUiEvent("example-gateway", "usage", persona.core.contracts.api.PluginViewBadge("3", "danger")), event)
        val cleared = ServerEvent.decode("""{"type":"plugin_ui","plugin":"example-gateway","id":"usage","badge":null}""") as PluginUiEvent
        assertNull(cleared.badge)
        assertNull((ServerEvent.decode("""{"type":"plugin_ui","plugin":"example-gateway","id":"usage"}""") as PluginUiEvent).badge)
        // Without the plugin or the id it is not one.
        assertNull(ServerEvent.decode("""{"type":"plugin_ui","id":"usage"}"""))
    }
}
