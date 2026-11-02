import Foundation

/// The hub owns the purpose catalog and inheritance; clients edit only supported fields.
nonisolated struct ModelBinding: Codable, Hashable, Sendable {
    var provider: String?
    var model: String?
    var effort: String?

    var isEmpty: Bool { provider == nil && model == nil && effort == nil }
}

nonisolated struct ModelPurpose: Codable, Hashable, Identifiable, Sendable {
    var id: String
    var name: String
    var group: String
    var kind: String
    var parent: String?
    var machineID: String?
    var override: ModelBinding
    var effective: ModelBinding
    var revision: Int
    var inheritedFrom: String?
    var status: String?
    var fields: [String]?
    var providerIDs: [String]?

    enum CodingKeys: String, CodingKey {
        case id, name, group, kind, parent, override, effective, revision, status, fields
        case machineID = "machine_id", inheritedFrom = "inherited_from", providerIDs = "provider_ids"
    }

    func supports(_ field: String) -> Bool { (fields ?? ["provider", "model"]).contains(field) }
}

nonisolated struct ModelAvailableModel: Codable, Hashable, Identifiable, Sendable {
    var id: String
    var name: String?
}

nonisolated struct ModelProvider: Codable, Hashable, Identifiable, Sendable {
    var id: String
    var name: String
    var kind: String
    var api: String?
    var model: String?
    var machines: [String]?
    var hidden: Bool?
    var hasKey: Bool?
    var models: [ModelAvailableModel]?

    enum CodingKeys: String, CodingKey {
        case id, name, kind, api, model, machines, hidden, models
        case hasKey = "has_key"
    }
}

nonisolated struct ModelConfiguration: Decodable, Sendable {
    var purposes: [ModelPurpose]
    var providers: [ModelProvider]
}

nonisolated struct ModelPurposeDetail: Decodable, Sendable {
    var purpose: ModelPurpose
    var providers: [ModelProvider]
}

/// Missing fields are unchanged; explicit null removes an override. Never send unsupported fields.
nonisolated struct ModelConfigurationPatch: Encodable, Sendable {
    var revision: Int
    var binding: ModelBinding
    var fields: [String]

    enum CodingKeys: String, CodingKey { case revision, provider, model, effort }

    func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(revision, forKey: .revision)
        for (field, value, key) in [("provider", binding.provider, CodingKeys.provider),
                                    ("model", binding.model, CodingKeys.model), ("effort", binding.effort, CodingKeys.effort)] where fields.contains(field) {
            if let value { try c.encode(value, forKey: key) } else { try c.encodeNil(forKey: key) }
        }
    }
}

extension APIClient {
    func modelConfiguration() async throws -> ModelConfiguration { try await get("/api/v1/model-config") }

    func modelPurpose(_ id: String) async throws -> ModelPurposeDetail {
        try await get(Self.modelPurposePath(id))
    }

    func saveModelPurpose(_ id: String, patch: ModelConfigurationPatch) async throws -> ModelPurposeDetail {
        let data = try await request("PATCH", Self.modelPurposePath(id), body: patch)
        return try JSON.decoder.decode(ModelPurposeDetail.self, from: data)
    }

    private static func modelPurposePath(_ id: String) -> String {
        let encoded = id.addingPercentEncoding(withAllowedCharacters: .alphanumerics.union(CharacterSet(charactersIn: "-_."))) ?? id
        return "/api/v1/model-config/purposes/\(encoded)"
    }
}
