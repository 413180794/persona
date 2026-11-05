package persona.platform.capsule

import kotlinx.coroutines.flow.StateFlow

/** Content providers own routing and business actions; the host owns only the Android window. */
interface CapsuleContentSource {
    val content: StateFlow<CapsuleContent>
    fun shown(expanded: Boolean) {}
    fun select(id: String?)
    suspend fun send(id: String?, text: String)
    suspend fun perform(id: String?, actionId: String)
    fun open(id: String?)
}

enum class CapsuleState { NeedsYou, Working, Done, Idle }
data class CapsuleAction(val id: String, val label: String)
data class CapsuleMessage(val id: String, val text: String, val fromUser: Boolean)
data class CapsuleEntry(
    val id: String,
    val title: String,
    val summary: String,
    val state: CapsuleState,
    val actions: List<CapsuleAction> = emptyList(),
)
data class CapsuleContent(
    val entries: List<CapsuleEntry> = emptyList(),
    val selectedId: String? = null,
    val messages: List<CapsuleMessage> = emptyList(),
    val canSend: Boolean = true,
    val busy: Boolean = false,
    val error: String? = null,
    val available: Boolean = false,
) {
    val selected: CapsuleEntry? get() = entries.firstOrNull { it.id == selectedId }
    val urgent: CapsuleEntry? get() = entries.minByOrNull { it.state.ordinal }
    val activeCount: Int get() = entries.count { it.state != CapsuleState.Idle }
    val urgentCount: Int get() = urgent?.state?.takeIf { it != CapsuleState.Idle }?.let { state -> entries.count { it.state == state } } ?: 0
    val active: Boolean get() = activeCount > 0
}

enum class CapsuleMode(val key: String, val label: String) {
    Off("off", "关闭"), Active("active", "有任务时显示"), Always("always", "一直显示");
    companion object { fun fromKey(key: String?) = entries.firstOrNull { it.key == key } ?: Off }
}

object CapsuleVisibility {
    fun visible(mode: CapsuleMode, foreground: Boolean, permitted: Boolean, active: Boolean, expanded: Boolean = false): Boolean =
        permitted && !foreground && when (mode) { CapsuleMode.Off -> false; CapsuleMode.Active -> active || expanded; CapsuleMode.Always -> true }
}

/** Coordinates are screen pixels; the vertical fraction survives rotation and window resizing. */
data class CapsuleDock(val right: Boolean = true, val fraction: Float = .15f)
data class CapsuleArea(val left: Int, val top: Int, val right: Int, val bottom: Int) {
    fun position(dock: CapsuleDock, width: Int, height: Int): Pair<Int, Int> {
        val x = if (dock.right) (right - width).coerceAtLeast(left) else left
        val travel = (bottom - top - height).coerceAtLeast(0)
        return x to top + (travel * dock.fraction.coerceIn(0f, 1f)).toInt()
    }
    fun dock(x: Int, y: Int, width: Int, height: Int): CapsuleDock {
        val travel = (bottom - top - height).coerceAtLeast(0)
        return CapsuleDock(x + width / 2 >= (left + right) / 2,
            if (travel == 0) 0f else ((y - top).toFloat() / travel).coerceIn(0f, 1f))
    }
}

/** Full-display insets and a view's clipped insets use different coordinate spaces. */
object CapsuleIme {
    fun top(screenBottom: Int, fullInset: Int, windowTop: Int, windowHeight: Int,
        localInset: Int, visible: Boolean, previous: Int?): Int? = when {
        !visible -> null
        fullInset > 0 -> screenBottom - fullInset
        localInset > 0 && windowHeight > 0 -> windowTop + windowHeight - localInset
        else -> previous
    }
}
