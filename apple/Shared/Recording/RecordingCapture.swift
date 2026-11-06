#if os(iOS) || os(visionOS) || os(watchOS)
import AVFoundation
import Observation
#if os(iOS) || os(visionOS)
import UIKit
#else
import WatchKit
#endif

/// Hourhand's phone / watch session management, sharing the segment engine, pausing, bookmarks and resource protection.
@Observable
final class RecordingCapture {
    static let shared = RecordingCapture()
    enum State { case idle, starting, recording, paused }
    private(set) var state: State = .idle
    private(set) var elapsed: Double = 0
    private(set) var recordingID: UUID?
    var error: String?
    /// Non-blocking hint, e.g. the Bluetooth microphone was not found and the phone mic is used.
    private(set) var notice: String?
    var onSaved: ((UUID) -> Void)?
    @ObservationIgnored private var input: RecordingInput = .phone
    @ObservationIgnored private var writer: RecordingSegmentWriter?
    @ObservationIgnored private var ticker: Task<Void, Never>?
    @ObservationIgnored private var interrupted = false
    @ObservationIgnored private var observers: [NSObjectProtocol] = []
    @ObservationIgnored private var generation = UUID()
    @ObservationIgnored private var activeMicOwner: UUID?
    @ObservationIgnored private var resumeTask: Task<Void, Never>?

    private init() {
        RecordingLibrary.shared.recover(source: Self.source)
        let center = NotificationCenter.default
        observers.append(center.addObserver(forName: AVAudioSession.interruptionNotification, object: nil, queue: .main) { @Sendable [weak self] note in
            let type = (note.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt).flatMap(AVAudioSession.InterruptionType.init)
            MainActor.assumeIsolated { self?.interruption(type) }
        })
        observers.append(center.addObserver(forName: .AVAudioEngineConfigurationChange, object: nil, queue: .main) { @Sendable [weak self] _ in
            MainActor.assumeIsolated {
                guard let self, self.state == .recording else { return }
                do { try self.writer?.restartAfterConfigurationChange() }
                catch { self.error = String(localized: "麦克风发生变化，录音已暂停"); self.writer?.pause(); self.state = .paused }
            }
        })
        observers.append(center.addObserver(forName: AVAudioSession.mediaServicesWereResetNotification, object: nil, queue: .main) { @Sendable [weak self] _ in
            MainActor.assumeIsolated {
                guard let self, self.state == .recording else { return }
                self.stop(); self.error = String(localized: "系统音频服务已重置，已保存定稿部分，请重新开始录音")
            }
        })
    }

    private static var source: RecordingEntry.Source {
        #if os(watchOS)
        .watch
        #else
        .phone
        #endif
    }

    private func configureSession(token: UUID) async throws {
        let session = AVAudioSession.sharedInstance()
        guard generation == token else { return }
        notice = nil
        #if os(iOS) || os(visionOS)
        #if os(iOS)
        if input == .bluetooth {
            try session.setCategory(.playAndRecord, mode: .default, options: [.allowBluetoothHFP])
            try session.setActive(true)
            if let port = await bluetoothInput(session, token: token) {
                guard generation == token else { return }
                try session.setPreferredInput(port)
                return
            }
            guard generation == token else { return }
            notice = String(localized: "没有找到眼镜或蓝牙耳机的麦克风，已改用手机麦克风")
        }
        #endif
        try session.setCategory(.playAndRecord, mode: .default, options: [.defaultToSpeaker])
        try session.setActive(true)
        #if os(iOS)
        let ports = session.availableInputs ?? []
        if let index = RecordingInput.preferredIndex(ports.map(\.portType), for: .phone) {
            try session.setPreferredInput(ports[index])
        }
        #else
        if let mic = session.availableInputs?.first(where: { $0.portType == .builtInMic }) { try session.setPreferredInput(mic) }
        #endif
        #else
        try session.setCategory(.record, mode: .default)
        try session.setActive(true)
        #endif
    }

    #if os(iOS)
    /// The Bluetooth HFP input appears only after the route settles, so poll for up to two seconds.
    private func bluetoothInput(_ session: AVAudioSession, token: UUID) async -> AVAudioSessionPortDescription? {
        var waited = 0
        while generation == token, !Task.isCancelled {
            let ports = session.availableInputs ?? []
            if let index = RecordingInput.preferredIndex(ports.map(\.portType), for: .bluetooth) { return ports[index] }
            guard waited < 10 else { return nil }
            do { try await Task.sleep(for: .milliseconds(200)) } catch { return nil }
            waited += 1
        }
        return nil
    }

    /// Name of the input the current route records from, shown while recording.
    var inputName: String? {
        guard state == .recording || state == .paused else { return nil }
        return AVAudioSession.sharedInstance().currentRoute.inputs.first?.portName
    }
    #endif

    private static func permission() async -> Bool {
        if #available(iOS 17.0, watchOS 10.0, *) { return await AVAudioApplication.requestRecordPermission() }
        return await withCheckedContinuation { continuation in
            AVAudioSession.sharedInstance().requestRecordPermission { @Sendable allowed in continuation.resume(returning: allowed) }
        }
    }

