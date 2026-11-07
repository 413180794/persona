package persona.core.contracts.stream

import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test

/** Every native callback slot uses its displayed node, action ID and event kind, never its parent or neighbor. */
class NativeStreamActionSlotsTest {
    @Test fun completeNativeSlotVocabularyIsNodeLocal() {
        val items = listOf(
            """{"type":"button","key":"button","title":"Run","action":{"id":"button","submit":true}}""",
            """{"type":"row","key":"row","title":"Row","action":{"id":"row"},"control":{"kind":"switch","name":"on","on":false,"action":{"id":"control"}},"actions":[{"title":"Menu","action":{"id":"menu"}}]}""",
            """{"type":"segmented","key":"segmented","name":"range","value":"a","choices":[{"value":"a"},{"value":"b"}],"action":{"id":"segment"}}""",
            """{"type":"breadcrumb","key":"breadcrumb","crumbs":[{"title":"Root","action":{"id":"crumb"}},{"title":"Current"}]}""",
            """{"type":"file_tree","key":"files","entries":[{"path":"dir","kind":"dir"}],"action":{"id":"file"},"expand":{"id":"directory"}}""",
            """{"type":"code","key":"code","text":"first","truncated":true,"load":{"id":"code-load"}}""",
            """{"type":"diff","key":"diff","path":"file.txt","load":{"id":"diff-load"},"expand":{"id":"expand"},"comment":{"id":"comment"},"draft":{"id":"draft"},"mark_viewed":{"id":"mark"},"threads":[{"id":"thread","side":"new","line":1,"comments":[{"author":"Demo","body":"A comment"}],"reply":{"id":"thread-reply"},"resolve":{"id":"resolve"}}]}""",
            """{"type":"comment_thread","key":"comments","comments":[],"reply":{"id":"reply"}}""",
            """{"type":"composer","key":"composer","name":"body","buttons":[{"title":"Post","action":{"id":"post"}}]}""",
            """{"type":"empty","key":"empty","title":"Empty","button":{"title":"Retry","action":{"id":"retry"}}}""",
            """{"type":"grid","key":"grid","tiles":[{"key":"tile","title":"Tile","action":{"id":"tile"},"control":{"kind":"switch","name":"active","action":{"id":"tile-control"}},"actions":[{"title":"Reset","action":{"id":"tile-menu"}}]}]}""",
            """{"type":"carousel","key":"carousel","tiles":[{"key":"camera","title":"Camera","action":{"id":"camera"}}]}""",
        ).joinToString(",")
        val raw = """{"protocol":1,"session":"session","epoch":"epoch","version":1,"root_key":"root","view":{"search":{"action":{"id":"search"}},"filter":{"name":"scope","value":"a","choices":[{"value":"a"},{"value":"b"}],"action":{"id":"filter"}},"tabs":{"name":"tab","value":"a","items":[{"value":"a","title":"A"},{"value":"b","title":"B"}],"action":{"id":"tab"}},"review":{"count":1,"name":"summary","buttons":[{"title":"Approve","action":{"id":"approve"}}],"discard":{"id":"discard"}},"sections":[{"key":"section","items":[$items],"more":{"action":{"id":"more"}}}]}}"""
        val reducer = NativeStreamReducer()
        assertTrue(reducer.snapshot(raw) is NativeStreamUpdate.Applied)
        val tree = reducer.tree!!
        val press = mapOf("row" to listOf("row", "menu"), "breadcrumb" to listOf("crumb"), "files" to listOf("file", "directory"),
            "code" to listOf("code-load"), "diff" to listOf("diff-load", "expand", "comment", "draft", "mark", "thread-reply", "resolve"),
            "comments" to listOf("reply"), "button" to listOf("button"), "empty" to listOf("retry"), "tile" to listOf("tile", "tile-menu"),
            "camera" to listOf("camera"), "root" to listOf("discard"), "section" to listOf("more"))
        val changes = mapOf("root" to listOf("search", "filter", "tab"), "row" to listOf("control"), "tile" to listOf("tile-control"), "segmented" to listOf("segment"))
        val submissions = mapOf("composer" to listOf("post"), "root" to listOf("approve"))
        (press.map { it to "press" } + changes.map { it to "change" } + submissions.map { it to "submit" }).forEach { (entry, event) ->
            val (key, actions) = entry
            actions.forEach { action ->
                assertTrue("$key/$action/$event", tree.callback(key, action, event))
                assertFalse(tree.callback(key, action, if (event == "press") "change" else "press"))
                tree.nodes.keys.filter { it != key }.forEach { other -> assertFalse("$other must not borrow $key/$action", tree.callback(other, action, event)) }
            }
        }
        assertTrue(tree.callback("button", "button", "press"))
        assertFalse(tree.callback("button", "button", "submit"))
    }
}
