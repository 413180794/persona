package jarvis.android.ui.machines

import jarvis.android.ui.settings.*
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AcUnit
import androidx.compose.material.icons.filled.Computer
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Landscape
import androidx.compose.material.icons.filled.LaptopMac
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.data.Machine
import jarvis.android.data.MachineSystem
import jarvis.android.settings.MetricLevel
import jarvis.android.settings.OSFamily
import kotlin.math.cos
import kotlin.math.sin

/**
 * Colors of the machine charts (iOS `MetricColor`, docs/machines.md「图」): one hue per measure, a separate step for
 * dark. They don't follow the 界面风格 (a CPU line is the same blue in every style, like the state colors).
 */
object MetricColors {
    private fun pick(dark: Boolean, light: Long, darkV: Long) = Color(0xFF000000 or if (dark) darkV else light)
    val cpu: Color @Composable @ReadOnlyComposable get() = pick(kitColors.dark, 0x2A78D6, 0x3987E5)
    val mem: Color @Composable @ReadOnlyComposable get() = pick(kitColors.dark, 0x1BAF7A, 0x199E70)
    val rx: Color @Composable @ReadOnlyComposable get() = pick(kitColors.dark, 0xEB6834, 0xD95926)
    val tx: Color @Composable @ReadOnlyComposable get() = pick(kitColors.dark, 0x6250D6, 0x9085E9)
    val load: Color @Composable @ReadOnlyComposable get() = pick(kitColors.dark, 0xE87BA4, 0xD55181)
    val disk: Color @Composable @ReadOnlyComposable get() = pick(kitColors.dark, 0x8A6A00, 0xE3B341)
    val grid: Color @Composable @ReadOnlyComposable get() = pick(kitColors.dark, 0xE1E0D9, 0x2C2C2A)
    private val warning = Color(0xFFFAB219)
    private val critical = Color(0xFFD03B3B)

    @Composable @ReadOnlyComposable
    fun level(l: MetricLevel): Color = when (l) {
        MetricLevel.NORMAL -> kitColors.secondaryLabel
        MetricLevel.WARNING -> warning
        MetricLevel.CRITICAL -> critical
    }
}

/** The distributions' own colors, stepped for each appearance (iOS `OSBadge.color`). */
@Composable
@ReadOnlyComposable
private fun osColor(f: OSFamily): Color {
    val dark = kitColors.dark
    fun pick(light: Long, darkV: Long) = Color(0xFF000000 or if (dark) darkV else light)
    return when (f) {
        OSFamily.MACOS -> kitColors.label
        OSFamily.UBUNTU -> pick(0xE95420, 0xF26B3A)
        OSFamily.DEBIAN -> pick(0xA80030, 0xE0245E)
        OSFamily.FEDORA -> pick(0x3C6EB4, 0x51A2DA)
        OSFamily.REDHAT -> pick(0xCC0000, 0xFF4D4D)
        OSFamily.CENTOS -> pick(0x932279, 0xC14FA8)
        OSFamily.ROCKY -> pick(0x10B981, 0x34D399)
        OSFamily.ALMA -> pick(0x0F4266, 0x6CA6D9)
        OSFamily.ARCH -> pick(0x1793D1, 0x3BAEE8)
        OSFamily.ALPINE -> pick(0x0D597F, 0x5AA9D6)
        OSFamily.OPENSUSE -> pick(0x4E8A1E, 0x73BA25)
        OSFamily.NIXOS -> pick(0x5277C3, 0x7EBAE4)
        OSFamily.LINUX, OSFamily.OTHER -> kitColors.accent
    }
}

/** The hub's icon name for a machine without a known OS. */
private fun machineIcon(m: Machine?): ImageVector = when (m?.icon) {
    "server" -> Icons.Default.Dns
    "laptop" -> Icons.Default.LaptopMac
    else -> Icons.Default.Computer
}

/**
 * A machine's OS as a small mark (iOS `OSBadge`, docs/machines.md「系统图标」): a laptop for macOS, a drawn circle of
 * friends for Ubuntu, simple shapes or a letter in the distribution's color for the rest. Grey when offline.
 */
@Composable
fun OSBadge(system: MachineSystem?, machine: Machine?, size: Dp = 44.dp, online: Boolean = true) {
    val known = system != null || machine?.os != null
    val family = if (known) OSFamily.of(system, machine?.os) else null
    val c = kitColors
    val tint = if (online) family?.let { osColor(it) } ?: c.accent else c.secondaryLabel
    Box(
        Modifier.size(size).background(tint.copy(alpha = 0.13f), RoundedCornerShape(size * 0.27f)).clearAndSetSemantics { },
        contentAlignment = Alignment.Center,
    ) {
        val mark = Modifier.size(size * 0.56f)
        when (family) {
            OSFamily.MACOS -> Icon(Icons.Default.LaptopMac, null, tint = if (online) c.label else c.secondaryLabel, modifier = mark)
            OSFamily.UBUNTU -> UbuntuMark(tint, mark)
            OSFamily.ARCH -> Canvas(mark) {
                val p = Path().apply {
                    moveTo(this@Canvas.size.width / 2, this@Canvas.size.height * 0.08f)
                    lineTo(this@Canvas.size.width * 0.95f, this@Canvas.size.height * 0.92f)
                    lineTo(this@Canvas.size.width * 0.05f, this@Canvas.size.height * 0.92f)
                    close()
                }
                drawPath(p, tint)
            }
            OSFamily.ALPINE -> Icon(Icons.Default.Landscape, null, tint = tint, modifier = mark)
            OSFamily.NIXOS -> Icon(Icons.Default.AcUnit, null, tint = tint, modifier = mark)
            OSFamily.LINUX -> Icon(Icons.Default.Terminal, null, tint = tint, modifier = mark)
            null, OSFamily.OTHER -> Icon(machineIcon(machine), null, tint = tint, modifier = mark)
            else -> Text(family.letter ?: "?", fontSize = (size.value * 0.5f).sp, fontWeight = FontWeight.Black, color = tint, maxLines = 1)
        }
    }
}

/** Ubuntu's circle of friends, simplified: a ring broken in three places, a dot at each break. */
@Composable
private fun UbuntuMark(color: Color, modifier: Modifier) {
    Canvas(modifier) {
        val s = minOf(size.width, size.height)
        val c = Offset(size.width / 2, size.height / 2)
        val r = s * 0.31f
        val dot = s * 0.13f
        val line = s * 0.13f
        val away = r + dot * 0.95f
        for (i in 0 until 3) {
            val mid = i * 2 * Math.PI / 3 - Math.PI / 2
            val startDeg = Math.toDegrees(mid + 0.42).toFloat()
            val sweepDeg = Math.toDegrees(2 * Math.PI / 3 - 0.84).toFloat()
            drawArc(color, startDeg, sweepDeg, useCenter = false, topLeft = Offset(c.x - r, c.y - r), size = Size(r * 2, r * 2),
                style = Stroke(width = line))
            drawCircle(color, dot, Offset(c.x + cos(mid).toFloat() * away, c.y + sin(mid).toFloat() * away))
        }
    }
}

/** A bar for a share (disk, memory): the used part in its color, the rest a faint track. */
@Composable
fun MeterBar(fraction: Double, color: Color, modifier: Modifier = Modifier) {
    val f = fraction.coerceIn(0.0, 1.0).toFloat()
    Box(modifier.fillMaxWidth().height(6.dp).background(kitColors.label.copy(alpha = 0.08f), CircleShape)) {
        Box(Modifier.fillMaxWidth(f).height(6.dp).background(color, CircleShape))
    }
}
