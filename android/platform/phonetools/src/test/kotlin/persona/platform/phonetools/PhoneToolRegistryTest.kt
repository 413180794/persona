package persona.platform.phonetools

import persona.core.contracts.api.*
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test

class PhoneToolRegistryTest {
    @Test fun registrationAndStaleCallsRespectAvailabilityPermissionsAndSchemas() = runTest {
        var online = true
        var foreground = true
        var permission = true
        var sources = PhoneSource.entries.toSet()
        var executions = 0
        val registry = PhoneToolRegistry({ sources }, { online }, { foreground }, { _, _ -> permission },
            { error("Reads must not ask for confirmation") }, { _, _ -> executions++; PhoneToolResult.text("ok") })
        assertEquals(11, registry.enabledSpecs().size)
        assertFalse(registry.all.any { it.name.startsWith("phone_reminders_") || it.name.startsWith("phone_health_") })
        assertTrue(registry.all.all { it.inputSchema["type"]?.jsonPrimitive?.content == "object" })
        foreground = false
        assertFalse(registry.enabledSpecs().any { it.name == "phone_clipboard_get" || it.name == "phone_location_current" })
        assertTrue(registry.call(PhoneToolCall("background", "phone_clipboard_get", buildJsonObject {})).isError)
        sources = emptySet()
        assertTrue(registry.enabledSpecs().isEmpty())
        assertTrue(registry.call(PhoneToolCall("disabled", "phone_device_info", buildJsonObject {})).isError)
        sources = PhoneSource.entries.toSet()
        permission = false
        assertTrue(registry.enabledSpecs().isEmpty())
        permission = true
        online = false
        assertTrue(registry.enabledSpecs().isEmpty())
        online = true
        assertTrue(registry.call(PhoneToolCall("bad", "phone_contacts_search", buildJsonObject { put("query", "test"); put("limit", 0) })).isError)
        assertFalse(registry.call(PhoneToolCall("valid", "phone_device_info", buildJsonObject {})).isError)
        assertEquals(1, executions)
    }

    @Test fun calendarWritesRequireOneLocalDecisionAndRecheckStateAfterApproval() = runTest {
        var allow = false
        var enabled = true
        var revokeOnConfirm = false
        var confirmations = 0
        var executions = 0
        val registry = PhoneToolRegistry({ if (enabled) setOf(PhoneSource.Calendar) else emptySet() }, { true }, { true }, { _, _ -> true },
            { summary ->
                assertTrue(summary.contains("测试日程")); assertTrue(summary.contains("2026-11-01T10:00+08:00"))
                confirmations++
                if (revokeOnConfirm) enabled = false
                allow
            }, { _, _ -> executions++; PhoneToolResult.text("created") })
        val args = buildJsonObject {
            put("title", "测试日程"); put("start", "2026-11-01T10:00:00+08:00"); put("end", "2026-11-01T11:00:00+08:00")
        }
        assertTrue(registry.call(PhoneToolCall("refused", "phone_calendar_create_event", args)).isError)
        assertEquals(0, executions)
        allow = true
        val call = PhoneToolCall("accepted", "phone_calendar_create_event", args)
        assertFalse(registry.call(call).isError)
        assertFalse(registry.call(call).isError)
        assertEquals(1, executions)
        assertEquals(2, confirmations)
        revokeOnConfirm = true
        assertTrue(registry.call(PhoneToolCall("revoked", call.name, args)).isError)
        assertEquals(1, executions)
        enabled = true
        val invalid = JsonObject(args + ("end" to JsonPrimitive("2026-11-01T09:00:00+08:00")))
        assertTrue(registry.call(PhoneToolCall("invalid", call.name, invalid)).isError)
        assertEquals(3, confirmations)
    }
}
