package jarvis.core.domain.sort

/** Space detail always includes pinned members and preserves the terminal's tab order. */
data class AgentSpace(val id: String, val name: String, val members: List<AgentSession>) {
    val machineId: String get() = members.first().session.machineId
    val preferenceKey: String get() = "$machineId/$name"
    val cwd: String get() = members.first().session.cwd
    val priority: Int get() = members.minOf { it.state.priority }
    val updatedAt get() = members.mapNotNull { it.updatedAt }.maxOrNull()
    val quiet: Boolean get() = members.all { it.isQuiet && !it.isWaiting }
}

fun agentSpaces(sessions: List<AgentSession>): List<AgentSpace> = sessions
    .filter { it.isLive && !it.session.assistant }
    .groupBy { "${it.session.machineId}/${it.session.spaceId ?: "-"}" }
    .map { (id, members) -> AgentSpace(id, members.first().session.let { it.spaceName ?: it.folderName },
        members.sortedWith(compareBy<AgentSession> { it.session.slotIndex ?: 0 }.thenBy { it.session.sessionId })) }
    .sortedWith(compareBy<AgentSpace> { it.priority }.thenByDescending { it.updatedAt }.thenBy { it.id })

fun AgentPreferences.togglePin(id: String, space: Boolean = false): AgentPreferences {
    fun List<String>.toggle() = if (id in this) filterNot { it == id } else this + id
    return if (space) copy(spaces = spaces.toggle()) else copy(sessions = sessions.toggle())
}

/** Same rule as iOS: muting inside a muted space is the agent's own mute, so it survives unmuting the space. */
fun AgentPreferences.toggleMute(id: String, spaceKey: String?): AgentPreferences {
    val rest = copy(sessions = sessions.filterNot { it == id }, allowed = allowed - id)
    return when {
        !mutes(id, spaceKey) -> rest.copy(sessions = rest.sessions + id)
        spaceKey != null && spaceKey in spaces -> rest.copy(allowed = rest.allowed + id)
        else -> rest
    }
}
