package persona.feature.chat

import android.content.res.Resources
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons

/**
 * Persona's own MCP tools (server `persona`: `mcp__persona__agent_read`…) on a step: which one is ask_user, and their
 * icons. Their words are `core:domain` `namedToolVerb` (#3100). Display only: the wire keeps `persona`.
 */
internal object PersonaTools {
    /** The server of an MCP tool's full name ("mcp__persona__agent_read" → "persona"), null for any other tool. */
    fun server(name: String): String? {
        if (!name.startsWith("mcp__")) return null
        val rest = name.removePrefix("mcp__")
        val i = rest.lastIndexOf("__")
        return if (i <= 0) null else rest.substring(0, i)
    }

    /** The tool's own name: what follows the last `__` of an MCP tool, the name itself otherwise. */
    fun base(name: String): String = if (name.startsWith("mcp__")) name.substringAfterLast("__") else name

    fun isPersona(name: String): Boolean = server(name) == "persona"

    /** ask_user: the assistant's own question tool (docs/assistant/assistant.md), which takes AskUserQuestion's questions. */
    fun isAskUser(name: String): Boolean = isPersona(name) && base(name) == "ask_user"

    /** The step's icon for a Persona tool (or a conversation / computer / phone / mail one); null for every other tool. */
    fun icon(name: String): PhIcon? {
        if (!name.startsWith("mcp__")) return null
        val n = base(name)
        return when {
            isAskUser(name) -> PhIcons.Question
            n.startsWith("conversation") -> PhIcons.ChatsCircle
            n.startsWith("computer_") -> PhIcons.Desktop
            n.startsWith("browser_") -> PhIcons.GlobeSimple
            n.startsWith("phone_") -> PhIcons.DeviceMobile
            n.startsWith("mail_") -> PhIcons.EnvelopeSimple
            n.startsWith("agent") || n == "list_agents" || n == "send_to_agent" -> PhIcons.Robot
            n.startsWith("kb_") || n == "search_knowledge" -> PhIcons.BookOpen
            n.startsWith("view_") -> PhIcons.SquaresFour
            n.contains("followup") || n.contains("routine") || n == "notify_user" -> PhIcons.BellSimple
            isPersona(name) -> PhIcons.Sparkle
            else -> null
        }
    }
}

/** What an ask_user call asked: each question with its options' labels and whether several could be picked. */
internal data class AskUserQuestion(val question: String, val options: List<String>, val multiSelect: Boolean)

/** The assistant's ask_user, opened (iOS `AskUserAnswers`): what was asked and, from its result, what came back. */
internal object AskUserAnswers {
    fun questions(input: JsonElement?): List<AskUserQuestion> =
        ((input as? JsonObject)?.get("questions") as? JsonArray).orEmpty().mapNotNull { value ->
            val q = value as? JsonObject ?: return@mapNotNull null
            AskUserQuestion(q.text("question").orEmpty(),
                (q["options"] as? JsonArray).orEmpty().mapNotNull { (it as? JsonObject)?.text("label") },
                ((q["multiSelect"] ?: q["multi_select"]) as? JsonPrimitive)?.booleanOrNull == true)
        }

    /** Each question's answer; empty when the result says nobody answered (or is not the hub's answer at all). */
    fun parse(result: String, questions: List<AskUserQuestion>): Map<String, String> {
        if (!result.startsWith("The user answered:")) return emptyMap() // i18n-ignore: the hub's tool result, matched as written
        val marks = questions.mapNotNull { q ->
            val mark = "\n- ${q.question} → "
            result.indexOf(mark).takeIf { it >= 0 }?.let { Triple(q.question, it, it + mark.length) }
        }.sortedBy { it.second }
        return buildMap {
            marks.forEachIndexed { index, (question, _, start) ->
                val end = marks.getOrNull(index + 1)?.second ?: result.length
                result.substring(start, end).trim().takeIf { it.isNotEmpty() }?.let { put(question, it) }
            }
        }
    }

    /** Whether [label] is among what was picked. */
    fun picked(label: String, answer: String?, multiSelect: Boolean): Boolean {
        if (answer == null) return false
        if (answer == label) return true
        return multiSelect && label in answer.split(", ")
    }

    /** An answer typed instead of picking an option. */
    fun typed(q: AskUserQuestion, answer: String?): String? =
        answer?.takeIf { a -> q.options.none { picked(it, a, q.multiSelect) } }
}

/**
 * What a tool that operates a computer does, in words (iOS `ComputerSteps`, #1323): "Click AC → 6 → × → 7 → =" rather
 * than its JSON.
 */
internal object ComputerSteps {
    /** A computer_run's subject when it says what it does (steps or a script): "MacBook · Click AC → 6 → ×"; null otherwise. */
    fun subject(res: Resources, name: String, input: JsonObject?): String? {
        if (PersonaTools.base(name) != "computer_run" || input == null) return null
        val what = describe(res, input) ?: return null
        val machine = (input["machine"] as? JsonPrimitive)?.contentOrNull?.takeIf(String::isNotBlank)
        return listOfNotNull(machine, what).joinToString(" · ")
    }

