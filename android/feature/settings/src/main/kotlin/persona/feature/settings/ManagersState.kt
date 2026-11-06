package persona.feature.settings

import android.content.res.Resources
import androidx.annotation.StringRes
import persona.core.contracts.api.HubManager
import persona.core.contracts.api.HubManagers
import persona.core.contracts.api.ManagerCode
import persona.core.contracts.api.ManagerRoster
import persona.core.contracts.api.ManagersUnsupported
import persona.core.contracts.api.hubCodedFailure
import persona.core.contracts.parseTime
import persona.core.designsystem.Status
import persona.core.designsystem.failureReasonText
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import java.time.Duration
import java.time.Instant
import java.time.ZoneId

// 设置 › 管理者 (docs/managers.md): the words of the list and a manager's page (the same as the web's), and the model
// both pages share. The pages are in ManagersScreen.kt.

/** What the roster says about a manager, in the words the web uses too. */
internal object ManagerText {
    /** The OS the manager said it runs on, by its usual name. */
    fun osName(os: String?): String? = when (val value = os?.trim()?.takeIf { it.isNotEmpty() }) {
        null -> null
        "darwin" -> "macOS"
        "linux" -> "Linux"
        "windows" -> "Windows"
        else -> value
    }

    /** How it runs, in words; the hub's own assistant is 「hub 自己的」 whatever it said. */
    fun deploy(res: Resources, m: HubManager): String? {
        val value = m.deploy?.trim()?.takeIf { it.isNotEmpty() }
        if (m.builtin) return res.getString(R.string.settings_managers_deploy_hub)
        return when (value?.lowercase()) {
            null -> null
            "child" -> res.getString(R.string.settings_managers_deploy_hub)
            "service", "systemd", "launchd" -> res.getString(R.string.settings_managers_deploy_service)
            "docker", "container" -> res.getString(R.string.settings_managers_deploy_container)
            "external" -> res.getString(R.string.settings_managers_deploy_external)
            else -> value
        }
    }

    /** Where it runs: host · OS · how, leaving out what it did not say; null when it said nothing (still installing). */
    fun where(res: Resources, m: HubManager): String? =
        listOfNotNull(m.host?.trim()?.takeIf { it.isNotEmpty() }, osName(m.os), deploy(res, m)).joinToString(" · ").ifEmpty { null }

    /** The dot beside the status: green online, the waiting dot while installing, grey offline. */
    fun status(m: HubManager): Status = when {
        m.isInstalling -> Status.Waiting
        m.online -> Status.Unread
        else -> Status.Offline
    }

    /** Whole minutes left on an install code, rounded up; null once it ran out (or the time is unreadable). */
    fun minutesLeft(until: String?, now: Instant): Int? {
        val end = parseTime(until) ?: return null
        val left = Duration.between(now, end)
        if (left.isNegative || left.isZero) return null
        return ((left.seconds + 59) / 60).toInt().coerceAtLeast(1)
    }

    /** The list's status line: 在线; 离线 · 最后同步于 …; 安装中 · 码 n 分钟后过期 / 码已过期. */
    fun statusLine(res: Resources, m: HubManager, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String = when {
        m.isInstalling -> minutesLeft(m.installing, now)?.let { res.getQuantityString(R.plurals.settings_managers_installing, it, it) }
            ?: res.getString(R.string.settings_managers_installing_expired)
        m.online -> res.getString(R.string.settings_managers_online)
        else -> parseTime(m.lastSeen)?.let { res.getString(R.string.settings_managers_offline_since, agoText(res, it, now, zone)) }
            ?: res.getString(R.string.settings_managers_offline)
    }

    /** A manager's page: 连上于 … (online) or 最后同步于 … (offline), as a label and its value; null without a time. */
    fun seen(res: Resources, m: HubManager, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): Pair<String, String>? = when {
        m.isInstalling -> null
        m.online -> parseTime(m.since)?.let { res.getString(R.string.settings_managers_row_since) to agoText(res, it, now, zone) }
        else -> parseTime(m.lastSeen)?.let { res.getString(R.string.settings_managers_row_last_seen) to agoText(res, it, now, zone) }
    }

    /** An install code's countdown, m:ss; null once it ran out. */
    fun countdown(until: String?, now: Instant): String? {
        val end = parseTime(until) ?: return null
        val left = Duration.between(now, end).seconds
        if (left <= 0) return null
        return String.format(java.util.Locale.ROOT, "%d:%02d", left / 60, left % 60)
    }

    /** A name the hub takes: 1 to 40 characters once trimmed. */
    fun validName(name: String): Boolean = name.trim().let { it.isNotEmpty() && it.codePointCount(0, it.length) <= MaxName }

    /** Why deleting is not offered, for the note under the button; null when it can be deleted. */
    @StringRes fun whyNoDelete(m: HubManager): Int? = when {
        m.builtin -> R.string.settings_managers_delete_builtin
        m.isDefault -> R.string.settings_managers_delete_default
        else -> null
    }

    /**
     * How a refused change reads: the roster's own codes and 400 / 403 in their words, else what did not happen
     * ([verb]) and the app's usual reason (the network, the hub's word).
     */
    fun failure(error: Throwable, @StringRes verb: Int = R.string.settings_src_channel_not_changed): ResText {
        val coded = hubCodedFailure(error)
        return when {
            coded?.code == "default_manager" -> ResText.of(R.string.settings_managers_err_default)
            coded?.code == "builtin_manager" -> ResText.of(R.string.settings_managers_err_builtin)
            coded?.code == "no_manager" -> ResText.of(R.string.settings_managers_err_gone)
            coded?.code == "installing" -> ResText.of(R.string.settings_managers_err_installing)
            coded?.status == 400 -> ResText.of(R.string.settings_managers_err_name)
            coded?.status == 403 -> ResText.of(R.string.settings_managers_err_forbidden)
            else -> ResText.of(verb, failureReasonText(error))
        }
    }

    const val MaxName = 40
}

/** An install code on screen, for [managerId] (named [name]). */
internal data class ManagerCodeSheet(val managerId: String, val name: String, val code: ManagerCode)

internal data class ManagersState(
    val roster: ManagerRoster? = null,
    val loading: Boolean = false,
    /** The hub predates the roster (404). */
    val unsupported: Boolean = false,
    /** Reading the list went wrong. */
    val error: ResText? = null,
    /** A change is on its way (one at a time). */
    val busy: Boolean = false,
    /** Why the last change did not go. */
    val failure: ResText? = null,
    val code: ManagerCodeSheet? = null,
) {
    val managers: List<HubManager> get() = roster?.managers.orEmpty()
    fun manager(id: String): HubManager? = managers.firstOrNull { it.id == id }
}

/** 设置 › 管理者: the roster, and making, renaming, moving and deleting managers. One model per page. */
internal class ManagersModel(private val api: HubManagers) {
    private val mutable = MutableStateFlow(ManagersState())
    val state = mutable.asStateFlow()
    private var again = false

