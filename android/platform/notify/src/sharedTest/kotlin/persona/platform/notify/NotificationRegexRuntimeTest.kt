package persona.platform.notify

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import persona.core.contracts.Interaction
import persona.core.contracts.Question
import persona.core.contracts.QuestionOption
import persona.core.contracts.api.ConnectionState

/** Runs unchanged on the device regex engine and the desktop JVM used by Robolectric. */
@RunWith(AndroidJUnit4::class)
class NotificationRegexRuntimeTest {
    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Test fun foregroundNotificationCopyInitializes() {
        assertEquals(R.string.notify_connection_online, ongoingTitle(ConnectionState.Online))
    }

    @Test fun questionOptionsNormalizeEveryUnicodeWhiteSpace() {
        val whitespace = listOf(
            '\u0009', '\u000A', '\u000B', '\u000C', '\u000D', '\u0020', '\u0085',
            '\u00A0', '\u1680', '\u2000', '\u2001', '\u2002', '\u2003', '\u2004',
            '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200A', '\u2028',
            '\u2029', '\u202F', '\u205F', '\u3000',
        )
        whitespace.forEach { space ->
            assertEquals("U+${space.code.toString(16)}", "Pick one\nFirst second", body("${space}First${space}${space}second${space}"))
        }
        assertEquals("Pick one", body(whitespace.joinToString("")))
    }

    @Test fun questionOptionsPreserveNonWhitespaceFormatAndControlCharacters() {
        // These are not Unicode White_Space, even when a platform's isWhitespace says otherwise.
        listOf('\u001C', '\u001D', '\u001E', '\u001F', '\u180E', '\u200B', '\uFEFF').forEach { character ->
            assertEquals("Pick one\nA${character}B", body("A${character}B"))
        }
    }

    private fun body(label: String): String = requestCopy(context, Interaction(
        "regex-regression",
        kind = "question",
        questions = listOf(Question(question = "Pick one", options = listOf(QuestionOption(label = label)))),
    )).second
}
