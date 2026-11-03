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
        completed[call.callId]?.let { return@withLock it }
        val result = try {
            val tool = PhoneToolCatalog.all.firstOrNull { it.spec.name == call.name }
                ?: return@withLock PhoneToolText.unknownTool(call.name, enabledSpecs().map { it.name }).also { remember(call, it) }
            refusal(tool)?.let { return@withLock it.also { result -> remember(call, result) } }
            // Arguments that cannot even be shown are refused before anyone is asked.
            val approved = tool.confirmation?.let { summary -> confirm(summary(call.args)) } ?: true
            if (!approved) PhoneToolText.declined()
            else {
                refusal(tool)?.let { return@withLock it.also { result -> remember(call, result) } }
                withTimeoutOrNull(EXECUTION_TIMEOUT_MILLIS) { execute(call.name, call.args) } ?: PhoneToolText.timedOut()
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: PhoneToolFailure) { PhoneToolText.failure(failure) }
        catch (invalid: IllegalArgumentException) { PhoneToolText.invalid(invalid.message ?: "invalid arguments") }
        catch (_: SecurityException) {
            PhoneToolText.denied(PhoneToolCatalog.all.firstOrNull { it.spec.name == call.name }?.source)
        }
        catch (unavailable: IllegalStateException) { PhoneToolText.unavailable(unavailable.message) }
        catch (error: Exception) { PhoneToolText.failed(error) }
        remember(call, result)
        result
    }

    /** Why this tool cannot run now, in words the model can act on; null when it can. */
    private fun refusal(tool: ToolDefinition): PhoneToolResult? = when {
        tool.source !in enabled() -> PhoneToolText.turnedOff(tool.source)
        !permitted(tool.spec.name, tool.source) -> PhoneToolText.denied(tool.source)
        tool.foregroundOnly && !foreground() -> PhoneToolText.needsForeground()
        !available() -> PhoneToolText.unavailable(null)
        else -> null
    }

    private fun remember(call: PhoneToolCall, result: PhoneToolResult) {
        completed[call.callId] = result
        if (completed.size > 128) completed.remove(completed.keys.first())
    }

    private companion object { const val EXECUTION_TIMEOUT_MILLIS = 30_000L }
}

/** The words a refused or failed call returns (as the iOS app says them, with Android's settings path). */
internal object PhoneToolText {
    /** The iOS app's exact sentence, so the model reads the same refusal whichever phone answered. */
    const val DECLINED = "The user declined on the phone."

    fun declined() = PhoneToolResult.text(DECLINED, true)

    fun unknownTool(name: String, available: List<String>) = PhoneToolResult.text(
        "未知工具 / Unknown tool: $name. 可用工具 / Available tools: ${available.joinToString(", ")}.", true)

    fun turnedOff(source: PhoneSource) = PhoneToolResult.text(
        "“${source.title}” 已在手机上关闭 / The ${source.key} data source is turned off on the phone. " +
            "The user can enable it in Persona › 设置 › 手机数据.", true)

    fun denied(source: PhoneSource?) = PhoneToolResult.text(source?.let {
        "没有权限：${it.title}。请在安卓手机的 Persona › 设置 › 手机数据 里授权。\n" +
            "Permission denied: ${it.key}. Grant access in Persona › Settings › Phone data on the Android phone."
    } ?: "没有权限。请在安卓手机的 Persona › 设置 › 手机数据 里授权。\nPermission denied. Grant access in Persona › Settings › Phone data on the Android phone.", true)

    fun needsForeground() = PhoneToolResult.text(
        "安卓只允许打开着的 Persona 使用这项数据 / Android only allows this while Persona is open on screen: " +
            "ask the user to open Persona, then retry.", true)

    fun interrupted() = PhoneToolResult.text(
        "手机工具已关闭或 Persona 已进入后台 / The tool was turned off or Persona left the screen before it finished.", true)

    fun unavailable(message: String?) = PhoneToolResult.text(message ?: "手机数据暂不可用 / Phone data is not available right now.", true)

    fun invalid(message: String) = PhoneToolResult.text("参数错误 / Invalid argument: $message", true)

    fun timedOut() = PhoneToolResult.text(
        "手机操作超时 / The phone did not finish within 30 seconds. Check what happened before trying again.", true)

    fun failed(error: Exception) = PhoneToolResult.text(
        "请求失败 / Request failed: ${error.message ?: error.javaClass.simpleName}", true)

    fun failure(failure: PhoneToolFailure) = when (failure.kind) {
        PhoneToolFailure.Kind.InvalidArgument -> invalid(failure.message.orEmpty())
        PhoneToolFailure.Kind.Denied -> PhoneToolResult.text(
            "没有权限：${failure.message}。请在安卓手机的 Persona › 设置 › 手机数据 里授权。\n" +
                "Permission denied: ${failure.message}. Grant access in Persona › Settings › Phone data on the Android phone.", true)
        PhoneToolFailure.Kind.Unavailable -> unavailable(failure.message)
    }
}
