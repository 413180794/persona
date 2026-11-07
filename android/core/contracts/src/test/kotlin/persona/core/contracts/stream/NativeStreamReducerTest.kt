package persona.core.contracts.stream

import java.io.File
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test

class NativeStreamReducerTest {
    private val examples = File("../../../schema/extension/examples")
    private fun fixture(name: String) = File(examples, name).readText()
    private fun snapshot(items: String = "", sections: String? = null, version: String = "1") =
        """{"protocol":1,"session":"session-a","epoch":"epoch-a","version":$version,"root_key":"root","view":{"title":"Page","sections":${sections ?: """[{"key":"section","items":[$items]}]"""}}}"""
    private fun delta(ops: String, base: String = "1", version: String = "2") =
        """{"protocol":1,"session":"session-a","epoch":"epoch-a","base_version":$base,"version":$version,"operations":[$ops]}"""

    @Test fun keyedMarkdownDetailRetainsNativeTextAndNavigation() {
        val text = snapshot("""{"type":"markdown","key":"detail-note","text":"**Sample item**\n\nA synthetic note."},{"type":"button","key":"detail-edit","title":"Edit","action":{"id":"edit","push":{"state":"edit-state","title":"Edit"}}}""")
        NativeStreamSchema.parse(text, "UIStreamSnapshot")
        val reducer = NativeStreamReducer()
        assertTrue(reducer.snapshot(text) is NativeStreamUpdate.Applied)
        assertEquals("**Sample item**\n\nA synthetic note.", reducer.tree!!.nodes.getValue("detail-note").getValue("text").jsonPrimitive.content)
    }

    @Test fun sharedTraceMatchesCanonicalExpectedSnapshot() {
        val reducer = NativeStreamReducer()
        assertTrue(reducer.snapshot(fixture("UIStreamSnapshot.keyed.json")) is NativeStreamUpdate.Applied)
        assertTrue(reducer.delta(fixture("UIStreamDelta.operations.json")) is NativeStreamUpdate.Applied)
        val expected = Json.parseToJsonElement(fixture("UIStreamSnapshot.after-operations.json"))
        assertEquals(expected, reducer.tree!!.raw)
    }

    @Test fun emptyRequiredCollectionsSurviveProductionDecode() {
        val reducer = NativeStreamReducer()
        assertTrue(reducer.snapshot(fixture("UIStreamSnapshot.required-empty.json")) is NativeStreamUpdate.Applied)
        assertEquals(Json.parseToJsonElement(fixture("UIStreamSnapshot.required-empty.json")), reducer.tree!!.raw)
    }

    @Test fun reorderAndCrossSectionMovePreservePayload() {
        val reducer = NativeStreamReducer()
        reducer.snapshot(snapshot(sections = """[{"key":"left","items":[{"type":"text","key":"a","text":"A"},{"type":"text","key":"b","text":"B"}]},{"key":"right","items":[]}]"""))
        assertTrue(reducer.delta(delta("""{"op":"move","key":"b","parent":"left","before":"a"},{"op":"move","key":"a","parent":"right"}""")) is NativeStreamUpdate.Applied)
        assertEquals("b", reducer.tree!!.raw["view"]!!.jsonObject["sections"]!!.jsonArray[0].jsonObject["items"]!!.jsonArray[0].jsonObject["key"]!!.jsonPrimitive.content)
        assertEquals("a", reducer.tree!!.raw["view"]!!.jsonObject["sections"]!!.jsonArray[1].jsonObject["items"]!!.jsonArray[0].jsonObject["key"]!!.jsonPrimitive.content)
    }

    @Test fun replacementClearsOptionalMetadataAndPreservesFalseZeroEmpty() {
        val reducer = NativeStreamReducer()
        reducer.snapshot(snapshot("""{"type":"progress","key":"p","label":"Progress","ratio":0.8,"detail":"old"}"""))
        val op = """{"op":"replace","key":"root","view":{"sections":[{"key":"section","items":[{"type":"progress","key":"p","label":"Progress","ratio":0},{"type":"image","key":"image","src":{"url":"https://example.com/a.png"},"alt":"Picture","caption":"","no_zoom":false}]}]}}"""
        assertTrue(reducer.delta(delta(op)) is NativeStreamUpdate.Applied)
        assertFalse(reducer.tree!!.raw["view"]!!.jsonObject.containsKey("title"))
        val p = reducer.tree!!.nodes.getValue("p")
        assertFalse(p.containsKey("detail"))
        assertEquals(JsonPrimitive(0), p["ratio"])
        assertEquals(JsonPrimitive(false), reducer.tree!!.nodes.getValue("image")["no_zoom"])
        assertEquals(JsonPrimitive(""), reducer.tree!!.nodes.getValue("image")["caption"])
    }

