import SwiftUI

// 设置 › 连接器 › GitHub › 收件箱 (docs/inbox.md「连接器：GitHub」): which CI failures come in, and repositories
// that bring nothing in. Kept on the hub (PUT /api/v1/connectors/github/settings {"options": …}).

/// GitHub's inbox options, as GET /api/v1/connectors gives them.
nonisolated struct GitHubInboxOptions: Codable, Equatable, Sendable {
    var ci: String
    var mutedRepos: [String]
    var ignoreBranches: [String]?

    enum CodingKeys: String, CodingKey {
        case ci
        case mutedRepos = "muted_repos", ignoreBranches = "ignore_branches"
    }

    init?(_ json: JSONValue?) {
        guard let json, let data = try? JSON.encoder.encode(json),
              let o = try? JSON.decoder.decode(GitHubInboxOptions.self, from: data) else { return nil }
        self = o
    }

    /// The CI choices, most quiet first: (value, title).
    static let ciModes: [(String, String)] = [("off", String(localized: "state.off", defaultValue: "关闭")), ("default_branch", String(localized: "只看默认分支")), ("mine", String(localized: "加上我的 PR")), ("all", String(localized: "全部"))]
}

extension APIClient {
    /// Saves GitHub's inbox options; the hub answers with the connector as it lists it.
    func setGitHubInboxOptions(_ options: GitHubInboxOptions) async throws -> HubConnector {
        struct Body: Encodable, Sendable { var options: GitHubInboxOptions }
        return try JSON.decoder.decode(HubConnector.self, from: await request("PUT", "/api/v1/connectors/github/settings", body: Body(options: options)))
    }
}

/// The 收件箱 section of GitHub's page: the CI failures picker and the muted repositories.
struct GitHubInboxSettings: View {
    @Environment(AppModel.self) private var model
    @State var options: GitHubInboxOptions
    var saved: (HubConnector) -> Void
    @State private var newRepo = ""
    @State private var error: String?

    var body: some View {
        Section {
            Picker("CI 失败", selection: Binding(get: { options.ci }, set: { options.ci = $0; save() })) {
                ForEach(GitHubInboxOptions.ciModes, id: \.0) { value, title in Text(title).tag(value) }
            }
            .pickerStyle(.menu)
            ForEach(options.mutedRepos, id: \.self) { repo in
                Label(repo, systemImage: Theme.Icon.muted).lineLimit(1)
            }
            .onDelete { idx in
                options.mutedRepos.remove(atOffsets: idx)
                save()
            }
            HStack {
                TextField("静音仓库：owner/name", text: $newRepo)
                    #if os(macOS)
                    .labelsHidden() // the title is the prompt (#1221)
                    #endif
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .onSubmit(add)
                Button(action: add) { Text("添加").lineLimit(1) }
                    .disabled(!newRepo.contains("/"))
            }
            if let error {
                Text(error).font(Theme.Font.label).foregroundStyle(Theme.danger)
            }
        } header: {
            Text("收件箱")
        } footer: {
            Text(footer)
        }
    }

    private var footer: String {
        let ci = switch options.ci {
        case "off": String(localized: "CI 失败不进收件箱。")
        case "default_branch": String(localized: "只有默认分支的 CI 失败进收件箱，算「需要你」。")
        case "all": String(localized: "所有分支的 CI 失败都算「需要你」。")
        default: String(localized: "默认分支和你自己开着的 PR 的 CI 失败算「需要你」，其他分支的算「知悉即可」。")
        }
        let auto = (options.ignoreBranches ?? ["train/*"]).joined(separator: String(localized: "、", comment: "Separator between items in a list"))
        return ci + String(localized: "同一个分支的失败合成一件，重跑也算同一件；后来跑成功、分支删了或 PR 合了就自动处理掉。自动分支（\(auto)）不提醒。")
            + String(localized: "真人的 @、请你 review、要求修改算「需要你」，机器人发的算「知悉即可」。静音的仓库什么都不进收件箱，已有的也会处理掉。")
    }

    private func add() {
        let repo = newRepo.trimmingCharacters(in: .whitespaces)
        guard repo.contains("/"), !options.mutedRepos.contains(repo) else { return }
        options.mutedRepos.append(repo)
        newRepo = ""
        save()
    }

    private func save() {
        let o = options
        Task {
            do {
                if let c = try await model.api?.setGitHubInboxOptions(o) {
                    error = nil
                    saved(c)
                }
            } catch {
                self.error = error.shown.map { String(localized: "没存上：\($0)") }
            }
        }
    }
}
