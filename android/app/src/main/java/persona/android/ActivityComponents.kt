package persona.android

import android.content.Context
import android.content.Intent

// All app entry points use the canonical Activity component.
internal fun mainActivityIntent(context: Context): Intent = Intent(context, MainActivity::class.java)
