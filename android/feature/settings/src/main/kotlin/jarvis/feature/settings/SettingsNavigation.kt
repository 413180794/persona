package jarvis.feature.settings

import jarvis.core.navigation.SettingsPage

/** Stable page names shared by standalone settings and the navigation shell. */
fun SettingsPage.toInitialPage(): String = when (this) {
    SettingsPage.Diagnostics -> "diag"
    SettingsPage.Readiness -> "ready"
    SettingsPage.Notifications -> "notify"
    SettingsPage.PhoneData -> "phone"
    SettingsPage.Appearance -> "style"
    SettingsPage.Devices -> "devices"
    SettingsPage.About -> "about"
    SettingsPage.Pairing -> "pair"
    SettingsPage.Assistant -> "assistant"
    SettingsPage.Hubs -> "hubs"
}

/** The root and unknown page names have no detail destination. */
fun settingsPageFromInitialPage(initialPage: String): SettingsPage? =
    SettingsPage.entries.firstOrNull { it.toInitialPage() == initialPage }
