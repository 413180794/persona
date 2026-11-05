package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.junit.Assert.assertEquals
import org.junit.Test

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ChatQueueStripTest {
    private val res = ApplicationProvider.getApplicationContext<Context>().resources
    @Test fun titleSaysHowManyAndForWhom() {
        assertEquals("排队中 2 条，Claude 忙完会依次处理", queueTitle(2, "Claude", res))
        assertEquals("排队中 1 条，Codex 忙完会依次处理", queueTitle(1, "Codex", res))
    }

    @Test fun pullingOneOutKeepsTheRestInOrder() {
        assertEquals(listOf(1, 2), queueKeep(3, 0))
        assertEquals(listOf(0, 2), queueKeep(3, 1))
        // The only queued message: nothing is put back, and the empty list is what the hub gets.
        assertEquals(emptyList<Int>(), queueKeep(1, 0))
    }

    @Test fun aPulledBackMessageGoesAfterTheDraftOnANewLine() {
        val empty = ChatComposerState()
        empty.restoreQueued("顺便把 README 也更新一下")
        assertEquals("顺便把 README 也更新一下", empty.value.text)
        assertEquals(empty.value.text.length, empty.value.selection.start)

        val typed = ChatComposerState(ChatComposerDraft("先跑测试"))
        typed.restoreQueued("改完以后跑一遍 UI 测试")
        assertEquals("先跑测试\n改完以后跑一遍 UI 测试", typed.value.text)

        // Photos it carried come back as attachments, not as their tag.
        val photos = ChatComposerState(ChatComposerDraft("看下"))
        photos.restoreQueued("截图在这\n\n<attached_images>\n/tmp/shot.png\n</attached_images>")
        assertEquals("看下\n截图在这", photos.value.text)
        assertEquals(listOf("/tmp/shot.png"), photos.attachments.map { it.path })
    }
}
