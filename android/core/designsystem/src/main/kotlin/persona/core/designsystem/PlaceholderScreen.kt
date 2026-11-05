package persona.core.designsystem

import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier

@Composable
fun PlaceholderScreen(title: String, description: String = stringResource(R.string.designsystem_placeholder)) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { JEmptyState(title, description) }
}
