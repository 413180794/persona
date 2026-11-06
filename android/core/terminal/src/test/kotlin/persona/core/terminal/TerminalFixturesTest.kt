package persona.core.terminal

import java.io.File
import java.util.Base64
import java.util.Locale
import kotlinx.serialization.json.*
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.Parameterized

/** Every shared fixture is a separate JUnit case; compare the complete expected JSON. */
@RunWith(Parameterized::class)
class TerminalFixturesTest(private val label: String, private val kind: String, private val case: JsonObject) {
    @Test fun matchesSharedFixture() {
        val input = case.getValue("input").jsonObject
        val actual: JsonObject = when (kind) {
            "grid" -> {
                val grid = TerminalGrid(input.int("cols"), input.int("rows"))
                obj("steps" to input.array("frames").map { element ->
                    val frame = element.jsonObject
                    frame["reset"]?.jsonObject?.let { grid.reset(it.int("cols"), it.int("rows")) }
                    val old = grid.lines
                    if ("vt" in frame) grid.feed(frame.string("vt"))
                    else grid.feed(Base64.getDecoder().decode(frame.string("base64")))
                    val lines = grid.lines
                    JsonObject(snapshot(lines, grid.cols) + obj(
                        "cols" to grid.cols, "rows" to grid.rows,
                        "cursor" to obj("row" to grid.cursorRow, "col" to grid.cursorCol, "visible" to grid.cursorVisible),
                        "scrolled" to TerminalGrid.scrolled(old, lines),
                    ))
                })
            }
            "ansi_rows" -> snapshot(TerminalGrid.rows(input.string("text")))
            "width" -> obj("width" to TerminalGrid.width(input.string("character")))
            "wrap" -> {
                val cursor = input["cursor"]?.takeUnless { it == JsonNull }?.jsonObject?.let {
                    TerminalCursor(it.int("row"), it.int("col"))
                }
                obj("lines" to TerminalWrap.lines(
                    TerminalGrid.rows(input.string("ansi")), cursor, input.int("per_line"), input.bool("wrap"),
                ).map { obj("row" to it.row, "from" to it.from, "to" to it.to, "indent" to it.indent) })
            }
            "key" -> {
                val name = modifiers(input).apply(input.string("key"))
                JsonObject(bytes(TerminalKeyBytes.encode(name), TerminalKeyBytes.encodeBytes(name)) + obj("name" to name))
            }
            "encode" -> bytes(TerminalKeyBytes.encode(input.string("name")), TerminalKeyBytes.encodeBytes(input.string("name")))
            "paste" -> bytes(TerminalKeyBytes.text(input.string("text")), TerminalKeyBytes.textBytes(input.string("text")))
            "typed" -> when (val event = modifiers(input).typed(input.string("character"))) {
                is TerminalInputEvent.Text -> obj("key" to null, "text" to event.text)
                is TerminalInputEvent.Key -> obj("key" to event.key, "text" to null)
            }
            "modifiers" -> {
                val mods = TerminalModifiers()
                obj("steps" to input.array("actions").map { element ->
                    val action = element.jsonObject
                    val key = action["modifier"]?.jsonPrimitive?.content?.let(::modifierKey)
                    when (action.string("op")) {
                        "tap" -> mods.tap(requireNotNull(key), action.bool("quick"))
                        "hold" -> mods.hold(requireNotNull(key))
                        "release" -> mods.release(requireNotNull(key))
                        "pressed" -> mods.pressed()
                        else -> error("Unknown modifier action")
                    }
                    obj("ctrl" to mods[TerminalModifiers.Key.CTRL].name.lowercase(Locale.ROOT),
                        "alt" to mods[TerminalModifiers.Key.ALT].name.lowercase(Locale.ROOT),
                        "shift" to mods[TerminalModifiers.Key.SHIFT].name.lowercase(Locale.ROOT),
                        "active" to mods.active, "applied" to mods.apply(input.string("probe_key")))
                })
            }
            "batch" -> {
                val batcher = KeyBatcher()
                input.array("events").forEach { element ->
                    val event = element.jsonObject
                    batcher.push(if ("text" in event) TerminalInputEvent.Text(event.string("text")) else TerminalInputEvent.Key(event.string("key")))
                }
                val requests = generateSequence { batcher.take() }.map { obj("keys" to it.keys, "text" to it.text) }.toList()
                obj("requests" to requests, "empty" to batcher.isEmpty)
            }
            else -> error("Unknown fixture kind: $kind")
        }
        assertEquals(label, case.getValue("expected"), actual)
    }

