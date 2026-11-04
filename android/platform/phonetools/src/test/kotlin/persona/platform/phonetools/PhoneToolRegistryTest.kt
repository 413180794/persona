package persona.platform.phonetools

import persona.core.contracts.api.*
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test

class PhoneToolRegistryTest {
    private fun text(result: PhoneToolResult) = result.content.single().jsonObject.getValue("text").jsonPrimitive.content

    @Test fun registrationAndStaleCallsRespectAvailabilityPermissionsAndSources() = runTest {
        var online = true
        var foreground = true
        var permission = true
        var sources = PhoneSource.entries.toSet()
        var executions = 0
        val registry = PhoneToolRegistry({ sources }, { online }, { foreground }, { _, _ -> permission },
            { error("Reads must not ask for confirmation") }, { _, _ -> executions++; PhoneToolResult.text("ok") })
        assertEquals(12, registry.enabledSpecs().size)
        assertFalse(registry.all.any { it.name.startsWith("phone_reminders_") || it.name == "phone_health_write_sample" })
        assertTrue(registry.all.any { it.name == "phone_health_summary" })
        assertTrue(registry.all.all { it.inputSchema["type"]?.jsonPrimitive?.content == "object" })
        foreground = false
        assertFalse(registry.enabledSpecs().any { it.name == "phone_clipboard_get" || it.name == "phone_location_current" })
        val background = registry.call(PhoneToolCall("background", "phone_clipboard_get", buildJsonObject {}))
        assertTrue(background.isError)
        assertTrue(text(background).contains("open on screen"))
        sources = emptySet()
        assertTrue(registry.enabledSpecs().isEmpty())
        val off = registry.call(PhoneToolCall("disabled", "phone_device_info", buildJsonObject {}))
        assertTrue(off.isError)
        assertEquals("“设备信息” 已在手机上关闭 / The device data source is turned off on the phone. " +
            "The user can enable it in Persona › 设置 › 手机数据.", text(off))
        sources = PhoneSource.entries.toSet()
        permission = false
        assertTrue(registry.enabledSpecs().isEmpty())
        assertTrue(text(registry.call(PhoneToolCall("denied", "phone_contacts_search", buildJsonObject {}))).startsWith("没有权限：通讯录"))
        permission = true
        online = false
        assertTrue(registry.enabledSpecs().isEmpty())
        online = true
        val unknown = registry.call(PhoneToolCall("unknown", "phone_teleport", buildJsonObject {}))
        assertTrue(unknown.isError)
        assertTrue(text(unknown).startsWith("未知工具 / Unknown tool: phone_teleport."))
        // Out-of-range numbers and unknown arguments are no reason to refuse (iOS clamps and ignores them).
        assertFalse(registry.call(PhoneToolCall("clamped", "phone_contacts_search",
            buildJsonObject { put("query", "test"); put("limit", 0); put("extra", true) })).isError)
        assertFalse(registry.call(PhoneToolCall("valid", "phone_device_info", buildJsonObject {})).isError)
        assertEquals(2, executions)
        // The same call delivered twice runs once.
        assertFalse(registry.call(PhoneToolCall("valid", "phone_device_info", buildJsonObject {})).isError)
        assertEquals(2, executions)
    }

    @Test fun calendarWritesRequireOneLocalDecisionAndRecheckStateAfterApproval() = runTest {
        var allow = false
        var enabled = true
        var revokeOnConfirm = false
        var confirmations = 0
        var executions = 0
        val registry = PhoneToolRegistry({ if (enabled) setOf(PhoneSource.Calendar) else emptySet() }, { true }, { true }, { _, _ -> true },
            { summary ->
                assertTrue(summary.startsWith("新建日程「测试日程」\n")); assertTrue(summary.contains(" → "))
                confirmations++
                if (revokeOnConfirm) enabled = false
                allow
            }, { _, _ -> executions++; PhoneToolResult.text("created") })
        val args = buildJsonObject {
            put("title", "测试日程"); put("start", "2026-11-01T10:00:00+08:00"); put("end", "2026-11-01T11:00:00+08:00")
        }
        val refused = registry.call(PhoneToolCall("refused", "phone_calendar_create_event", args))
        assertTrue(refused.isError)
        // Word for word what the iOS app answers.
        assertEquals("The user declined on the phone.", text(refused))
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
        // Arguments that cannot be shown are refused without asking anyone.
        val invalid = registry.call(PhoneToolCall("invalid", call.name, JsonObject(args + ("end" to JsonPrimitive("2026-11-01T09:00:00+08:00")))))
        assertTrue(invalid.isError)
        assertEquals("参数错误 / Invalid argument: end must be after start", text(invalid))
        assertEquals(3, confirmations)
        assertTrue(text(registry.call(PhoneToolCall("untitled", call.name, JsonObject(args - "title")))).contains("title is required"))
        assertEquals(3, confirmations)
    }
}
