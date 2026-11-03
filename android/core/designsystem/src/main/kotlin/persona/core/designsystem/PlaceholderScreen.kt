package persona.core.designsystem

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier

@Composable
fun PlaceholderScreen(title: String, description: String = "功能正在准备中") {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { JEmptyState(title, description) }
}
