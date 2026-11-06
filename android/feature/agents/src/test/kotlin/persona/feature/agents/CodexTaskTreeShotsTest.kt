package persona.feature.agents

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Session
import persona.core.designsystem.*
import persona.core.domain.sort.*
import java.io.File
import java.time.Instant

@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp-xhdpi")
class CodexTaskTreeShotsTest {
    @get:Rule val rule = createComposeRule()

    @Test fun expandsAndCollapsesRealTaskRowsInEveryStyleAndTheme() {
        fun task(id: String, parent: String? = null) = AgentSession(Session(machineId = "machine", sessionId = id,
            title = id, cwd = "/code/app", sourceKind = "codex_history", sourceThreadId = id, parentThreadId = parent, state = "working"))
        val list = buildAgentList(listOf(task("Main task"), task("Review", "Main task"), task("Regression", "Review")))
        var expanded by mutableStateOf(emptySet<String>())
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background)) {
                    agentTreeEntries(list, "machine", expandedTasks = expanded).forEach { entry ->
                        when (entry) {
                            is TreeRowEntry -> SlotRow(entry.row, null, AgentRowMetadata(), Instant.EPOCH, false, false, false,
                                depth = entry.depth, position = GroupPosition(entry.first, entry.last), located = entry.located, onOpen = {}, menuItems = { emptyList() }, branch = entry.branch)
                            is TreeChildrenEntry -> AgentSpaceHeader(stringResource(R.string.agents_child_tasks, entry.count), !entry.expanded,
                                { expanded = if (entry.expanded) expanded - entry.parent.id else expanded + entry.parent.id },
                                Modifier.testTag(entry.key), depth = entry.depth, kind = stringResource(R.string.agents_child_tasks_kind))
                            is TreeGroupEntry -> AgentSpaceHeader(entry.group.title.text(), false, {}, depth = entry.depth)
                            else -> Unit
                        }
                    }
                }
            }
        }
        val out = File("build/codex-task-shots").also { it.mkdirs() }
        fun capture(name: String) {
            val image = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out, "$name.png").outputStream().use { assertTrue(image.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        }
        for (nextStyle in AppStyle.entries) for (nextDark in listOf(false, true)) {
            rule.runOnIdle { style = nextStyle; dark = nextDark; expanded = emptySet() }
            rule.onNodeWithText("Review").assertDoesNotExist()
            capture("${nextStyle.name}-$nextDark-collapsed")
            rule.onNodeWithTag("children:machine/Main task").performClick()
            rule.onNodeWithText("Review").assertIsDisplayed()
            rule.onNodeWithTag("children:machine/Review").performClick()
            rule.onNodeWithText("Regression").assertIsDisplayed()
            capture("${nextStyle.name}-$nextDark-expanded")
            rule.onNodeWithTag("children:machine/Main task").performClick()
            rule.onNodeWithText("Review").assertDoesNotExist()
            rule.onNodeWithText("Regression").assertDoesNotExist()
        }
    }
}
