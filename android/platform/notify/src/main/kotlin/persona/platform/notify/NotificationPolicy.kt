package persona.platform.notify

import persona.core.contracts.Interaction
import persona.core.contracts.NotifyEvent
import persona.core.contracts.Resolution
import persona.core.contracts.SessionKey
import persona.core.domain.answer.AnswerEntry
import persona.core.domain.answer.AnswerRequirement
import persona.core.domain.answer.ApprovalAction
import persona.core.domain.answer.ApprovalPolicy
import persona.core.domain.answer.Authentication
import persona.core.domain.answer.AnswerAction
import persona.core.domain.answer.AnswerBuilder
import persona.core.domain.answer.AnswerContext
import persona.core.domain.answer.AnswerDraft
import persona.core.domain.answer.AnswerRetry
import persona.core.domain.answer.RequestExpiry
import java.time.Instant
import java.util.UUID

internal data class NotificationAction(
    val key: String,
    val label: String,
    val authenticationRequired: Boolean,
    val replyLabel: String? = null,
    val answer: AnswerAction,
    val draft: AnswerDraft,
    val retry: AnswerRetry,
)

/** Notification presentation only; action availability and authentication belong to ApprovalPolicy. */
internal object NotificationPolicy {
    fun expired(ix: Interaction, now: Instant = Instant.now()): Boolean = RequestExpiry.expired(ix.expiresAt, now)

    fun actions(ix: Interaction): List<NotificationAction> =
        ApprovalPolicy.actions(AnswerEntry.Notification, ix.kind, locked = true).sortedBy { order(it.id) }.flatMap { policy ->
            fun action(key: String, label: String, answer: AnswerAction, draft: AnswerDraft = AnswerDraft()) = NotificationAction(
                key, label, policy.authentication != Authentication.None,
                when (policy.input) { "message" -> "拒绝原因"; "feedback" -> "修改意见"; else -> null },
                answer, draft, policy.retry,
            )
            when (policy.id) {
                ApprovalAction.Allow -> listOf(action(policy.id.wire, "允许", AnswerAction.Allow))
                ApprovalAction.Deny -> listOf(action(policy.id.wire, "拒绝", AnswerAction.Deny))
                ApprovalAction.ApproveEdits -> listOf(action(policy.id.wire, "批准", AnswerAction.ApproveEdits))
                ApprovalAction.Reject -> listOf(action(policy.id.wire, "打回", AnswerAction.Reject))
                ApprovalAction.ChooseOption -> when {
                    AnswerRequirement.SingleQuestionSingleChoice in policy.requirements ->
                        ix.questions?.singleOrNull()?.takeUnless { it.multiSelect }?.let { question ->
                            question.options.take(policy.maxOptions ?: 0).mapIndexed { index, option ->
                                action("answer.$index", option.label, AnswerAction.PickOption, AnswerDraft(optionIndex = index))
                            }
                        }.orEmpty()
                    AnswerRequirement.PlainDialogOption in policy.requirements -> ix.dialog?.options.orEmpty()
                        .filter { it.kind == null || it.kind == "" || it.kind == "plain" }
                        .take(policy.maxOptions ?: 0).map { action("option.${it.key}", it.label, AnswerAction.ScreenOption, AnswerDraft(option = it.key)) }
                    else -> emptyList()
                }
                else -> emptyList()
            }
        }

    /**
     * Saying no on the left, yes on the right (拒绝 / 允许, 打回 / 批准), as in Android's dialogs and on the ongoing
     * notification; a question's or a terminal dialog's options keep their own order. The policy's order only keeps its
     * fixtures stable.
     */
    private fun order(id: ApprovalAction) = when (id) {
        ApprovalAction.Deny, ApprovalAction.Reject -> 0
        ApprovalAction.Allow, ApprovalAction.ApproveEdits -> 1
        else -> 2
    }

    fun resolution(ix: Interaction, key: String, reply: String?): Resolution? {
        val action = actions(ix).firstOrNull { it.key == key } ?: return null
        if (reply.orEmpty().length > 4000) return null
        if (action.replyLabel == null && !reply.isNullOrBlank()) return null
        val draft = when (action.answer) {
            AnswerAction.Deny -> action.draft.copy(message = reply.orEmpty())
            AnswerAction.Reject -> action.draft.copy(feedback = reply.orEmpty())
            else -> action.draft
        }
        return AnswerBuilder.build(ix, action.answer, draft, AnswerContext(entry = AnswerEntry.Notification)).resolution
    }

    fun suppressed(session: SessionKey, foreground: Boolean, visibleSession: SessionKey?): Boolean =
        foreground && session == visibleSession

    fun channel(event: NotifyEvent): String? = when (event.kind) {
        "done" -> Notifier.CH_DONE
        "assistant" -> Notifier.CH_ASSISTANT
        "alert" -> Notifier.CH_ALERT
        else -> null
    }

    fun session(event: NotifyEvent): SessionKey? =
        if (event.machineId.isBlank() || event.sessionId.isBlank()) null else SessionKey(event.machineId, event.sessionId)

    fun tag(event: NotifyEvent): String = if (event.kind == "alert") "alert:${UUID.randomUUID()}"
        else "${event.kind}:${SessionKey(event.machineId, event.sessionId).path}"
}