    func start(locale: String = "zh-CN", input: RecordingInput = .phone) async {
        guard state == .idle else { return }
        generation = UUID(); let token = generation
        state = .starting; error = nil; notice = nil
        self.input = input
        let allowed = await Self.permission()
        guard generation == token, state == .starting else { return }
        guard allowed else { state = .idle; error = String(localized: "请在系统设置中允许 Persona 使用麦克风"); return }
        if let problem = Self.resourceProblem(starting: true) { state = .idle; error = problem; return }
        let micOwner = UUID()
        guard RecordingMicrophone.shared.claim(micOwner) else {
            state = .idle; error = String(localized: "麦克风正在用于听写，请先结束听写再录音"); return
        }
        activeMicOwner = micOwner
        do {
            try await configureSession(token: token)
            // Cancelled while Bluetooth discovery was running: cancelStarting() already released the session and mic.
            guard generation == token else { return }
            let entry = try RecordingLibrary.shared.create(source: Self.source, locale: locale)
            recordingID = entry.id
            let writer = RecordingSegmentWriter(directory: RecordingLibrary.shared.folder(entry.id))
            writer.segmentLength = Self.segmentLength
            writer.onSegment = { @Sendable [weak self] segment in
                Task { @MainActor in
                    self?.save(segment, id: entry.id)
                }
            }
            try writer.start()
            self.writer = writer
            elapsed = 0; state = .recording
            startTicker()
        } catch {
            guard generation == token else { return }
            if let id = recordingID { RecordingLibrary.shared.change(id) { $0.active = false; $0.expectedSegments = 0 } }
            recordingID = nil; state = .idle
            releaseAudioSession()
            self.error = String(localized: "麦克风打不开，请稍后重试")
        }
    }

    func cancelStarting() {
        guard state == .starting else { return }
        generation = UUID(); state = .idle; notice = nil
        releaseAudioSession()
    }

    func togglePause() {
        guard let writer else { return }
        interrupted = false
        if state == .recording {
            resumeTask?.cancel(); resumeTask = nil
            writer.pause(); state = .paused
        } else if state == .paused, resumeTask == nil {
            let token = generation
            resumeTask = Task { @MainActor [weak self] in
                defer { self?.resumeTask = nil }
                guard let self else { return }
                do {
                    try await self.configureSession(token: token)
                    guard self.generation == token, self.writer === writer, self.state == .paused else { return }
                    try writer.resume(); self.state = .recording
                } catch {
                    guard self.generation == token else { return }
                    self.error = String(localized: "麦克风打不开，录音仍已保存")
                }
            }
        }
    }

    func addMark() {
        guard let id = recordingID, let writer else { return }
        let time = writer.elapsed
        RecordingLibrary.shared.change(id) { entry in
            if let last = entry.marks.last, time - last < 1 { return }
            entry.marks.append(time)
        }
    }

    @discardableResult func stop() -> UUID? {
        if state == .starting { cancelStarting(); return nil }
        guard let writer, let id = recordingID else { return nil }
        generation = UUID()
        resumeTask?.cancel(); resumeTask = nil
        ticker?.cancel(); ticker = nil
        writer.stop()
        // A stop on a segment boundary leaves no tail segment: take every finalized part before writing the end message.
        for segment in writer.finishedSegments { save(segment, id: id) }
        RecordingLibrary.shared.change(id) { entry in
            entry.active = false
            entry.expectedSegments = (entry.segments.map(\.index).max() ?? -1) + 1
        }
        self.writer = nil; recordingID = nil; state = .idle; interrupted = false; notice = nil
        releaseAudioSession()
        onSaved?(id)
        return id
    }

    private func releaseAudioSession() {
        #if os(iOS)
        try? AVAudioSession.sharedInstance().setPreferredInput(nil)
        #endif
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        if let owner = activeMicOwner {
            activeMicOwner = nil
            RecordingMicrophone.shared.release(owner)
        }
    }

    private func save(_ segment: RecordingSegmentWriter.Segment, id: UUID) {
        RecordingLibrary.shared.addSegment(id, RecordingSegment(index: segment.index, start: segment.start, duration: segment.duration))
        onSaved?(id)
    }

    private func startTicker() {
        ticker = Task { [weak self] in
            var count = 0
            while !Task.isCancelled {
                do { try await Task.sleep(for: .milliseconds(250)) } catch { return }
                guard let self, let writer = self.writer else { return }
                self.elapsed = writer.elapsed
                writer.segmentLength = Self.segmentLength
                count += 1
                if let failure = writer.failure ?? RecordingLibrary.shared.error { self.stop(); self.error = failure; return }
                if count % 20 == 0, let problem = Self.resourceProblem(starting: false) { self.stop(); self.error = problem; return }
            }
        }
    }

    private func interruption(_ type: AVAudioSession.InterruptionType?) {
        if type == .began, state == .recording { interrupted = true; writer?.pause(); state = .paused }
        else if type == .ended, interrupted, state == .paused { togglePause() }
    }

    private static var segmentLength: Double {
        #if os(iOS) || os(visionOS)
        UIApplication.shared.applicationState == .active ? RecordingAudioSpec.liveSegmentSeconds : RecordingAudioSpec.segmentSeconds
        #else
        WatchRecordingTransfer.shared.reachable ? RecordingAudioSpec.liveSegmentSeconds : RecordingAudioSpec.segmentSeconds
        #endif
    }

    private static func resourceProblem(starting: Bool) -> String? {
        let attributes = try? FileManager.default.attributesOfFileSystem(forPath: NSHomeDirectory())
        let free = (attributes?[.systemFreeSize] as? NSNumber)?.int64Value
        if let free, free < (starting ? 300_000_000 : 150_000_000) { return String(localized: "存储空间不足，已保存的录音不会丢失") }
        #if os(watchOS)
        let device = WKInterfaceDevice.current(); device.isBatteryMonitoringEnabled = true
        if device.batteryState == .unplugged, device.batteryLevel >= 0, device.batteryLevel < 0.05 { return String(localized: "手表电量过低，已保存的录音不会丢失") }
        #endif
        return nil
    }
}
#endif
