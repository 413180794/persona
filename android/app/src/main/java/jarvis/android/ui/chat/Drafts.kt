package jarvis.android.ui.chat

import androidx.compose.runtime.mutableStateMapOf
import kotlin.reflect.KProperty

/**
 * What is typed but not sent, per conversation (iOS `model.drafts`): kept for the app's life, outside any page, so it
 * survives a page moving in the tree — folding or unfolding a foldable puts the chat in another layout (one page or
 * two panes) — and leaving the chat and coming back.
 */
object Drafts {
    private val map = mutableStateMapOf<String, String>()

    operator fun get(key: String): String = map[key].orEmpty()

    operator fun set(key: String, text: String) {
        if (text.isEmpty()) map.remove(key) else map[key] = text
    }

    /** `var draft by Drafts.of(key)`: one conversation's draft as a local variable a page reads and writes. */
    fun of(key: String) = Ref(key)

    class Ref(private val key: String) {
        operator fun getValue(thisRef: Any?, property: KProperty<*>): String = Drafts[key]
        operator fun setValue(thisRef: Any?, property: KProperty<*>, value: String) { Drafts[key] = value }
    }

    /** The new-conversation page's (助手's empty page). */
    const val NEW_CONVERSATION = "assistant/new"
}
