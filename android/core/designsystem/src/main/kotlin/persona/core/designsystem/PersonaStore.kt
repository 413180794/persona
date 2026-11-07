package persona.core.designsystem

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.util.LruCache
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.staticCompositionLocalOf
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.LookCurrent
import persona.core.contracts.api.PersonaInfo
import persona.core.contracts.api.PersonaPacks
import persona.core.contracts.api.PersonaPacksUnsupported
import java.io.File

/**
 * The hub's persona packages on this phone (docs/persona.md "On the phone"), as on iOS: what the hub offers (kept across
 * launches in index.json), the packages downloaded, one directory per rev under files/personas/<id>/<rev>, and a
 * thumbnail each for Settings' cards. A package is used only once all of it is there (it is downloaded beside and moved
 * in whole); the chosen one shows from disk at launch, before the hub answers or without it. The choice itself is
 * [AppStyleStore.personaId], kept when its package leaves the hub, so it comes back with it.
 *
 * A hub that keeps the choice (GET /api/v1/looks, #3200) is the one truth of it: its choice wins over this phone's,
 * this phone's old choice moves up once when the hub has none, a choice made here goes to the hub, and another
 * device's arrives as a `look` push ([follow]). [AppStyleStore.personaId] is then this phone's copy of it.
 */
class PersonaStore(context: Context, private val style: AppStyleStore, private val packs: () -> PersonaPacks?) {
    private val app = context.applicationContext
    private val root = File(app.filesDir, "personas")
    private val index = File(root, "index.json")
    private val thumbnails = File(root, ".thumbnails")
    private val lock = Mutex()
    private val frameCache = LruCache<String, PersonaFrames>(6)

    private val _offered = MutableStateFlow(readIndex())
    /** The hub's packages that check out, as last listed. */
    val offered: StateFlow<List<PersonaInfo>> = _offered.asStateFlow()
    private val _current = MutableStateFlow(_offered.value.firstOrNull { it.id == style.personaId.value && onPhone(it) })
    /** The chosen package when it is on the phone; null shows the built-in ([AppStyleStore.face]). */
    val current: StateFlow<PersonaInfo?> = _current.asStateFlow()
    private val _downloading = MutableStateFlow(emptySet<String>())
    val downloading: StateFlow<Set<String>> = _downloading.asStateFlow()
    private val _error = MutableStateFlow<String?>(null)
    /** Why the last list or download failed (Settings › Persona look shows it). */
    val error: StateFlow<String?> = _error.asStateFlow()
    private val _pictures = MutableStateFlow(0)
    /** Bumped when pictures land on disk: the views that asked for one before ask again. */
    val pictures: StateFlow<Int> = _pictures.asStateFlow()

    /** Whether the hub keeps the choice (it answered GET /api/v1/looks): choices go there. */
    @Volatile private var synced = false
    private val background = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private fun directory(p: PersonaInfo) = File(File(root, p.id), p.rev)
    fun onPhone(p: PersonaInfo) = directory(p).isDirectory
    private fun thumbnail(p: PersonaInfo) = File(thumbnails, "${p.id}-${p.rev}.png")

    private fun readIndex(): List<PersonaInfo> = runCatching {
        JsonCodec.decodeFromString<List<PersonaInfo>>(index.readText()).filter { it.usable }
    }.getOrDefault(emptyList())

