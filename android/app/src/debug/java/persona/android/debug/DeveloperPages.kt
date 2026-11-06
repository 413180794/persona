package persona.android.debug

import androidx.compose.foundation.layout.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import persona.android.navigation.DeveloperTools
import persona.core.designsystem.catalog.ComponentCatalogScreen
import persona.core.navigation.Navigator
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable @SerialName("persona.android.debug.ComponentCatalogRoute") data object ComponentCatalogRoute : NavKey

internal object DeveloperPages : DeveloperTools {
    override val home: NavKey = ComponentCatalogRoute
    override val homeTitle = "组件目录（调试版）"  // i18n-ignore: debug-only developer tool

    override fun EntryProviderScope<NavKey>.register(navigator: Navigator) {
        entry<ComponentCatalogRoute> { CatalogPage(navigator) }
    }
}

/** The catalog is a page of the active style; back returns to settings. */
@Composable
private fun CatalogPage(navigator: Navigator) {
    ComponentCatalogScreen(Modifier.fillMaxSize(), onBack = navigator::back)
}
