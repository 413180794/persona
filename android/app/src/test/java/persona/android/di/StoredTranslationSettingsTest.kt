package persona.android.di

import android.content.SharedPreferences
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.HubTranslatePrefs
import persona.core.contracts.api.TranslateChat
import persona.core.contracts.api.TranslationPrefs

class StoredTranslationSettingsTest {
    /** Just enough of SharedPreferences for the store: a map, written at apply. */
    private class MemoryPrefs(val values: MutableMap<String, Any?> = mutableMapOf()) : SharedPreferences {
        override fun getAll() = values
        override fun getString(key: String, defValue: String?) = values[key] as? String ?: defValue
        override fun getStringSet(key: String, defValues: MutableSet<String>?) = defValues
        override fun getInt(key: String, defValue: Int) = values[key] as? Int ?: defValue
        override fun getLong(key: String, defValue: Long) = values[key] as? Long ?: defValue
        override fun getFloat(key: String, defValue: Float) = values[key] as? Float ?: defValue
        override fun getBoolean(key: String, defValue: Boolean) = values[key] as? Boolean ?: defValue
        override fun contains(key: String) = key in values
        override fun registerOnSharedPreferenceChangeListener(l: SharedPreferences.OnSharedPreferenceChangeListener?) {}
        override fun unregisterOnSharedPreferenceChangeListener(l: SharedPreferences.OnSharedPreferenceChangeListener?) {}
        override fun edit(): SharedPreferences.Editor = object : SharedPreferences.Editor {
            val pending = mutableMapOf<String, Any?>()
            val removed = mutableSetOf<String>()
            override fun putString(key: String, value: String?) = apply { pending[key] = value }
            override fun putStringSet(key: String, values: MutableSet<String>?) = apply { pending[key] = values }
            override fun putInt(key: String, value: Int) = apply { pending[key] = value }
            override fun putLong(key: String, value: Long) = apply { pending[key] = value }
            override fun putFloat(key: String, value: Float) = apply { pending[key] = value }
            override fun putBoolean(key: String, value: Boolean) = apply { pending[key] = value }
            override fun remove(key: String) = apply { removed += key }
            override fun clear() = apply { removed += values.keys }
            override fun commit(): Boolean { apply(); return true }
            override fun apply() { removed.forEach(values::remove); values.putAll(pending) }
        }
    }

    @Test fun defaultsAreOffMailOnNoChats() {
        val prefs = StoredTranslationSettings(MemoryPrefs()).prefs.value
        assertEquals(TranslationPrefs(enabled = false, target = "zh-Hans", mail = true, chats = emptyList()), prefs)
        assertFalse(prefs.translatesMail)
        assertEquals(HubTranslatePrefs(false, "zh-Hans", true), prefs.hubCopy)
    }

    @Test fun changesAreKeptAcrossALaunch() {
        val stored = MemoryPrefs()
        StoredTranslationSettings(stored).update {
            it.copy(enabled = true, target = "ja", mail = false, chats = listOf(TranslateChat("f2", "设计评审")))
        }
        val again = StoredTranslationSettings(stored).prefs.value
        assertTrue(again.enabled)
        assertEquals("ja", again.target)
        assertFalse(again.mail)
        assertEquals(listOf(TranslateChat("f2", "设计评审")), again.chats)
        assertTrue(again.translates("f2"))
        assertFalse(again.translates("f3"))

        StoredTranslationSettings(stored).update { it.copy(chats = emptyList()) }
        assertNull(stored.values["translate.larkChats"])
    }

    @Test fun anUnknownTargetOrUnreadableChatsFallBack() {
        val stored = MemoryPrefs(mutableMapOf("translate.target" to "tlh", "translate.larkChats" to "{not json"))
        val prefs = StoredTranslationSettings(stored).prefs.value
        assertEquals("zh-Hans", prefs.target)
        assertEquals(emptyList<TranslateChat>(), prefs.chats)
        StoredTranslationSettings(stored).update { it.copy(target = "xx") }
        assertEquals("zh-Hans", stored.values["translate.target"])
    }

    @Test fun nothingIsTranslatedWhileOff() {
        val off = TranslationPrefs(enabled = false, chats = listOf(TranslateChat("f2", "设计评审")))
        assertFalse(off.translatesMail)
        assertFalse(off.translates("f2"))
        assertFalse(off.copy(enabled = true).translates(null))
    }
}
