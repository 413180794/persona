package persona.android

import android.content.Context
import android.content.Intent

// Shares one component with the pre-upgrade launcher and PendingIntents, so opening the new class directly does not start a second task stack.
internal fun mainActivityIntent(context: Context): Intent =
    Intent().setClassName(context, "jarvis.android.MainActivity")
