package persona.core.contracts

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MessageEchoTest {
    private val sentAt = java.time.Instant.parse("2026-11-03T09:00:00Z")
    private fun user(text: String, secondsAfter: Long = 0, fullSize: Int? = null) =
        Item("uuid", 1, sentAt.plusSeconds(secondsAfter).toString(), role = "user", kind = "text", text = text, fullSize = fullSize)

    @Test fun sameWordsIgnoringWhitespaceAndPlaceholders() {
        assertTrue(MessageEcho.arrived("看看现在有多少\n已经合入的", sentAt.toEpochMilli(), user("[Image #3] 看看现在有多少 已经合入的")))
        assertTrue(MessageEcho.same("<pasted_content>一段比较长的粘贴内容</pasted_content>", "一段比较长的粘贴内容"))
    }

    @Test fun shortTextsMustBeEqual() {
        assertFalse(MessageEcho.same("好", "好的，删掉吧"))
        assertTrue(MessageEcho.same("好", " 好 "))
    }

    @Test fun clockSlackAndOlderMessages() {
        val created = sentAt.toEpochMilli()
        assertTrue(MessageEcho.arrived("继续", created, user("继续", secondsAfter = -100)))
        assertFalse(MessageEcho.arrived("继续", created, user("继续", secondsAfter = -200)))
        assertTrue(MessageEcho.arrived("继续", created, user("继续").copy(ts = null)))
    }

    @Test fun cutTextMatchesByItsStart() {
        val whole = "这是一条很长很长的消息".repeat(50)
        val cut = whole.take(40) + "\n…（已截断，共 9000 字节）"
        assertTrue(MessageEcho.arrived(whole, sentAt.toEpochMilli(), user(cut, fullSize = 9000)))
        assertFalse(MessageEcho.arrived("别的内容" + whole, sentAt.toEpochMilli(), user(cut, fullSize = 9000)))
    }

    @Test fun onlyUserText() {
        assertFalse(MessageEcho.arrived("继续", sentAt.toEpochMilli(), user("继续").copy(role = "assistant")))
    }
}
