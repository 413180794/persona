import Foundation

/// 详情和安装预览里的依赖状态；不是清单里的版本约束本身。
nonisolated struct ExtensionDependencyStatus: Codable, Hashable, Sendable, Identifiable {
    var id: String
    var version: String?
    var state: String
    var installed: String?
    var name: String?

    var title: String { name?.isEmpty == false ? name! : id }
    var description: String {
        switch state {
        case "ok": return installed.map { String(localized: "已就绪 · \($0)") } ?? String(localized: "已就绪")
        case "missing": return String(localized: "先安装这个扩展")
        case "disabled": return String(localized: "先启用这个扩展")
        case "old":
            let needed = version ?? String(localized: "更新的版本"), current = installed ?? String(localized: "未知")
            return String(localized: "需要 \(needed)，当前 \(current)")
        default: return String(localized: "状态未知")
        }
    }
}

/// 每项贡献的有效开关；旧 hub 没有 contributions 字段，不合成假的开关。
nonisolated struct ExtensionContributionSwitch: Codable, Hashable, Sendable, Identifiable {
    var point: String
    var contributionID: String
    var title: String?
    var enabled: Bool
    var canDisable: Bool
    var via: String?

    var id: String { point + "/" + contributionID }
    var label: String { title?.isEmpty == false ? title! : contributionID }

    enum CodingKeys: String, CodingKey {
        case point, title, enabled, via
        case contributionID = "id", canDisable = "can_disable"
    }
}

/// 停用前展示由服务端返回的被影响扩展；确认后才发送 cascade。
nonisolated struct ExtensionDisableConfirmation {
    var message: String
    var dependents: [String]

    init(error: APIError) {
        dependents = (error.details?["dependents"]?.array ?? []).compactMap { item in
            item["name"]?.string ?? item["id"]?.string
        }
        let names = dependents.joined(separator: "、")
        message = dependents.isEmpty ? error.message : String(localized: "以下扩展依赖它，将一起停用：\(names)。只停用，不卸载；设置和数据保留。")
    }
}

nonisolated struct ExtensionRemovalResult: Decodable, Sendable {
    var removedMembers: [String]?
    enum CodingKeys: String, CodingKey { case removedMembers = "removed_members" }
}

/// 一次确认对应每个预览各自的权限摘要，旧 hub 不需要 bundle 时不发送新字段。
nonisolated struct ExtensionInstallRequest: Encodable {
    struct BundleReference: Encodable {
        var previewID: String
        var permissionsDigest: String
        enum CodingKeys: String, CodingKey {
            case previewID = "preview_id", permissionsDigest = "permissions_digest"
        }
    }
    var previewID: String
    var permissionsDigest: String
    var bundle: [BundleReference]?

    init(preview: ExtensionPreview) {
        previewID = preview.previewID
        permissionsDigest = preview.permissionsDigest
        let refs = (preview.bundle ?? []).map { BundleReference(previewID: $0.previewID, permissionsDigest: $0.permissionsDigest) }
        bundle = refs.isEmpty ? nil : refs
    }

    enum CodingKeys: String, CodingKey {
        case previewID = "preview_id", permissionsDigest = "permissions_digest", bundle
    }
}

/// 未保存的表单只留在内存，按 hub 和扩展隔离，外观重建或重新读取不会丢掉草稿。
@MainActor final class ExtensionDrafts {
    static let shared = ExtensionDrafts()
    struct Key: Hashable { var source: String; var id: String }
    struct Draft { var config: [String: JSONValue]; var numbers: [String: String] }
    var values: [Key: Draft] = [:]
}

extension APIClient {
    func setExtensionContribution(_ ext: String, point: String, id: String, enabled: Bool) async throws -> HubExtensionDetail {
        try JSON.decoder.decode(HubExtensionDetail.self, from: try await request("PUT",
            extensionPath("/\(ext)/-/contributions/\(point)/\(id)"), body: ["enabled": enabled]))
    }
}
