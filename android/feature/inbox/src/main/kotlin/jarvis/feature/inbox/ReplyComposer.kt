package jarvis.feature.inbox

import jarvis.core.designsystem.*
import androidx.compose.runtime.Composable
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.ui.Modifier
import jarvis.core.contracts.api.MailReplyTarget
import jarvis.core.contracts.api.MailReplyTargetChanged
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.withContext
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

data class ReplyComposerState(
    val draft: ReplyDraft = ReplyDraft(""),
    val loading: Boolean = true,
    val sending: Boolean = false,
    val error: String? = null,
    val sent: Boolean = false,
    val delivery: ReplyDelivery = ReplyDelivery.Ready,
    val targetChanged: Boolean = false,
) {
    val canEdit: Boolean get() = !loading && !sending && delivery in setOf(ReplyDelivery.Ready, ReplyDelivery.Refused)
    val actionLabel: String get() = when {
        delivery == ReplyDelivery.Verifying -> PendingStatus.Verifying.label
        delivery in setOf(ReplyDelivery.Unknown, ReplyDelivery.NeedsConfirmation) -> "核对结果"
        delivery in setOf(ReplyDelivery.Queued, ReplyDelivery.WaitingForNetwork) -> PendingStatus.Waiting.label
        sending -> PendingStatus.Sending.label
        else -> "发送"
    }
}

