package persona.core.designsystem

import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.click
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.longClick
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeUp
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.withLink
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/**
 * A tap on a page's blank space clears the text field's focus (and with it the keyboard); a tap that something under
 * the finger handles (a button, a long press, a scroll, the field itself, the composer around it) still does its own
 * thing and leaves the focus where it was.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh-w400dp-h900dp-mdpi")
class KeyboardDismissTest {
    @get:Rule val rule = createComposeRule()

    private var focused by mutableStateOf(false)
    private var clicks = 0
    private var linkClicks = 0
    private var longPresses = 0
    private var style by mutableStateOf(AppStyle.Glass)
    private lateinit var listState: LazyListState

    private fun setPage(nested: Boolean = false) {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = style) {
                var text by remember { mutableStateOf("") }
                val state = rememberLazyListState()
                listState = state
                val page: @Composable () -> Unit = {
                    JPage("对话", bottomBar = {
                        ComposerFrame(Modifier.testTag("composer")) {
                            BasicTextField(text, { text = it }, Modifier.fillMaxWidth().testTag("field").onFocusChanged { focused = it.isFocused })
                        }
                    }) { padding ->
                        Column(Modifier.fillMaxSize().padding(padding)) {
                            Box(Modifier.size(120.dp, 48.dp).testTag("button").clickable { clicks++ })
                            Text(buildAnnotatedString {
                                withLink(LinkAnnotation.Clickable("message-link") { linkClicks++ }) { append("消息链接") }
                            }, Modifier.testTag("link"))
                            Box(Modifier.size(120.dp, 48.dp).testTag("card").pointerInput(Unit) { detectTapGestures(onLongPress = { longPresses++ }) })
                            Box(Modifier.fillMaxWidth().height(48.dp).testTag("message").onLongPress(Unit) { longPresses++ })
                            LazyColumn(Modifier.fillMaxWidth().height(200.dp).testTag("list"), state = state) {
                                items(50) { Text("消息 $it", Modifier.fillMaxWidth().height(40.dp)) }
                            }
                            Box(Modifier.fillMaxWidth().weight(1f).testTag("blank"))
                        }
                    }
                }
                if (nested) JPage("外页") { _ -> page() } else page()
            }
        }
        focusField()
    }

    private fun focusField() {
        rule.onNodeWithTag("field").performTouchInput { click() }
        rule.waitForIdle()
        assertTrue("the field takes focus when tapped", focused)
    }

    @Test fun blankTapClearsFocusInEveryStyle() {
        setPage()
        for (each in AppStyle.entries) {
            style = each
            rule.waitForIdle()
            focusField()
            rule.onNodeWithTag("blank").performTouchInput { click(center) }
            rule.waitForIdle()
            assertFalse("blank tap clears the focus in $style", focused)
        }
    }

    @Test fun tapOnTheFieldKeepsFocus() {
        setPage()
        rule.onNodeWithTag("field").performTouchInput { click() }
        rule.waitForIdle()
        assertTrue(focused)
    }

    @Test fun tapOnTheComposerAroundTheFieldKeepsFocus() {
        setPage(nested = true)
        // The composer's padding, outside the field: in glass the composer floats over the page's content.
        for (each in AppStyle.entries) {
            style = each
            rule.waitForIdle()
            focusField()
            rule.onNodeWithTag("composer").performTouchInput { click(centerLeft.copy(x = 20f)) }
            rule.waitForIdle()
            assertTrue("composer padding keeps focus inside nested pages in $style", focused)
        }
    }

    @Test fun buttonStillClicksAndKeepsFocus() {
        setPage()
        rule.onNodeWithTag("button").performTouchInput { click() }
        rule.waitForIdle()
        assertEquals(1, clicks)
        assertTrue(focused)
    }

    @Test fun messageLinkStillOpensAndKeepsFocus() {
        setPage()
        rule.onNodeWithTag("link").performTouchInput { click() }
        rule.waitForIdle()
        assertEquals(1, linkClicks)
        assertTrue(focused)
    }

    @Test fun longPressStillFiresAndKeepsFocus() {
        setPage()
        rule.onNodeWithTag("card").performTouchInput { longClick() }
        rule.waitForIdle()
        assertEquals(1, longPresses)
        assertTrue(focused)
    }

    @Test fun tapOnWhatOnlyHasALongPressClearsFocus() {
        setPage()
        rule.onNodeWithTag("message").performTouchInput { click() }
        rule.waitForIdle()
        assertFalse(focused)
        assertEquals(0, longPresses)
        focusField()
        rule.onNodeWithTag("message").performTouchInput { longClick() }
        rule.waitForIdle()
        assertEquals(1, longPresses)
        assertTrue(focused)
    }

    @Test fun scrollKeepsFocus() {
        setPage()
        rule.onNodeWithTag("list").performTouchInput { swipeUp() }
        rule.waitForIdle()
        assertTrue("the list actually scrolled", listState.firstVisibleItemIndex > 0 || listState.firstVisibleItemScrollOffset > 0)
        assertTrue(focused)
    }

    private fun setSheet() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                var text by remember { mutableStateOf("") }
                JSheet(onDismiss = {}) {
                    BasicTextField(text, { text = it }, Modifier.fillMaxWidth().height(48.dp).testTag("field").onFocusChanged { focused = it.isFocused })
                    Box(Modifier.fillMaxWidth().height(48.dp).testTag("button").clickable { clicks++ })
                    Box(Modifier.fillMaxWidth().height(120.dp).testTag("blank"))
                }
            }
        }
        focusField()
    }

    @Test fun sheetBlankTapClearsFocus() {
        setSheet()
        rule.onNodeWithTag("blank").performTouchInput { click() }
        rule.waitForIdle()
        assertFalse(focused)
    }

    @Test fun sheetControlsKeepFocus() {
        setSheet()
        rule.onNodeWithTag("field").performTouchInput { click() }
        rule.onNodeWithTag("button").performTouchInput { click() }
        rule.waitForIdle()
        assertEquals(1, clicks)
        assertTrue(focused)
    }
}
