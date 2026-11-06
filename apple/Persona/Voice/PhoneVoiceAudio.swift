#if os(iOS)
import AVFoundation
import Foundation
import os

// The call's audio on an iPhone (docs/voice.md 6.4). Adapted from the audio half of `DuplexVoiceSession` in PR #1931
// (CarPlay voice conversations, another contributor's work, not merged; coordination asked in #1951): the
// play-and-record / voice-chat session with voice processing, AVAudioConverter down to the extension's PCM16, an
// AVAudioPlayerNode at its output rate, dropping what `clear` cancels by generation, and ending on a call or a media
// services reset. Changed for the phone: the microphone goes into a `VoiceUplink` (held until `ready`, muted as silence,
// measured for the orb), a route change rebuilds the tap and converter instead of ending the call, voice processing
// that will not run (the simulator) falls back to plain capture, and the player can take the app's own cues. Written as
// one engine for every surface, so CarPlay can use it as is. Only APIs in the Xcode 26.0 SDK.

/// Turns the microphone's buffers into PCM16 at the extension's input rate and hands them to the uplink. The tap calls
/// `feed` on the audio thread, one buffer at a time (the converter's one-buffer handoff is #1931's).
nonisolated final class VoiceCapturePump: @unchecked Sendable {
    private let converter: AVAudioConverter
    private let target: AVAudioFormat
    private let sourceRate: Double
    private let uplink: VoiceUplink
    private let rate: Int
    // Touched only from the audio thread (feed) and the converter's input block, which runs inside feed.
    private var input: AVAudioPCMBuffer?
    private var supplied = false

    init(converter: AVAudioConverter, target: AVAudioFormat, sourceRate: Double, uplink: VoiceUplink, rate: Int) {
        self.converter = converter
        self.target = target
        self.sourceRate = sourceRate
        self.uplink = uplink
        self.rate = rate
    }

    func feed(_ buffer: AVAudioPCMBuffer) {
        guard sourceRate > 0,
              let out = AVAudioPCMBuffer(pcmFormat: target,
                                         frameCapacity: AVAudioFrameCount(Double(buffer.frameLength) * target.sampleRate / sourceRate) + 1)
        else { return }
        input = buffer
        supplied = false
        var failure: NSError?
        let status = converter.convert(to: out, error: &failure, withInputFrom: { _, inputStatus in
            self.supply(inputStatus)
        })
        input = nil
        guard status != .error, failure == nil, out.frameLength > 0, let channel = out.int16ChannelData?[0] else { return }
        uplink.push(VoicePCM.data(UnsafeBufferPointer(start: channel, count: Int(out.frameLength))), rate: rate)
    }

    private func supply(_ status: UnsafeMutablePointer<AVAudioConverterInputStatus>) -> AVAudioBuffer? {
        guard !supplied, let buffer = input else {
            status.pointee = .noDataNow
            return nil
        }
        supplied = true
        status.pointee = .haveData
        return buffer
    }
}

@MainActor final class PhoneVoiceAudio: VoiceAudio {
    nonisolated static let log = Logger(subsystem: "jarvis", category: "voice")

    /// Whether the microphone runs with echo cancellation (false where voice processing would not start: the simulator).
    private(set) var cancelsEcho = false

    private var engine: AVAudioEngine?
    private var player: AVAudioPlayerNode?
    private var playFormat: AVAudioFormat?
    private var pump: VoiceCapturePump?
    private var uplink: VoiceUplink?
    private var format = VoiceAudioFormat.provisional
    private var events: (@MainActor (VoiceAudioEvent) -> Void)?
    /// Bumped by `clear` and every rebuild: completions of buffers scheduled before are ignored.
    private var generation = 0
    /// The loudness of the voice's buffers still queued, in order: the first is the one playing.
    private var queued: [Float] = []
    private var activated = false
    private var observers: [NSObjectProtocol] = []