    private fun modifiers(input: JsonObject): TerminalModifiers = TerminalModifiers().also { mods ->
        input.array("modifiers").forEach { mods[modifierKey(it.jsonPrimitive.content)] = ModifierState.ONCE }
    }
    private fun modifierKey(name: String) = TerminalModifiers.Key.valueOf(name.uppercase(Locale.ROOT))
    private fun bytes(text: String?, utf8: ByteArray?) = obj("bytes" to text, "utf8" to utf8?.map { it.toInt() and 255 })

    private fun snapshot(rows: List<TermRow>, cols: Int? = null): JsonObject {
        val wide = mutableListOf<JsonElement>()
        val styles = mutableListOf<JsonElement>()
        rows.forEachIndexed { r, row ->
            row.forEachIndexed { c, cell -> if (cell.width != 1) wide.add(json(listOf(r, c, cell.width))) }
            var c = 0
            while (c < row.size) {
                val from = c
                val style = row[c].style
                while (c < row.size && row[c].style == style) c++
                if (style != TermStyle()) styles.add(obj("row" to r, "from" to from, "to" to c, "style" to styleJson(style)))
            }
        }
        return obj("text" to rows.map { row ->
            row.joinToString("") { if (it.width == 0) "\ufff0" else it.text } + " ".repeat((cols ?: row.size) - row.size)
        }, "row_lengths" to rows.map { it.size }, "wide_cells" to wide, "style_runs" to styles)
    }

    private fun styleJson(style: TermStyle): JsonObject = buildJsonObject {
        fun color(color: TermColor): JsonObject = when (color) {
            is TermColor.Indexed -> obj("indexed" to color.index)
            is TermColor.Rgb -> obj("rgb" to listOf(color.red, color.green, color.blue))
            TermColor.Standard -> error("Default colors are omitted")
        }
        if (style.fg != TermColor.Standard) put("fg", color(style.fg))
        if (style.bg != TermColor.Standard) put("bg", color(style.bg))
        listOf("bold" to style.bold, "dim" to style.dim, "italic" to style.italic, "underline" to style.underline,
            "inverse" to style.inverse, "strike" to style.strike, "hidden" to style.hidden).forEach { (key, value) ->
            if (value) put(key, true)
        }
        style.link?.let { put("link", it) }
    }

    companion object {
        @JvmStatic @Parameterized.Parameters(name = "{0}")
        fun fixtures(): List<Array<Any>> {
            val root = File(requireNotNull(System.getProperty("persona.repo.root")), "spec/fixtures/terminal")
            val files = requireNotNull(root.listFiles { file -> file.extension == "json" }).sortedBy { it.name }
            require(files.isNotEmpty()) { "Terminal fixtures are missing" }
            return files.flatMap { file ->
                val fixture = Json.parseToJsonElement(file.readText()).jsonObject
                require(fixture.int("schema_version") == 1)
                fixture.array("cases").map { element ->
                    val case = element.jsonObject
                    arrayOf("${file.name}/${case.string("name")}", fixture.string("kind"), case)
                }
            }
        }
        private fun JsonObject.string(key: String) = getValue(key).jsonPrimitive.content
        private fun JsonObject.int(key: String) = getValue(key).jsonPrimitive.int
        private fun JsonObject.bool(key: String) = getValue(key).jsonPrimitive.boolean
        private fun JsonObject.array(key: String) = getValue(key).jsonArray
        private fun obj(vararg values: Pair<String, Any?>) = JsonObject(values.associate { it.first to json(it.second) })
        private fun json(value: Any?): JsonElement = when (value) {
            null -> JsonNull
            is JsonElement -> value
            is String -> JsonPrimitive(value)
            is Boolean -> JsonPrimitive(value)
            is Number -> JsonPrimitive(value)
            is List<*> -> JsonArray(value.map(::json))
            else -> error("Unsupported JSON value: $value")
        }
    }
}
