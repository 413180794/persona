import SwiftUI

/// 数据由非空的设置页面读取；空的 List 行不会启动自己的 task。
/// 替换前确认，选择后仍需页面的保存按钮。
struct ExtensionTriageTemplatePicker: View {
    @Binding var text: String
    var templates: [ExtensionTextTemplate]
    @State private var selected: ExtensionTextTemplate?

    var body: some View {
        if !templates.isEmpty {
            Menu {
                ForEach(templates) { template in
                    Button { selected = template } label: { Text(template.title).lineLimit(1) }
                        .accessibilityIdentifier("extension-triage-template-\(template.id)")
                }
            } label: {
                Label { Text("从模板填").lineLimit(1) } icon: { Image(systemName: Theme.Icon.insert) }
            }
            .confirmationDialog("替换当前的规则草稿？", isPresented: Binding(
                get: { selected != nil }, set: { if !$0 { selected = nil } }), titleVisibility: .visible) {
                if let template = selected {
                    Button {
                        text = ExtensionTextContributions.filling(template)
                        selected = nil
                    } label: { Text("替换草稿").lineLimit(1) }
                    .accessibilityIdentifier("extension-triage-template-confirm")
                }
                Button("取消", role: .cancel) {}
            } message: {
                Text("现有草稿会被模板替换；不会自动保存到 hub。")
            }
        }
    }
}
