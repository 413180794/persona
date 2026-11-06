package persona.core.designsystem

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.Dp

/**
 * Persona's own mark: the app icon itself (the iOS app icon, apple/Persona/Resources/Assets.xcassets/AppIcon.appiconset
 * icon.png / icon-dark.png scaled down, as the web's `AppMark`), not a redrawing. The theme's light or dark picture,
 * in an icon's rounded square. Decorative: the row it leads names it.
 */
@Composable
fun JAppMark(size: Dp, modifier: Modifier = Modifier) {
    val picture = if (PersonaTheme.darkTheme) R.drawable.app_mark_dark else R.drawable.app_mark
    Image(painterResource(picture), null, modifier.size(size).clip(RoundedCornerShape(size * AppMarkCorner)),
        contentScale = ContentScale.Crop)
}

/** The app icon's corner, as a share of its side (iOS's icon mask is about 22.5 %). */
private const val AppMarkCorner = 0.225f
