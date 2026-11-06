package persona.feature.agents

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.unit.dp
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.emptyPreferences
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File
import java.time.Instant

/**
 * "Lark groups" / "Terminal" on an agent's row (#3132, iOS #2698), under its time (#3043), in every style and appearance, the detailed
 * and the compact list. Written as PNGs to `build/source-shots/` (or `-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1400dp-xhdpi")
class AgentsSourceShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/source-shots").also { it.mkdirs() }

    private val now = Instant.now()
    private fun ago(minutes: Long) = now.minusSeconds(minutes * 60).toString()
    private val machines = listOf(Machine("mac", "MacBook Pro", online = true, engine = "herdr"))
    private val sessions = listOf(
        Session("mac", "s1", title = "修登录页，顺便把注册页的校验也改一下", cwd = "/code/app", state = "needs_you", spaceId = "w1",
            spaceName = "app", slotName = "1", lastText = "要跑测试吗？", updatedAt = ago(50), paneId = "w1:p1"),
        Session("mac", "s2", title = "群里问的发布时间", cwd = "/code/app", state = "unread", spaceId = "bm", spaceName = "botmux",
            lastText = "已经回复到群里了", updatedAt = ago(2), paneId = "botmux:abc", externalUrl = "https://example.com/topic"),
        Session("mac", "s3", title = "重构接口", cwd = "/code/api", state = "working", spaceId = "w3", spaceName = "api",
            slotName = "1", lastText = "在改重试", updatedAt = ago(10), paneId = "orca:1"),
    )

    private class Connection : HubConnection {
        override val state = MutableStateFlow(ConnectionState.Online)
        override val events = MutableSharedFlow<ServerEvent>()
        override val lastHello: Hello get() = Hello(serverVersion = "1.0.0", protocol = 1)
        override fun reconnect() = Unit
        override fun send(message: JsonObject) = true
    }
    private class Interactions : InteractionCenter {
        override val pending = MutableStateFlow<List<Interaction>>(emptyList())
        override suspend fun refresh() = Unit
        override suspend fun resolve(id: String, resolution: Resolution) = Unit
    }
    private class Sender : MessageSender {
        override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
    }
    private class Repository(sessions: List<Session>, machines: List<Machine>) : SessionsRepository {
        override val sessions = MutableStateFlow(sessions)
        override val machines = MutableStateFlow(machines)
        override suspend fun refresh() = Unit
    }
    private object Nowhere : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }

    private fun preferences(): AgentsPreferences {
        val store = object : DataStore<Preferences> {
            override val data = MutableStateFlow<Preferences>(emptyPreferences())
            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences =
                transform(data.value).also { data.value = it }
        }
        val hub = object : HubPreferences {
            override val preferences = MutableStateFlow(HubPreferencesSnapshot())
            override suspend fun refresh() = Unit
            override suspend fun togglePin(path: String, space: Boolean) = Unit
            override suspend fun toggleMute(path: String, space: Boolean) = Unit
            override suspend fun markSeen(session: SessionKey, version: String) = Unit
        }
        return AgentsPreferences(store, hub)
    }

    @Test fun sourceTagsUnderTheTime() {
        val model = AgentsViewModel(Repository(sessions, machines), Interactions(), Connection(), Sender(), preferences())
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(1400.dp).background(PersonaTheme.palette.background)) {
                    AgentsScreen(model, Nowhere, AgentsMetadata())
                }
            }
        }
        rule.waitForIdle()
        for (compact in listOf(false, true)) {
            if (compact) { rule.runOnIdle { model.compact() }; rule.waitForIdle() }
            for (s in AppStyle.entries) for (d in listOf(false, true)) {
                rule.runOnIdle { style = s; dark = d }
                rule.waitForIdle()
                // one tag each on the botmux and the herdr agent; the Orca agent carries none
                assertEquals(1, rule.onAllNodesWithText("飞书群", useUnmergedTree = true).fetchSemanticsNodes()
                    .count { node -> node.config.getOrElseNullable(androidx.compose.ui.semantics.SemanticsActions.OnClick) { null } != null })
                assertTrue(rule.onAllNodesWithText("终端", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
                val list = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                val name = "source-${if (compact) "compact" else "list"}-${s.name.lowercase()}-${if (d) "dark" else "light"}.png"
                File(out(), name).outputStream().use { assertTrue(list.compress(Bitmap.CompressFormat.PNG, 100, it)) }
            }
        }
    }
}
