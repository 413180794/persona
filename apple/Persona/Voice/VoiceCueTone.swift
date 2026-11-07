import Foundation

/// The call's cues as sound (docs/assistant/voice.md 6.6): `Theme.VoiceCue`'s notes as PCM16 at the rate the voice plays at, for
/// `VoiceConversation.play(_:)`. Sine notes with ramped ends (no click), the quiet between them included.
nonisolated enum VoiceCueTone {
    static func pcm(_ cue: Theme.VoiceCue, rate: Int) -> Data {
        VoicePCM.data(samples(cue, rate: rate))
    }

    static func samples(_ cue: Theme.VoiceCue, rate: Int) -> [Int16] {
        guard rate > 0 else { return [] }
        let r = Double(rate)
        let note = Int(Theme.VoiceCue.note * r)
        let gap = Int(Theme.VoiceCue.gap * r)
        let ramp = max(1, Int(Theme.VoiceCue.ramp * r))
        let peak = Theme.VoiceCue.gain * Double(Int16.max)
        var out: [Int16] = []
        out.reserveCapacity(cue.notes.count * (note + gap))
        for (n, hz) in cue.notes.enumerated() {
            if n > 0 { out.append(contentsOf: repeatElement(0, count: gap)) }
            for i in 0..<note {
                let edge = min(i, note - 1 - i)
                let envelope = edge < ramp ? Double(edge) / Double(ramp) : 1
                out.append(Int16(peak * envelope * sin(2 * .pi * hz * Double(i) / r)))
            }
        }
        return out
    }
}
