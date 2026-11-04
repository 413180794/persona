package persona.platform.phonetools

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant
import java.time.ZoneId

/** phone_health_summary (Health.kt): the arguments it takes and the sleep arithmetic, as iOS `HealthTool`. */
class HealthTest {
    private val shanghai = ZoneId.of("Asia/Shanghai")
    private fun at(s: String) = Instant.parse(s)
    private fun args(vararg metrics: Any) = buildJsonObject {
        put("metrics", buildJsonArray { metrics.forEach { add(if (it is String) JsonPrimitive(it) else JsonPrimitive(it as Number)) } })
    }

    @Test fun metricsDefaultToStepsAndKeepTheAskedOrderOnce() {
        assertEquals(listOf("steps"), Health.metrics(JsonObject(emptyMap())))
        assertEquals(listOf("steps"), Health.metrics(args()))
        assertEquals(listOf("sleep", "steps"), Health.metrics(args("sleep", "steps", "sleep")))
        // Non-strings are skipped, as iOS does.
        assertEquals(listOf("weight"), Health.metrics(args(3, "weight")))
    }

    @Test fun unknownMetricsAreRefusedWithTheNames() {
        val e = runCatching { Health.metrics(args("steps", "blood_sugar")) }.exceptionOrNull()
        assertTrue(e is PhoneToolFailure)
        assertTrue(e!!.message!!.contains("blood_sugar"))
        assertTrue(e.message!!.contains("resting_heart_rate"))
    }

    @Test fun schemaIsIosOwn() {
        val t = PhoneToolCatalog.all.firstOrNull { it.spec.name == "phone_health_summary" }!!
        assertEquals(PhoneSource.Health, t.source)
        assertEquals(null, t.confirmation) // read only: never asks
        val props = t.spec.inputSchema["properties"]!!.jsonObject
        val metrics = props["metrics"]!!.jsonObject
        assertEquals("array", metrics["type"]!!.jsonPrimitive.content)
        val names = metrics["items"]!!.jsonObject["enum"]!!.jsonArray.map { it.jsonPrimitive.content }.toSet()
        assertEquals(setOf("steps", "distance", "active_energy", "heart_rate", "resting_heart_rate", "sleep", "weight"), names)
        val days = props["days"]!!.jsonObject
        assertEquals(1, days["minimum"]!!.jsonPrimitive.int)
        assertEquals(90, days["maximum"]!!.jsonPrimitive.int)
        assertTrue(t.spec.inputSchema["required"] == null || t.spec.inputSchema["required"] !is JsonArray || (t.spec.inputSchema["required"] as JsonArray).isEmpty())
    }

    @Test fun theWindowStartsAtLocalMidnight() {
        val now = at("2026-11-02T03:30:00Z") // 11:30 in Shanghai
        assertEquals(at("2026-11-01T16:00:00Z") to now, Health.window(1, now, shanghai))
        assertEquals(at("2026-10-26T16:00:00Z"), Health.window(7, now, shanghai).first)
    }

    @Test fun aNightAcrossMidnightCountsOnBothDays() {
        // 23:00 → 07:00 local.
        val night = at("2026-10-31T15:00:00Z") to at("2026-10-31T23:00:00Z")
        val got = Health.sleepByDay(listOf(night), at("2026-10-30T16:00:00Z"), at("2026-11-02T03:30:00Z"), shanghai)
        assertEquals(mapOf("2026-10-31" to 1.0, "2026-11-01" to 7.0), got)
    }

    @Test fun sleepIsClippedToTheWindowAndSummed() {
        val start = at("2026-10-31T16:00:00Z") // 11-01 00:00 local
        val spans = listOf(
            at("2026-10-31T14:00:00Z") to at("2026-10-31T18:00:00Z"), // 22:00 → 02:00: only 2 h inside
            at("2026-10-31T19:00:00Z") to at("2026-10-31T22:20:00Z"), // 03:00 → 06:20
            at("2026-11-01T06:00:00Z") to at("2026-11-01T06:00:00Z"), // empty
        )
        assertEquals(mapOf("2026-11-01" to 5.3), Health.sleepByDay(spans, start, at("2026-11-02T03:30:00Z"), shanghai))
    }

    @Test fun roundsToOneDecimal() {
        assertEquals(8.3, Health.round1(8.25), 0.0)
        assertEquals(1234.6, Health.round1(1234.56), 0.0)
    }
}
