package jarvis.core.demo

import android.content.res.AssetManager
import jarvis.core.contracts.JsonCodec
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

/** Read and decode away from the UI thread; each entry creates an independent demo. */
suspend fun DemoHub.Companion.fromAssets(
    assets: AssetManager,
    parentScope: kotlinx.coroutines.CoroutineScope,
    now: () -> Instant = Instant::now,
): DemoHub = withContext(Dispatchers.IO) {
    DemoHub(assets.open("demo/Demo.json").bufferedReader().use { it.readText() }, parentScope, now)
}
