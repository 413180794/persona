import SwiftUI

// Two of 设置 › 外观's three choices of how the app looks (docs/ios.md「设计语言」), each apart from the others: 主题色
// (`ThemeColorPicker`) and 界面风格 (`StylePicker`); the third is 形象 (`PersonaPicker`). A choice rebuilds the view tree
// (`AppModel.look`); 外观 stays open because the settings path is AppModel's.

/// 主题色: the app as it looks now, small (drawn with the live tokens), and the colors under it: 跟随界面风格, 跟随形象
/// when the persona brings colors, then the styles' own colors, each a disc of its accent.
struct ThemeColorPicker: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dynamicTypeSize) private var typeSize

    /// 跟随形象 only when the persona brings colors, or when it is the choice already (so the choice stays in sight).
    private var options: [ThemeColor] {
        let persona = model.persona.palette != nil || model.themeColor == .persona
        return [.style] + (persona ? [.persona] : []) + ThemeColor.presets
    }

    /// Four a row; three at accessibility sizes.
    private var columns: [GridItem] {
        Array(repeating: GridItem(.flexible(), spacing: Theme.Space.s, alignment: .top), count: typeSize.isAccessibilitySize ? 3 : 4)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.l) {
            LookSample()
            LazyVGrid(columns: columns, alignment: .leading, spacing: Theme.Space.m) {
                ForEach(options) { swatch($0) }
            }
            if !ExtensionThemesModel.shared.offers.isEmpty {
                Text("扩展主题").font(Theme.Font.sectionHeader).foregroundStyle(.secondary)
                ForEach(ExtensionThemesModel.shared.offers) { offer in
                    if let palette = ExtensionThemeContrast.palette(offer) {
                        Button {
                            ExtensionThemesModel.shared.select(offer)
                            Haptics.success()
                        } label: {
                            HStack(spacing: Theme.Space.m) {
                                Circle().fill(palette.accent.color)
                                    .scaledFrame(width: Theme.Size.iconL, height: Theme.Size.iconL)
                                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                    Text(offer.theme.name).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                                    Text(offer.ownerName).font(Theme.Font.label).foregroundStyle(Color.secondary).lineLimit(1)
                                }
                                Spacer(minLength: Theme.Space.s)
                                if model.themeColor == .extensionTheme, ExtensionThemesModel.shared.palette?.id == offer.id {
                                    Image(systemName: Theme.Icon.done).foregroundStyle(Theme.accent)
                                }
                            }
                        }
                        .themedButton(prominent: false)
                        .accessibilityIdentifier("extension-theme-\(offer.id)")
                        .accessibilityValue(model.themeColor == .extensionTheme && ExtensionThemesModel.shared.palette?.id == offer.id ? "已选择" : "未选择")
                    }
                }
            }
        }
        .padding(.horizontal, Theme.Space.l)
    }

    /// Where a choice's colors come from, under its name.
    private func origin(_ c: ThemeColor) -> String {
        switch c {
        case .style: model.style.component.colors.title
        case .persona: model.persona.palette == nil ? "这个形象没带" : model.persona.name
        default: c.preset?.subtitle ?? ""
        }
    }

    private func swatch(_ c: ThemeColor) -> some View {
        let selected = c == model.themeColor
        let colors = c.colors(style: model.style, persona: model.persona.palette)
        return Button {
            guard !selected else { return }
            Haptics.success()
            withAnimation(Theme.Motion.standard) { model.themeColor = c }
        } label: {
            VStack(spacing: Theme.Space.xs) {
                ZStack {
                    Circle().fill(colors.accent)
                    if c == .persona, model.persona.palette != nil {
                        PersonaAvatar(size: Theme.Size.iconL, persona: model.persona)
                    } else if selected {
                        Image(systemName: Theme.Icon.done).font(Theme.Font.heading).foregroundStyle(.white)
                    }
                }
                .scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
                .padding(Theme.Space.xs)
                .overlay(Circle().stroke(selected ? colors.accent : .clear, lineWidth: Theme.Size.ruleWide))
                VStack(spacing: Theme.Space.xxs) {
                    Text(c.title).font(selected ? Theme.Font.labelStrong : Theme.Font.label).lineLimit(1).minimumScaleFactor(0.75)
                    Text(origin(c)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1).minimumScaleFactor(0.75)
                }
            }
            .frame(maxWidth: .infinity)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(c.title)，\(origin(c))")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityIdentifier("color-\(c.rawValue)")
    }
}

