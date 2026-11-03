package persona.core.designsystem

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.util.zip.CRC32
import java.util.zip.Deflater

/** APNG frames are composed whole, by each frame's offset, dispose op and blend op (what Pillow writes is partial frames). */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35])
class ApngTest {
    private val red = 0xFFFF0000.toInt()
    private val clear = 0x00000000

    @Test fun partialFramesAreComposedByDisposeAndBlend() {
        val png = apng(4, 4, fps = null,
            // A whole red frame, 1/10 s.
            Frame(4, 4, 0, 0, 1, 10, DISPOSE_NONE, BLEND_SOURCE, red),
            // A clear corner that replaces what is there, then is cleared again; no delay, so the fallback fps times it.
            Frame(2, 2, 2, 2, 0, 0, DISPOSE_BACKGROUND, BLEND_SOURCE, clear),
            // A clear corner drawn over: nothing changes under it.
            Frame(2, 2, 0, 0, 1, 4, DISPOSE_NONE, BLEND_OVER, clear),
        )
        val frames = requireNotNull(decodeApng(png, fps = 4.0))
        assertEquals(3, frames.bitmaps.size)
        assertArrayEquals(intArrayOf(100, 250, 250), frames.delaysMs)
        assertEquals(red, frames.bitmaps[0].getPixel(3, 3))
        assertEquals(clear, frames.bitmaps[1].getPixel(3, 3))
        assertEquals(red, frames.bitmaps[1].getPixel(0, 0))
        assertEquals(red, frames.bitmaps[2].getPixel(0, 0))
        assertEquals(clear, frames.bitmaps[2].getPixel(3, 3))
    }

    @Test fun aStillPngIsOneFrameAndOtherBytesAreNone() {
        val still = png(2, 2, listOf(chunk("IDAT", pixels(2, 2, red))))
        val frames = requireNotNull(decodeApng(still, fps = 8.0))
        assertEquals(1, frames.bitmaps.size)
        assertEquals(red, frames.bitmaps[0].getPixel(1, 1))
        assertNull(decodeApng("not a png".encodeToByteArray(), null))
    }

    private class Frame(val w: Int, val h: Int, val x: Int, val y: Int, val num: Int, val den: Int, val dispose: Int, val blend: Int, val color: Int)

    private fun apng(width: Int, height: Int, fps: Double?, vararg frames: Frame): ByteArray {
        var seq = 0
        val chunks = mutableListOf(chunk("acTL", ByteBuffer.allocate(8).putInt(frames.size).putInt(0).array()))
        frames.forEachIndexed { i, f ->
            chunks += chunk("fcTL", ByteBuffer.allocate(26).putInt(seq++).putInt(f.w).putInt(f.h).putInt(f.x).putInt(f.y)
                .putShort(f.num.toShort()).putShort(f.den.toShort()).put(f.dispose.toByte()).put(f.blend.toByte()).array())
            val data = pixels(f.w, f.h, f.color)
            chunks += if (i == 0) chunk("IDAT", data) else chunk("fdAT", ByteBuffer.allocate(4 + data.size).putInt(seq++).put(data).array())
        }
        return png(width, height, chunks)
    }

    private fun png(width: Int, height: Int, body: List<ByteArray>): ByteArray {
        val out = ByteArrayOutputStream()
        out.write(byteArrayOf(-119, 80, 78, 71, 13, 10, 26, 10))
        out.write(chunk("IHDR", ByteBuffer.allocate(13).putInt(width).putInt(height).put(8).put(6).put(0).put(0).put(0).array()))
        body.forEach(out::write)
        out.write(chunk("IEND", ByteArray(0)))
        return out.toByteArray()
    }

    /** Rows of RGBA pixels of one colour, each with filter 0, deflated. */
    private fun pixels(w: Int, h: Int, argb: Int): ByteArray {
        val raw = ByteArrayOutputStream()
        repeat(h) {
            raw.write(0)
            repeat(w) { raw.write(argb shr 16 and 0xFF); raw.write(argb shr 8 and 0xFF); raw.write(argb and 0xFF); raw.write(argb ushr 24) }
        }
        val deflater = Deflater().apply { setInput(raw.toByteArray()); finish() }
        val buf = ByteArray(1024)
        return buf.copyOf(deflater.deflate(buf))
    }

    private fun chunk(type: String, data: ByteArray): ByteArray {
        val t = type.encodeToByteArray()
        val crc = CRC32().apply { update(t); update(data) }
        return ByteBuffer.allocate(12 + data.size).putInt(data.size).put(t).put(data).putInt(crc.value.toInt()).array()
    }

    private companion object {
        const val DISPOSE_NONE = 0
        const val DISPOSE_BACKGROUND = 1
        const val BLEND_SOURCE = 0
        const val BLEND_OVER = 1
    }
}
