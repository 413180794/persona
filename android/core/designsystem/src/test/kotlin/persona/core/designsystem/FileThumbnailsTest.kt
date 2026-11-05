package persona.core.designsystem

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.yield
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.IOException
import java.util.concurrent.atomic.AtomicInteger

/** [FileThumbnails]: what it keeps, what it remembers as failed, how many it asks at once, and the icon's keys. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35])
class FileThumbnailsTest {
    @Before fun forget() = FileThumbnails.clear()
    @After fun forgetAgain() = FileThumbnails.clear()

    private val picture = ImageBitmap(1, 1)
    private val decode: suspend (ByteArray) -> ImageBitmap? = { picture }

    @Test fun aPictureIsKeptAndANoPictureIsRemembered() = runTest {
        val calls = AtomicInteger()
        assertSame(picture, FileThumbnails.picture("a", 64, { calls.incrementAndGet(); byteArrayOf(1) }, decode))
        assertSame(picture, FileThumbnails.picture("a", 64, { calls.incrementAndGet(); byteArrayOf(1) }, decode))
        assertEquals(1, calls.get())
        // The machine has none: the icon stays, and it is not asked again this launch.
        assertNull(FileThumbnails.picture("b", 64, { calls.incrementAndGet(); null }, decode))
        assertNull(FileThumbnails.picture("b", 64, { calls.incrementAndGet(); byteArrayOf(1) }, decode))
        assertEquals(2, calls.get())
        // Bytes that don't decode, a transport without thumbnails: remembered too.
        assertNull(FileThumbnails.picture("c", 64, { byteArrayOf(1) }, { throw IOException("decode") }))
        assertNull(FileThumbnails.picture("c", 64, { calls.incrementAndGet(); byteArrayOf(1) }, decode))
        assertNull(FileThumbnails.picture("d", 64, { throw UnsupportedOperationException("thumbnail") }, decode))
        assertNull(FileThumbnails.picture("d", 64, { calls.incrementAndGet(); byteArrayOf(1) }, decode))
        assertEquals(2, calls.get())
    }

    @Test fun aTimeoutOrACancellationIsAskedAgain() = runTest {
        assertNull(FileThumbnails.picture("slow", 64, { throw IOException("timeout") }, decode))
        assertSame(picture, FileThumbnails.picture("slow", 64, { byteArrayOf(1) }, decode))
        val started = CompletableDeferred<Unit>()
        val job = launch { FileThumbnails.picture("gone", 64, { started.complete(Unit); delay(10_000); byteArrayOf(1) }, decode) }
        started.await()
        job.cancelAndJoin()
        assertSame(picture, FileThumbnails.picture("gone", 64, { byteArrayOf(1) }, decode))
    }

    @Test fun theLastNinetySixStay() = runTest {
        repeat(FileThumbnails.KEEP + 1) { i -> FileThumbnails.picture("k$i", 64, { byteArrayOf(1) }, decode) }
        assertNull(FileThumbnails.cached("k0"))
        assertNotNull(FileThumbnails.cached("k1"))
        assertNotNull(FileThumbnails.cached("k${FileThumbnails.KEEP}"))
    }

    @Test fun threeAtATimeAndOneRequestPerFile() = runTest {
        val running = AtomicInteger()
        var most = 0
        val calls = AtomicInteger()
        val release = CompletableDeferred<Unit>()
        val load: suspend (Int) -> ByteArray? = {
            calls.incrementAndGet()
            most = maxOf(most, running.incrementAndGet())
            release.await()
            running.decrementAndGet()
            byteArrayOf(1)
        }
        val asks = (0 until 6).map { i -> async(start = CoroutineStart.UNDISPATCHED) { FileThumbnails.picture("f$i", 64, load, decode) } } +
            (0 until 3).map { async(start = CoroutineStart.UNDISPATCHED) { FileThumbnails.picture("f0", 64, load, decode) } }
        repeat(5) { yield() }
        assertEquals(3, running.get())
        release.complete(Unit)
        assertTrue(asks.awaitAll().all { it === picture })
        assertEquals(3, most)
        // The same file asked four times went out once.
        assertEquals(6, calls.get())
    }
}
