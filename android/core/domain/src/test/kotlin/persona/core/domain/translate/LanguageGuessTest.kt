package persona.core.domain.translate

import persona.core.domain.zhAny

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LanguageGuessTest {
    @Test fun scripts() {
        assertEquals("zh", (LanguageGuess.detect("明天下午三点开设计评审会议")).zhAny())
        assertEquals("ja", (LanguageGuess.detect("明日の会議は午後三時からです")).zhAny())
        assertEquals("ko", (LanguageGuess.detect("내일 회의는 오후 세 시입니다")).zhAny())
        assertEquals("ru", (LanguageGuess.detect("Встреча завтра в три часа")).zhAny())
        assertEquals("en", (LanguageGuess.detect("Thanks for the update, please send the slides when you can")).zhAny())
        assertEquals("fr", (LanguageGuess.detect("Merci pour la réunion, nous sommes prêts")).zhAny())
        assertEquals("de", (LanguageGuess.detect("Danke für das Update, wir sind bereit")).zhAny())
    }

    @Test fun tooLittleToTell() {
        assertNull(LanguageGuess.detect("OK 👍"))
        assertNull(LanguageGuess.detect("12345"))
        assertFalse(LanguageGuess.isForeign("OK", "zh-Hans"))
    }

    @Test fun chineseWithAFewEnglishTermsIsChinese() {
        assertFalse(LanguageGuess.isForeign("这个 PR 的 CI 挂了，帮我看一下 build log", "zh-Hans"))
        assertFalse(LanguageGuess.isForeign("繁體中文的訊息內容在這裡", "zh-Hans")) // the two Chinese scripts are one language
    }

    @Test fun foreignToTheTarget() {
        assertTrue(LanguageGuess.isForeign("Thanks for the update, please send the slides", "zh-Hans"))
        assertTrue(LanguageGuess.isForeign("明天下午三点开会", "en"))
        assertFalse(LanguageGuess.isForeign("Thanks for the update, please send the slides", "en"))
        // Latin script of unclear language: foreign only to a target in another script.
        assertTrue(LanguageGuess.isForeign("Projekt Alpha Beta Gamma", "ja"))
        assertFalse(LanguageGuess.isForeign("Projekt Alpha Beta Gamma", "fr"))
    }
}
