import AudioToolbox
import Foundation
import Testing
@testable import Persona

/// How a call opens the microphone (docs/assistant/voice.md 6.4, #5029): nothing reads `AVAudioEngine.inputNode` unless the session
/// has an input that may be recorded and, on the simulator, a short recording started; the simulator records without
/// voice processing; a probe that does not come back is not started again until it does.
struct VoiceCaptureTests {
    /// Counts the probes a plan ran.
    private final class Probes {
        var count = 0
        let answer: VoiceCapture.Probe
        init(_ answer: VoiceCapture.Probe) { self.answer = answer }
        func run() -> VoiceCapture.Probe {
            count += 1
            return answer
        }
    }

    private func plan(allowed: Bool = true, input: Bool = true, simulator: Bool, _ probes: Probes) -> VoiceCapture {
        VoiceCapture.plan(recordingAllowed: allowed, inputAvailable: input, simulator: simulator, probe: probes.run)
    }

    @Test func aDeviceWithAnInputUsesVoiceProcessingWithoutAProbe() {
        let probes = Probes(.timedOut)
        #expect(plan(simulator: false, probes) == .voiceProcessing)
        #expect(probes.count == 0)
    }

    @Test func noPermissionOrNoInputEndsBeforeTheProbe() {
        for simulator in [false, true] {
            let probes = Probes(.started)
            #expect(plan(allowed: false, simulator: simulator, probes) == .unavailable(.permission))
            #expect(plan(input: false, simulator: simulator, probes) == .unavailable(.noInput))
            #expect(plan(allowed: false, input: false, simulator: simulator, probes) == .unavailable(.permission))
            #expect(probes.count == 0, "the probe runs only for an input that may be recorded")
        }
    }

    @Test func theSimulatorRecordsPlainOnceTheProbeStarted() {
        let probes = Probes(.started)
        #expect(plan(simulator: true, probes) == .plain, "no voice processing on the simulator")
        #expect(probes.count == 1)
    }

    @Test func aSimulatorInputThatWillNotStartEndsTheCall() {
        #expect(plan(simulator: true, Probes(.timedOut)) == .unavailable(.inputWillNotStart))
        #expect(plan(simulator: true, Probes(.failed(-50))) == .unavailable(.inputWillNotStart))
    }

    // MARK: Probe

    @Test func theProbeSaysWhatTheRecordingCameTo() {
        #expect(VoiceInputProbe(attempt: { noErr }).run(timeout: 5) == .started)
        #expect(VoiceInputProbe(attempt: { -50 }).run(timeout: 5) == .failed(-50))
    }

    @Test func aProbeThatDoesNotComeBackIsNotStartedAgainUntilItDoes() async throws {
        let gate = DispatchSemaphore(value: 0)
        let attempts = Attempts()
        let probe = VoiceInputProbe(attempt: {
            attempts.add()
            gate.wait()
            return noErr
        })
        #expect(probe.run(timeout: 0.1) == .timedOut)
        #expect(probe.isOutstanding)
        #expect(probe.run(timeout: 0.1) == .timedOut, "still waiting: no second recording")
        #expect(attempts.count == 1)

        gate.signal()
        let deadline = Date().addingTimeInterval(5)
        while probe.isOutstanding, Date() < deadline { try await Task.sleep(for: .milliseconds(10)) }
        #expect(!probe.isOutstanding)
        gate.signal()
        #expect(probe.run(timeout: 5) == .started, "back: the next call tries again")
        #expect(attempts.count == 2)
    }

    private final class Attempts: @unchecked Sendable {
        private let lock = NSLock()
        private var value = 0
        var count: Int { lock.withLock { value } }
        func add() { lock.withLock { value += 1 } }
    }
}
