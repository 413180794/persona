package persona.feature.inbox

import persona.core.designsystem.*
import androidx.compose.runtime.Composable
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.ui.Modifier
import persona.core.contracts.api.MailReplyTarget
import persona.core.contracts.api.MailReplyTargetChanged
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

/**
 * Serialize draft writes and preserve the request ID when a send needs retrying. [initialText] fills an empty reply
 * that has no saved draft; [initialAi] says it came from AI (the AI 分身's draft), which sending then confirms.
 */
class ReplyComposer(
    private val repository: InboxRepository,
    private val target: ReplyTarget,
    private val scope: CoroutineScope,
    initialText: String = "",
    initialAi: Boolean = initialText.isNotBlank(),
) {
    private val mutableState = MutableStateFlow(ReplyComposerState())
    val state = mutableState.asStateFlow()
    private val writes = Mutex()
    /** Nothing saved and nothing typed yet: an AI draft offered later may still fill the reply. */
    @Volatile private var pristine = true
    @Volatile private var offered: String? = null

    init {
        scope.launch {
            try {
                val saved = repository.readDraft(target)
                if (saved != null) pristine = false
                val draft = saved ?: ReplyDraft(initialText, ai = initialAi && initialText.isNotBlank())
                val delivery = repository.replyStatus(target, draft.clientId)
                mutableState.value = ReplyComposerState(
                    draft = if (delivery == ReplyDelivery.Sent) ReplyDraft("") else draft,
                    loading = false, sent = delivery == ReplyDelivery.Sent,
                    delivery = if (delivery == ReplyDelivery.Sent) ReplyDelivery.Ready else delivery,
                    error = if (delivery == ReplyDelivery.Unknown) "结果待确认，请先核对，避免重复发送" else null,
                )
                applyOffer()
            } catch (error: CancellationException) {
                throw error
            } catch (_: Exception) {
                mutableState.value = mutableState.value.copy(error = "草稿读取失败，请重新打开")
            }
        }
    }

    /**
     * The AI 分身's draft, known only once the inbox item is: it fills the reply if the user has neither a saved
     * draft nor typed anything (a page that opened before the list came in); otherwise the user's text stays.
     */
    fun offerAiDraft(text: String) {
        if (text.isBlank()) return
        offered = text
        applyOffer()
    }

    private fun applyOffer() {
        val text = offered ?: return
        val current = state.value
        if (!pristine || current.loading || current.sent || !current.canEdit || current.draft.text.isNotBlank()) return
        offered = null
        pristine = false
        mutableState.value = current.copy(draft = ReplyDraft(text, ai = true), error = null)
    }

    /**
     * Replaces the reply with a draft AI wrote (AI 草拟 / AI 改), under a new request ID. Refused while a send is
     * on its way or its result is unclear: that request keeps its text and ID.
     */
    fun replaceWithAi(text: String): Boolean {
        val current = state.value
        if (!current.canEdit || text.isBlank()) return false
        pristine = false
        val draft = ReplyDraft(text, ai = true)
        mutableState.value = current.copy(draft = draft, error = null, sent = false, delivery = ReplyDelivery.Ready, targetChanged = false)
        save(draft)
        return true
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
        pristine = false
        // Edited AI text is still AI's draft; clearing it makes the reply the user's own.
        val draft = ReplyDraft(text, ai = current.draft.ai && text.isNotBlank())
        mutableState.value = current.copy(draft = draft, error = null, sent = false, delivery = ReplyDelivery.Ready)
        save(draft)
    }

    private fun save(draft: ReplyDraft) {
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
        val draft = ReplyDraft(previous.text, ai = previous.ai)
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
                    val fresh = ReplyDraft(draft.text, ai = draft.ai)
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
