package persona.feature.inbox

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.designsystem.*
import persona.core.domain.inbox.InboxItem
import persona.core.navigation.*
import java.io.File
import java.lang.reflect.Proxy

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w600dp-h900dp-xhdpi")
class InboxGroupNameRowTest {
    @get:Rule val rule = createComposeRule()
    private val repository = Proxy.newProxyInstance(InboxRepository::class.java.classLoader, arrayOf(InboxRepository::class.java)) { proxy, method, args ->
        when (method.name) {
            "equals" -> proxy === args[0]
            "hashCode" -> System.identityHashCode(proxy)
            "toString" -> "RowTestRepository"
            else -> error("Unexpected repository access: ${method.name}")
        }
    } as InboxRepository
    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) = Unit
        override fun back() = Unit
        override fun selectTab(tab: AppTab) = Unit
    }
    private fun record(): InboxRecord {
        val path = System.getenv("PERSONA_INBOX_LIVE_FIXTURE")
        if (path == null) return InboxRecord(InboxItem("event", kind = "group", title = "Event title", sender = "Sender",
            summary = "Summary"), feedId = "feed", source = "lark", isGroup = true, groupName = "Source group", count = 1)
        val fixture = Json.parseToJsonElement(File(path).readText()).jsonObject
        val item = fixture.getValue("item").jsonObject
        fun text(key: String) = item[key]?.jsonPrimitive?.contentOrNull
        return InboxRecord(InboxItem(text("id")!!, kind = text("kind")!!, title = text("title")!!, sender = text("sender"),
            preview = text("preview").orEmpty(), summary = text("summary"), updatedAt = text("updated_at")),
            feedId = item.getValue("lark").jsonObject.getValue("feed_id").jsonPrimitive.content,
            source = "lark", isGroup = true, groupName = fixture.getValue("group_name").jsonPrimitive.content, count = 1)
    }
    @Test fun eventSenderAndSourceHaveDistinctOrderedLinesInEveryStyle() {
        val record = record()
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var shown by mutableStateOf(record)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Box(Modifier.testTag("row").width(580.dp).background(PersonaTheme.palette.background)) {
                    InboxItemRow(shown, repository, navigator, ReplyComposers(repository, rememberCoroutineScope()), GroupPosition.Single,
                        done = false, busy = false, onlineSince = null, onDone = {}, onHandOff = {})
                }
            }
        }
        for (appearance in AppStyle.entries) for (night in listOf(false, true)) {
            rule.runOnIdle { style = appearance; dark = night }
            rule.waitForIdle()
            val title = rule.onNodeWithText(record.item.title, useUnmergedTree = true).fetchSemanticsNode().boundsInRoot
            val sender = rule.onNodeWithText(record.item.sender!!, useUnmergedTree = true).fetchSemanticsNode().boundsInRoot
            val group = rule.onNodeWithText(record.groupName!!, useUnmergedTree = true).fetchSemanticsNode().boundsInRoot
            assertTrue(title.top < sender.top && sender.top < group.top)
            rule.onNodeWithText(record.item.summary!!, useUnmergedTree = true).assertExists()
            val out = File(System.getenv("PERSONA_INBOX_SHOTS") ?: "build/group-name-shots").apply { mkdirs() }
            File(out, "${appearance.name.lowercase()}-${if (night) "dark" else "light"}.png").outputStream().use {
                assertTrue(rule.onNodeWithTag("row").captureToImage().asAndroidBitmap().compress(Bitmap.CompressFormat.PNG, 100, it))
            }
        }
        rule.runOnIdle { shown = record.copy(groupName = null) }
        rule.onAllNodesWithText(record.item.title, useUnmergedTree = true).assertCountEquals(1)
        rule.onNodeWithText(record.item.sender!!, useUnmergedTree = true).assertExists()
        for (kind in listOf("dm", "mail")) {
            rule.runOnIdle { shown = record.copy(item = record.item.copy(kind = kind), isGroup = false) }
            rule.onNodeWithText(record.groupName!!, useUnmergedTree = true).assertDoesNotExist()
            rule.onAllNodesWithText(record.item.title, useUnmergedTree = true).assertCountEquals(1)
            rule.onNodeWithText(record.item.sender!!, useUnmergedTree = true).assertExists()
        }
    }
}
