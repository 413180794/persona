package persona.core.contracts

/**
 * How the merged lists tell hub groups apart (docs/android/android.md "Multiple hubs"). A group is the set of paired hubs linked by
 * the hub mesh; separate groups never see each other's machines. Every group keeps its own data and connection, and
 * speaks plain machine ids with its hubs. Above that layer the app shows all groups in one list: machines of the
 * primary group keep their ids, every other group's machine ids carry `~<group id>`. The suffix never depends on
 * which group is selected for hub-level pages, so routes, deep links, notifications and stored keys stay valid; with
 * a single (primary) group every id and key is byte for byte what it was before groups existed.
 */
object GroupKeys {
    const val SEPARATOR = "~"
    private val groupId = Regex("g[0-9a-f]{8}")

    /** Whether [id] has the shape of a locally generated group id (`g` and eight hex digits). */
    fun isGroupId(id: String): Boolean = groupId.matches(id)

    /** [raw] as the merged lists show it: unchanged when [namespace] is null (the primary group). */
    fun machine(raw: String, namespace: String?): String =
        if (namespace == null || raw.isEmpty()) raw else "$raw$SEPARATOR$namespace"

    /** The group suffix of [machineId]; null for the primary group's ids and anything that only looks similar. */
    fun namespaceOf(machineId: String): String? {
        val at = machineId.lastIndexOf(SEPARATOR)
        if (at <= 0) return null
        return machineId.substring(at + 1).takeIf(::isGroupId)
    }

    /** The id the group's own hubs use. */
    fun raw(machineId: String): String =
        namespaceOf(machineId)?.let { machineId.substring(0, machineId.length - it.length - SEPARATOR.length) } ?: machineId

    fun key(key: SessionKey, namespace: String?): SessionKey =
        if (namespace == null || key.machineId.isEmpty()) key else SessionKey(machine(key.machineId, namespace), key.sessionId)

    fun rawKey(key: SessionKey): SessionKey =
        if (namespaceOf(key.machineId) == null) key else SessionKey(raw(key.machineId), key.sessionId)

    /** Preference paths are `machine/…` (a session or a space); only the machine part carries the suffix. */
    fun path(path: String, namespace: String?): String {
        if (namespace == null) return path
        val slash = path.indexOf('/')
        return if (slash <= 0) machine(path, namespace) else machine(path.substring(0, slash), namespace) + path.substring(slash)
    }

    fun rawPath(path: String): String {
        val slash = path.indexOf('/')
        return if (slash <= 0) raw(path) else raw(path.substring(0, slash)) + path.substring(slash)
    }

    /** The suffix of a preference path's machine part. */
    fun pathNamespace(path: String): String? = namespaceOf(path.substringBefore('/'))
}