    @Test fun invalidBatchesRollBackEveryOperation() {
        val bad = listOf(
            """{"op":"unknown","key":"a"}""",
            """{"op":"insert","parent":"section","before":"missing","item":{"type":"text","key":"b","text":"B"}}""",
            """{"op":"move","key":"a","parent":"root"}""",
            """{"op":"move","key":"a","parent":"section","before":"a"}""",
            """{"op":"remove","key":"root"}""",
            """{"op":"replace","key":"a","item":{"type":"text","key":"wrong","text":"B"}}""",
            """{"op":"replace","key":"a","view":{"sections":[]}}""",
            """{"op":"insert","parent":"section","item":{"type":"text","key":"section","text":"B"}}""",
            """{"op":"replace","key":"a","item":{"type":"progress","key":"a","label":"P","ratio":"wrong"}}""",
            """{"op":"remove","key":"a","before":"extra"}""",
            """{"op":"insert","parent":"section","before":"","item":{"type":"text","key":"b","text":"B"}}""",
            """{"op":"replace","key":"a","item":{"type":"text","key":"a"}}""",
        )
        bad.forEach { op ->
            val reducer = NativeStreamReducer()
            reducer.snapshot(snapshot("""{"type":"text","key":"a","text":"A"}"""))
            val old = reducer.tree
            val good = """{"op":"insert","parent":"section","item":{"type":"text","key":"staged","text":"S"}}"""
            assertEquals(op, NativeStreamUpdate.SnapshotRequired, reducer.delta(delta("$good,$op")))
            assertSame(old, reducer.tree)
        }
    }

    @Test fun snapshotRejectsDuplicateMissingKeysAndExtensionHtml() {
        val bad = listOf(
            """{"type":"text","key":"root","text":"A"}""",
            """{"type":"text","text":"A"}""",
            """{"type":"html","key":"a","html":"<p>forbidden</p>"}""",
            """{"type":"grid","key":"a","tiles":[{"title":"T"}]}""",
            """{"type":"grid","key":"a","tiles":[{"key":"root","title":"T"}]}""",
        )
        bad.forEach { assertEquals(it, NativeStreamUpdate.SnapshotRequired, NativeStreamReducer().snapshot(snapshot(it))) }
    }

    @Test fun tilesCannotBeMovedRemovedOrReplacedIndividually() {
        val reducer = NativeStreamReducer()
        assertTrue(reducer.snapshot(snapshot("""{"type":"grid","key":"grid","tiles":[{"key":"tile","title":"T"}]}""")) is NativeStreamUpdate.Applied)
        listOf("""{"op":"remove","key":"tile"}""", """{"op":"move","key":"tile","parent":"section"}""").forEach {
            assertEquals(NativeStreamUpdate.SnapshotRequired, reducer.delta(delta(it)))
        }
    }

    @Test fun gapsDuplicatesStaleEpochAndEmptyBatchRules() {
        val reducer = NativeStreamReducer()
        reducer.snapshot(snapshot())
        val old = reducer.tree
        listOf(delta("", base = "2", version = "2"), delta("", version = "3"), delta("", version = "0"),
            delta("", version = "1").replace("epoch-a", "epoch-b")).forEach {
            assertEquals(NativeStreamUpdate.SnapshotRequired, reducer.delta(it)); assertSame(old, reducer.tree)
        }
        assertTrue(reducer.delta(delta("", version = "1")) is NativeStreamUpdate.Applied)
        val fenceReducer = NativeStreamReducer().also { it.snapshot(snapshot()) }
        assertTrue(fenceReducer.delta(delta("", version = "2")) is NativeStreamUpdate.Applied)
        assertEquals(2L, fenceReducer.tree!!.snapshot.version)
        assertEquals(old!!.raw["view"], fenceReducer.tree!!.raw["view"])
        assertEquals(NativeStreamUpdate.SnapshotRequired, fenceReducer.delta(delta("", version = "2")))
        val changed = delta("""{"op":"remove","key":"section"}""")
        assertTrue(reducer.delta(changed) is NativeStreamUpdate.Applied)
        assertEquals(NativeStreamUpdate.SnapshotRequired, reducer.delta(changed))
    }

