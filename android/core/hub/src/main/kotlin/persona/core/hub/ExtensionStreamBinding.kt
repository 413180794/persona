package persona.core.hub

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.*
import persona.core.contracts.Hello
import persona.core.contracts.api.ExtensionStreamBinding

/** Hello events are connection generations; an equal feature set still retries a failed roster lookup. */
@OptIn(ExperimentalCoroutinesApi::class)
fun extensionStreamBindings(connection: OkHttpHubConnection, identity: () -> Pair<String, String>): Flow<ExtensionStreamBinding?> =
    connection.events.filterIsInstance<Hello>().onStart { connection.lastHello?.let { emit(it) } }.mapLatest {
        val (owner, group) = identity()
        resolveExtensionStreamBinding(connection, owner, group)
    }

/** Uses the existing manager roster, never an invented stream-discovery API or a default ID literal. */
suspend fun resolveExtensionStreamBinding(connection: OkHttpHubConnection, owner: String, group: String): ExtensionStreamBinding? {
    val hello = connection.lastHello ?: return null
    if ("extension_ui_stream_v1" !in hello.features || "extension_ui_stream_v1" !in connection.serverFeatures.value) return null
    return try {
        HttpHubManagers(connection).roster().defaultId.takeIf { it.isNotBlank() && connection.lastHello === hello }
            ?.let { ExtensionStreamBinding(connection, owner, group, it, approvals = HubChatTransport(connection)) }
    } catch (cancelled: CancellationException) { throw cancelled }
    catch (_: Exception) { null }
}
