package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class SlashCommandsTest {
    private val res = ApplicationProvider.getApplicationContext<Context>().resources
    private fun words(draft: String, clear: Boolean = true) = SlashCommand.matching(draft, clear, res).map { it.word }

    @Test fun aSlashListsThemAndMoreLettersNarrow() {
        assertEquals(listOf("/继续", "/compact", "/context", "/提交", "/汇报", "/clear"), words("/"))
        assertEquals(listOf("/compact", "/context", "/clear"), words("/c"))
        assertEquals(listOf("/compact", "/context"), words("/co"))
        assertEquals(listOf("/汇报"), words("/汇"))
        assertEquals(listOf("/compact", "/context", "/clear"), words("/C"))
    }

    @Test fun onlyWhileTheDraftIsOneSlashWord() {
        assertEquals(emptyList<String>(), words("帮我看下"))
        assertEquals(emptyList<String>(), words("/compact 一下"))
        assertEquals(emptyList<String>(), words("/tmp/a\n"))
        assertEquals(listOf("/继续", "/compact", "/context", "/提交", "/汇报"), words("/", clear = false)) // Persona, Codex
    }

    @Test fun theSameFiveAsIosWithTheirMessages() {
        assertEquals(listOf("继续", "/compact", "/context", "把当前改动按规范提交并推送", "简要汇报一下目前的进展、还剩什么"),
            SlashCommand.all(res).map { it.text })
        assertEquals(listOf("继续", "压缩上下文 /compact", "查看上下文 /context", "提交并推送", "汇报进展"), SlashCommand.all(res).map { it.title })
        assertTrue(SlashCommand.clearCommand(res).clear)
        assertEquals("/clear", SlashCommand.clearCommand(res).text)
    }

    @Test fun aWordThatIsAlreadyThereHidesTheListButClearStays() {
        assertEquals(emptyList<SlashCommand>(), SlashCommand.suggestions("/compact", clear = true, res))
        assertEquals(emptyList<SlashCommand>(), SlashCommand.suggestions("/Context", clear = true, res))
        assertEquals(listOf("/clear"), SlashCommand.suggestions("/clear", clear = true, res).map { it.word })
        assertEquals(listOf("/compact", "/context"), SlashCommand.suggestions("/co", clear = true, res).map { it.word })
        assertEquals(SlashCommand.matching("/", true, res), SlashCommand.suggestions("/", true, res))
    }
}
