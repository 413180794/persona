package persona.core.domain.answer

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import kotlinx.serialization.json.*
import java.math.BigDecimal
import java.net.URI
import java.time.Instant
import java.time.LocalDate

/** A deliberately bounded JSON Schema subset. Unknown validation rules block acceptance. */
object ElicitationForm {
    data class Field(val key: String, val schema: JsonObject, val required: Boolean) {
        val type get() = schema.string("type")
        val title get() = schema.string("title") ?: key
        val description get() = schema.string("description")
        val secret get() = schema["writeOnly"] == JsonPrimitive(true) || schema.string("format") == "password"
        val choices: List<JsonPrimitive> get() = ((if (type == "array") schema["items"] as? JsonObject else schema)
            ?.get("enum") as? JsonArray).orEmpty().filterIsInstance<JsonPrimitive>()
        val choiceLabels: List<String> get() = ((if (type == "array") schema["items"] as? JsonObject else schema)
            ?.get("enumNames") as? JsonArray).orEmpty().mapNotNull { (it as? JsonPrimitive)?.contentOrNull }
    }
    data class Validation(val content: JsonObject, val errors: Map<String, CoreText>, val supported: Boolean) {
        val valid get() = supported && errors.isEmpty()
    }
    fun fields(schema: JsonObject?): List<Field> {
        val required = (schema?.get("required") as? JsonArray).orEmpty().mapNotNull { (it as? JsonPrimitive)?.contentOrNull }
        return (schema?.get("properties") as? JsonObject).orEmpty().mapNotNull { (key, value) ->
            (value as? JsonObject)?.let { Field(key, it, key in required) }
        }
    }
    fun defaults(schema: JsonObject?) = JsonObject(fields(schema).filterNot { it.secret }
        .mapNotNull { field -> field.schema["default"]?.let { field.key to it } }.toMap())

    fun validate(schema: JsonObject?, values: JsonObject): Validation {
        val fields = fields(schema)
        val supported = schema != null && schema.string("type") == "object" && schema["properties"] is JsonObject &&
            schema.keys.all { it in setOf("type", "properties", "required", "title", "description", "additionalProperties", "\$schema") } &&
            (schema["required"] == null || schema["required"] is JsonArray) &&
            (schema["properties"] as JsonObject).size == fields.size &&
            (schema["required"] as? JsonArray).orEmpty().all { item -> fields.any { JsonPrimitive(it.key) == item } } &&
            fields.all { supported(it.schema) }
        val errors = linkedMapOf<String, CoreText>()
        val result = linkedMapOf<String, JsonElement>()
        fields.forEach { field ->
            val value = values[field.key]
            if (value == null) {
                if (field.required) errors[field.key] = CoreText.of(Key.FieldRequired)
            } else {
                val converted = convert(field, value)
                if (converted == null) errors[field.key] = CoreText.of(Key.FieldInvalid)
                else result[field.key] = converted
            }
        }
        return Validation(JsonObject(result), errors, supported)
    }

    private fun supported(s: JsonObject): Boolean {
        if (s.keys.any { it !in setOf("type", "title", "description", "default", "enum", "enumNames", "minLength", "maxLength",
                "format", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "items", "minItems", "maxItems", "uniqueItems", "writeOnly") }) return false
        if (listOf("minLength", "maxLength", "minItems", "maxItems").any { key ->
                s[key]?.let { it !is JsonPrimitive || it.isString || (it.intOrNull ?: -1) < 0 } == true
            }) return false
        if (listOf("minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf").any { key ->
                s[key]?.let { it !is JsonPrimitive || it.isString || it.content.toBigDecimalOrNull() == null } == true
            }) return false
        if (listOf("writeOnly", "uniqueItems").any { key ->
                s[key]?.let { it !is JsonPrimitive || it.isString || it.booleanOrNull == null } == true
            }) return false
        if (s.string("format") !in setOf(null, "email", "uri", "date", "date-time", "password")) return false
        if (s["enum"] != null && ((s["enum"] as? JsonArray)?.isNotEmpty() != true ||
                (s["enum"] as JsonArray).any { it !is JsonPrimitive || it == JsonNull })) return false
        return when (s.string("type")) {
            "string", "number", "integer", "boolean" -> true
            "array" -> (s["items"] as? JsonObject)?.let {
                it.string("type") == "string" && it["enum"] is JsonArray && supported(it)
            } == true
            else -> false
        }
    }
    private fun convert(f: Field, value: JsonElement): JsonElement? = runCatching {
        val s = f.schema
        val p = value as? JsonPrimitive
        val converted: JsonElement = when (f.type) {
            "string" -> {
                require(p != null && p.isString)
                val text = p.content
                val length = text.codePointCount(0, text.length)
                require(s.int("minLength")?.let { length >= it } != false && s.int("maxLength")?.let { length <= it } != false)
                require(when (s.string("format")) {
                    "email" -> text.matches(Regex("[^\\s@]+@[^\\s@]+\\.[^\\s@]+"))
                    "uri" -> URI(text).isAbsolute
                    "date" -> runCatching { LocalDate.parse(text) }.isSuccess
                    "date-time" -> runCatching { Instant.parse(text) }.isSuccess
                    else -> true
                })
                p
            }
            "number", "integer" -> {
                require(p != null && p != JsonNull)
                val number = p.content.toBigDecimal()
                require(f.type != "integer" || number.stripTrailingZeros().scale() <= 0)
                require(s.decimal("minimum")?.let { number >= it } != false && s.decimal("maximum")?.let { number <= it } != false)
                require(s.decimal("exclusiveMinimum")?.let { number > it } != false && s.decimal("exclusiveMaximum")?.let { number < it } != false)
                require(s.decimal("multipleOf")?.let { it > BigDecimal.ZERO && number.remainder(it) == BigDecimal.ZERO } != false)
                JsonPrimitive(number)
            }
            "boolean" -> { require(p != null && !p.isString && p.booleanOrNull != null); p }
            "array" -> {
                require(value is JsonArray && value.all { it in f.choices })
                require(value.distinct().size == value.size)
                require(s.int("minItems")?.let { value.size >= it } != false && s.int("maxItems")?.let { value.size <= it } != false)
                value
            }
            else -> error("Unsupported field")
        }
        require(s["enum"]?.let { converted in (it as JsonArray) } != false)
        converted
    }.getOrNull()
}

private fun JsonObject.string(key: String) = (get(key) as? JsonPrimitive)?.contentOrNull
private fun JsonObject.int(key: String) = (get(key) as? JsonPrimitive)?.intOrNull
private fun JsonObject.decimal(key: String) = (get(key) as? JsonPrimitive)?.contentOrNull?.toBigDecimalOrNull()
