package persona.feature.knowledge

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
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.navigation3.runtime.NavKey
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.HubScope
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.navigation.AppTab
import persona.core.navigation.Navigator
import java.io.File

/**
 * 知识库: the list and an entry's page in each of the app's languages (docs/i18n.md), Glass light and dark and the other
 * styles light. Written as PNGs to `build/i18n-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class KnowledgeShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h1100dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", list = "知识库", detail = "问 Persona")

    @Config(sdk = [35], qualifiers = "en-w400dp-h1100dp-xhdpi")
    @Test fun english() = shots("en", list = "Knowledge", detail = "Ask Persona")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h1100dp-xhdpi")
    @Test fun japanese() = shots("ja", list = "ナレッジ", detail = "Personaに質問")

    private fun shots(language: String, list: String, detail: String) {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var page by mutableStateOf("list")
        val connection = object : KnowledgeConnection { override val current = MutableStateFlow<KnowledgeRepository?>(DemoKnowledge) }
        val listModel = KnowledgeViewModel(connection)
        val detailModel = KnowledgeDetailViewModel(KnowledgeKind.Projects, "weather", connection, NoSender)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").width(400.dp).height(1060.dp).background(PersonaTheme.palette.background)) {
                    if (page == "list") KnowledgeScreen(listModel, NoNavigator)
                    else KnowledgeDetailScreen(detailModel, NoNavigator, openLinkInApp = {})
                }
            }
        }
        for (name in listOf("list", "entry")) {
            page = name
            for ((s, d) in listOf(AppStyle.Glass to false, AppStyle.Glass to true, AppStyle.Brief to false, AppStyle.Board to false)) {
                style = s; dark = d
                rule.waitForIdle()
                rule.onNodeWithText(if (name == "list") list else detail).assertExists()
                val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
                val file = "knowledge-$name-$language-${s.name.lowercase()}-${if (d) "dark" else "light"}.png"
                File(out(), file).outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            }
        }
    }
}

private object NoNavigator : Navigator {
    override fun navigate(key: NavKey) = Unit
    override fun back() = Unit
    override fun selectTab(tab: AppTab) = Unit
}

private object NoSender : MainConversationSender {
    override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
    override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) = Unit
}

/** Made-up projects: one important, one ongoing, one done, one that is not a project. */
private object DemoKnowledge : KnowledgeRepository {
    override val scope = HubScope("demo", "shots")
    private val projects = listOf(
        KnowledgeEntry("projects", "weather", "Weather App", "Widgets and a watch face for the forecast", "P0", 92,
            listOf("ios", "widget"), "2026-11-04T09:30:00+08:00", "short · active"),
        KnowledgeEntry("projects", "site", "Website", "The landing page and docs", "P2", 45, listOf("web"), "2026-11-01T18:05:00+08:00", "long · active"),
        KnowledgeEntry("projects", "launch", "Beta launch", "Shipped to testers", "P1", 60, emptyList(), "2026-10-20T10:00:00+08:00", "short · done"),
        KnowledgeEntry("projects", "promo", "Newsletter", null, "P3", 5, emptyList(), null, "short · ignored"),
    )
    override suspend fun list(kind: KnowledgeKind, tag: String?) =
        KnowledgeList(if (kind == KnowledgeKind.Projects) projects else emptyList(), listOf("ios", "web"))
    override suspend fun detail(kind: KnowledgeKind, id: String) = KnowledgeDetail(kind.wire, id, "~/.persona/data/projects/weather",
        meta = buildJsonObject {
            put("name", "Weather App"); put("one_line", "Widgets and a watch face for the forecast"); put("importance", "P0")
            put("relevance", 92); put("kind", "short"); put("status", "active"); put("updated", "2026-11-04T09:30:00+08:00")
            put("tags", buildJsonArray { add("ios"); add("widget") })
            put("dirs", buildJsonArray { add("laptop:/home/me/code/weather/app") })
        },
        summary = "# Weather App\n\n- Widgets ship next week\n- Watch face in review",
        log = (1..8).map { "- 2026-11-0$it 10:00 · Agent · laptop · Step $it done" },
        links = listOf(KnowledgeEntry("people", "alex", "Alex", "Designer"), KnowledgeEntry("docs", "spec", "Widget spec")))
    override suspend fun search(query: String) = emptyList<KnowledgeHit>()
}
