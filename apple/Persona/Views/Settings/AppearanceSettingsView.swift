import BeautifulUI
import SwiftUI

// 设置 › 外观 (2026-11-01): laid out like 连接器, one row per thing that changes how the app looks (形象, 主题色, 界面风格,
// 深浅色 on iPhone and iPad, and 布局 on an iPhone), each opening its own page; the first three are apart (docs/ios.md「设计语言」).
// The home screen icon is the 形象's, not a row of its own (#3203). Picking anything
// there rebuilds the whole tree (`AppModel.look`, the layout switch); the settings path lives in AppModel, so the
// rebuilt app is back on the same page.

/// The 外观 section of 设置: one row per scene with what it is set to now.
struct AppearanceSection: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        Section {
            Group {
                NavigationLink(value: SettingsPage.persona) {
                    SettingsRow(title: String(localized: "形象"), symbol: Theme.Icon.persona, tint: Theme.persona, value: model.persona.cardTitle, look: .oneLine)
                }
                .settingsListRow(SettingsPage.persona)
                .accessibilityIdentifier("settings-persona")
                NavigationLink(value: SettingsPage.themeColor) {
                    SettingsRow(title: String(localized: "主题色"), symbol: Theme.Icon.themeColor, tint: Theme.accent, value: model.themeColor.title, look: .oneLine)
                }
                .settingsListRow(SettingsPage.themeColor)
                .accessibilityIdentifier("settings-color")
                NavigationLink(value: SettingsPage.style) {
                    SettingsRow(title: String(localized: "界面风格"), symbol: Theme.Icon.style, tint: Theme.Hue.purple, value: styleSummary, look: .oneLine)
                }
                .settingsListRow(SettingsPage.style)
                .accessibilityIdentifier("settings-style")
                NavigationLink(value: SettingsPage.language) {
                    SettingsRow(title: String(localized: "语言"), symbol: Theme.Icon.language, tint: Theme.Hue.green,
                                value: AppLanguage.settingsSummary, look: .oneLine)
                }
                .accessibilityIdentifier("settings-language")
                #if os(iOS) || os(visionOS)
                NavigationLink(value: SettingsPage.colorScheme) {
                    SettingsRow(title: String(localized: "深浅色"), symbol: Theme.Icon.colorScheme, tint: Theme.Hue.indigo,
                                value: model.colorScheme.title, look: .oneLine)
                }
                .settingsListRow(SettingsPage.colorScheme)
                .accessibilityIdentifier("settings-scheme")
                if UIDevice.current.userInterfaceIdiom == .phone {
                    NavigationLink(value: SettingsPage.layout) {
                        SettingsRow(title: String(localized: "布局"), symbol: Theme.Icon.layout, tint: Theme.Hue.blue,
                                    value: model.oldLayout ? String(localized: "旧布局") : String(localized: "新布局"), look: .oneLine)
                    }
                    .settingsListRow(SettingsPage.layout)
                    .accessibilityIdentifier("settings-layout")
                }
                #endif
            }
            .bareListRow()
        } header: {
            SettingsHeading(String(localized: "外观"))
        }
    }

    /// The style, and the text size when it is not the standard one.
    private var styleSummary: String {
        model.textSize == .standard ? model.style.title : String(localized: "\(model.style.title) · 字号\(model.textSize.title)")
    }
}

