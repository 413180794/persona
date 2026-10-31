import SwiftUI

/// 设置 › 外观: the persona, the style and the text size, everything that changes how the app looks. Picking one rebuilds
/// the whole tree (`AppModel.look`); the settings path lives in AppModel, so the rebuilt app is back on this page.
struct AppearanceSettingsView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        List {
            Section {
                PersonaPicker()
                    .listRowInsets(EdgeInsets(top: 10, leading: 0, bottom: 10, trailing: 0))
            } header: {
                Text("形象")
            } footer: {
                #if os(macOS)
                Text(model.isDemo ? "体验模式只有默认形象。" :
                    "Jarvis 的名字、头像、顶栏的状态圆点和 App 的主色跟着形象一起换。形象包放在 hub 的 ~/.jarvis/personas 里，选中时下载到这台 Mac。")
                #else
                Text(model.isDemo ? "体验模式只有默认形象。" :
                    "Jarvis 的名字、头像、顶栏的状态圆点和 App 的主色跟着形象一起换。形象包放在 hub 的 ~/.jarvis/personas 里，选中时下载到手机。")
                #endif
            }

            #if os(iOS)
            if UIDevice.current.userInterfaceIdiom == .phone {
                Section {
                    Toggle(isOn: Binding(get: { model.oldLayout }, set: { setOldLayout($0) })) {
                        Text("旧布局").lineLimit(1)
                    }
                    .accessibilityIdentifier("old-layout")
                } header: {
                    Text("布局")
                } footer: {
                    Text("打开后回到底部四个标签（Jarvis、Agents、收件箱、设置）。新布局是顶部「聊天 | 工作」，收件箱、定时任务、知识库在左上角的侧边栏里。过渡几天后会去掉这个开关。")
                }
            }
            #endif

            Section {
                StylePicker()
                    .listRowInsets(EdgeInsets(top: 10, leading: 0, bottom: 10, trailing: 0))
                LabeledContent("字号") {
                    Picker("字号", selection: Binding(get: { model.textSize }, set: { model.textSize = $0 })) {
                        ForEach(TextSize.allCases) { Text($0.title).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    .fixedSize()
                }
                .accessibilityIdentifier("text-size")
            } header: {
                Text("界面风格")
            } footer: {
                Text("整个 App 一起换，包括收件箱里的飞书页面。字号在系统的「文字大小」之上再调小或调大一档。")
            }
        }
        .themedGroupedPage()
        .navigationTitle("外观")
        .navigationBarTitleDisplayMode(.inline)
    }

    /// Switching rebuilds the root: land on 设置 › 外观 again either way (the old layout's 设置 tab, the shell's sheet).
    private func setOldLayout(_ on: Bool) {
        model.shellSettings = !on
        model.oldLayout = on
        model.selectedTab = .settings
    }
}
