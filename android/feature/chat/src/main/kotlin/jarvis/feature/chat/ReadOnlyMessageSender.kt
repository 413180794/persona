package jarvis.feature.chat

import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.MessageSender

/** Historical readers cannot enqueue or retry messages, even through an indirect action. */
internal class ReadOnlyMessageSender(source: MessageSender) : MessageSender {
    override val outbox = source.outbox
    override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
        throw IllegalStateException("历史会话只读，请先继续这个会话")
    }
}
