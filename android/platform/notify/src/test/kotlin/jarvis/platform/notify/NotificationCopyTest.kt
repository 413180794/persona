package jarvis.platform.notify

import jarvis.core.contracts.api.ConnectionState
import jarvis.core.designsystem.PendingStatus
import jarvis.core.domain.answer.RequestExpiry
import org.junit.Assert.*
import org.junit.Test

class NotificationCopyTest {
    @Test fun systemNoticesUseTheUxVocabularyWithoutReportingUnknownResultsAsFailures() {
        val titles = listOf(
            ConnectionState.Online, ConnectionState.Connecting, ConnectionState.Updating,
            ConnectionState.WaitingForNetwork, ConnectionState.NeedsLocalNetworkPermission,
            ConnectionState.CertificateMismatch, ConnectionState.Revoked,
        ).map(::ongoingTitle)
        assertEquals(listOf("已连接 hub", "连接中…", "更新中…", "等待网络", "需要本地网络权限", "hub 的证书变了", "配对已失效"), titles)
        assertEquals(PendingStatus.Unconfirmed.label, actionResultCopy(NotificationActionResult.Duplicate)?.first)
        assertEquals("已在电脑上处理", actionResultCopy(NotificationActionResult.Gone)?.first)
        assertEquals(RequestExpiry.expiredLabel, actionResultCopy(NotificationActionResult.Expired)?.first)
        assertEquals(PendingStatus.WaitingForNetwork.label, ongoingTitle(ConnectionState.WaitingForNetwork))
        assertNull(actionResultCopy(NotificationActionResult.Submitted))
    }
}
