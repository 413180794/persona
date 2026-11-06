import Foundation

/// Dependency status in the details and the install preview; not the manifest's version constraint itself.
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

/// Each contribution's effective switch; an older hub has no contributions field, and no fake switches are made up.
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

/// Before disabling, shows the affected extensions the server returned; cascade is sent only after confirming.
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

/// One confirmation covers each preview's own permission summary; for an older hub that needs no bundle, the new field is not sent.
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
    /// `async`: a hub with `extension_operations` answers at once with the operation and pushes its steps (#3307).
    var background: Bool?

    init(preview: ExtensionPreview, background: Bool = false) {
        self.background = background ? true : nil
        previewID = preview.previewID
        permissionsDigest = preview.permissionsDigest
        let refs = (preview.bundle ?? []).map { BundleReference(previewID: $0.previewID, permissionsDigest: $0.permissionsDigest) }
        bundle = refs.isEmpty ? nil : refs
    }

    enum CodingKeys: String, CodingKey {
        case previewID = "preview_id", permissionsDigest = "permissions_digest", bundle, background = "async"
    }
}

/// Unsaved forms live only in memory, kept apart per hub and extension; rebuilding the view or reloading does not drop the drafts.
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
