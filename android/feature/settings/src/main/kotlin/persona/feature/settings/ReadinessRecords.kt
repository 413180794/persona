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

    /**
     * The machine whose engine pairing still has to ask about (docs/integrations/engines.md): set when pairing finds one, cleared
     * once the hub took a choice, so a restarted app shows the step again instead of skipping it.
     */
    fun enginePending(): String? = preferences.getString("engine-pending", null)?.takeIf { it.isNotBlank() }
    fun enginePending(machineId: String?) {
        preferences.edit().apply { if (machineId == null) remove("engine-pending") else putString("engine-pending", machineId) }.apply()
    }

    /**
     * Set from saving a pairing's credentials until its next step (the engine or the checklist) is chosen. Saving
     * replaces the pairing and, with it, the shell that a pairing page in Settings sits in: the root's pairing page
     * that comes up instead sees the mark and takes the steps over.
     */
    fun finishing(): Boolean = preferences.getBoolean("pairing-finishing", false)
    fun finishing(value: Boolean) { preferences.edit().putBoolean("pairing-finishing", value).apply() }
}
