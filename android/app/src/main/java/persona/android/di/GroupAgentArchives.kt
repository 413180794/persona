package persona.android.di

import persona.core.contracts.SessionKey
import persona.core.contracts.api.AgentArchives
import persona.core.hub.HubError
import persona.core.hub.HubFailure

internal fun AgentArchives.inGroup(names: GroupNames): AgentArchives {
    if (names.plain) return this
    val source = this
    return object : AgentArchives {
        override val supported get() = source.supported
        override fun supports(key: SessionKey) = names.owns(key.machineId) && source.supports(names.raw(key))
        override suspend fun list() = source.list().map(names::session)
        override suspend fun archive(key: SessionKey) = source.archive(names.raw(key))
        override suspend fun restore(key: SessionKey) = source.restore(names.raw(key))
    }
}

/** Supported groups must all answer: an offline hub must not silently look like an empty archive. */
internal fun mergedAgentArchives(names: List<GroupNames>, services: List<AgentArchives?>): AgentArchives = object : AgentArchives {
    private fun of(key: SessionKey) = names.indexOfFirst { it.owns(key.machineId) }
        .takeIf { it >= 0 }?.let(services::get)
    override val supported get() = services.any { it?.supported == true }
    override fun supports(key: SessionKey) = of(key)?.supports(key) == true
    override suspend fun list() = services.filterNotNull().filter { it.supported }.flatMap { it.list() }
    override suspend fun archive(key: SessionKey) = (of(key) ?: throw HubFailure(HubError.NoNetwork)).archive(key)
    override suspend fun restore(key: SessionKey) = (of(key) ?: throw HubFailure(HubError.NoNetwork)).restore(key)
}
