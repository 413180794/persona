import SwiftUI

/// Add Provider › From Extension (docs/extensions.md「模型供应商」): the providers the hub's plugins offer, then only the name, the
/// key and maybe another of the provider's models; the address and the kind come from the plugin.
struct LauncherPresetSections: View {
    @Bindable var model: LauncherPresetModel

    var body: some View {
        Section {
            ForEach(model.presets) { p in row(p) }
        } header: {
            Text("供应商")
        } footer: {
            if let p = model.selected {
                Text("地址、接入方式和模型由扩展「\(p.plugin)」提供；加好以后和自己填写的一样可以编辑。")
            }
        }
        Section {
            TextField("名字，比如 jv_team", text: $model.name)
                .font(Theme.Font.body.monospaced())
                .textInputAutocapitalization(.never).autocorrectionDisabled()
                .accessibilityIdentifier("launcher.preset.name")
        } header: {
            Text("名字")
        } footer: {
            if let problem = model.nameProblem {
                Text(problem).foregroundStyle(Theme.danger)
            } else {
                Text("字母、数字、_ 和 -；按这个名字选择供应商。")
            }
        }
        Section {
            SecretField("API Key", filled: String(localized: "已填写，点保存"), text: $model.apiKey, identifier: "launcher.preset.key")
        } header: {
            Text("Key")
        } footer: {
            Text([model.selected?.keyHint, String(localized: "Key 加密存在 hub，不会返回到客户端。")].compactMap { $0 }.joined(separator: "\n"))
        }
        if let p = model.selected {
            Section {
                if p.choices.isEmpty {
                    TextField("默认模型", text: $model.model).textInputAutocapitalization(.never).autocorrectionDisabled()
                } else {
                    Picker("默认模型", selection: $model.model) {
                        ForEach(p.choices) { m in Text(m.label).tag(m.id) }
                    }
                }
            } header: {
                Text("默认模型")
            } footer: {
                if let e = p.modelsError, !e.isEmpty {
                    Text("读不到扩展的模型列表（\(e)），这里是它声明的。")
                } else {
                    Text("各用途的具体选择在「模型配置」里统一管理；未单独指定时使用这里的默认。")
                }
            }
        }
        if let error = model.error {
            Section { Label(error, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger) }
        }
    }

    private func row(_ p: LauncherPreset) -> some View {
        let picked = model.selectedID == p.id
        return Button { model.selectedID = p.id } label: {
            HStack(spacing: Theme.Space.m) {
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(p.name).font(Theme.Font.body).foregroundStyle(.primary).lineLimit(1)
                    if let d = p.description, !d.isEmpty {
                        Text(d).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                    Text(LauncherPresetText.subtitle(p)).font(Theme.Font.caption).foregroundStyle(.tertiary).lineLimit(1)
                }
                Spacer(minLength: Theme.Space.s)
                if picked {
                    Image(systemName: Theme.Icon.done).font(Theme.Font.labelStrong).foregroundStyle(Theme.accent)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(picked ? .isSelected : [])
        .accessibilityIdentifier("launcher.preset.\(p.id)")
    }
}
