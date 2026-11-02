package jarvis.android.shots

import androidx.activity.OnBackPressedDispatcher
import androidx.activity.OnBackPressedDispatcherOwner
import androidx.activity.compose.LocalActivityResultRegistryOwner
import androidx.activity.compose.LocalOnBackPressedDispatcherOwner
import androidx.activity.result.ActivityResultRegistry
import androidx.activity.result.ActivityResultRegistryOwner
import androidx.activity.result.contract.ActivityResultContract
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.app.ActivityOptionsCompat
import androidx.lifecycle.Lifecycle
import jarvis.android.ui.JTheme
import jarvis.android.ui.JarvisTheme

/**
 * What the screenshot tests draw a page in: the app's theme in light or dark, a phone's status bar and gesture bar
 * (Paparazzi has no system bars), and the activity pieces Compose screens expect (back, activity results).
 */
@Composable
fun ShotHost(dark: Boolean, content: @Composable () -> Unit) {
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val back = object : OnBackPressedDispatcherOwner {
        override val onBackPressedDispatcher = OnBackPressedDispatcher()
        override val lifecycle: Lifecycle = lifecycle
    }
    val results = object : ActivityResultRegistryOwner {
        override val activityResultRegistry = object : ActivityResultRegistry() {
            override fun <I, O> onLaunch(requestCode: Int, contract: ActivityResultContract<I, O>, input: I, options: ActivityOptionsCompat?) {}
        }
    }
    CompositionLocalProvider(LocalOnBackPressedDispatcherOwner provides back, LocalActivityResultRegistryOwner provides results) {
        JarvisTheme(dark = dark) {
            Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
                StatusBar()
                Box(Modifier.weight(1f).fillMaxWidth()) { content() }
                Box(Modifier.fillMaxWidth().height(20.dp), contentAlignment = Alignment.Center) {
                    Box(Modifier.size(width = 108.dp, height = 4.dp).background(JTheme.colors.label.copy(alpha = 0.8f), CircleShape))
                }
            }
        }
    }
}

@Composable
private fun StatusBar() {
    val c = JTheme.colors.label
    Row(Modifier.fillMaxWidth().height(28.dp).padding(horizontal = 24.dp), verticalAlignment = Alignment.CenterVertically) {
        Text("9:41", fontSize = 14.sp, fontWeight = FontWeight.SemiBold, color = c)
        Spacer(Modifier.weight(1f))
        Box(Modifier.size(width = 14.dp, height = 10.dp).background(c, RoundedCornerShape(2.dp)))
        Spacer(Modifier.width(6.dp))
        Box(Modifier.size(width = 22.dp, height = 11.dp).background(c, RoundedCornerShape(3.dp)))
    }
}
