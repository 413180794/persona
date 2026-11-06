import Foundation
import Observation

/// API entries for quick prompts and triage templates; a contribution id is unique only within its extension.
nonisolated struct ExtensionTextTemplate: Codable, Hashable, Sendable, Identifiable {
    var owner: String
    var ownerName: String
    var contributionID: String
    var title: String
    var text: String
    var id: String { owner + "/" + contributionID }

    enum CodingKeys: String, CodingKey {
        case owner = "extension", ownerName = "extension_name", contributionID = "id", title, text
    }
}

extension APIClient {
    func extensionPrompts() async throws -> [ExtensionTextTemplate] {
        struct Answer: Decodable { var prompts: [ExtensionTextTemplate] }
        let answer: Answer = try await get(extensionPath("/-/prompts"))
        return answer.prompts
    }

    func extensionTriageTemplates() async throws -> [ExtensionTextTemplate] {
        struct Answer: Decodable { var templates: [ExtensionTextTemplate] }
        let answer: Answer = try await get(extensionPath("/-/triage-templates"))
        return answer.templates
    }
}

nonisolated struct ExtensionTextSource: Hashable, Sendable {
    var group: String
    var features: Set<String>
    var hub: String? = nil
    var active = true
}

/// Prompts of the hub the current input field belongs to, not carried across groups; a failed read keeps the last result from the same source.
@Observable final class ExtensionTextContributions {
    private(set) var prompts: [ExtensionTextTemplate] = []
    private(set) var templates: [ExtensionTextTemplate] = []
    @ObservationIgnored private var source: String?
    @ObservationIgnored private var generation = 0

    func load(api: APIClient?) async {
        generation += 1
        let mine = generation
        guard let api else { return }
        let next = api.group + "|" + api.config.url.absoluteString
        if next != source {
            source = next
            prompts = []
            templates = []
        }
        guard api.extensionsAPI else {
            prompts = []
            templates = []
            return
        }
        async let promptResult = try? api.extensionPrompts()
        async let templateResult = try? api.extensionTriageTemplates()
        let (newPrompts, newTemplates) = await (promptResult, templateResult)
        guard mine == generation, !Task.isCancelled else { return }
        if let newPrompts, newPrompts != prompts { prompts = newPrompts }
        if let newTemplates, newTemplates != templates { templates = newTemplates }
    }

    func refreshWhileActive(api: APIClient?) async {
        await load(api: api)
        guard api?.extensionsAPI == true else { return }
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(60))
            guard !Task.isCancelled else { return }
            await load(api: api)
        }
    }

    /// Filling the draft is the only action: the caller decides when to confirm replacing; nothing here sends a message or saves a rule.
    static func filling(_ template: ExtensionTextTemplate) -> String { template.text }
}
