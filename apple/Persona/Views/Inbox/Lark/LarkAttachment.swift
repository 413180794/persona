import Foundation
import ImageIO
import UniformTypeIdentifiers

/// Owns a private snapshot: picker access can end and the original can change without affecting a retry.
/// The last draft/outbox reference releases only this snapshot, never the user's original file.
nonisolated final class LarkAttachment: Identifiable, Equatable, Sendable {
    static let fileLimit = 100 << 20
    static let imageLimit = 15 << 19
    static let selectionLimit = 4
    static let staging = LarkAttachmentStaging()
    /// Allow a bounded file upload on a slow uplink, with one minute for the response.
    static func uploadTimeout(bytes: Int) -> TimeInterval { max(180, TimeInterval(min(max(bytes, 0), fileLimit) / (128 << 10)) + 60) }
    enum Kind: Sendable { case image, file }
    let id = UUID()
    let kind: Kind
    let file: URL
    let name: String
    let mime: String
    let size: Int
    let thumbnail: Data?

    private init(kind: Kind, file: URL, name: String, mime: String, size: Int, thumbnail: Data? = nil) {
        self.kind = kind; self.file = file; self.name = name; self.mime = mime; self.size = size; self.thumbnail = thumbnail
    }
    static func == (lhs: LarkAttachment, rhs: LarkAttachment) -> Bool { lhs.id == rhs.id }
    deinit { try? FileManager.default.removeItem(at: file.deletingLastPathComponent()) }

    enum Failure: String, LocalizedError {
        case invalid = "无法读取附件，请重新选择"
        case empty = "不能发送空文件"
        case tooLarge = "文件不能超过 100 MiB"
        case imageTooLarge = "图片过大，请选择较小的图片"
        var errorDescription: String? { rawValue }
    }

    static func stageFile(_ source: URL) async throws -> LarkAttachment {
        let scoped = source.startAccessingSecurityScopedResource()
        defer { if scoped { source.stopAccessingSecurityScopedResource() } }
        let values = try source.resourceValues(forKeys: [.isRegularFileKey, .fileSizeKey, .contentTypeKey])
        guard values.isRegularFile == true else { throw Failure.invalid }
        guard let size = values.fileSize, size > 0 else { throw Failure.empty }
        guard size <= fileLimit else { throw Failure.tooLarge }
        let copy = try await FileUploads.stage(source, directory: stagingDirectory())
        do {
            try Task.checkCancellation()
            let bytes = (try FileManager.default.attributesOfItem(atPath: copy.path)[.size] as? NSNumber)?.intValue ?? 0
            guard bytes > 0 else { throw Failure.empty }
            guard bytes <= fileLimit else { throw Failure.tooLarge }
            return LarkAttachment(kind: .file, file: copy, name: source.lastPathComponent,
                                  mime: values.contentType?.preferredMIMEType ?? "application/octet-stream", size: bytes)
        } catch {
            try? FileManager.default.removeItem(at: copy.deletingLastPathComponent())
            throw error
        }
    }

    /// Decode off the main actor, apply orientation, resize, and remove photo location metadata.
    static func stagePhoto(_ data: Data) async throws -> LarkAttachment {
        try await Task.detached(priority: .userInitiated) {
            guard let source = CGImageSourceCreateWithData(data as CFData, nil) else { throw Failure.invalid }
            func jpeg(maxPixel: Int, quality: Double) throws -> Data {
                let options: [CFString: Any] = [kCGImageSourceCreateThumbnailFromImageAlways: true,
                    kCGImageSourceCreateThumbnailWithTransform: true, kCGImageSourceThumbnailMaxPixelSize: maxPixel]
                guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary) else { throw Failure.invalid }
                let bytes = NSMutableData()
                guard let dest = CGImageDestinationCreateWithData(bytes, UTType.jpeg.identifier as CFString, 1, nil) else { throw Failure.invalid }
                CGImageDestinationAddImage(dest, image, [kCGImageDestinationLossyCompressionQuality: quality] as CFDictionary)
                guard CGImageDestinationFinalize(dest) else { throw Failure.invalid }
                return bytes as Data
            }
            let bytes = try jpeg(maxPixel: 2560, quality: 0.88)
            guard bytes.count <= imageLimit else { throw Failure.imageTooLarge }
            let thumb = try jpeg(maxPixel: 320, quality: 0.8)
            let dir = try stagingDirectory()
            let file = dir.appending(path: "photo.jpg")
            do { try bytes.write(to: file, options: .atomic) }
            catch { try? FileManager.default.removeItem(at: dir); throw error }
            return LarkAttachment(kind: .image, file: file, name: "照片.jpg", mime: "image/jpeg", size: bytes.count, thumbnail: thumb)
        }.value
    }

    private static func stagingDirectory() throws -> URL {
        staging.sweep()
        var dir = staging.root.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try dir.setResourceValues(values)
        #if os(iOS)
        try FileManager.default.setAttributes([.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication], ofItemAtPath: dir.path)
        #endif
        return dir
    }

    func imagePayload() async throws -> ImagePayload {
        let file = file, mime = mime
        return try await Task.detached { ImagePayload(mime: mime, data: try Data(contentsOf: file).base64EncodedString()) }.value
    }
}

nonisolated struct LarkSendReceipt: Decodable, Sendable {
    var messageID: String?
    enum CodingKeys: String, CodingKey { case messageID = "message_id" }
}

extension APIClient {
    func larkSendAttachment(_ feed: String, attachment: LarkAttachment, clientID: String) async throws -> LarkSendReceipt {
        let root = "/api/v1/lark/conversations/\(feed)"
        switch attachment.kind {
        case .image:
            struct Body: Encodable { var client_id: String; var name: String; var mime: String; var data: Data }
            let bytes = try Data(contentsOf: attachment.file)
            return try await post(root + "/images", body: Body(client_id: clientID, name: attachment.name, mime: attachment.mime, data: bytes), timeout: 180)
        case .file:
            let data = try await uploadBytes(file: attachment.file, path: root + "/files",
                                            query: ["client_id": clientID, "name": attachment.name, "mime": attachment.mime],
                                            timeout: LarkAttachment.uploadTimeout(bytes: attachment.size))
            return try JSON.decoder.decode(LarkSendReceipt.self, from: data)
        }
    }
}

/// Picker staging is never referenced by a durable record: the outbox copies payloads to its own directory.
/// Sweep once before any new staging, including when startup cleanup races with the first picker.
nonisolated final class LarkAttachmentStaging: @unchecked Sendable {
    let root: URL
    private let lock = NSLock()
    private var swept = false

    init(root: URL = URL.applicationSupportDirectory.appending(path: "lark-attachment-staging")) { self.root = root }

    func sweep() {
        lock.withLock {
            guard !swept else { return }
            swept = true
            let files = FileManager.default
            for child in (try? files.contentsOfDirectory(at: root, includingPropertiesForKeys: [.isDirectoryKey, .isSymbolicLinkKey])) ?? [] {
                guard UUID(uuidString: child.lastPathComponent) != nil,
                      let values = try? child.resourceValues(forKeys: [.isDirectoryKey, .isSymbolicLinkKey]),
                      values.isDirectory == true, values.isSymbolicLink != true else { continue }
                try? files.removeItem(at: child)
            }
        }
    }
}
