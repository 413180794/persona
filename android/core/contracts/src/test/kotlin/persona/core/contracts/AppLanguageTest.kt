package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Test
import java.util.Locale

class AppLanguageTest {
    private fun resolve(vararg tags: String) = AppLanguage.resolve(tags.map(Locale::forLanguageTag))

    @Test fun anyChineseShowsSimplifiedChinese() {
        assertEquals(AppLanguage.ZhHans, resolve("zh-CN"))
        assertEquals(AppLanguage.ZhHans, resolve("zh-Hant-TW"))
        assertEquals(AppLanguage.ZhHans, resolve("zh-HK"))
    }

    @Test fun japaneseShowsJapaneseAndTheRestEnglish() {
        assertEquals(AppLanguage.Ja, resolve("ja-JP"))
        assertEquals(AppLanguage.En, resolve("en-GB"))
        assertEquals(AppLanguage.En, resolve("fr-FR"))
        assertEquals(AppLanguage.En, resolve())
    }

    @Test fun onlyThePhonesFirstLanguageCounts() {
        assertEquals(AppLanguage.En, resolve("fr-FR", "ja-JP", "zh-CN"))
        assertEquals(AppLanguage.Ja, resolve("ja-JP", "zh-CN"))
    }

    @Test fun storedTagsRoundTrip() {
        AppLanguage.entries.forEach { assertEquals(it, AppLanguage.fromTag(it.tag)) }
        assertEquals(AppLanguage.ZhHans, AppLanguage.fromTag("zh-Hant"))
        assertEquals(AppLanguage.System, AppLanguage.fromTag("fr"))
        assertEquals(AppLanguage.System, AppLanguage.fromTag(""))
    }

    @Test fun theHubIsToldTheShownLanguage() {
        try {
            ClientLanguage.current = AppLanguage.Ja
            assertEquals("ja", (ClientLanguage.tag).zhAny())
            ClientLanguage.current = AppLanguage.ZhHans
            assertEquals("zh-Hans", (ClientLanguage.tag).zhAny())
            ClientLanguage.current = AppLanguage.System
            assertEquals("en", (ClientLanguage.tag).zhAny())
        } finally { ClientLanguage.current = AppLanguage.En }
    }
    @Test fun generatedLanguagesMatchTheirTagsAndHaveNames() {
        AppLanguage.entries.filter { it != AppLanguage.System }.forEach { language ->
            assertEquals(language, AppLanguage.of(Locale.forLanguageTag(language.tag)))
            org.junit.Assert.assertTrue(language.ownName.isNotBlank())
        }
    }
}
