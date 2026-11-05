package persona.feature.inbox

import androidx.compose.foundation.clickable
import persona.core.navigation.ConnectionSettingsRoute
import persona.core.designsystem.PersonaTheme
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.LocalContentColor
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.api.ConnectionState
import persona.core.designsystem.ConnectionTitle
import persona.core.designsystem.rememberConnectionDetails

@Composable
internal fun InboxConnectionTitle(repository: InboxRepository, title: String, modifier: Modifier = Modifier) {
    val connection by repository.connectionState.collectAsStateWithLifecycle(ConnectionState.Connecting)
    val navigator = LocalInboxNavigator.current
    val details = rememberConnectionDetails(connection, navigator?.let { nav -> { nav.navigate(ConnectionSettingsRoute) } })
    val action = if (connection != ConnectionState.Online) Modifier.clickable(onClick = details) else Modifier
    CompositionLocalProvider(LocalTextStyle provides PersonaTheme.type.headline, LocalContentColor provides PersonaTheme.palette.ink) {
        ConnectionTitle(if (connection == ConnectionState.Online) title else inboxTitle(connection),
        connection in setOf(ConnectionState.Connecting, ConnectionState.Updating), modifier.then(action))
    }
}