    /** Reads the list; asked again while reading (a push), it reads once more after. */
    suspend fun load() {
        if (state.value.loading) { again = true; return }
        mutable.update { it.copy(loading = true) }
        try {
            do {
                again = false
                try {
                    val roster = api.roster()
                    mutable.update { it.copy(roster = roster, error = null, unsupported = false) }
                } catch (cancelled: CancellationException) { throw cancelled }
                catch (_: ManagersUnsupported) { mutable.update { it.copy(unsupported = true, error = null) } }
                catch (error: Exception) { mutable.update { it.copy(error = ManagerText.failure(error, R.string.settings_src_read_failed), unsupported = false) } }
            } while (again)
        } finally { mutable.update { it.copy(loading = false) } }
    }

    /** 新建管理者: the code sheet opens on success. */
    suspend fun create(name: String): Boolean {
        if (!ManagerText.validName(name)) { mutable.update { it.copy(failure = ResText.of(R.string.settings_managers_err_name)) }; return false }
        return change {
            val made = api.create(name.trim())
            val manager = made.manager
            mutable.update { it.copy(code = ManagerCodeSheet(manager?.id.orEmpty(), manager?.name ?: name.trim(), made)) }
            // The hub pushes the change too; reading now does not wait on it.
            load()
        }
    }

    /** 换个地方装 / 新的安装码: a new code, on the sheet. */
    suspend fun newCode(id: String): Boolean = change {
        val code = api.newCode(id)
        val name = state.value.manager(id)?.name ?: state.value.code?.takeIf { it.managerId == id }?.name.orEmpty()
        mutable.update { it.copy(code = ManagerCodeSheet(id, name, code)) }
        load()
    }

    suspend fun rename(id: String, name: String): Boolean {
        if (!ManagerText.validName(name)) { mutable.update { it.copy(failure = ResText.of(R.string.settings_managers_err_name)) }; return false }
        return change { replace(api.rename(id, name.trim())) }
    }

    suspend fun makeDefault(id: String): Boolean = change {
        api.makeDefault(id)
        // Every manager's 默认 changes: the whole list again.
        load()
    }

    suspend fun delete(id: String): Boolean = change {
        api.delete(id)
        mutable.update { s -> s.copy(roster = s.roster?.let { r -> r.copy(managers = r.managers.filterNot { it.id == id }) }) }
    }

    fun closeCode() = mutable.update { it.copy(code = null) }

    fun clearFailure() = mutable.update { it.copy(failure = null) }

    private fun replace(manager: HubManager) = mutable.update { s ->
        s.copy(roster = s.roster?.let { r -> r.copy(managers = r.managers.map { if (it.id == manager.id) manager.copy(isDefault = manager.isDefault || it.isDefault) else it }) })
    }

    private suspend fun change(block: suspend () -> Unit): Boolean {
        if (state.value.busy) return false
        mutable.update { it.copy(busy = true, failure = null) }
        return try { block(); true }
        catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(failure = ManagerText.failure(error)) }; false }
        finally { mutable.update { it.copy(busy = false) } }
    }
}