    /** Lists the hub's packages; the chosen one is downloaded when its rev is new, and revs nobody uses go. */
    suspend fun refresh() = lock.withLock {
        val api = packs() ?: return@withLock
        val list = try {
            val looks = try { api.looks() } catch (e: PersonaPacksUnsupported) { null }
            synced = looks != null
            if (looks != null) {
                settle(looks.current, api)
                looks.looks.mapNotNull { it.pack }.filter { it.usable }
            } else api.list().filter { it.usable }
        } catch (e: CancellationException) { throw e } catch (e: PersonaPacksUnsupported) {
            _error.value = e.message; return@withLock
        } catch (e: Exception) {
            _error.value = app.getString(R.string.designsystem_avatars_list_failed, e.message ?: e::class.simpleName); return@withLock
        }
        _error.value = null
        _offered.value = list
        withContext(Dispatchers.IO) {
            root.mkdirs()
            File(root, ".index.json.part").apply { writeText(JsonCodec.encodeToString(list)); renameTo(index) }
        }
        val chosen = list.firstOrNull { it.id == style.personaId.value }
        if (chosen != null && !onPhone(chosen)) download(chosen, api)
        // Re-read the choice: a built-in picked during the download wins. A new rev that did not come keeps the old
        // one on screen (prune keeps what is shown); a package gone from the hub shows the built-in, the choice kept.
        val id = style.personaId.value
        _current.value = when {
            chosen == null || chosen.id != id -> null
            onPhone(chosen) -> chosen
            else -> _current.value?.takeIf { it.id == id && onPhone(it) }
        }
        // The others' faces for Settings' cards, one small picture each (the package itself comes when chosen).
        for (p in list) if (!onPhone(p) && !thumbnail(p).exists()) {
            val file = p.manifest.avatarFile(THUMBNAIL_PX) ?: continue
            val bytes = try { api.file(p.id, file) } catch (e: CancellationException) { throw e } catch (_: Exception) { continue }
            withContext(Dispatchers.IO) { thumbnails.mkdirs(); writeWhole(thumbnail(p), bytes) }
            _pictures.value++
        }
        prune(list)
    }

    /** The choice to keep given the hub's: the hub's once it has one; else this phone's moves up, once, if unset there. */
    private suspend fun settle(current: LookCurrent, api: PersonaPacks) {
        val id = current.id ?: if (style.personaChosen) {
            runCatching { api.choose(style.personaId.value, ifUnset = true).id }.getOrNull()
        } else null
        if (id != null && id != style.personaId.value) {
            choices++
            style.setPersonaId(id)
        }
    }

    /** Picks a package, downloading it first; on failure the choice stays as it was and [error] says why. */
    suspend fun choose(p: PersonaInfo) {
        if (!p.usable) return
        val asked = ++choices
        lock.withLock {
            if (!onPhone(p)) download(p, packs() ?: return@withLock)
            // Another choice made while this one downloaded wins.
            if (!onPhone(p) || asked != choices) return@withLock
            style.setPersonaId(p.id)
            _current.value = p
        }
        tellHub(p.id)
    }

    /** Picks a built-in face at once (a download under way for an earlier choice is then not applied). */
    fun choose(design: PersonaDesign) {
        choices++
        style.setFace(design)
        _current.value = null
        tellHub(design.id)
    }

    /** Sends a choice made here to a hub that keeps it, so every device follows; when that fails it stays this phone's. */
    private fun tellHub(id: String) {
        if (!synced) return
        val api = packs() ?: return
        background.launch { runCatching { api.choose(id) } }
    }

    /** Another device chose [id] (the hub's `look` push): show it here, downloading its package first. */
    suspend fun follow(id: String) {
        val restored = restoredPersonaId(id)
        if (restored == style.personaId.value) return
        val design = PersonaDesign.entries.firstOrNull { it.id == restored }
        if (design != null) {
            choices++
            style.setFace(design)
            _current.value = null
            return
        }
        val asked = ++choices
        style.setPersonaId(restored)
        _current.value = null
        val p = _offered.value.firstOrNull { it.id == restored } ?: run { refresh(); return }
        lock.withLock {
            if (!onPhone(p)) download(p, packs() ?: return@withLock)
            if (asked == choices && onPhone(p)) _current.value = p
        }
    }

    /** Counts choices, so a download finishing after a later choice does not undo it. */
    @Volatile private var choices = 0

