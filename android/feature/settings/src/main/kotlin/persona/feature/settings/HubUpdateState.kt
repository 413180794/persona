package persona.feature.settings

import persona.core.contracts.api.HubUpdateRecord
import persona.core.contracts.api.HubUpdateStatus
import persona.core.contracts.api.HubUpdates
import persona.core.contracts.api.HubUpdatesUnsupported
import persona.core.contracts.parseTime
import persona.core.designsystem.failureReason
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import java.time.Instant

internal data class HubUpdateState(
    val status: HubUpdateStatus? = null,
    val history: List<HubUpdateRecord> = emptyList(),
    /** The hub predates updates in the app (no /api/v1/hub/update). */
    val unsupported: Boolean = false,
    val checking: Boolean = false,
    /** A start or settings call is on its way: its buttons wait. */
    val sending: Boolean = false,
    /** Reading the status failed (the page has nothing else to show). */
    val error: String? = null,
    /** The last action that failed, for a notice over the groups. */
    val actionError: String? = null,
)

/**
 * 设置 › hub 版本 (iOS `HubUpdateModel`, the web's `HubSettings`): reads the status and the history, applies every newer
 * status (an answer here or a `hub_update` push), and reads the history again when an update ends.
 */
internal class HubUpdateModel(private val api: HubUpdates) {
    private val mutable = MutableStateFlow(HubUpdateState())
    val state = mutable.asStateFlow()

    suspend fun load() {
        try {
            val before = state.value.status
            val read = api.status()
            // A push that came while asking is newer than the answer: keep it.
            if (state.value.status === before) apply(read)
            val history = try { api.history() } catch (cancelled: CancellationException) { throw cancelled } catch (_: Exception) { state.value.history }
            mutable.update { it.copy(history = history, error = null, unsupported = false) }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: HubUpdatesUnsupported) {
            mutable.update { it.copy(unsupported = true) }
        } catch (error: Exception) {
            mutable.update { it.copy(error = "读不到 hub 的版本：${failureReason(error)}") }
        }
    }

    /** A newer status; true when it ends an update (the history has a new line). */
    fun apply(status: HubUpdateStatus): Boolean {
        val finished = HubUpdateText.finished(state.value.status?.state, status.state)
        mutable.update { it.copy(status = status, unsupported = false, error = null) }
        return finished
    }

    /** [apply], then the history again when it ended an update. */
    suspend fun receive(status: HubUpdateStatus) {
        if (apply(status)) reloadHistory()
    }

    private suspend fun reloadHistory() {
        try { mutable.update { it.copy(history = api.history()) } }
        catch (cancelled: CancellationException) { throw cancelled } catch (_: Exception) { }
    }

    suspend fun check() {
        if (state.value.checking) return
        mutable.update { it.copy(checking = true, actionError = null) }
        try { receive(api.check()) } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(actionError = "检查失败：${failureReason(error)}") } }
        finally { mutable.update { it.copy(checking = false) } }
    }

    /** Starts the update; [now] is the confirmation's 现在就更新 (it does not wait for idle). */
    suspend fun update(now: Boolean) = send("没能开始更新") { api.start(now) }

    suspend fun setAuto(on: Boolean) = send("保存失败") { api.settings(auto = on) }

    /** 「自动更新已开启」 seen: not shown again. */
    suspend fun noticeSeen() = send("保存失败") { api.settings(noticeSeen = true) }

    private suspend fun send(failed: String, call: suspend () -> HubUpdateStatus) {
        if (state.value.sending) return
        mutable.update { it.copy(sending = true, actionError = null) }
        try { receive(call()) } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(actionError = "$failed：${failureReason(error)}") } }
        finally { mutable.update { it.copy(sending = false) } }
    }
}

/** What the 更新 group shows (the web's `updateView`). */
internal sealed interface HubUpdateView {
    data object Docker : HubUpdateView
    data class Cannot(val why: String) : HubUpdateView
    data class Newer(val working: Boolean) : HubUpdateView
    data object Working : HubUpdateView
    data object Current : HubUpdateView
}

/** A line to read and, under it, what to copy (a Docker hub's steps). */
internal data class CopyStep(val text: String, val copy: String)

/** The page's words, the same as iOS `HubUpdateStatus` / `HubSettingsPage` and the web's `lib/hub-update.ts`. */
internal object HubUpdateText {
    const val DockerCommand = "docker compose pull && docker compose up -d"

    fun finished(previous: String?, next: String): Boolean = previous != next && next in setOf("updated", "rolled_back", "failed")

