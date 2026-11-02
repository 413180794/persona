package jarvis.android.debug

import androidx.compose.foundation.layout.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import jarvis.android.navigation.DeveloperTools
import jarvis.core.designsystem.catalog.ComponentCatalogScreen
import jarvis.core.navigation.Navigator
import kotlinx.serialization.Serializable

@Serializable data object ComponentCatalogRoute : NavKey

internal object DeveloperPages : DeveloperTools {
    override val home: NavKey = ComponentCatalogRoute
    override val homeTitle = "组件目录（调试版）"

    override fun EntryProviderScope<NavKey>.register(navigator: Navigator) {
        entry<ComponentCatalogRoute> { CatalogPage(navigator) }
    }
}

/** The catalog is a page of the active style; back returns to settings. */
@Composable
private fun CatalogPage(navigator: Navigator) {
    ComponentCatalogScreen(Modifier.fillMaxSize(), onBack = navigator::back)
}
