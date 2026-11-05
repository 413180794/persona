package persona.core.demo

import android.content.Context
import persona.core.contracts.JsonCodec
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.*
import java.time.Instant

/** The asset contains only fictional data exported from the mock hub. */
internal object DemoData {
    fun load(text: String, now: Instant): JsonObject {
        fun resolve(value: JsonElement): JsonElement = when (value) {
            is JsonObject -> JsonObject(value.mapValues { resolve(it.value) })
            is JsonArray -> JsonArray(value.map(::resolve))
            is JsonPrimitive -> when {
                value.isString && value.content.startsWith("@rel:") ->
                    JsonPrimitive(now.plusSeconds(value.content.removePrefix("@rel:").toLong()).toString())
                value.isString && value.content.startsWith("@unix:") ->
                    JsonPrimitive(now.epochSecond + value.content.removePrefix("@unix:").toLong())
                else -> value
            }
        }
        return resolve(JsonCodec.parseToJsonElement(text)).jsonObject
    }
}

/**
 * Read and decode away from the UI thread; each entry creates an independent demo, in the language [context]'s text
 * is in (Demo.json is the Chinese data, Demo.en.json and Demo.ja.json the same data translated).
 */
suspend fun DemoHub.Companion.fromAssets(
    context: Context,
    parentScope: kotlinx.coroutines.CoroutineScope,
    now: () -> Instant = Instant::now,
): DemoHub = withContext(Dispatchers.IO) {
    val asset = when (context.resources.configuration.locales[0].language) {
        "zh" -> "demo/Demo.json"
        "ja" -> "demo/Demo.ja.json"
        else -> "demo/Demo.en.json"
    }
    DemoHub(context.assets.open(asset).bufferedReader().use { it.readText() }, parentScope, now,
        words = DemoText.from(context.resources))
}