    fun channel(channel: String): String = when (channel) { "release" -> "发布版"; "docker" -> "Docker"; else -> "开发版" }

    fun state(s: HubUpdateStatus): String = when (s.state) {
        "checking" -> "正在检查"
        "downloading" -> "正在下载"
        "waiting_idle" -> if (s.busy.isEmpty()) "等助手和 Agent 空闲" else "等 ${s.busy.size} 个在忙的结束"
        "restarting" -> "正在重启，会断开一会儿"
        "confirming" -> "新版本正在确认"
        "updated" -> "已更新"
        "rolled_back" -> "已回滚"
        "failed" -> "没更新成功"
        "available" -> "有新版本"
        else -> "已是最新"
    }

    /** The settings row's value (iOS `hubSummary`). */
    fun summary(s: HubUpdateStatus?): String? = when {
        s == null -> null
        s.working -> state(s)
        s.latest?.newer == true -> if (s.channel == "docker") "有新镜像" else "有新版本"
        else -> s.version.ifBlank { null }
    }

    fun view(s: HubUpdateStatus): HubUpdateView = when {
        s.channel == "docker" -> HubUpdateView.Docker
        !s.canUpdate -> HubUpdateView.Cannot(s.whyNot?.takeIf { it.isNotBlank() }?.let { "这个 hub 不会自己更新：$it。" } ?: "这个 hub 不会自己更新。")
        s.latest?.newer == true -> HubUpdateView.Newer(s.working)
        s.working -> HubUpdateView.Working
        else -> HubUpdateView.Current
    }

    /** The warning under a newer release: it went back before, or auto-update holds it back. */
    fun latestWarning(s: HubUpdateStatus): String? {
        val l = s.latest?.takeIf { it.newer } ?: return null
        if (l.bad) return "这一版上次装完起不来，已经回滚过；自动更新不会再装它。"
        if (!l.held.isNullOrBlank() && s.auto) return "自动更新不会装这一版：${l.held}。确认没问题可以手动更新。"
        return null
    }

    /** A Docker hub's lines: what to say and what to copy. */
    fun dockerSteps(s: HubUpdateStatus): Pair<String?, List<CopyStep>> {
        val l = s.latest?.takeIf { it.newer }
            ?: return null to listOf(CopyStep("Docker 部署的 hub 由新镜像更新：有新镜像时在 compose 文件所在的目录里运行", DockerCommand))
        val image = s.image
        val steps = if (image?.pinned == true) listOf(
            // `pull` alone keeps the version the tag names: the tag changes first.
            CopyStep("在 hub 所在的机器上，把 compose 文件旁边 .env 里的 JARVIS_IMAGE 改成：", "${image.name}:${l.version}"),
            CopyStep("再在那个目录里运行：", DockerCommand),
        ) else listOf(CopyStep("在 hub 所在的机器上，compose 文件所在的目录里运行：", DockerCommand))
        return "新镜像 ${l.version}" to steps
    }

    fun record(result: String): String = when (result) { "updated" -> "已更新"; "rolled_back" -> "已回滚"; "held" -> "没有自动装"; else -> "没更新成功" }

    fun recordTime(r: HubUpdateRecord, now: Instant = Instant.now()): String? =
        parseTime(r.ended ?: r.started)?.let { ChannelText.ago(it, now) }

    /** 「3 小时前发布」 under a newer release. */
    fun released(s: HubUpdateStatus, now: Instant = Instant.now()): String? = parseTime(s.latest?.released)?.let { ChannelText.ago(it, now) + "发布" }

    fun lastCheck(s: HubUpdateStatus, now: Instant = Instant.now()): String? = parseTime(s.lastCheck)?.let { ChannelText.ago(it, now) }

    const val UpdateFooter = "更新会等助手和所有 Agent 都空闲时再重启 hub；新版本起不来会自动回到上一版。重启期间连接会断开一会儿。"
    const val AutoNotice = "hub 会在空闲时自动更新。"
    const val AutoFooter = "开着时，有新版本就在助手和所有 Agent 都空闲时自己装上，装完推一条通知。"
    const val NodesFooter = "hub 更新后会把各台电脑上的 personad 也换成同一个版本；没换上的可以点进去「重新部署」。"
    const val NowWarning = "不再等它们结束：会中断正在等 hub 的操作（助手这一轮、正在上传的文件、终端连接）。herdr 里的 Agent 不受影响，hub 起来后会自己连回来。"
    const val Unsupported = "这个 hub 的版本太旧，不支持在 App 里查看和更新版本。在 hub 所在的机器上用 install.sh 更新一次以后就可以了。"
}
