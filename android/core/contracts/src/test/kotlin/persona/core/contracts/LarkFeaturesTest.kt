package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Test

class LarkFeaturesTest {
    @Test fun unknownFeaturesKeepEverything() {
        assertEquals(LarkFeatures.All, LarkFeatures.of(emptySet()))
    }

    @Test fun extensionFeaturesShowChatsAndFeed() {
        assertEquals(LarkFeatures.All, LarkFeatures.of(setOf("client_ops", "lark", "lark_moments", "lark_attachments")))
        assertEquals(LarkFeatures(chats = true, moments = false), LarkFeatures.of(setOf("client_ops", "lark", "lark_attachments")))
    }

    @Test fun hubWithoutTheExtensionHasNoFeishu() {
        assertEquals(LarkFeatures.None, LarkFeatures.of(setOf("client_ops", "plugin_manager")))
        // A feed without the chats is no use: its reader is one of the extension's routes too.
        assertEquals(LarkFeatures.None, LarkFeatures.of(setOf("client_ops", "lark_moments")))
    }

    @Test fun hubFromBeforeTheExtensionServesFeishuItself() {
        assertEquals(LarkFeatures.All, LarkFeatures.of(setOf("client_ops", "lark_attachments")))
        assertEquals(LarkFeatures.All, LarkFeatures.of(setOf("lark_attachments", "lark_native_encryption")))
    }
}
