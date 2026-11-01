import SwiftUI

// 设置 › 外观 (2026-11-01): laid out like 连接器, one row per thing that changes how the app looks (形象, 主题色, 界面风格,
// and 布局 on an iPhone), each opening its own page; the first three are apart (docs/ios.md「设计语言」). Picking anything
// there rebuilds the whole tree (`AppModel.look`, the layout switch); the settings path lives in AppModel, so the
// rebuilt app is back on the same page.

/// The 外观 section of 设置: one row per scene with what it is set to now.
struct AppearanceSection: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        Section {
            NavigationLink(value: SettingsPage.persona) {
                LabeledContent {
                    Text(model.persona.name).lineLimit(1)
                } label: {
                    Label("形象", systemImage: Theme.Icon.persona)
                }
            }
            .accessibilityIdentifier("settings-persona")
            NavigationLink(value: SettingsPage.themeColor) {
                LabeledContent {
                    Text(model.themeColor.title).lineLimit(1)
                } label: {
                    Label("主题色", systemImage: Theme.Icon.themeColor)
                }
            }
            .accessibilityIdentifier("settings-color")
            NavigationLink(value: SettingsPage.style) {
                LabeledContent {
                    Text(styleSummary).lineLimit(1)
                } label: {
                    Label("界面风格", systemImage: Theme.Icon.style)
                }
            }
            .accessibilityIdentifier("settings-style")
            #if os(iOS)
            if UIDevice.current.userInterfaceIdiom == .phone {
                NavigationLink(value: SettingsPage.layout) {
                    LabeledContent {
                        Text(model.oldLayout ? "旧布局" : "新布局").lineLimit(1)
                    } label: {
                        Label("布局", systemImage: Theme.Icon.layout)
                    }
                }
                .accessibilityIdentifier("settings-layout")
            }
            #endif
        } header: {
            Text("外观")
        }
    }

    /// The style, and the text size when it is not the standard one.
    private var styleSummary: String {
        model.textSize == .standard ? model.style.title : "\(model.style.title) · 字号\(model.textSize.title)"
    }
}

/// 设置 › 外观 › 形象: Jarvis' name, face and status dot (its colors are the 主题色 跟随形象).
struct PersonaSettingsPage: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        List {
            Section {
                PersonaPicker()
                    .listRowInsets(EdgeInsets(top: 10, leading: 0, bottom: 10, trailing: 0))
            } footer: {
                #if os(macOS)
                Text(model.isDemo ? "体验模式只有默认形象。" :
                    "Jarvis 的名字、头像和顶栏的状态圆点跟着形象换，App 的颜色不变；想用形象的配色，在主题色里选「跟随形象」。形象包放在 hub 的 ~/.jarvis/personas 里，选中时下载到这台 Mac。")
                #else
                Text(model.isDemo ? "体验模式只有默认形象。" :
                    "Jarvis 的名字、头像和顶栏的状态圆点跟着形象换，App 的颜色不变；想用形象的配色，在主题色里选「跟随形象」。形象包放在 hub 的 ~/.jarvis/personas 里，选中时下载到手机。")
                #endif
            }
        }
        .themedGroupedPage()
        .navigationTitle("形象")
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// 设置 › 外观 › 主题色: the sample of the look and the colors.
struct ThemeColorSettingsPage: View {
    var body: some View {
        List {
            Section {
                ThemeColorPicker()
                    .listRowInsets(EdgeInsets(top: 10, leading: 0, bottom: 10, trailing: 0))
            } footer: {
                Text("按钮、链接、选中态和你自己的气泡用主题色，黏土和 Island 的底色是它的淡色调。「跟随界面风格」用每种风格原来的颜色。")
            }
        }
        .themedGroupedPage()
        .navigationTitle("主题色")
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// 设置 › 外观 › 界面风格: the style and the text size.
struct StyleSettingsPage: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        List {
            Section {
                StylePicker()
                    .listRowInsets(EdgeInsets(top: 10, leading: 0, bottom: 10, trailing: 0))
            } footer: {
                Text("浮层和卡片的画法、按钮、字体和对话里的组件，整个 App 一起换，包括收件箱里的飞书页面。")
            }
            Section {
                LabeledContent("字号") {
                    Picker("字号", selection: Binding(get: { model.textSize }, set: { model.textSize = $0 })) {
                        ForEach(TextSize.allCases) { Text($0.title).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    .fixedSize()
                }
                .accessibilityIdentifier("text-size")
            } footer: {
                Text("在系统的「文字大小」之上再调小或调大一档。")
            }
        }
        .themedGroupedPage()
        .navigationTitle("界面风格")
        .navigationBarTitleDisplayMode(.inline)
    }
}

#if os(iOS)
/// 设置 › 外观 › 布局 (iPhone): the ChatGPT-style shell or the old four tabs.
struct LayoutSettingsPage: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        List {
            Section {
                Toggle(isOn: Binding(get: { model.oldLayout }, set: { setOldLayout($0) })) {
                    Text("旧布局").lineLimit(1)
                }
                .accessibilityIdentifier("old-layout")
            } footer: {
                Text("打开后回到底部四个标签（Jarvis、Agents、收件箱、设置）。新布局是顶部「助手 | Agents」，收件箱、定时任务、知识库在左上角的侧边栏里。过渡几天后会去掉这个开关。")
            }
        }
        .themedGroupedPage()
        .navigationTitle("布局")
        .navigationBarTitleDisplayMode(.inline)
    }

    /// Switching rebuilds the root: land on 设置 › 外观 › 布局 again either way (the old layout's 设置 tab, the shell's sheet).
    private func setOldLayout(_ on: Bool) {
        model.shellSettings = !on
        model.oldLayout = on
        model.selectedTab = .settings
    }
}
#endif
