import Foundation

/// 听写与长录音共享麦克风，失败的启动不能停掉另一个使用者的音频会话。
@MainActor final class RecordingMicrophone {
    static let shared = RecordingMicrophone()
    private var owner: UUID?

    func claim(_ id: UUID) -> Bool {
        guard owner == nil else { return false }
        owner = id
        return true
    }

    @discardableResult func release(_ id: UUID) -> Bool {
        guard owner == id else { return false }
        owner = nil
        return true
    }
}
