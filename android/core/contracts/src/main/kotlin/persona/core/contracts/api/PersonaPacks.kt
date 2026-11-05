package persona.core.contracts.api

import persona.core.contracts.CoreTextException
import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import kotlinx.serialization.Serializable

/**
 * The persona packages on the hub (docs/persona.md「包格式」): GET /api/v1/personas lists the ones that check out,
 * and each file of a package comes from GET /api/v1/personas/<id>/<file>.
 */
interface PersonaPacks {
    /** Throws [PersonaPacksUnsupported] when the hub has no such route (an older hub). */
    suspend fun list(): List<PersonaInfo>
    suspend fun file(id: String, file: String): ByteArray
}

/** The hub predates persona packages (404 on the list). */
class PersonaPacksUnsupported : CoreTextException(CoreText.of(Key.PersonaPacksUnsupported))

/** The body of GET /api/v1/personas. */
@Serializable
data class PersonaListing(val personas: List<PersonaInfo> = emptyList())

/** One entry of GET /api/v1/personas: the manifest and a rev that changes with any byte of the package. */
@Serializable
data class PersonaInfo(val id: String, val rev: String, val bytes: Long = 0, val manifest: PersonaManifest) {
    /** Usable on the phone: the manifest checks out and is this entry's, and the rev is the hub's 16 hex digits (it names a directory). */
    val usable: Boolean get() = manifest.problem == null && manifest.id == id && REV.matches(rev)

    private companion object { val REV = Regex("^[0-9a-f]{16}$") }
}

/** persona.json, format 1. Colors are not read on Android (no 主题色「跟随形象」 here). */
@Serializable
data class PersonaManifest(
    val format: Int,
    val id: String,
    val name: String,
    val subtitle: String? = null,
    /** A few words for the look's card in 设置 › 外观 › 形象, before the subtitle (奶蛙: where its level comes from). */
    val about: String? = null,
    val avatar: Avatar,
    val dot: Dot,
) {
    @Serializable data class Avatar(val sizes: Map<String, String>)
    @Serializable data class Dot(val size: Int? = null, val states: Map<String, DotState>)
    @Serializable data class DotState(val file: String, val fps: Double? = null)

    /** Every file it names: what the phone downloads. */
    val files: List<String> get() = (avatar.sizes.values + dot.states.values.map { it.file }).toSortedSet().toList()

    /** The smallest avatar at least [pixels] wide, else the largest. */
    fun avatarFile(pixels: Int): String? {
        val sizes = avatar.sizes.mapNotNull { (k, v) -> k.toIntOrNull()?.let { it to v } }.sortedBy { it.first }
        return (sizes.firstOrNull { it.first >= pixels } ?: sizes.lastOrNull())?.second
    }

    /** The state's frames (by its wire name), idle's for a state the package lacks. */
    fun dotState(state: String): DotState? = dot.states[state] ?: dot.states["idle"]

    /** What the hub checks too (server/internal/hub/persona.go), so a package it would leave out is refused here. */
    val problem: String? get() = when {
        format != FORMAT -> "format $format"
        id in BUILT_IN || !ID.matches(id) -> "id $id"
        name.isBlank() -> "no name"
        avatar.sizes.isEmpty() -> "no avatar"
        "idle" !in dot.states -> "no idle dot"
        else -> files.firstOrNull { f -> f.isEmpty() || f.startsWith("/") || f.split('/').any { it == ".." || it.startsWith(".") } }
            ?.let { "file $it" }
    }

    companion object {
        const val FORMAT = 1
        /** The built-in faces' ids; a package may not take them. */
        val BUILT_IN = setOf("persona", "jarvis", "jarvis-circuit")
        private val ID = Regex("^[a-z0-9][a-z0-9-]{0,31}$")
    }
}