    /** The steps (or the script) in words; null when the call has neither. */
    fun describe(res: Resources, input: JsonObject): String? {
        val steps = input["steps"] as? JsonArray
        val script = (input["script"] as? JsonPrimitive)?.contentOrNull
        return when {
            !steps.isNullOrEmpty() -> join(res, steps.mapNotNull { step(res, it) })
            script != null -> firstLine(script.trim()).let { if (it.isEmpty()) res.getString(R.string.chat_computer_script) else res.getString(R.string.chat_computer_script_named, it) }
            else -> null
        }
    }

    /** Steps in a line, clicks after a click joined by → alone (Click AC → 6 → ×). */
    private fun join(res: Resources, words: List<Pair<String, String?>>): String {
        var clicking = false
        return words.joinToString(" → ") { (word, clicked) ->
            (if (clicked != null && clicking) clicked else word).also { clicking = clicked != null }
        }
    }

    /** One run_steps step: {"click": {…}} or {"tool": "click", "args": {…}}; with what a click hit, for [join]. */
    private fun step(res: Resources, value: JsonElement): Pair<String, String?>? {
        val o = value as? JsonObject ?: return null
        (o["tool"] as? JsonPrimitive)?.contentOrNull?.let { return action(res, it, o["args"] as? JsonObject ?: JsonObject(emptyMap())) }
        o.entries.firstOrNull { it.key !in setOf("wait_for", "expect", "args", "tool") }?.let { (key, args) ->
            return action(res, key, args as? JsonObject ?: JsonObject(emptyMap()))
        }
        return if (o["expect"] != null || o["wait_for"] != null) res.getString(R.string.chat_computer_check) to null else null
    }

    private fun action(res: Resources, tool: String, args: JsonObject): Pair<String, String?>? {
        val target = args.text("name") ?: args.text("label") ?: args.text("text")
        val shown = target?.takeIf(String::isNotEmpty)?.let { symbols[it] ?: short(it) }
        val spot = shown ?: res.getString(R.string.chat_computer_somewhere)
        return when (tool) {
            "click" -> res.getString(R.string.chat_computer_click, spot) to spot
            "double_click" -> res.getString(R.string.chat_computer_double_click, spot) to null
            "right_click" -> res.getString(R.string.chat_computer_right_click, spot) to null
            "type_text" -> res.getString(R.string.chat_computer_type, short(args.text("text").orEmpty())) to null
            "set_value" -> res.getString(R.string.chat_computer_set_value, short(args.text("value").orEmpty())) to null
            "press_key" -> res.getString(R.string.chat_computer_press, key(res, args.text("key").orEmpty())) to null
            "hotkey" -> res.getString(R.string.chat_computer_press, (args["keys"] as? JsonArray).orEmpty()
                .mapNotNull { (it as? JsonPrimitive)?.contentOrNull }.joinToString("") { key(res, it) }) to null
            "scroll" -> res.getString(R.string.chat_computer_scroll) to null
            "drag" -> res.getString(R.string.chat_computer_drag) to null
            "launch_app" -> res.getString(R.string.chat_computer_launch, args.text("name") ?: args.text("bundle_id") ?: "App") to null
            "bring_to_front" -> (args.text("app") ?: args.text("name"))?.let { res.getString(R.string.chat_computer_front, it) to null }
            "get_window_state" -> res.getString(R.string.chat_computer_window) to null
            "get_desktop_state", "screenshot" -> res.getString(R.string.chat_computer_screenshot) to null
            "get_accessibility_tree", "list_windows", "list_apps" -> res.getString(R.string.chat_computer_tree) to null
            "zoom" -> res.getString(R.string.chat_computer_zoom) to null
            else -> null
        }
    }

    /** Calculator's buttons read as on its keys. */
    private val symbols = mapOf(
        "All Clear" to "AC", "Clear" to "C", "Multiply" to "×", "Divide" to "÷", "Add" to "+", "Plus" to "+", "Subtract" to "−",
        "Minus" to "−", "Equals" to "=", "Percent" to "%", "Decimal" to ".", "Change Sign" to "±", "Negate" to "±",
    )

    private fun key(res: Resources, k: String): String = when (k.lowercase()) {
        "cmd", "command" -> "⌘"
        "shift" -> "⇧"
        "option", "alt" -> "⌥"
        "ctrl", "control" -> "⌃"
        "return", "enter" -> "⏎"
        "escape", "esc" -> "esc"
        "tab" -> "⇥"
        "space" -> res.getString(R.string.chat_computer_space)
        "delete", "backspace" -> "⌫"
        else -> if (k.length == 1) k.uppercase() else k
    }

    private fun short(s: String): String = firstLine(s).let { if (it.length > 24) it.take(23) + "…" else it }
}

private fun firstLine(s: String): String = s.lineSequence().firstOrNull { it.isNotEmpty() } ?: s

private fun JsonObject.text(key: String): String? = (get(key) as? JsonPrimitive)?.contentOrNull
