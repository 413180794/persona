package persona.android.ui

import android.app.Application
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.navigation.AppTab

@RunWith(AndroidJUnit4::class)
@Config(application = Application::class, sdk = [35])
class ProtocolCompatibilityPanesTest {
    private val context get() = ApplicationProvider.getApplicationContext<Context>()
    private val current get() = context.getSharedPreferences("persona-panes", Context.MODE_PRIVATE)
    private val old get() = context.getSharedPreferences("jarvis-panes", Context.MODE_PRIVATE)
    @After fun clear() { current.edit().clear().commit(); old.edit().clear().commit() }

    @Test fun restoresOnlyKnownPaneKeysAndKeepsCanonicalPresenceAuthoritative() {
        old.edit().putFloat("split.jarvis", 0.4f).putFloat("split.agents", 0.6f).putString("user-data", "unchanged").commit()
        val panes = PaneSplitStore(context)
        assertEquals(0.4f, panes.share(AppTab.Persona)!!, 0f)
        assertEquals(0.6f, panes.share(AppTab.Agents)!!, 0f)
        assertFalse(current.contains("user-data"))
        current.edit().putFloat("split.persona", -1f).commit()
        assertNull(panes.share(AppTab.Persona))
    }

    @Test fun savesBothGenerationsWithoutRenamingTheApplicationIdentity() {
        PaneSplitStore(context).save(AppTab.Persona, 0.45f)
        assertEquals(0.45f, current.getFloat("split.persona", -1f), 0f)
        assertEquals(0.45f, old.getFloat("split.jarvis", -1f), 0f)
    }
}
