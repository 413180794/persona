package jarvis.android.settings

import java.time.Instant
import java.time.ZoneId
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.log10
import kotlin.math.pow

// The machine charts' math (what Swift Charts does for iOS): nice axis ticks, time ticks, monotone curves.

object ChartMath {
    /** The y top when not fixed: a little over the largest value (and the reference) so the line never touches it. */
    fun niceMax(max: Double): Double = if (max > 0) max * 1.15 else 1.0

    /** About [count] round values from 0 to [top] (0, 50, 100 / 0, 2 MB, 4 MB …), all ≤ top. */
    fun yTicks(top: Double, count: Int = 3, binary: Boolean = false): List<Double> {
        if (top <= 0) return listOf(0.0)
        if (binary) {
            // Bytes: round in the unit the labels use (KB / MB / GB are 1024-based), so 2 MB, 4 MB rather than 1.91 MB.
            var unit = 1.0
            while (top / unit >= 1024) unit *= 1024
            return yTicks(top / unit, count).map { it * unit }
        }
        val raw = top / count
        val mag = 10.0.pow(floor(log10(raw)))
        val step = listOf(1.0, 2.0, 2.5, 5.0, 10.0).map { it * mag }.first { it >= raw }
        val out = mutableListOf<Double>()
        var v = 0.0
        while (v <= top + step * 1e-9) { out += v; v += step }
        return out
    }

    /** Round local-time ticks (Unix seconds) in [start, end]: the smallest interval giving at most [maxCount]. */
    fun timeTicks(start: Long, end: Long, maxCount: Int = 4, zone: ZoneId = ZoneId.systemDefault()): List<Long> {
        if (end <= start) return emptyList()
        val steps = listOf(60L, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200, 86400)
        val offset = zone.rules.getOffset(Instant.ofEpochSecond(start)).totalSeconds.toLong()
        fun ticks(step: Long): List<Long> {
            var t = ceil((start + offset).toDouble() / step).toLong() * step - offset
            val out = mutableListOf<Long>()
            while (t <= end) { out += t; t += step }
            return out
        }
        for (step in steps) ticks(step).let { if (it.size <= maxCount) return it }
        return ticks(steps.last())
    }

    /**
     * Tangents of a monotone cubic through (xs, ys) (Fritsch–Carlson, what `.interpolationMethod(.monotone)` draws):
     * no overshoot between points, so a CPU line never dips below 0 or above 100.
     */
    fun monotoneTangents(xs: DoubleArray, ys: DoubleArray): DoubleArray {
        val n = xs.size
        val m = DoubleArray(n)
        if (n < 2) return m
        val d = DoubleArray(n - 1) { i ->
            val dx = xs[i + 1] - xs[i]
            if (dx == 0.0) 0.0 else (ys[i + 1] - ys[i]) / dx
        }
        m[0] = d[0]
        m[n - 1] = d[n - 2]
        for (i in 1 until n - 1) m[i] = if (d[i - 1] * d[i] <= 0) 0.0 else (d[i - 1] + d[i]) / 2
        for (i in 0 until n - 1) {
            if (d[i] == 0.0) { m[i] = 0.0; m[i + 1] = 0.0; continue }
            val a = m[i] / d[i]
            val b = m[i + 1] / d[i]
            val s = a * a + b * b
            if (s > 9) {
                val t = 3 / kotlin.math.sqrt(s)
                m[i] = t * a * d[i]
                m[i + 1] = t * b * d[i]
            }
        }
        return m
    }
}
