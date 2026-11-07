package persona.feature.settings

import org.junit.Assert.*
import org.junit.Test

class ProtocolCompatibilityTest {
    @Test fun baselineExtensionSourceLinksUseTheSameInAppTargetAsCurrentLinks() {
        for (scheme in listOf("jarvis", "persona")) {
            assertEquals("test:item", ExtensionViewText.inboxSource("$scheme://inbox/test%3Aitem/source"))
            assertNull(ExtensionViewText.inboxSource("$scheme://user@example.com/inbox/test/source"))
            assertNull(ExtensionViewText.inboxSource("$scheme://inbox/test/source?other=1"))
        }
        assertNull(ExtensionViewText.inboxSource("other://inbox/test/source"))
    }
}
