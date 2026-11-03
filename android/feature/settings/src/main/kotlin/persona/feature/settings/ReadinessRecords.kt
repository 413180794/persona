package persona.feature.settings

import android.content.Context

/** The app must keep the onboarding route visible while this flag is set. */
fun needsOnboardingReadiness(context: Context): Boolean =
    context.getSharedPreferences("settings-readiness", Context.MODE_PRIVATE)
        .getBoolean("onboarding-pending", false)

internal class ReadinessRecords(context: Context) {
    private val preferences = context.getSharedPreferences("settings-readiness", Context.MODE_PRIVATE)

    fun pending(value: Boolean) { preferences.edit().putBoolean("onboarding-pending", value).apply() }
    fun decision(id: String): String? = preferences.getString("decision:$id", null)
    fun record(id: String, decision: String) {
        preferences.edit().putString("decision:$id", decision)
            .putLong("decision-at:$id", System.currentTimeMillis()).apply()
    }
    fun skipped(id: String): Boolean = decision(id) == "skipped"
}
