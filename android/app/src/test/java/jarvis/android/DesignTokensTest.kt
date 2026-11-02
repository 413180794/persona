package jarvis.android

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Pages take their numbers from the design system (docs/android.md「设计系统」, iOS TokenRatchetTests): per file under
 * ui/ (the design system itself and Theme.kt aside), what is written by hand — font sizes and Material's type scale,
 * `Color(0x…)`, dp numbers, Material icons by name — may only go down. A new file starts at zero. After lowering a
 * count, delete src/test/resources/design-baseline.json and run once with -PrecordDesignBaseline to write the new one.
 */
class DesignTokensTest {
    private val ui = File("src/main/java/jarvis/android/ui")
    private val baseline = File("src/test/resources/design-baseline.json")

    private val kinds = linkedMapOf(
        "font" to Regex("""fontSize\s*=|MaterialTheme\.typography|\b\d+(\.\d+)?\.sp\b"""),
        "color" to Regex("""Color\(0x"""),
        "dp" to Regex("""\b\d+(\.\d+)?\.dp\b"""),
        "icon" to Regex("""\bIcons\.(Default|Filled|Outlined|Rounded|Sharp|TwoTone|AutoMirrored)\."""),
    )

    private fun count(): Map<String, Map<String, Int>> =
        ui.walkTopDown().filter { it.isFile && it.extension == "kt" }
            .filterNot { it.path.contains("/ui/design/") || it.name == "Theme.kt" }
            .associate { f ->
                // Comments and imports do not count.
                val code = f.readLines().filterNot { it.trimStart().startsWith("import ") || it.trimStart().startsWith("//") || it.trimStart().startsWith("*") }
                    .joinToString("\n") { it.substringBefore(" // ") }
                f.relativeTo(ui).path to kinds.mapValues { (_, re) -> re.findAll(code).count() }.filterValues { it > 0 }
            }
            .filterValues { it.isNotEmpty() }
            .toSortedMap()

    @Test fun pagesOnlyGoDown() {
        val now = count()
        if (System.getProperty("recordDesignBaseline") == "true" || !baseline.exists()) {
            val json = buildJsonObject { now.forEach { (f, m) -> put(f, buildJsonObject { m.forEach { (k, v) -> put(k, v) } }) } }
            baseline.writeText(Json { prettyPrint = true }.encodeToString(JsonObject.serializer(), json) + "\n")
            return
        }
        val base = Json.parseToJsonElement(baseline.readText()).jsonObject
        val worse = mutableListOf<String>()
        val better = mutableListOf<String>()
        for ((file, counts) in now) {
            val was = base[file]?.jsonObject
            for ((kind, n) in counts) {
                val b = was?.get(kind)?.jsonPrimitive?.int ?: 0
                if (n > b) worse += "$file: $kind $b → $n"
                if (n < b) better += "$file: $kind $b → $n"
            }
        }
        for ((file, was) in base) for ((kind, b) in was.jsonObject) {
            val n = now[file]?.get(kind) ?: 0
            if (n < b.jsonPrimitive.int && "$file: $kind" !in better.map { it.substringBefore(" ") + " " + it.substringAfter(": ").substringBefore(" ") }) {
                better += "$file: $kind ${b.jsonPrimitive.int} → $n"
            }
        }
        assertTrue(
            "Hand-written sizes / colors / icons went up — take them from ui/design (JTheme.type, JSpace, JSize, JRadius, JIcons):\n" + worse.joinToString("\n"),
            worse.isEmpty(),
        )
        if (better.isNotEmpty()) println("design-baseline.json can go down (delete it and run with -PrecordDesignBaseline):\n" + better.joinToString("\n"))
    }
}
