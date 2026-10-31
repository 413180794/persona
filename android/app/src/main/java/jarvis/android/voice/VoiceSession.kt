package jarvis.android.voice

import android.annotation.SuppressLint
import android.content.Context
import android.media.AudioAttributes
import android.media.AudioDeviceInfo
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioRecord
import android.media.AudioTrack
import android.media.MediaRecorder
import android.media.audiofx.AcousticEchoCanceler
import android.media.audiofx.NoiseSuppressor
import android.os.Build
import jarvis.android.data.ApiClient
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import okio.ByteString.Companion.toByteString
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import kotlin.math.sqrt

/** What the voice page shows. */
data class VoiceState(
    val phase: Phase = Phase.CONNECTING,
    val lines: List<Line> = emptyList(),
    /** The tool being used right now (「查知识库」), shown as a status line. */
    val tool: String? = null,
    val speaking: Boolean = false,
    /** 0…1, the microphone's loudness (the orb follows it while listening). */
    val level: Float = 0f,
    val muted: Boolean = false,
    val error: String? = null,
) {
    enum class Phase { CONNECTING, LIVE, ENDED }
}

/**
 * One voice conversation with the hub's voice plugin (docs/plugins.md「语音」): the microphone goes up continuously
 * (silence too, the model decides when a turn ends), the model's speech comes down and plays at once, and a `clear`
 * drops what has not been played (the user talked over it). Echo cancellation keeps the model from hearing itself
 * on the loudspeaker. [start] once, [stop] when the page goes away.
 */
