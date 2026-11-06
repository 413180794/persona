package persona.feature.settings

import persona.core.navigation.SettingsPage

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
    SettingsPage.ClaudeCode -> "claude"
    SettingsPage.ModelConfig -> "models"
    SettingsPage.Hubs -> "hubs"
    SettingsPage.Persona -> "persona"
    SettingsPage.Extensions -> "plugins"
    SettingsPage.Mail -> "mail"
    SettingsPage.Feishu -> "feishu"
    SettingsPage.Channels -> "channels"
    SettingsPage.HubUpdate -> "hub-update"
    SettingsPage.Translation -> "translate"
    SettingsPage.Triage -> "triage"
    SettingsPage.Capsule -> "capsule"
    SettingsPage.Language -> "language"
    SettingsPage.Codex -> "codex"
    SettingsPage.Providers -> "providers"
    SettingsPage.Storage -> "storage"
    SettingsPage.Desktops -> "desktops"
    SettingsPage.Managers -> "managers"
    SettingsPage.FeishuBot -> "feishu-bot"
    SettingsPage.Sent -> "sent"
}

/** The root and unknown page names have no detail destination. */
fun settingsPageFromInitialPage(initialPage: String): SettingsPage? =
    SettingsPage.entries.firstOrNull { it.toInitialPage() == initialPage }
