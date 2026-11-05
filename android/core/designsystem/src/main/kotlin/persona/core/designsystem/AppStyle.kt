package persona.core.designsystem

import android.content.Context
import android.os.Build
import android.os.PowerManager
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * The three interface styles (设置 › 界面风格). Pages are written once against the components in this module;
 * each style supplies its own tokens, component skins and app chrome. Light and dark follow the system unless
 * 深浅色 ([ColorSchemeChoice]) pins one; the accent can be another 主题色 ([ThemeColor]).
 */
enum class AppStyle(val key: String, @StringRes val labelRes: Int, @StringRes val summaryRes: Int) {
    Glass("glass", R.string.designsystem_style_glass, R.string.designsystem_style_glass_summary),
    Brief("brief", R.string.designsystem_style_brief, R.string.designsystem_style_brief_summary),
    Board("board", R.string.designsystem_style_board, R.string.designsystem_style_board_summary);

    val label: String @Composable get() = stringResource(labelRes)
    val summary: String @Composable get() = stringResource(summaryRes)

    companion object {
        val Default = Glass
        fun fromKey(key: String?): AppStyle = entries.firstOrNull { it.key == key } ?: Default
    }
}

/** What the user asked for in the glass style; the device and the frame monitor can still lower it. */
enum class GlassPreference(val key: String, @StringRes val labelRes: Int) {
    Auto("auto", R.string.designsystem_glass_auto), Reduced("reduced", R.string.designsystem_glass_reduced);

    val label: String @Composable get() = stringResource(labelRes)

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
    private val currentTheme = MutableStateFlow(preferences.getString(KEY_THEME, null))
    val themeKey: StateFlow<String?> = currentTheme.asStateFlow()
    fun setTheme(key: String?) {
        preferences.edit().putString(KEY_THEME, key).apply()
        currentTheme.value = key
    }
    private val currentPersona = MutableStateFlow(preferences.getString(KEY_PERSONA, null) ?: PersonaDesign.Default.id)
    /** 设置 › 外观 › 形象: a built-in's id or a hub package's ([PersonaStore]), kept while the package is away. */
    val personaId: StateFlow<String> = currentPersona.asStateFlow()
    private val currentFace = MutableStateFlow(PersonaDesign.fromId(currentPersona.value))
    /** The built-in face to draw: the chosen one, or the default while a package is chosen (it shows over this). */
    val face: StateFlow<PersonaDesign> = currentFace.asStateFlow()

    private val currentScheme = MutableStateFlow(ColorSchemeChoice.fromKey(preferences.getString(KEY_SCHEME, null)))
    /** 设置 › 外观 › 深浅色: the system's (the default), or always light or dark. */
    val scheme: StateFlow<ColorSchemeChoice> = currentScheme.asStateFlow()
    private val currentColor = MutableStateFlow(ThemeColor.fromKey(preferences.getString(KEY_COLOR, null)))
    /** 设置 › 外观 › 主题色: the style's own (the default), the persona's, or a fixed accent. */
    val color: StateFlow<ThemeColor> = currentColor.asStateFlow()

    init {
        ActiveStyle.value = currentStyle.value
        AppAppearance.scheme = currentScheme.value
    }

    fun setScheme(choice: ColorSchemeChoice) {
        preferences.edit().putString(KEY_SCHEME, choice.key).apply()
        currentScheme.value = choice
        AppAppearance.scheme = choice
    }

    fun setColor(choice: ThemeColor) {
        preferences.edit().putString(KEY_COLOR, choice.key).apply()
        currentColor.value = choice
    }

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

    /** Whether this phone chose a look itself (the key is written only by a choice): what moves up to a hub that keeps the choice. */
    val personaChosen: Boolean get() = preferences.contains(KEY_PERSONA)

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
        const val KEY_THEME = "extension-theme"
        const val KEY_SCHEME = "scheme"
        const val KEY_COLOR = "color"
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
