package persona.feature.settings

import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.jsonObject
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.ExtensionView
import persona.core.contracts.generated.ExtensionPersonaCard
import java.io.File

// An assistant's ```persona-view / ```persona-card drawn with the extensions' components (#1850 / #1851).
class PersonaViewBlocksTest {
    @Test fun aViewOrOneComponent() {
        val view = PersonaViewBlocks.parse("persona-view", """{"title":"本周","sections":[{"items":[
            {"type":"stat_grid","stats":[{"label":"会议","value":"12"}]},
            {"type":"button","title":"删除","action":{"id":"x"}},
            {"type":"row","title":"周报","action":{"id":"open"}}]}]}""")!!
        assertEquals("本周", view.title)
        // Nothing in a reply acts: buttons go, rows lose their action.
        assertEquals(listOf("stat_grid", "row"), view.sections[0].items.map { it.type })
        assertNull(view.sections[0].items[1].action)
        val one = PersonaViewBlocks.parse("Persona-View", """{"type":"markdown","text":"**要点**"}""")!!
        assertEquals("markdown", one.sections.single().items.single().type)
    }

    @Test fun whatStaysCode() {
        assertNull("Another language", PersonaViewBlocks.parse("json", """{"type":"markdown","text":"x"}"""))
        assertNull("Still streaming in", PersonaViewBlocks.parse("persona-view", """{"title":"本周","sections":[{"items":[{"type":"te"""))
        assertNull("Nothing drawable", PersonaViewBlocks.parse("persona-view", """{"sections":[{"items":[{"type":"button","title":"x","action":{"id":"x"}}]}]}"""))
        assertNull("A kind this app does not know", PersonaViewBlocks.parse("persona-card", """{"type":"pie","value":1}"""))
        assertNull("A stat without its value", PersonaViewBlocks.parse("persona-card", """{"type":"stat","title":"x"}"""))
        assertNull("A line of one point", PersonaViewBlocks.parse("persona-card", """{"type":"line","points":[{"label":"a","value":1}]}"""))
        assertNull("Negative parts", PersonaViewBlocks.parse("persona-card", """{"type":"stacked_bar","segments":[{"label":"a","value":-1}]}"""))
    }

    // Every case of schema/extension/examples/persona-card/ (a card and the view it becomes, or null): the same view on
    // every app (the reference is sdk/go/wire/personacard.go).
    @Test fun cardsBecomeTheSameViewsAsOnEveryApp() {
        val dir = generateSequence(File(System.getProperty("persona.repo.root") ?: System.getProperty("user.dir")).absoluteFile) { it.parentFile }
            .map { File(it, "schema/extension/examples/persona-card") }.firstOrNull { it.isDirectory }
            ?: error("schema/extension/examples/persona-card not found")
        val cases = dir.listFiles { f -> f.name.endsWith(".json") }!!.sortedBy { it.name }
        assertTrue(cases.size >= 5)
        for (case in cases) {
            val json = JsonCodec.parseToJsonElement(case.readText()).jsonObject
            val card = JsonCodec.decodeFromJsonElement(ExtensionPersonaCard.serializer(), json.getValue("card"))
            val want = json["view"]?.takeUnless { it is JsonNull }?.let { JsonCodec.decodeFromJsonElement(ExtensionView.serializer(), it) }
            assertEquals(case.name, want, PersonaViewBlocks.fromCard(card))
        }
    }

    @Test fun numbersAsTheReferenceWritesThem() {
        assertEquals("2,250", PersonaViewBlocks.number(2250.0))
        assertEquals("7.5", PersonaViewBlocks.number(7.5))
        assertEquals("7.3", PersonaViewBlocks.number(7.25))
        assertEquals("-1,200.3", PersonaViewBlocks.number(-1200.25))
        assertEquals("0", PersonaViewBlocks.number(-0.04))
        assertEquals("1,000", PersonaViewBlocks.number(999.96))
        assertEquals("1,234,567", PersonaViewBlocks.number(1234567.0))
    }
}
