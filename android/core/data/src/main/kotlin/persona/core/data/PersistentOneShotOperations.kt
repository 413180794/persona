package persona.core.data

import persona.core.contracts.CoreTextException
import persona.core.contracts.CoreText
import persona.core.contracts.CoreText.Key
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

class PersistentChatRecoveryService(
    private val delegate: ChatRecoveryService,
    private val journal: OneShotOperationRepository,
    private val transport: ChatTransport,
) : ChatRecoveryService {
    private val lock = Mutex()
    override suspend fun resume(session: Session): ChatRecoveryResult = lock.withLock {
        val claim = journal.claim(OneShotOperationKind.Resume, session.machineId, session.sessionId)
        val operation = claim.operation
        val result = if (claim.firstAttempt) delegate.resume(session) else try {
            val restored = transport.sessions().firstOrNull {
                it.key == session.key && it.status in setOf("idle", "working", "blocked", "done")
            }
            restored?.let { ChatRecoveryResult.Restored(it.key) } ?: ChatRecoveryResult.Unknown
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { ChatRecoveryResult.Unknown }
        if (result != ChatRecoveryResult.Unknown) journal.forget(operation)
        result
    }

    override suspend fun reconnect(machineId: String): ChatActionResult = lock.withLock {
        val claim = journal.claim(OneShotOperationKind.Reconnect, machineId)
        val result = if (claim.firstAttempt) delegate.reconnect(machineId) else try {
            if (transport.machines().any { it.id == machineId && it.online }) ChatActionResult.Done else ChatActionResult.Unknown
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { ChatActionResult.Unknown }
        if (result != ChatActionResult.Unknown) journal.forget(claim.operation)
        result
    }
}

class PersistentAgentOperations(
    private val delegate: AgentOperations,
    private val journal: OneShotOperationRepository,
    private val transport: ChatTransport,
) : AgentOperations by delegate {
    private val lock = Mutex()
    override suspend fun closeSession(key: SessionKey) = lock.withLock {
        val claim = journal.claim(OneShotOperationKind.Close, key.machineId, key.sessionId)
        if (!claim.firstAttempt) {
            val confirmed = try {
                transport.machines().any { it.id == key.machineId && it.online } &&
                    transport.sessions().none { it.key == key && it.isLive }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (_: Exception) { false }
            if (!confirmed) throw AgentOperationFailure(true, CoreTextException(CoreText.of(Key.CloseUnconfirmed)))
            journal.forget(claim.operation)
            return@withLock
        }
        try {
            delegate.closeSession(key)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) {
            if ((failure as? AgentOperationFailure)?.uncertain == false) journal.forget(claim.operation)
            throw AgentOperationFailure((failure as? AgentOperationFailure)?.uncertain != false, failure)
        }
        journal.forget(claim.operation)
    }
}
