package persona.core.data

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import org.junit.Assert.assertEquals
import org.junit.Test
import java.util.Base64

class InboxReplyReceiptTest {
    private fun receipt(body: String) = inboxReplyReceipt(Json.parseToJsonElement(
        """{"state":"done","result":{"status":200,"body":"${Base64.getEncoder().encodeToString(body.toByteArray())}"}}""").jsonObject)

    @Test fun aFeishuSendNamesItsMessageAsMessageId() {
        // hub lark.go answers a send or a thread reply with {"ok":true,"message_id":…} (#950).
        assertEquals(InboxReplyReceipt(OutboxState.Sent, "om_1"), receipt("""{"ok":true,"message_id":"om_1"}"""))
        assertEquals("om_2", receipt("""{"message":{"id":"om_2"}}""").serverId)
        assertEquals("om_3", receipt("""{"id":"om_3"}""").serverId)
    }
}