/// The app as it looks now, small: the user's bubble, a reply on a card with a link in it, and the buttons. Drawn
/// with the live tokens and recipes (`.surface`, `.themedButton`), so it is what the three choices give together.
private struct LookSample: View {
    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            Text("明天出门要带伞吗？")
                .font(Theme.Font.secondary)
                .foregroundStyle(Theme.userBubbleText)
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.s)
                .background(Theme.userBubbleFill, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                .frame(maxWidth: .infinity, alignment: .trailing)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Text("上午有小雨，带一把折叠伞。").font(Theme.Font.secondary)
                Text("查看逐小时天气").font(Theme.Font.secondaryMedium).foregroundStyle(Theme.accent)
            }
            .padding(Theme.Space.m)
            .frame(maxWidth: .infinity, alignment: .leading)
            .surface(in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            HStack(spacing: Theme.Space.s) {
                Button("加到提醒") {}.themedButton(prominent: true)
                Button("不用了") {}.themedButton()
            }
            .font(Theme.Font.secondaryMedium)
            .lineLimit(1)
            .allowsHitTesting(false)
        }
        .padding(Theme.Space.m)
        .background(Theme.groupedPage, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(Theme.cardStroke, lineWidth: Theme.Size.line))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("预览：现在的主题色和界面风格")
        .accessibilityIdentifier("look-sample")
    }
}

/// 界面风格: the styles in a grid, each drawn as a tiny screen in the colors it would have with the 主题色 chosen (with
/// 跟随界面风格, its own); tap to switch the whole app. A grid, not a row: side by side they were 560 pt wide and the
/// last two were off a 402 pt screen with nothing saying so.
struct StylePicker: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dynamicTypeSize) private var typeSize

    /// Three previews a row; two at accessibility sizes, where three cut every name.
    private var columns: [GridItem] {
        Array(repeating: GridItem(.flexible(), spacing: 12, alignment: .top), count: typeSize.isAccessibilitySize ? 2 : 3)
    }

    var body: some View {
        LazyVGrid(columns: columns, alignment: .leading, spacing: 14) {
            ForEach(AppStyle.allCases) { s in
                Button {
                    guard s != model.style else { return }
                    Haptics.success()
                    withAnimation(Theme.Motion.standard) { model.style = s }
                } label: {
                    StyleCard(style: s, spec: .compose(style: s, color: model.themeColor, persona: model.persona.palette),
                              selected: s == model.style)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(s.title)，\(s.subtitle)")
                .accessibilityAddTraits(s == model.style ? .isSelected : [])
                .accessibilityIdentifier("style-\(s.rawValue)")
            }
        }
        .padding(.horizontal, Theme.Space.l)
    }
}

private struct StyleCard: View {
    var style: AppStyle
    var spec: StyleSpec
    var selected: Bool

    var body: some View {
        let corner = style.corner * 0.45
        VStack(spacing: Theme.Space.s) {
            ZStack {
                RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
                    .fill(spec.usesGlass ? AnyShapeStyle(spec.accent.opacity(0.10)) : AnyShapeStyle(spec.page))
                VStack(alignment: .leading, spacing: Theme.Space.sm) {
                    RoundedRectangle(cornerRadius: corner, style: .continuous).fill(spec.card)
                        .scaledFrame(height: Theme.Size.markM)
                        .shadow(color: [.puffy, .floating].contains(spec.surface) ? .black.opacity(0.12) : .clear, radius: 4, y: 2)
                    HStack { Spacer(); Capsule().fill(spec.userBubble).scaledFrame(width: Theme.Size.avatar, height: Theme.Size.markXS) }
                    Capsule().fill(Color.secondary.opacity(0.25)).scaledFrame(width: Theme.Size.avatarL, height: Theme.Size.dot)
                    HStack(spacing: Theme.Space.xs) {
                        Circle().fill(spec.accent).scaledFrame(width: Theme.Size.dotXL, height: Theme.Size.dotXL)
                        Capsule().fill(spec.accent.opacity(0.35)).scaledFrame(height: Theme.Size.dot)
                    }
                }
                .padding(Theme.Space.m)
            }
            .frame(maxWidth: .infinity)
            .scaledFrame(height: Theme.Size.box120)
            .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).stroke(selected ? spec.accent : Color.primary.opacity(0.1), lineWidth: selected ? 2.5 : 1))
            VStack(spacing: Theme.Space.xxs) {
                HStack(spacing: Theme.Space.xs) {
                    if selected { Image(systemName: Theme.Icon.successFilled).foregroundStyle(spec.accent) }
                    Text(style.title).font(Theme.Font.captionStrong).lineLimit(1)
                }
                Text(style.subtitle).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
        }
        .frame(maxWidth: .infinity)
    }
}
