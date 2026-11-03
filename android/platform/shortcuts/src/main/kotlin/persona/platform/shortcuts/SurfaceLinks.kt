package persona.platform.shortcuts

import android.content.Context
import android.content.Intent
import android.net.Uri
import persona.core.contracts.FleetState
import persona.core.contracts.SessionKey
import persona.core.domain.fleet.FleetSummary
import persona.core.navigation.AppTab
import persona.core.navigation.AskPersonaLink
import persona.core.navigation.NotificationRoute

/**
 * Where the widget, the tile and the shortcuts take the user: `jarvis://open` links handled by the app's own activity
 * exactly like a notification's (the same Router), cold start or not.
 */
internal object SurfaceLinks {
    /** The app's launcher activity, made explicit: a shortcut's intent has to name the activity it starts. */
    private fun view(context: Context, uri: Uri): Intent = Intent(Intent.ACTION_VIEW, uri).apply {
        context.packageManager.getLaunchIntentForPackage(context.packageName)?.component?.let { component = it }
            ?: setPackage(context.packageName)
        addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
    }

    /** An agent's conversation; [assistant] picks the tab without waiting for session metadata when it is known. */
    fun conversation(context: Context, key: SessionKey, assistant: Boolean?): Intent =
        view(context, NotificationRoute.conversation(key.machineId, key.sessionId, assistant).uri())

    fun agents(context: Context): Intent = view(context, NotificationRoute(AppTab.Agents).uri())

    /** The Persona tab with the cursor in its composer (「问 Persona」). */
    fun ask(context: Context): Intent = view(context, Uri.parse(AskPersonaLink))

    /** The app as the launcher opens it (the pairing page when not paired). */
    fun app(context: Context): Intent = context.packageManager.getLaunchIntentForPackage(context.packageName)
        ?.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK) ?: Intent().setPackage(context.packageName)

    /** A tap on the whole widget or the tile: the only agent when there is one, otherwise Agents. */
    fun fleet(context: Context, paired: Boolean, fleet: FleetState, assistant: (SessionKey) -> Boolean?): Intent = when {
        !paired -> app(context)
        else -> FleetSummary.single(fleet)?.let { conversation(context, it, assistant(it)) } ?: agents(context)
    }
}
