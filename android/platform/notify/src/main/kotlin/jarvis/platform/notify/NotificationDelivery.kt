package jarvis.platform.notify

import jarvis.core.contracts.Interaction
import jarvis.core.contracts.InteractionResolvedEvent
import jarvis.core.contracts.NotifyEvent
import jarvis.core.contracts.ServerEvent
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.DeviceNotificationPreferences
import jarvis.core.contracts.api.DoneNotification
import jarvis.core.contracts.api.InteractionCenter
import jarvis.core.contracts.api.SessionsRepository

/** Only hub-approved notify events may create alerts; raw interactions are state updates. */
internal class NotificationDelivery(
    private val interactions: InteractionCenter,
    private val sessions: SessionsRepository,
    private val suppressed: (SessionKey) -> Boolean,
    private val postInteraction: (Interaction) -> Unit,
    private val postNotify: (NotifyEvent) -> Unit,
    private val retract: (String) -> Unit,
    private val preferences: suspend () -> DeviceNotificationPreferences? = { DeviceNotificationPreferences() },
) {
    suspend fun accept(event: ServerEvent) {
        when (event) {
            is InteractionResolvedEvent -> retract(event.id)
            is NotifyEvent -> {
                val session = SessionKey(event.machineId, event.sessionId)
                if (suppressed(session)) return
                if (event.kind != "interaction" && NotificationPolicy.channel(event) == null) return
                val settings = preferences() ?: return
                if (event.kind == "interaction" && !settings.input) return
                if (event.kind == "done" && settings.done == DoneNotification.Off) return
                // Long/Always and session mute/seen suppression remain the hub's decision.
                if (event.kind != "assistant" && sessions.sessions.value.none { it.key == session }) sessions.refresh()
                if (event.kind == "interaction") {
                    val id = event.interactionId ?: return
                    interactions.refresh()
                    val ix = interactions.pending.value.firstOrNull { it.id == id && it.sessionKey == session } ?: return
                    if (NotificationPolicy.expired(ix)) { retract(id); return }
                    if (!suppressed(session)) postInteraction(ix)
                } else if (!suppressed(session)) postNotify(event)
            }
            else -> Unit
        }
    }
}
