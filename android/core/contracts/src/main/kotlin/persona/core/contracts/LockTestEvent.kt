package persona.core.contracts

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class LockTestEvent(
    @SerialName("lock_test_id") val testId: String,
    @SerialName("lock_test_due_at") val dueAt: String,
    @SerialName("lock_test_sent_at") val sentAt: String,
    @SerialName("lock_test_title") val title: String? = null,
    @SerialName("lock_test_body") val body: String? = null,
) : ServerEvent {
    /** The hub's words, else the app's (an older hub sends none). */
    val titleText: CoreText get() = title?.let(CoreText::plain) ?: CoreText.of(Key.LockTestTitle)
    val bodyText: CoreText get() = body?.let(CoreText::plain) ?: CoreText.of(Key.LockTestBody)
}
