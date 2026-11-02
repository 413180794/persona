import BeautifulUI
import SwiftUI

// 设置 › 外观 (2026-11-01): laid out like 连接器, one row per thing that changes how the app looks (形象, 主题色, 界面风格,
// App 图标 on iPhone and iPad, and 布局 on an iPhone), each opening its own page; the first three are apart (docs/ios.md「设计语言」). Picking anything
// there rebuilds the whole tree (`AppModel.look`, the layout switch); the settings path lives in AppModel, so the
// rebuilt app is back on the same page.

/// The 外观 section of 设置: one row per scene with what it is set to now.
struct AppearanceSection: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        Section {
            Group {
                NavigationLink(value: SettingsPage.persona) {
                    SettingsRow(title: "形象", symbol: Theme.Icon.persona, tint: Theme.persona, value: model.persona.name, look: .oneLine)
                }
                .accessibilityIdentifier("settings-persona")
                NavigationLink(value: SettingsPage.themeColor) {
                    SettingsRow(title: "主题色", symbol: Theme.Icon.themeColor, tint: Theme.accent, value: model.themeColor.title, look: .oneLine)
                }
                .accessibilityIdentifier("settings-color")
                NavigationLink(value: SettingsPage.style) {
                    SettingsRow(title: "界面风格", symbol: Theme.Icon.style, tint: Theme.Hue.purple, value: styleSummary, look: .oneLine)
                }
                .accessibilityIdentifier("settings-style")
                #if os(iOS)
                if model.appIcon.available {
                    NavigationLink(value: SettingsPage.appIcon) {
                        SettingsRow(title: "App 图标", symbol: Theme.Icon.appIcon, tint: Theme.Hue.orange,
                                    value: model.appIcon.current.title, look: .oneLine)
                    }
                    .accessibilityIdentifier("settings-app-icon")
                }
                if UIDevice.current.userInterfaceIdiom == .phone {
                    NavigationLink(value: SettingsPage.layout) {
                        SettingsRow(title: "布局", symbol: Theme.Icon.layout, tint: Theme.Hue.blue,
                                    value: model.oldLayout ? "旧布局" : "新布局", look: .oneLine)
                    }
                    .accessibilityIdentifier("settings-layout")
                }
                #endif
            }
            .bareListRow()
        } header: {
            SettingsHeading("外观")
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
                Group {
                    if AppStyle.current.usesBeautifulUI {
                        BUIListRow("字号") {
                            BUIIconTile(Theme.Icon.textSize, tint: Theme.Hue.gray)
                        } trailing: {
                            BUISegmented(TextSize.allCases.map { .init($0, $0.title) },
                                         selection: Binding(get: { model.textSize }, set: { model.textSize = $0 }))
                                .fixedSize()
                        }
                    } else {
                        LabeledContent("字号") {
                            Picker("字号", selection: Binding(get: { model.textSize }, set: { model.textSize = $0 })) {
                                ForEach(TextSize.allCases) { Text($0.title).tag($0) }
                            }
                            .pickerStyle(.segmented)
                            .fixedSize()
                        }
                    }
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

#if os(iOS)
/// 设置 › 外观 › App 图标: the home screen icon, each choice drawn as it looks on the home screen now (light or dark).
/// The system confirms a switch with its own alert.
struct AppIconSettingsPage: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dynamicTypeSize) private var typeSize

    /// Three a row; two at accessibility sizes.
    private var columns: [GridItem] {
        Array(repeating: GridItem(.flexible(), spacing: Theme.Space.m, alignment: .top), count: typeSize.isAccessibilitySize ? 2 : 3)
    }

    var body: some View {
        let icons = model.appIcon
        List {
            Section {
                LazyVGrid(columns: columns, alignment: .leading, spacing: Theme.Space.l) {
                    ForEach(AppIconChoice.allCases) { tile($0, icons: icons) }
                }
                .padding(.horizontal, Theme.Space.l)
                .listRowInsets(EdgeInsets(top: Theme.Space.l, leading: 0, bottom: Theme.Space.l, trailing: 0))
            } footer: {
                if let failure = icons.failure {
                    Text(failure).foregroundStyle(Theme.danger)
                } else {
                    Text("只换主屏上的图标，App 里的颜色和形象不变。深色模式和「着色」主屏的图标跟着系统换。")
                }
            }
        }
        .themedGroupedPage()
        .navigationTitle("App 图标")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func tile(_ c: AppIconChoice, icons: AppIconModel) -> some View {
        let selected = c == icons.current
        let side = Theme.Size.box80
        let shape = RoundedRectangle(cornerRadius: Theme.Radius.appIcon(side), style: .continuous)
        return Button {
            guard !selected else { return }
            Haptics.tap()
            Task { await icons.select(c) }
        } label: {
            VStack(spacing: Theme.Space.s) {
                Image(c.preview)
                    .resizable()
                    .interpolation(.high)
                    .frame(width: side, height: side)
                    .clipShape(shape)
                    .overlay(shape.stroke(Theme.cardStroke, lineWidth: Theme.Size.line))
                    .overlay {
                        if icons.switching == c { ProgressView().tint(.white) }
                    }
                    .padding(Theme.Space.xs)
                    .overlay(
                        RoundedRectangle(cornerRadius: Theme.Radius.appIcon(side) + Theme.Space.xs, style: .continuous)
                            .stroke(selected ? Theme.accent : .clear, lineWidth: Theme.Size.ruleWide)
                    )
                VStack(spacing: Theme.Space.xxs) {
                    // The tick at the label's size, so the selected tile's labels line up with the others'.
                    HStack(spacing: Theme.Space.xs) {
                        if selected { Image(systemName: Theme.Icon.successFilled).foregroundStyle(Theme.accent) }
                        Text(c.title).font(selected ? Theme.Font.labelStrong : Theme.Font.label).lineLimit(1)
                    }
                    .font(Theme.Font.label)
                    Text(c.subtitle).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(icons.switching != nil)
        .accessibilityLabel("\(c.title)，\(c.subtitle)")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityIdentifier("app-icon-\(c.rawValue)")
    }
}
#endif
