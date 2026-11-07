package persona.core.designsystem

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.GroupKeys
import persona.core.contracts.api.HubManager
import persona.core.contracts.api.ManagerRoster

/**
 * What a conversation says where its composer was while its manager is offline (docs/assistant/managers.md "Clients"): by the
 * conversation's own manager, whichever one the page shows, and only for the selected group's hub.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class ManagerOfflineNoteTest {
    private val res = ApplicationProvider.getApplicationContext<android.content.Context>().resources
    private val roster = ManagerRoster(listOf(
        HubManager("assistant", "助手", online = true, isDefault = true, builtin = true),
        HubManager("m-3fa21c", "工作", online = true),
        HubManager("m-8b04d2", "家里", online = false),
        HubManager("m-c1e9f0", "测试", online = false, installing = "2026-11-05T06:07:00Z"),
    ), "assistant")

    @Test fun anOfflineManagersConversation() {
        assertEquals("家里 · 离线", managerOfflineNote(res, roster, null, "nas", "m-8b04d2"))
        // Online, the default's (no stamp), or one still installing: it takes messages as before.
        assertNull(managerOfflineNote(res, roster, null, "mac", "m-3fa21c"))
        assertNull(managerOfflineNote(res, roster, null, "mac", null))
        assertNull(managerOfflineNote(res, roster, null, "mac", "m-c1e9f0"))
        // One manager: nothing changes.
        assertNull(managerOfflineNote(res, ManagerRoster(listOf(roster.managers.first()), "assistant"), null, "nas", "m-8b04d2"))
    }

    @Test fun onlyTheSelectedGroupsHub() {
        val elsewhere = GroupKeys.machine("nas", "g0000abcd")
        assertNull(managerOfflineNote(res, roster, null, elsewhere, "m-8b04d2"))
        assertEquals("家里 · 离线", managerOfflineNote(res, roster, "g0000abcd", elsewhere, "m-8b04d2"))
    }
}