/** Serialize draft writes and preserve the request ID when a send needs retrying. */
class ReplyComposer(
    private val repository: InboxRepository,
    private val target: ReplyTarget,
    private val scope: CoroutineScope,
    initialText: String = "",
) {
    private val mutableState = MutableStateFlow(ReplyComposerState())
    val state = mutableState.asStateFlow()
    private val writes = Mutex()

    init {
        scope.launch {
            try {
                val draft = repository.readDraft(target) ?: ReplyDraft(initialText)
                val delivery = repository.replyStatus(target, draft.clientId)
                mutableState.value = ReplyComposerState(
                    draft = if (delivery == ReplyDelivery.Sent) ReplyDraft("") else draft,
                    loading = false, sent = delivery == ReplyDelivery.Sent,
                    delivery = if (delivery == ReplyDelivery.Sent) ReplyDelivery.Ready else delivery,
                    error = if (delivery == ReplyDelivery.Unknown) "结果待确认，请先核对，避免重复发送" else null,
                )
            } catch (error: CancellationException) {
                throw error
            } catch (_: Exception) {
                mutableState.value = mutableState.value.copy(error = "草稿读取失败，请重新打开")
            }
        }
    }

    init {
        scope.launch {
            repository.replyUpdates(target).collect {
                val current = state.value
                if (!current.loading && !current.sending) {
                    val delivery = repository.replyStatus(target, current.draft.clientId)
                    mutableState.value = if (delivery == ReplyDelivery.Sent)
                        ReplyComposerState(draft = ReplyDraft(""), loading = false, sent = true)
                    else current.copy(delivery = delivery)
                }
            }
        }
    }

    fun update(text: String) {
        val current = state.value
        if (!current.canEdit || current.draft.text == text) return
        val draft = ReplyDraft(text)
        mutableState.value = current.copy(draft = draft, error = null, sent = false, delivery = ReplyDelivery.Ready)
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            try {
                withContext(NonCancellable) { writes.withLock { repository.saveDraft(target, draft) } }
            } catch (error: CancellationException) {
                throw error
            } catch (_: Exception) {
                mutableState.value = state.value.copy(error = "草稿保存失败，发送时会重试保存")
            }
        }
    }

    fun send(mailTarget: MailReplyTarget? = null) {
        val current = state.value
        if (current.loading || current.sending || current.draft.text.isBlank()) return
        if (current.delivery in setOf(ReplyDelivery.Queued, ReplyDelivery.WaitingForNetwork, ReplyDelivery.Verifying)) return
        if (current.delivery in setOf(ReplyDelivery.Unknown, ReplyDelivery.NeedsConfirmation)) { verify(); return }
        mutableState.value = current.copy(sending = true, error = null, sent = false, delivery = ReplyDelivery.Sending, targetChanged = false)
        scope.launch {
            var submitted = false
            try {
                writes.withLock {
                    repository.saveDraft(target, current.draft)
                    submitted = true
                    repository.reply(target, ReplyRequest(current.draft.text, current.draft.clientId, mailTarget))
                }
                mutableState.value = ReplyComposerState(draft = ReplyDraft(""), loading = false, sent = true)
            } catch (error: CancellationException) {
                mutableState.value = state.value.copy(sending = false, delivery = ReplyDelivery.Unknown, error = "结果待确认，请先核对")
                throw error
            } catch (_: MailReplyTargetChanged) {
                targetChanged(current.draft)
            } catch (_: Exception) {
                val delivery = if (submitted) {
                    try { repository.replyStatus(target, current.draft.clientId).takeUnless { it == ReplyDelivery.Ready } ?: ReplyDelivery.Unknown }
                    catch (e: CancellationException) { throw e }
                    catch (_: Exception) { ReplyDelivery.Unknown }
                } else ReplyDelivery.Ready
                mutableState.value = state.value.copy(sending = false, delivery = delivery,
                    error = when (delivery) {
                        ReplyDelivery.Queued, ReplyDelivery.WaitingForNetwork -> null
                        ReplyDelivery.Refused -> "没有发出，请修改后再发送"
                        ReplyDelivery.Ready -> "草稿未能保存，尚未发送，请重试"
                        else -> "结果待确认，请先核对，避免重复发送"
                    })
            }
        }
    }

    private suspend fun targetChanged(previous: ReplyDraft) {
        val draft = ReplyDraft(previous.text)
        mutableState.value = state.value.copy(draft = draft, sending = false, targetChanged = true,
            delivery = ReplyDelivery.Ready, error = "邮件回复对象已变化，尚未发送。请查看最新邮件并确认收件人")
        try { writes.withLock { repository.saveDraft(target, draft) } }
        catch (e: CancellationException) { throw e }
        catch (_: Exception) { mutableState.value = state.value.copy(error = "回复对象已变化；草稿保存失败，发送时会重试保存") }
    }

    private fun verify() {
        val draft = state.value.draft
        mutableState.value = state.value.copy(sending = true, delivery = ReplyDelivery.Verifying, error = null)
        scope.launch {
            try {
                val result = repository.verifyReply(target, draft.clientId)
                mutableState.value = if (result == ReplyDelivery.Sent) {
                    ReplyComposerState(draft = ReplyDraft(""), loading = false, sent = true)
                } else if (result == ReplyDelivery.Ready) {
                    val fresh = ReplyDraft(draft.text)
                    writes.withLock { repository.saveDraft(target, fresh) }
                    state.value.copy(draft = fresh, sending = false, delivery = ReplyDelivery.Ready, error = "尚未发送，可以重试")
                } else state.value.copy(sending = false, delivery = result,
                    error = when (result) {
                        ReplyDelivery.Refused -> "没有发出，请修改后再发送"
                        ReplyDelivery.Queued, ReplyDelivery.WaitingForNetwork -> null
                        else -> "暂时无法确认是否送达，请查看原会话；不会自动重发"
                    })
            } catch (e: CancellationException) {
                mutableState.value = state.value.copy(sending = false, delivery = ReplyDelivery.Unknown)
                throw e
            } catch (_: MailReplyTargetChanged) {
                targetChanged(draft)
            } catch (_: Exception) {
                mutableState.value = state.value.copy(sending = false, delivery = ReplyDelivery.Unknown, error = "核对未完成，请稍后再试")
            }
        }
    }
}

/** Shared reply surface; delivery and draft ownership stay in the composer. */
@Composable
internal fun ReplyComposerField(
    state: ReplyComposerState,
    onUpdate: (String) -> Unit,
    onSend: () -> Unit,
    placeholder: String,
    modifier: Modifier = Modifier,
    sendEnabled: Boolean = true,
    accessory: (@Composable ColumnScope.() -> Unit)? = null,
) {
    ComposerFrame(modifier,
        accessory = {
            accessory?.invoke(this)
            ReplyStatus(state)
        },
        trailing = {
            if (state.delivery in setOf(ReplyDelivery.Unknown, ReplyDelivery.NeedsConfirmation)) {
                JButton("核对", onSend,
                    size = JButtonSize.Small,
                    enabled = sendEnabled && !state.loading && !state.sending && state.draft.text.isNotBlank())
            } else SendButton(onSend,
                loading = state.sending,
                enabled = sendEnabled && !state.loading && !state.sending && state.draft.text.isNotBlank() &&
                    state.delivery !in setOf(ReplyDelivery.Queued, ReplyDelivery.WaitingForNetwork, ReplyDelivery.Verifying))
        },
        field = {
            ComposerTextField(state.draft.text, onUpdate, placeholder, enabled = state.canEdit)
        })
}
