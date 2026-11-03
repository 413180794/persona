package persona.platform.phonetools

import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.JsonObject

/** All execution paths, including stale model calls, pass through this gate. */
class PhoneToolRegistry(
    private val enabled: () -> Set<PhoneSource>,
    private val available: () -> Boolean,
    private val foreground: () -> Boolean,
    private val permitted: (String, PhoneSource) -> Boolean,
    private val confirm: suspend (String) -> Boolean,
    private val execute: suspend (String, JsonObject) -> PhoneToolResult,
) {
    private val lock = Mutex()
    private val completed = linkedMapOf<String, PhoneToolResult>()
    val all: List<PhoneToolSpec> get() = PhoneToolCatalog.all.map { it.spec }
    fun enabledSpecs(): List<PhoneToolSpec> = PhoneToolCatalog.all.filter(::usable).map { it.spec }
    private fun usable(tool: ToolDefinition) = available() && tool.source in enabled() &&
        (!tool.foregroundOnly || foreground()) && permitted(tool.spec.name, tool.source)

    suspend fun call(call: PhoneToolCall): PhoneToolResult = lock.withLock {
        // Never re-execute a write when the same call is delivered again in this pairing scope.
        val result = try {
            val tool = PhoneToolCatalog.all.firstOrNull { it.spec.name == call.name } ?: error("手机不支持这个工具。")
            check(usable(tool)) { "工具未开启、未授权或当前不可用，请打开 Persona 的「手机数据」检查。" }
            completed[call.callId]?.let { return@withLock it }
            PhoneToolCatalog.validate(tool, call.args)
            val approved = !tool.confirmation || confirm(CalendarArguments.parse(call.args).summary())
            if (!approved) PhoneToolResult.text("已在手机上拒绝或确认已过期。", true)
            else {
                check(usable(tool)) { "工具权限或可用状态已改变，请重新操作。" }
                withTimeoutOrNull(30_000) { execute(call.name, call.args) }
                    ?: PhoneToolResult.text("手机操作超时，请检查实际结果后再试。", true)
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (denied: SecurityException) { PhoneToolResult.text("系统未允许访问，请在「手机数据」中授权或打开系统设置。", true) }
        catch (invalid: IllegalArgumentException) { PhoneToolResult.text(invalid.message ?: "工具参数不正确。", true) }
        catch (unavailable: IllegalStateException) { PhoneToolResult.text(unavailable.message ?: "手机数据暂不可用。", true) }
        catch (_: Exception) { PhoneToolResult.text("手机操作失败，请检查权限和系统服务后重试。", true) }
        completed[call.callId] = result
        if (completed.size > 128) completed.remove(completed.keys.first())
        result
    }
}
