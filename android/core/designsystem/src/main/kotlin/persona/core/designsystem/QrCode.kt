package persona.core.designsystem

import androidx.compose.ui.res.stringResource
import android.graphics.Bitmap
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import androidx.compose.material3.Text
import androidx.compose.ui.text.style.TextOverflow

/**
 * A QR code's colours. Scanners want dark modules on a light ground, in dark mode too (an inverted code does not scan
 * in every app), so they are not the theme's ink and paper (iOS `Theme.Scan`).
 */
object ScanColors {
    val module: Color = Color.Black
    val ground: Color = Color.White
}

/** Modules of light ground around the code (the QR standard's quiet zone). */
private const val QuietZone = 4

/** How large a sign-in code is drawn on a page: room to scan from another phone at arm's length. */
private val QrSide = 220.dp

/**
 * A QR code from a hub's modules ([rows] of "0" / "1", without the quiet zone, docs/extensions/connectors.md), on its own light
 * tile with the quiet zone around it. Read out as [label].
 */
@Composable
fun JQrCode(rows: List<String>, modifier: Modifier = Modifier, label: String = stringResource(R.string.designsystem_qr_code)) {
    Canvas(modifier.size(QrSide).aspectRatio(1f).clip(PersonaTheme.shapes.controlShape).clearAndSetSemantics { contentDescription = label }) {
        drawRect(ScanColors.ground)
        val n = rows.size + 2 * QuietZone
        if (rows.isEmpty()) return@Canvas
        val cell = size.minDimension / n
        rows.forEachIndexed { y, row ->
            row.forEachIndexed { x, c ->
                // A hair wider than the cell, so neighbouring modules leave no seam between them.
                if (c == '1') drawRect(ScanColors.module, Offset((x + QuietZone) * cell, (y + QuietZone) * cell), Size(cell + 0.5f, cell + 0.5f))
            }
        }
    }
}

/** The same code as a picture to save (scanned from the photo library by the other app on this phone): [scale] pixels a module. */
fun qrBitmap(rows: List<String>, scale: Int = 12): Bitmap {
    val n = rows.size + 2 * QuietZone
    val side = n * scale
    val pixels = IntArray(side * side) { ScanColors.ground.toArgb() }
    val dark = ScanColors.module.toArgb()
    rows.forEachIndexed { y, row ->
        row.forEachIndexed { x, c ->
            if (c != '1') return@forEachIndexed
            for (dy in 0 until scale) {
                val start = ((y + QuietZone) * scale + dy) * side + (x + QuietZone) * scale
                pixels.fill(dark, start, start + scale)
            }
        }
    }
    return Bitmap.createBitmap(pixels, side, side, Bitmap.Config.ARGB_8888)
}

/** A code to type or send somewhere else (a pairing code, a device code): large, monospaced, spaced out, one line. */
@Composable
fun JCodeDisplay(code: String, modifier: Modifier = Modifier) {
    Text(code, modifier, color = PersonaTheme.palette.ink, maxLines = 1, softWrap = false, overflow = TextOverflow.Ellipsis,
        style = PersonaTheme.type.largeTitle.copy(fontFamily = PersonaTheme.type.code.fontFamily, letterSpacing = CodeTracking))
}

private val CodeTracking = 0.18.em