class VoiceSession(
    private val context: Context, private val api: ApiClient, private val plugin: String,
    private val voice: String?, private val model: String? = null,
) {
    private val _state = MutableStateFlow(VoiceState())
    val state: StateFlow<VoiceState> = _state.asStateFlow()

    @Volatile private var ws: WebSocket? = null
    @Volatile private var stopped = false
    @Volatile private var muted = false
    private var mic: Thread? = null
    private var player: Thread? = null
    private val playQueue = LinkedBlockingQueue<ByteArray>()
    private val tools = LinkedHashMap<String, String>()
    @Volatile private var track: AudioTrack? = null
    @Volatile private var lastAudioAt = 0L
    private val audio = context.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    private var savedMode = AudioManager.MODE_NORMAL
    @Volatile private var routed = false

    fun start() {
        val client = api.http.newBuilder().readTimeout(0, TimeUnit.MILLISECONDS).pingInterval(15, TimeUnit.SECONDS).build()
        val req = Request.Builder().url(api.voiceSocketUrl(plugin)).header("Authorization", "Bearer ${api.token}").build()
        ws = client.newWebSocket(req, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                webSocket.send(startFrame(voice, model))
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                when (val e = VoiceEvent.parse(text)) {
                    is VoiceEvent.Ready -> begin(e)
                    VoiceEvent.Clear -> clearPlayback()
                    is VoiceEvent.Transcript -> _state.update { it.copy(lines = it.lines.with(e)) }
                    is VoiceEvent.Tool -> {
                        // Several can be open (a quick lookup while a task handed to the main conversation runs): the
                        // latest open one is shown.
                        synchronized(tools) { if (e.done) tools.remove(e.id) else tools[e.id] = e.label ?: "处理中" }
                        _state.update { it.copy(tool = synchronized(tools) { tools.values.lastOrNull() }) }
                    }
                    is VoiceEvent.Error -> {
                        _state.update { it.copy(error = e.message) }
                        if (e.fatal) end(e.message)
                    }
                    null -> {}
                }
            }

            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                lastAudioAt = System.currentTimeMillis()
                playQueue.offer(bytes.toByteArray())
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) =
                end(if (code == 1001) RESTARTED else null) // 1001: the plugin is restarting (a deploy)

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                // A socket that drops mid-conversation (Broken pipe, reset) is nearly always the plugin or the hub
                // restarting, or the network: say that, not the exception.
                end(
                    when {
                        response?.code == 404 -> "hub 上没有语音插件"
                        _state.value.phase == VoiceState.Phase.LIVE -> RESTARTED
                        else -> "连不上语音服务：${t.message ?: t.javaClass.simpleName}"
                    },
                )
            }
        })
    }

    fun setMuted(on: Boolean) {
        muted = on
        _state.update { it.copy(muted = on) }
    }

    /** Stops the answer being said (the same as talking over it, without having to). */
    fun interrupt() {
        clearPlayback()
        ws?.send("""{"type":"interrupt"}""")
    }

    fun stop() {
        if (stopped) return
        stopped = true
        ws?.close(1000, "bye")
        mic?.interrupt()
        player?.interrupt()
        restoreAudio()
        _state.update { it.copy(phase = VoiceState.Phase.ENDED, speaking = false, level = 0f) }
    }

    private fun end(error: String?) {
        if (!stopped && error != null) _state.update { it.copy(error = error) }
        stop()
    }

    @SuppressLint("MissingPermission") // the page asks for RECORD_AUDIO before it starts a session
    private fun begin(r: VoiceEvent.Ready) {
        if (stopped || mic != null) return
        savedMode = audio.mode
        audio.mode = AudioManager.MODE_IN_COMMUNICATION
        routed = true
        routeToSpeaker()
        val outMin = AudioTrack.getMinBufferSize(r.outputRate, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_16BIT)
        val t = AudioTrack.Builder()
            .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION).setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
            .setAudioFormat(AudioFormat.Builder().setSampleRate(r.outputRate).setEncoding(AudioFormat.ENCODING_PCM_16BIT).setChannelMask(AudioFormat.CHANNEL_OUT_MONO).build())
            .setBufferSizeInBytes(maxOf(outMin, r.outputRate / 5 * 2)) // ~200 ms
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
        track = t
        t.play()
        player = Thread({
            while (!stopped) {
                val chunk = try { playQueue.poll(100, TimeUnit.MILLISECONDS) } catch (_: InterruptedException) { break }
                val speaking = chunk != null || System.currentTimeMillis() - lastAudioAt < 300
                if (speaking != _state.value.speaking) _state.update { it.copy(speaking = speaking) }
                if (chunk != null) runCatching { track?.write(chunk, 0, chunk.size) }
            }
            runCatching { t.stop(); t.release() }
        }, "voice-play").apply { start() }

        val frame = r.inputRate / 50 // 20 ms
        val inMin = AudioRecord.getMinBufferSize(r.inputRate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
        val rec = AudioRecord(MediaRecorder.AudioSource.VOICE_COMMUNICATION, r.inputRate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT, maxOf(inMin, frame * 2 * 4))
        if (rec.state != AudioRecord.STATE_INITIALIZED) {
            rec.release()
            end("麦克风打不开")
            return
        }
        if (AcousticEchoCanceler.isAvailable()) AcousticEchoCanceler.create(rec.audioSessionId)?.enabled = true
        if (NoiseSuppressor.isAvailable()) NoiseSuppressor.create(rec.audioSessionId)?.enabled = true
        _state.update { it.copy(phase = VoiceState.Phase.LIVE) }
        mic = Thread({
            val buf = ShortArray(frame)
            val bytes = ByteArray(frame * 2)
            rec.startRecording()
            try {
                while (!stopped) {
                    val n = rec.read(buf, 0, frame)
                    if (n <= 0) continue
                    if (muted) buf.fill(0, 0, n)
                    var sum = 0.0
                    for (i in 0 until n) {
                        val v = buf[i].toInt()
                        bytes[2 * i] = (v and 0xff).toByte()
                        bytes[2 * i + 1] = (v shr 8 and 0xff).toByte()
                        sum += v.toDouble() * v
                    }
                    ws?.send(bytes.toByteString(0, n * 2))
                    val level = (sqrt(sum / n) / 6000.0).toFloat().coerceIn(0f, 1f)
                    _state.update { it.copy(level = it.level * 0.6f + level * 0.4f) }
                }
            } finally {
                runCatching { rec.stop() }
                rec.release()
            }
        }, "voice-mic").apply { start() }
    }

    private fun clearPlayback() {
        playQueue.clear()
        track?.let { t -> runCatching { t.pause(); t.flush(); t.play() } }
        lastAudioAt = 0
    }

    private fun routeToSpeaker() {
        if (Build.VERSION.SDK_INT >= 31) {
            // A headset or Bluetooth wins when one is connected; else the loudspeaker, not the earpiece.
            val devices = audio.availableCommunicationDevices
            val pick = devices.firstOrNull { it.type in HEADSETS } ?: devices.firstOrNull { it.type == AudioDeviceInfo.TYPE_BUILTIN_SPEAKER }
            pick?.let { audio.setCommunicationDevice(it) }
        } else {
            @Suppress("DEPRECATION")
            audio.isSpeakerphoneOn = !audio.isWiredHeadsetOn && !audio.isBluetoothScoOn
        }
    }

    @Suppress("DEPRECATION")
    private fun restoreAudio() {
        if (!routed) return
        routed = false
        if (Build.VERSION.SDK_INT >= 31) audio.clearCommunicationDevice() else audio.isSpeakerphoneOn = false
        audio.mode = savedMode
    }

    private companion object {
        const val RESTARTED = "连接断了（语音服务可能刚重启，或网络不稳），点下面的按钮重新开始"
        val HEADSETS = setOf(
            AudioDeviceInfo.TYPE_WIRED_HEADSET, AudioDeviceInfo.TYPE_WIRED_HEADPHONES, AudioDeviceInfo.TYPE_BLUETOOTH_SCO,
            AudioDeviceInfo.TYPE_USB_HEADSET, 26, /* TYPE_BLE_HEADSET */
        )
    }
}
