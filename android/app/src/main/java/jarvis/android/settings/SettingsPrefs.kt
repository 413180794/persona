package jarvis.android.settings

import android.content.Context
import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.util.UUID

/**
 * What 设置 keeps on this phone (SharedPreferences "settings"): the notification choices the Notifier follows (Android
 * has no push service, so the phone decides; docs/android.md「通知」) as last read from / written to the hub, the id
 * those choices are kept under on the hub, and the Claude Code page's last machine. Works with defaults before [init]
 * (unit tests).
 */
object SettingsPrefs {
    private var sp: SharedPreferences? = null

    private val _notifyInput = MutableStateFlow(true)
    /** 「需要你处理时」. */
    val notifyInput: StateFlow<Boolean> = _notifyInput.asStateFlow()

    private val _doneMode = MutableStateFlow(DoneMode.LONG)
    /** 「Agent 做完时」. */
    val doneMode: StateFlow<DoneMode> = _doneMode.asStateFlow()

    /** What counts as a long turn (the hub's `long_turn_seconds`, 60 until the hub says otherwise). */
    @Volatile var longTurnSeconds: Int = 60
        private set

    fun init(ctx: Context) {
        val p = ctx.getSharedPreferences("settings", Context.MODE_PRIVATE)
        sp = p
        _notifyInput.value = p.getBoolean(K_INPUT, true)
        _doneMode.value = DoneMode.of(p.getString(K_DONE, null))
        longTurnSeconds = p.getInt(K_LONG, 60)
    }

    fun setNotify(input: Boolean, done: DoneMode) {
        _notifyInput.value = input
        _doneMode.value = done
        sp?.edit()?.putBoolean(K_INPUT, input)?.putString(K_DONE, done.wire)?.apply()
    }

    /** Takes what the hub keeps for this device (read on every connect, so a change made elsewhere applies here). */
    fun apply(p: NotifyPrefs) {
        setNotify(p.input, p.doneMode)
        p.longTurnSeconds?.takeIf { it > 0 }?.let { s ->
            longTurnSeconds = s
            sp?.edit()?.putInt(K_LONG, s)?.apply()
        }
    }

    /** A change made here the hub hasn't got yet (it was out of reach): sent on the next connect instead of reading. */
    var unsynced: Boolean
        get() = sp?.getBoolean(K_UNSYNCED, false) ?: false
        set(v) { sp?.edit()?.putBoolean(K_UNSYNCED, v)?.apply() }

    /** Does a turn that took [durationMs] (null: not seen from its start) get a 「做完了」? Unknown counts as long. */
    fun wantsDone(durationMs: Long?): Boolean = when (doneMode.value) {
        DoneMode.OFF -> false
        DoneMode.ALWAYS -> true
        DoneMode.LONG -> durationMs == null || durationMs >= longTurnSeconds * 1000L
    }

    /** The id this phone's notification prefs live under on the hub (`/api/v1/devices/{id}/prefs`), made once. */
    fun notifyDeviceId(): String {
        val p = sp ?: return "android"
        p.getString(K_DEVICE, null)?.let { return it }
        val id = "android-" + UUID.randomUUID().toString().take(13)
        p.edit().putString(K_DEVICE, id).apply()
        return id
    }

    var claudeMachine: String?
        get() = sp?.getString(K_CLAUDE_MACHINE, null)
        set(v) { sp?.edit()?.putString(K_CLAUDE_MACHINE, v)?.apply() }

    /** Claude Code 设置「同时改所有在线机器」, on by default (iOS too). */
    var claudeApplyAll: Boolean
        get() = sp?.getBoolean(K_CLAUDE_ALL, true) ?: true
        set(v) { sp?.edit()?.putBoolean(K_CLAUDE_ALL, v)?.apply() }

    /** Machine ids and the prefs id belong to the hub that was just forgotten. */
    fun forgetHub() {
        sp?.edit()?.remove(K_CLAUDE_MACHINE)?.remove(K_DEVICE)?.remove(K_UNSYNCED)?.apply()
    }

    /**
     * Brings this phone and the hub in line after a connect: a change the hub missed goes up, otherwise the hub's copy
     * (maybe changed on another device or by an older install) comes down. Failures leave things as they are.
     */
    suspend fun sync(api: jarvis.android.data.ApiClient) {
        val id = notifyDeviceId()
        runCatching {
            if (unsynced) {
                apply(api.setNotifyPrefs(id, notifyInput.value, doneMode.value))
                unsynced = false
            } else {
                apply(api.notifyPrefs(id))
            }
        }
    }

    /** The user changed a switch: applies at once, then saves it on the hub (or marks it for the next connect). */
    suspend fun change(api: jarvis.android.data.ApiClient, input: Boolean, done: DoneMode) {
        setNotify(input, done)
        unsynced = true
        runCatching { api.setNotifyPrefs(notifyDeviceId(), input, done) }.onSuccess { unsynced = false }
    }

    private const val K_INPUT = "notify_input"
    private const val K_DONE = "notify_done"
    private const val K_LONG = "long_turn_seconds"
    private const val K_DEVICE = "notify_device_id"
    private const val K_CLAUDE_MACHINE = "claude_machine"
    private const val K_CLAUDE_ALL = "claude_apply_all"
    private const val K_UNSYNCED = "notify_unsynced"
}
