package persona.android.di

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.api.HubTranslatePrefs
import persona.core.contracts.api.TranslateChat
import persona.core.contracts.api.TranslationPrefs

class TranslationSyncTest {
    @Test fun sendsOnlyWhatTheHubDoesNotHave() = runTest {
        val prefs = MutableStateFlow(TranslationPrefs(enabled = true, target = "en"))
        val sent = mutableListOf<HubTranslatePrefs>()
        val job = launch { syncTranslation(prefs, HubTranslatePrefs(true, "en", true)) { sent += it; it } }
        runCurrent()
        assertEquals(emptyList<HubTranslatePrefs>(), sent) // the hub already has it
        prefs.value = prefs.value.copy(chats = listOf(TranslateChat("f2", "设计评审")))
        runCurrent()
        assertEquals(emptyList<HubTranslatePrefs>(), sent) // chats stay on the phone
        prefs.value = prefs.value.copy(enabled = false)
        runCurrent()
        assertEquals(listOf(HubTranslatePrefs(false, "en", true)), sent)
        job.cancel()
    }

    @Test fun anOlderHubOrAFailedSaveIsSentAgainAtTheNextChange() = runTest {
        val prefs = MutableStateFlow(TranslationPrefs())
        val sent = mutableListOf<HubTranslatePrefs>()
        var fail = true
        val job = launch {
            syncTranslation(prefs, null) {
                sent += it
                if (fail) error("offline") else it
            }
        }
        runCurrent()
        assertEquals(listOf(HubTranslatePrefs(false, "zh-Hans", true)), sent)
        fail = false
        prefs.value = prefs.value.copy(enabled = true)
        runCurrent()
        prefs.value = prefs.value.copy(enabled = false) // back to the first: the hub never got it
        runCurrent()
        assertEquals(3, sent.size)
        job.cancel()
    }
}
