package persona.android.ui

import persona.android.R
import androidx.compose.ui.res.stringResource
import androidx.compose.runtime.Composable
import persona.core.contracts.api.ConnectionState

internal typealias FeedbackGate = persona.core.designsystem.FeedbackGate

@Composable
internal fun ConnectionState.title(): String? = when (this) {
    ConnectionState.Online -> null
    ConnectionState.Connecting -> R.string.app_connection_connecting
    ConnectionState.Updating -> R.string.app_connection_updating
    ConnectionState.WaitingForNetwork -> R.string.app_connection_waiting_network
    ConnectionState.NeedsLocalNetworkPermission -> R.string.app_connection_local_network
    ConnectionState.CertificateMismatch -> R.string.app_connection_certificate
    ConnectionState.Revoked -> R.string.app_connection_revoked
}?.let { stringResource(it) }
