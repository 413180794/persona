package persona.feature.settings

import androidx.compose.runtime.*
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import persona.core.contracts.api.*
import persona.core.designsystem.*

internal data class ExtensionDeclarations(
    val prompts: List<ExtensionQuickPrompt> = emptyList(),
    val themes: List<ExtensionTheme> = emptyList(),
    val templates: List<ExtensionTriageTemplate> = emptyList(),
    val themesLoaded: Boolean = false,
)

internal class ExtensionDeclarationsModel(private val api: ExtensionManagement) {
    private val lock = Mutex()
    private val mutable = MutableStateFlow(ExtensionDeclarations())
    val state = mutable.asStateFlow()

    suspend fun load() = lock.withLock {
        try { val list = api.quickPrompts(); mutable.update { it.copy(prompts = list) } }
        catch (e: CancellationException) { throw e } catch (_: Exception) { }
        try {
            val list = api.themes().filter { ExtensionThemeColors.problems(it).isEmpty() }
            mutable.update { it.copy(themes = list, themesLoaded = true) }
        } catch (e: CancellationException) { throw e } catch (_: Exception) { }
        try { val list = api.triageTemplates(); mutable.update { it.copy(templates = list) } }
        catch (e: CancellationException) { throw e } catch (_: Exception) { }
    }
}

@Composable
fun ExtensionDeclarationsHost(api: ExtensionManagement?, identity: String, content: @Composable () -> Unit) {
    val model = remember(api, identity) { api?.let(::ExtensionDeclarationsModel) }
    val declarations = model?.state?.collectAsState()?.value ?: ExtensionDeclarations()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val store = LocalAppStyleStore.current
    val selected = store?.themeKey?.collectAsState()?.value
    LaunchedEffect(model, lifecycle) {
        if (model != null) lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            launch { while (true) { model.load(); delay(RefreshMillis) } }
            launch { api?.changes?.collect { model.load() } }
        }
    }
    val theme = ExtensionThemeColors.selected(identity, selected, declarations.themes)
    LaunchedEffect(identity, declarations.themes, declarations.themesLoaded, selected) {
        if (declarations.themesLoaded && selected?.startsWith("$identity|") == true && theme == null) store?.setTheme(null)
    }
    CompositionLocalProvider(
        LocalExtensionPrompts provides declarations.prompts,
        LocalExtensionTriageTemplates provides declarations.templates,
        LocalExtensionThemeOptions provides declarations.themes,
        LocalExtensionThemeScope provides identity,
        LocalExtensionRoleTheme provides theme,
    ) {
        PersonaTheme(darkTheme = PersonaTheme.darkTheme, reduceMotion = PersonaTheme.reduceMotion, content = content)
    }
}

private const val RefreshMillis = 30_000L
