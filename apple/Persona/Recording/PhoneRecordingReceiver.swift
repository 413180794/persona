#if os(iOS) || os(visionOS)
import Foundation

/// WCSession 激活前安装接收器；系统可能只为了投递文件在后台启动 App。
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
            } catch { /* 不确认；手表保留原件并重试。 */ }
        }
        RecordingCapture.shared.onSaved = { RecordingTranscription.shared.enqueue($0) }
        RecordingTranscription.shared.resumePending()
    }
}
#endif
