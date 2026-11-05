package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
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

    @Test fun onlyAnExtensionThatSaysSoWritesDocumentComments() {
        assertTrue(LarkFeatures.of(setOf("lark", "lark_doc_comments")).docComments)
        assertFalse(LarkFeatures.of(setOf("lark")).docComments)
        assertFalse(LarkFeatures.of(setOf("lark_doc_comments", "client_ops")).docComments) // not without the extension's lark
        // Unknown is everything, but not the comments: a hub that does not say so has no such routes.
        assertFalse(LarkFeatures.of(emptySet()).docComments)
    }
}