    private suspend fun download(p: PersonaInfo, api: PersonaPacks) {
        _downloading.value += p.id
        val part = File(File(root, p.id), ".${p.rev}.part")
        try {
            withContext(Dispatchers.IO) { part.deleteRecursively() }
            for (file in p.manifest.files) {
                val bytes = api.file(p.id, file)
                withContext(Dispatchers.IO) { File(part, file).apply { parentFile?.mkdirs() }.writeBytes(bytes) }
            }
            withContext(Dispatchers.IO) {
                p.manifest.files.firstOrNull { !File(part, it).isFile }?.let { error(app.getString(R.string.designsystem_avatars_missing_file, it)) }
                directory(p).deleteRecursively()
                check(part.renameTo(directory(p))) { app.getString(R.string.designsystem_avatars_not_saved) }
            }
            _pictures.value++
            _error.value = null
        } catch (e: CancellationException) {
            withContext(Dispatchers.IO + kotlinx.coroutines.NonCancellable) { part.deleteRecursively() }; throw e
        } catch (e: Exception) {
            withContext(Dispatchers.IO) { part.deleteRecursively() }
            _error.value = app.getString(R.string.designsystem_avatars_download_failed, p.manifest.name, e.message ?: e::class.simpleName)
        } finally {
            _downloading.value -= p.id
        }
    }

    /** Removes the revs and thumbnails that are neither offered nor on screen. */
    private suspend fun prune(list: List<PersonaInfo>) = withContext(Dispatchers.IO) {
        val keep = list.map { "${it.id}/${it.rev}" }.toSet() + listOfNotNull(_current.value?.let { "${it.id}/${it.rev}" })
        thumbnails.listFiles()?.forEach { t -> if (keep.none { t.name == it.replace('/', '-') + ".png" }) t.delete() }
        root.listFiles()?.filter { it.isDirectory && !it.name.startsWith(".") }?.forEach { id ->
            id.listFiles()?.filter { !it.name.startsWith(".") && "${id.name}/${it.name}" !in keep }?.forEach { it.deleteRecursively() }
            if (id.list().isNullOrEmpty()) id.delete()
        }
    }

    /** The state's frames (by its wire name), decoded once and kept for the states last shown. */
    suspend fun frames(p: PersonaInfo, state: PersonaState): PersonaFrames? {
        val spec = p.manifest.dotState(state.wire) ?: return null
        val file = File(directory(p), spec.file)
        frameCache.get(file.path)?.let { return it }
        return withContext(Dispatchers.Default) {
            runCatching { decodeApng(file.readBytes(), spec.fps) }.getOrNull()
        }?.also { frameCache.put(file.path, it) }
    }

    /** The face [pixels] wide (its dark one when [dark]), from the package, or its thumbnail while the package is not on the phone. */
    suspend fun avatar(p: PersonaInfo, pixels: Int, dark: Boolean = false): Bitmap? = withContext(Dispatchers.IO) {
        val file = if (onPhone(p)) p.manifest.avatarFile(pixels, dark)?.let { File(directory(p), it) } else thumbnail(p)
        file?.takeIf { it.isFile }?.let { BitmapFactory.decodeFile(it.path) }
    }

    private fun writeWhole(file: File, bytes: ByteArray) {
        val part = File(file.parentFile, ".${file.name}.part")
        part.writeBytes(bytes)
        part.renameTo(file)
    }

    companion object {
        /** Settings' cards show faces 64 dp wide, at up to 3 pixels a dp. */
        const val THUMBNAIL_PX = 192
    }
}

val LocalPersonaStore = staticCompositionLocalOf<PersonaStore?> { null }

/** The chosen hub package when it is on the phone; [PersonaFace] plays its frames instead of drawing a built-in. */
val LocalPersonaPack = compositionLocalOf<PersonaInfo?> { null }

/** Persona's name: the chosen package's, else the product's. */
val LocalPersonaName = compositionLocalOf { "Persona" }
