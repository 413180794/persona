#if os(iOS) || os(visionOS)
import Foundation

/// Installs the receiver before WCSession activates; the system may launch the app in the background just to deliver a file.
final class PhoneRecordingReceiver {
    static let shared = PhoneRecordingReceiver()
    private var started = false

    func start() {
        guard !started else { return }
        started = true
        _ = RecordingCapture.shared
        WatchSync.shared.onRecording = { transfer in
            do {
                try RecordingLibrary.shared.receive(transfer)
                WatchSync.shared.confirmRecording(RecordingReceipt(id: transfer.id, index: transfer.segment?.index, total: transfer.total))
                RecordingTranscription.shared.enqueue(transfer.id)
            } catch { /* No ack: the watch keeps the original and retries. */ }
        }
        RecordingCapture.shared.onSaved = { RecordingTranscription.shared.enqueue($0) }
        RecordingTranscription.shared.resumePending()
        // Each transcribed segment is saved, and each save syncs the text to the knowledge base (docs/recording.md).
        RecordingSync.shared.start()
    }
}
#endif
