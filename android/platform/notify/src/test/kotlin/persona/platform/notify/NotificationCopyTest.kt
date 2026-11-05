package persona.platform.notify

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import persona.core.contracts.api.ConnectionState
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/** The notification text in each language: notifications are drawn by the system, so no screenshot shows them. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class NotificationCopyTest {
    private val context: Context get() = ApplicationProvider.getApplicationContext()
    private val states = listOf(
        ConnectionState.Online, ConnectionState.Connecting, ConnectionState.Updating,
        ConnectionState.WaitingForNetwork, ConnectionState.NeedsLocalNetworkPermission,
        ConnectionState.CertificateMismatch, ConnectionState.Revoked,
    )

    private fun text(id: Int) = context.getString(id)

    @Test fun systemNoticesUseTheUxVocabularyWithoutReportingUnknownResultsAsFailures() {
        assertEquals(listOf("已连接 hub", "连接中…", "更新中…", "等待网络", "需要本地网络权限", "hub 的证书变了", "配对已失效"),
            states.map { text(ongoingTitle(it)) })
        assertEquals("结果待确认", actionResultCopy(NotificationActionResult.Duplicate)?.first?.let(::text))
        assertEquals("已在电脑上处理", actionResultCopy(NotificationActionResult.Gone)?.first?.let(::text))
        assertEquals("已过期，回到终端处理", actionResultCopy(NotificationActionResult.Expired)?.first?.let(::text))
        assertEquals("这个会话只能查看，请打开会话或到电脑上处理。", actionResultCopy(NotificationActionResult.ReadOnly)?.second?.let(::text))
        assertNull(actionResultCopy(NotificationActionResult.Submitted))
        assertEquals(listOf("需要你", "提醒", "做完了", "实时状态"), listOf(R.string.notify_channel_input,
            R.string.notify_channel_alert, R.string.notify_channel_done, R.string.notify_channel_status).map(::text))
    }

    @Config(sdk = [35], qualifiers = "en")
    @Test fun english() {
        assertEquals(listOf("Connected to hub", "Connecting…", "Updating…", "Waiting for network", "Local network permission needed",
            "The hub's certificate changed", "Pairing is no longer valid"), states.map { text(ongoingTitle(it)) })
        assertEquals(listOf("Deny", "Allow", "View", "Approve", "Send back"), buttons().map(::text))
        assertEquals("Persona is asking you", text(R.string.notify_title_assistant_asks))
        assertEquals("Can't approve on this phone", actionResultCopy(NotificationActionResult.ReadOnly)?.first?.let(::text))
    }

    @Config(sdk = [35], qualifiers = "ja")
    @Test fun japanese() {
        assertEquals("ハブに接続済み", text(ongoingTitle(ConnectionState.Online)))
        assertEquals(listOf("拒否", "許可", "表示", "承認", "差し戻し"), buttons().map(::text))
        assertEquals("Personaが質問しています", text(R.string.notify_title_assistant_asks))
        assertEquals("「再開」をタップすると通知を再び受け取れます", text(R.string.notify_stopped_text))
    }

    private fun buttons() = listOf(R.string.notify_action_deny, R.string.notify_action_allow, R.string.notify_action_view,
        R.string.notify_action_approve, R.string.notify_action_reject)
}
