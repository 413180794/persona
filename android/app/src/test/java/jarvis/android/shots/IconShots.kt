package jarvis.android.shots

import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.res.colorResource
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import jarvis.android.R
import jarvis.android.ui.design.JarvisMark
import org.junit.Rule
import org.junit.Test

/** The launcher icon (the iOS icon's art) as a launcher masks it (a circle, a rounded square), the themed icon, the in-app mark. */
class IconShots {
    @get:Rule val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6, theme = "android:Theme.Material.Light.NoActionBar")

    @Test fun icon() {
        paparazzi.snapshot("icon") {
            Row(Modifier.fillMaxSize().background(Color(0xFFF2F2F7)).padding(24.dp), horizontalArrangement = Arrangement.spacedBy(16.dp), verticalAlignment = Alignment.Top) {
                for (shape in listOf(CircleShape, RoundedCornerShape(28.dp))) {
                    // What a launcher's mask shows of the adaptive icon: its middle 72 dp, here at 108 — the iOS icon's
                    // art (inset 18 dp in ic_launcher_fg.xml, so it fills that middle) over launcher_bg.
                    Box(Modifier.size(108.dp).clip(shape).background(colorResource(R.color.launcher_bg))) {
                        Image(painterResource(R.drawable.ic_launcher_art), null, Modifier.fillMaxSize())
                    }
                }
                Box(Modifier.size(108.dp).clip(CircleShape).background(Color(0xFF3B4A6B))) {
                    Image(painterResource(R.drawable.ic_launcher_mono_art), null, Modifier.fillMaxSize(),
                        colorFilter = ColorFilter.tint(Color(0xFFD7E3FF)))
                }
                JarvisMark(48.dp)
            }
        }
    }
}