    func start(_ format: VoiceAudioFormat, uplink: VoiceUplink, events: @escaping @MainActor (VoiceAudioEvent) -> Void) throws {
        stop()
        self.format = format
        self.uplink = uplink
        self.events = events
        let session = AVAudioSession.sharedInstance()
        do {
            // voiceChat also allows Bluetooth hands-free; defaultToSpeaker keeps the voice off the earpiece.
            try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker])
            try session.setActive(true)
        } catch {
            Self.log.error("voice: audio session failed: \(error.localizedDescription, privacy: .public)")
            throw VoiceAudioError.session
        }
        activated = true
        do {
            try build()
        } catch {
            stop()
            throw error
        }
        observe()
    }

    func reconfigure(_ format: VoiceAudioFormat) throws {
        guard format != self.format else { return }
        self.format = format
        guard let engine else { return }
        engine.stop()
        try rewire(engine)
    }

    func play(_ pcm: Data) { schedule(pcm, voice: true) }

    func cue(_ pcm: Data) { schedule(pcm, voice: false) }

    func clear() {
        generation += 1
        let was = !queued.isEmpty
        queued = []
        if let player {
            player.stop()
            if engine?.isRunning == true { player.play() }
        }
        if was {
            events?(.outputLevel(0))
            events?(.playing(false))
        }
    }

    func stop() {
        for token in observers { NotificationCenter.default.removeObserver(token) }
        observers = []
        if let engine {
            engine.inputNode.removeTap(onBus: 0)
            engine.stop()
        }
        player?.stop()
        engine = nil
        player = nil
        playFormat = nil
        pump = nil
        generation += 1
        queued = []
        if activated {
            activated = false
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        }
        events = nil
        uplink = nil
    }

    // MARK: Engine

    /// Voice processing first, so the speaker's sound is taken out of the microphone; where it will not run, plain capture.
    private func build() throws {
        let engine = AVAudioEngine()
        do {
            // Before the formats are read: voice processing changes the input node's format.
            try engine.inputNode.setVoiceProcessingEnabled(true)
            cancelsEcho = true
        } catch {
            cancelsEcho = false
            Self.log.notice("voice: no voice processing, capturing without echo cancellation: \(error.localizedDescription, privacy: .public)")
        }
        do {
            try wire(engine)
            engine.prepare()
            try engine.start()
            self.engine = engine
        } catch where cancelsEcho {
            // The simulator turns voice processing on and then cannot start the engine with it.
            Self.log.notice("voice: the engine would not start with voice processing, capturing without echo cancellation")
            engine.inputNode.removeTap(onBus: 0)
            engine.stop()
            cancelsEcho = false
            let plain = AVAudioEngine()
            try wire(plain)
            plain.prepare()
            do { try plain.start() } catch { throw VoiceAudioError.engine }
            self.engine = plain
        }
    }

    /// The tap and the converter for the input's format as it is now, and a player at the output rate.
    private func wire(_ engine: AVAudioEngine) throws {
        guard let uplink,
              let playFormat = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: Double(format.outputRate),
                                             channels: 1, interleaved: false),
              let captureFormat = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: Double(format.inputRate),
                                                channels: 1, interleaved: true)
        else { throw VoiceAudioError.format }
        let input = engine.inputNode
        let hardware = input.outputFormat(forBus: 0)
        guard hardware.sampleRate > 0, hardware.channelCount > 0,
              let converter = AVAudioConverter(from: hardware, to: captureFormat) else { throw VoiceAudioError.format }
        let player = AVAudioPlayerNode()
        engine.attach(player)
        engine.connect(player, to: engine.mainMixerNode, format: playFormat)
        let pump = VoiceCapturePump(converter: converter, target: captureFormat, sourceRate: hardware.sampleRate,
                                    uplink: uplink, rate: format.inputRate)
        Self.installTap(input, pump)
        self.player = player
        self.playFormat = playFormat
        self.pump = pump
    }

    /// Takes the engine apart and puts it together again for the input as it is now (a new route or new rates). What
    /// was queued to play is gone with the old player.
    private func rewire(_ engine: AVAudioEngine) throws {
        engine.inputNode.removeTap(onBus: 0)
        if let player { engine.detach(player) }
        player = nil
        pump = nil
        let was = !queued.isEmpty
        generation += 1
        queued = []
        if was {
            events?(.outputLevel(0))
            events?(.playing(false))
        }
        try wire(engine)
        engine.prepare()
        do { try engine.start() } catch { throw VoiceAudioError.engine }
    }

    // The tap runs on the audio thread: its closure is made here, outside the main actor.
    nonisolated private static func installTap(_ input: AVAudioInputNode, _ pump: VoiceCapturePump) {
        input.installTap(onBus: 0, bufferSize: 1024, format: input.outputFormat(forBus: 0)) { buffer, _ in
            pump.feed(buffer)
        }
    }

    // MARK: Playback

    private func schedule(_ pcm: Data, voice: Bool) {
        guard let player, let playFormat, engine?.isRunning == true else { return }
        let samples = VoicePCM.floats(pcm)
        guard !samples.isEmpty,
              let buffer = AVAudioPCMBuffer(pcmFormat: playFormat, frameCapacity: AVAudioFrameCount(samples.count)),
              let channel = buffer.floatChannelData?[0]
        else { return }
        buffer.frameLength = AVAudioFrameCount(samples.count)
        samples.withUnsafeBufferPointer { channel.update(from: $0.baseAddress!, count: samples.count) }
        let generation = generation
        if voice {
            let level = VoicePCM.rms(samples)
            if queued.isEmpty {
                events?(.playing(true))
                events?(.outputLevel(level))
            }
            queued.append(level)
        }
        player.scheduleBuffer(buffer, completionCallbackType: .dataPlayedBack) { @Sendable [weak self] _ in
            Task { @MainActor in self?.played(generation, voice: voice) }
        }
        if !player.isPlaying { player.play() }
    }

    /// One buffer finished: the next one's loudness, or quiet when none is left.
    private func played(_ generation: Int, voice: Bool) {
        guard voice, generation == self.generation, !queued.isEmpty else { return }
        queued.removeFirst()
        if let next = queued.first {
            events?(.outputLevel(next))
        } else {
            events?(.outputLevel(0))
            events?(.playing(false))
        }
    }

    // MARK: System

    private func observe() {
        let center = NotificationCenter.default
        observers.append(center.addObserver(forName: AVAudioSession.interruptionNotification, object: nil,
                                            queue: .main) { @Sendable [weak self] note in
            let type = (note.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt).flatMap(AVAudioSession.InterruptionType.init)
            guard type == .began else { return }
            MainActor.assumeIsolated { self?.events?(.interrupted) }
        })
        observers.append(center.addObserver(forName: AVAudioSession.mediaServicesWereResetNotification, object: nil,
                                            queue: .main) { @Sendable [weak self] _ in
            MainActor.assumeIsolated { self?.events?(.reset) }
        })
        // A route change (headphones in or out, Bluetooth) stops the engine and can change the input's format: set the
        // tap and the converter up again and go on; only when that fails does the call end.
        guard let engine else { return }
        observers.append(center.addObserver(forName: .AVAudioEngineConfigurationChange, object: engine,
                                            queue: .main) { @Sendable [weak self] _ in
            MainActor.assumeIsolated { self?.routeChanged() }
        })
    }

    private func routeChanged() {
        guard let engine, !engine.isRunning else { return }
        do {
            try rewire(engine)
            Self.log.notice("voice: audio route changed, microphone set up again")
        } catch {
            Self.log.error("voice: audio route changed and the microphone could not be set up again")
            events?(.failed)
        }
    }
}
#endif
