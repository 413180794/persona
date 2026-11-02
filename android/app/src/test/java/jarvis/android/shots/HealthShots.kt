package jarvis.android.shots

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.ui.Modifier
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import jarvis.android.phone.Source
import jarvis.android.ui.HealthRowState
import jarvis.android.ui.HealthRowView
import jarvis.android.ui.PhoneDataGroup
import jarvis.android.ui.design.PageHeader
import org.junit.Rule
import org.junit.Test

/**
 * 设置 › 手机数据 with the 健康 row in each of its states (Health Connect can't be asked in Paparazzi, so the row's
 * state is passed in): off, on and fully granted, partly granted, nothing granted, Health Connect missing.
 */
class HealthShots {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6, theme = "android:Theme.Material.Light.NoActionBar", maxPercentDifference = 100.0)

    private fun shot(name: String, health: HealthRowState) {
        // A few other sources on (one without its permission) so the 健康 row sits among real-looking rows.
        val enabled = setOf(Source.Device, Source.Calendar, Source.Location) + if (health is HealthRowState.Available && health.on) setOf(Source.Health) else emptySet()
        val granted = Source.entries.associateWith { it != Source.Location }
        for (dark in listOf(false, true)) {
            paparazzi.snapshot(name + if (dark) "-dark" else "") {
                ShotHost(dark) {
                    Column(Modifier.fillMaxSize()) {
                        PageHeader("设置", onBack = null)
                        Column(Modifier.verticalScroll(rememberScrollState())) {
                            PhoneDataGroup(enabled, granted, onToggle = { _, _ -> }, health = { HealthRowView(health, onGet = {}, onToggle = {}) })
                        }
                    }
                }
            }
        }
    }

    @Test fun off() = shot("health-off", HealthRowState.Available(on = false, some = true, all = true))

    @Test fun granted() = shot("health-on", HealthRowState.Available(on = true, some = true, all = true))

    @Test fun partlyGranted() = shot("health-partly", HealthRowState.Available(on = true, some = true, all = false))

    @Test fun notGranted() = shot("health-denied", HealthRowState.Available(on = true, some = false, all = false))

    @Test fun missing() = shot("health-missing", HealthRowState.Unavailable("这台手机没有 Health Connect"))
}
