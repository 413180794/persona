package persona.core.domain.views

import java.io.File
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.JsonCodec

// The reply's cards by the hub's fence rules (sdk/go/wire/assistantblocks.go) and the shared vectors.
class AssistantFencesTest {
    private fun repo(path: String): File =
        generateSequence(File(System.getProperty("persona.repo.root") ?: System.getProperty("user.dir")).absoluteFile) { it.parentFile }
            .map { File(it, path) }.firstOrNull { it.exists() } ?: error("$path not found")

    @Test fun theSharedVectors() {
        val cases = JsonCodec.parseToJsonElement(repo("schema/extension/examples/assistant-text/fences.json").readText()) as JsonArray
        assertTrue(cases.size >= 6)
        for (case in cases) {
            val o = case.jsonObject
            val name = o.getValue("name").jsonPrimitive.content
            val views = AssistantFences.blocks(o.getValue("text").jsonPrimitive.content).filter { it.isView }
            assertEquals(name, o.getValue("indexes").jsonArray.map { it.jsonPrimitive.int }, views.map { it.index })
            assertEquals(name, o.getValue("complete").jsonArray.map { it.jsonPrimitive.boolean }, views.map { it.complete })
        }
    }

    @Test fun keysCountBrokenBlocksAndSkipCards() {
        val valid = """{"fallback":"Example","sections":[]}"""
        val text = "Intro\n```persona-card\n{\"type\":\"stat\",\"title\":\"Old\",\"value\":3}\n```\n" +
            "```persona-view\ninvalid\n```\n" +
            "~~~persona-view\n$valid\n~~~\n" +
            "````markdown\n```persona-view\n$valid\n```\n````\n" +
            "````persona-view\n$valid\n````\n"
        val blocks = AssistantFences.blocks(text)
        assertEquals(listOf(-1, 0, 1, 2), blocks.map { it.index })
        assertNull(blocks[0].key("uuid#0"))
        assertEquals("uuid#0/2", blocks[3].key("uuid#0"))
        assertEquals(valid + "\n", blocks[2].raw)
        // The text around a block is everything outside it.
        assertEquals("Intro\n", text.substring(0, blocks[0].start))
    }

    @Test fun fallbackNeverShowsTheCode() {
        assertEquals("推荐 B", AssistantFences.fallback("""{"fallback":"推荐 B","sections":[]}"""))
        assertEquals("Written first", AssistantFences.fallback("""{"fallback":"Written first","sections":[{"items":[{"type":"te"""))
        assertNull(AssistantFences.fallback("""{"sections":[],"fallback":"cut off"""))
        assertNull(AssistantFences.fallback("<script>x</script>"))
        assertNull(AssistantFences.fallback("""{"fallback":"  "}"""))
        assertEquals(2000, AssistantFences.fallback("""{"fallback":"${"字".repeat(2100)}"}""")!!.length)
        assertFalse(AssistantFences.mentions("plain words"))
    }
}