/// 设置 › 外观 › 形象: Persona's name, face, status dot, logo and home screen icon (its colors are the 主题色 跟随形象).
struct PersonaSettingsPage: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        SettingsForm {
            Section {
                PersonaPicker()
                    .listRowInsets(EdgeInsets(top: 10, leading: 0, bottom: 10, trailing: 0))
            } footer: {
                VStack(alignment: .leading, spacing: Theme.Space.s) {
                    #if os(macOS)
                    Text(model.isDemo ? "体验模式只有默认形象。" :
                        "Persona 的名字、头像、顶栏的状态圆点和 App 里的标志跟着形象换，App 的颜色不变；想用形象的配色，在主题色里选「跟随形象」。形象包放在 hub 的 ~/.persona/personas 里，选中时下载到这台 Mac。")
                    #else
                    Text(model.isDemo ? "体验模式只有默认形象。" :
                        "Persona 的名字、头像、顶栏的状态圆点、App 里的标志和桌面图标跟着形象换，App 的颜色不变；想用形象的配色，在主题色里选「跟随形象」。形象包放在 hub 的 ~/.persona/personas 里，选中时下载到手机。")
                    if let failure = model.appIcon.failure {
                        Text(failure).foregroundStyle(Theme.danger)
                    }
                    Text("桌面图标只能用 App 自带的几张：hub 上或扩展的形象在 App 里全部生效，桌面图标用它指定的内置形象的，没指定就用默认。")
                        // The icon on now, or that the system cannot switch it here (AppIconTests).
                        .accessibilityValue(model.appIcon.available ? model.appIcon.current.rawValue : "unavailable")
                        .accessibilityIdentifier("persona-icon-note")
                    #endif
                }
            }
        }
        .themedGroupedPage()
        .detailTitle(String(localized: "形象"))
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// 设置 › 外观 › 主题色: the sample of the look and the colors.
struct ThemeColorSettingsPage: View {
    var body: some View {
        SettingsForm {
            Section {
                ThemeColorPicker()
                    .listRowInsets(EdgeInsets(top: 10, leading: 0, bottom: 10, trailing: 0))
            } footer: {
                Text("按钮、链接、选中态和你自己的气泡用主题色，黏土和 Island 的底色是它的淡色调。「跟随界面风格」用每种风格原来的颜色。")
            }
        }
        .themedGroupedPage()
        .detailTitle(String(localized: "主题色"))
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// 设置 › 外观 › 界面风格: the style and the text size.
struct StyleSettingsPage: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        SettingsForm {
            Section {
                StylePicker()
                    .listRowInsets(EdgeInsets(top: 10, leading: 0, bottom: 10, trailing: 0))
            } footer: {
                Text("浮层和卡片的画法、按钮、字体和对话里的组件，整个 App 一起换，包括收件箱里的飞书页面。")
            }
            Section {
                Group {
                    if AppStyle.current.usesBeautifulUI {
                        BUIListRow(String(localized: "字号")) {
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
                            #if os(macOS)
                            .labelsHidden() // the row says 字号; a Mac form drew it again after the control (#1221)
                            #endif
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
        .detailTitle(String(localized: "界面风格"))
        .navigationBarTitleDisplayMode(.inline)
    }
}

#if os(iOS) || os(visionOS)
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
                Text("打开后回到底部四个标签（Persona、Agents、收件箱、设置）。新布局是顶部「助手 | Agents」，收件箱、定时任务、知识库在左上角的侧边栏里。过渡几天后会去掉这个开关。")
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

#if os(iOS) || os(visionOS)
#if DEBUG
/// `probe.scheme` (`-probes YES`): the choice and the look the page is actually drawn in (ColorSchemeIconTests).
private struct SchemeProbe: ViewModifier {
    let choice: ColorSchemeChoice
    @Environment(\.colorScheme) private var shown

    func body(content: Content) -> some View {
        content.screenProbe("scheme", Value(choice: choice.rawValue, shown: shown == .dark ? "dark" : "light"))
    }

    struct Value: Encodable, Equatable {
        var choice: String
        var shown: String
    }
}
#endif

/// 设置 › 外观 › 深浅色: 跟随系统 / 浅色 / 深色 (`ColorSchemeChoice`), the system's own inline picker.
struct ColorSchemeSettingsPage: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        List {
            Section {
                Picker("深浅色", selection: Binding(get: { model.colorScheme }, set: { model.colorScheme = $0 })) {
                    ForEach(ColorSchemeChoice.allCases) { c in
                        Label { Text(c.title).lineLimit(1) } icon: { Image(systemName: c.symbol) }
                            .tag(c)
                            .accessibilityIdentifier("scheme-\(c.rawValue)")
                    }
                }
                .pickerStyle(.inline)
                .labelsHidden()
            } footer: {
                Text("跟随系统时，手机换深浅色 App 跟着换。选了浅色或深色，App 里的界面就一直是那样；形象是团子（默认）时，主屏上的图标也换成浅色或深色那张（系统会提示一次）。灵动岛、锁屏的实时活动、小组件和手表仍跟随系统。")
            }
        }
        .themedGroupedPage()
        #if DEBUG
        .modifier(SchemeProbe(choice: model.colorScheme))
        #endif
        .navigationTitle("深浅色")
        .navigationBarTitleDisplayMode(.inline)
    }
}
#endif
