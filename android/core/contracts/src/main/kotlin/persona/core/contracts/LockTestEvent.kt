package persona.core.contracts

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class LockTestEvent(
    @SerialName("lock_test_id") val testId: String,
    @SerialName("lock_test_due_at") val dueAt: String,
    @SerialName("lock_test_sent_at") val sentAt: String,
    @SerialName("lock_test_title") val title: String = "锁屏测试",
    @SerialName("lock_test_body") val body: String = "已收到测试通知，请回到应用查看结果。",
) : ServerEvent