    @Test fun rawNumericAndResourceBoundsFailClosed() {
        listOf("0", "-1", "9007199254740992", "9007199254740990.9", "1.1").forEach {
            assertEquals(it, NativeStreamUpdate.SnapshotRequired, NativeStreamReducer().snapshot(snapshot(version = it)))
        }
        assertTrue(NativeStreamReducer().snapshot(snapshot(version = "9007199254740991")) is NativeStreamUpdate.Applied)
        val items = (1..299).joinToString(",") { """{"type":"divider","key":"item-$it"}""" }
        assertEquals(NativeStreamUpdate.SnapshotRequired, NativeStreamReducer().snapshot(snapshot(items)))
        val reducer = NativeStreamReducer()
        reducer.snapshot(snapshot())
        assertEquals(NativeStreamUpdate.SnapshotRequired, reducer.delta(delta(List(257) { """{"op":"replace","key":"section","section":{"key":"section","items":[]}}""" }.joinToString(","))))
        assertEquals(NativeStreamUpdate.SnapshotRequired, reducer.snapshot(" ".repeat(1024 * 1024 + 1)))
        assertEquals(NativeStreamUpdate.SnapshotRequired, reducer.snapshot("[".repeat(17) + "]".repeat(17)))
        assertEquals(NativeStreamUpdate.SnapshotRequired, reducer.snapshot(snapshot().replace("\"title\":\"Page\"", "\"state\":\"" + "é".repeat(9000) + "\"")))
    }

    @Test fun allDiffReviewCallbackSlotsRemainLocalToTheirOwningNode() {
        val reducer = NativeStreamReducer()
        val item = """{"type":"diff","key":"diff","path":"file.txt","context":{"id":"context"},"draft":{"id":"draft"},"threads":[{"side":"new","line":1,"comments":[{"author":"Example"}],"resolve":{"id":"resolve"}}]}"""
        assertTrue(reducer.snapshot(snapshot(item)) is NativeStreamUpdate.Applied)
        listOf("context", "draft", "resolve").forEach { action ->
            assertTrue(action, reducer.tree!!.callback("diff", action, "press"))
            assertFalse(reducer.tree!!.callback("section", action, "press"))
            assertFalse(reducer.tree!!.callback("root", action, "press"))
            assertFalse(reducer.tree!!.callback("diff", action, "change"))
        }
    }

    @Test fun malformedFramesAndWrongSiblingDestinationsNeverCommitStagedChanges() {
        val reducer = NativeStreamReducer()
        assertTrue(reducer.snapshot(snapshot(sections = """[{"key":"left","items":[{"type":"text","key":"a","text":"A"}]},{"key":"right","items":[{"type":"text","key":"b","text":"B"}]}]""")) is NativeStreamUpdate.Applied)
        val original = reducer.tree
        val staged = """{"op":"replace","key":"a","item":{"type":"text","key":"a","text":"Staged"}}"""
        val frames = listOf("null", "[]", "{}", "{", "{\"protocol\":1,\"protocol\":1}",
            delta("$staged,{\"op\":\"move\",\"key\":\"a\",\"parent\":\"right\",\"before\":\"a\"}"),
            delta("$staged,{\"op\":\"move\",\"key\":\"left\",\"parent\":\"root\",\"before\":\"b\"}"),
            delta("$staged,{\"op\":\"insert\",\"parent\":\"left\",\"section\":{\"key\":\"new\",\"items\":[]}}"),
            delta(staged).replace("session-a", "other-session"))
        frames.forEach { frame ->
            assertEquals(frame, NativeStreamUpdate.SnapshotRequired, reducer.delta(frame))
            assertSame(original, reducer.tree)
        }
    }

    @Test fun callbacksAreLocalToNodeAndEventSlot() {
        val reducer = NativeStreamReducer()
        reducer.snapshot(snapshot("""{"type":"button","key":"a","title":"A","action":{"id":"press-a"}},
            {"type":"segmented","key":"b","name":"range","value":"day","choices":[{"value":"day"},{"value":"week"}],"action":{"id":"change-b"}}"""))
        val tree = reducer.tree!!
        assertTrue(tree.callback("a", "press-a", "press"))
        assertFalse(tree.callback("root", "press-a", "press"))
        assertFalse(tree.callback("section", "press-a", "press"))
        assertFalse(tree.callback("b", "press-a", "press"))
        assertTrue(tree.callback("b", "change-b", "change"))
        assertFalse(tree.callback("b", "change-b", "press"))
    }
}
