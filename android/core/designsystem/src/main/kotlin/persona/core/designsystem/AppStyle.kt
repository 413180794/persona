package persona.core.designsystem

import android.content.Context
import android.os.Build
import android.os.PowerManager
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.staticCompositionLocalOf
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * The three interface styles (设置 › 界面风格). Pages are written once against the components in this module;
 * each style supplies its own tokens, component skins and app chrome. Light and dark still follow the system.
 */
enum class AppStyle(val key: String, val label: String, val summary: String) {
    Glass("glass", "玻璃", "磨砂玻璃浮在状态光上，有事等你时泛起琥珀色"),
    Brief("brief", "晨报", "白纸墨字，一支荧光笔只标 Persona 的建议和等你的事"),
    Board("board", "看板", "每个 Agent 一条进程单，状态色一眼看清");

    companion object {
        val Default = Glass
        fun fromKey(key: String?): AppStyle = entries.firstOrNull { it.key == key } ?: Default
    }
}

/** What the user asked for in the glass style; the device and the frame monitor can still lower it. */
enum class GlassPreference(val key: String, val label: String) {
    Auto("auto", "自动"), Reduced("reduced", "减少透明");

    companion object { fun fromKey(key: String?) = entries.firstOrNull { it.key == key } ?: Auto }
}

/** How glass is drawn: refraction (API 33+), plain blur, or opaque tinted panels. */
enum class GlassLevel { Liquid, Frosted, Solid }

/** Device-wide appearance choices; not tied to a hub, so they survive re-pairing. */
class AppStyleStore(context: Context) {
    private val app = context.applicationContext
    private val preferences = app.getSharedPreferences("jarvis-appearance", Context.MODE_PRIVATE)
    private val currentStyle = MutableStateFlow(AppStyle.fromKey(preferences.getString(KEY_STYLE, null)))
    private val currentGlass = MutableStateFlow(GlassPreference.fromKey(preferences.getString(KEY_GLASS, null)))
    val style: StateFlow<AppStyle> = currentStyle.asStateFlow()
    val glass: StateFlow<GlassPreference> = currentGlass.asStateFlow()
    private val currentPersona = MutableStateFlow(preferences.getString(KEY_PERSONA, null) ?: PersonaDesign.Default.id)
    /** 设置 › 外观 › 形象: a built-in's id or a hub package's ([PersonaStore]), kept while the package is away. */
    val personaId: StateFlow<String> = currentPersona.asStateFlow()
    private val currentFace = MutableStateFlow(PersonaDesign.fromId(currentPersona.value))
    /** The built-in face to draw: the chosen one, or the default while a package is chosen (it shows over this). */
    val face: StateFlow<PersonaDesign> = currentFace.asStateFlow()

    init { ActiveStyle.value = currentStyle.value }

    fun setStyle(style: AppStyle) {
        preferences.edit().putString(KEY_STYLE, style.key).apply()
        currentStyle.value = style
        ActiveStyle.value = style
    }

    fun setGlass(preference: GlassPreference) {
        preferences.edit().putString(KEY_GLASS, preference.key).apply()
        currentGlass.value = preference
    }

    fun setFace(design: PersonaDesign) = setPersonaId(design.id)

    fun setPersonaId(id: String) {
        preferences.edit().putString(KEY_PERSONA, id).apply()
        currentPersona.value = id
        currentFace.value = PersonaDesign.fromId(id)
    }

    /** The level glass actually renders at, before any runtime downgrade. */
    fun deviceLevel(preference: GlassPreference): GlassLevel {
        val power = app.getSystemService(PowerManager::class.java)
        return when {
            preference == GlassPreference.Reduced || power?.isPowerSaveMode == true -> GlassLevel.Solid
            Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU -> GlassLevel.Liquid
            else -> GlassLevel.Frosted
        }
    }

    private companion object {
        const val KEY_STYLE = "style"
        const val KEY_GLASS = "glass"
        const val KEY_PERSONA = "persona"
    }
}

val LocalAppStyleStore = staticCompositionLocalOf<AppStyleStore?> { null }

/**
 * Lowered at runtime when glass makes scrolling miss frames (see the app's frame monitor). Session-only: a new
 * launch tries the device level again.
 */
object GlassThrottle {
    val ceiling = mutableStateOf(GlassLevel.Liquid)

    fun lower() {
        ceiling.value = when (ceiling.value) {
            GlassLevel.Liquid -> GlassLevel.Frosted
            else -> GlassLevel.Solid
        }
    }
}

/**
 * The style of the root theme, for the legacy helpers ([PersonaTypography], [statusTone]) that are not composable.
 * Reading it inside composition still subscribes, so a style switch recomposes their callers.
 */
internal val ActiveStyle = mutableStateOf(AppStyle.Default)
