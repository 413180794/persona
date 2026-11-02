package jarvis.android.shots

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import jarvis.android.data.Item
import jarvis.android.ui.ChatRow
import jarvis.android.ui.FoldedRow
import jarvis.android.ui.JTheme
import jarvis.android.ui.QueueStrip
import jarvis.android.ui.chat.ApiErrorCard
import jarvis.android.ui.chat.ApiErrorInfo
import jarvis.android.ui.chat.ApiErrorLine
import jarvis.android.ui.chat.SlashCommand
import jarvis.android.ui.chat.SlashPicker
import jarvis.android.ui.design.JSpace
import org.junit.Rule
import org.junit.Test
import androidx.compose.ui.Modifier

/** The chat's smaller parts no mock conversation shows: the queue strip, API error cards, the "/" picker. */
class ChatPartsShots {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6, theme = "android:Theme.Material.Light.NoActionBar", maxPercentDifference = 100.0)

    private fun error(text: String, count: Int): ApiErrorLine {
        val row = FoldedRow.Plain(ChatRow.Assistant(Item(id = "e$count", kind = "text", role = "assistant", text = text)))
        return ApiErrorLine(row, ApiErrorInfo.parse(text)!!, count)
    }

    @Test fun parts() {
        for (dark in listOf(false, true)) paparazzi.snapshot("chat-parts" + if (dark) "-dark" else "") {
            ShotHost(dark) {
                Column(
                    Modifier.fillMaxSize().background(JTheme.colors.page).padding(JSpace.page),
                    verticalArrangement = Arrangement.spacedBy(JSpace.m),
                ) {
                    ApiErrorCard(error("API Error: 529 overloaded request_id: req_011CUxyzABCD1234", 3), retry = {})
                    ApiErrorCard(error("API Error: 401 authentication_error · Please run /login", 1), retry = null)
                    QueueStrip(listOf("顺便把中号布局的降雨条也改成和 App 里一样", "改完跑一遍 UI 测试")) { _, _ -> }
                    SlashPicker(SlashCommand.matching("/", clear = true)) {}
                }
            }
        }
    }
}
