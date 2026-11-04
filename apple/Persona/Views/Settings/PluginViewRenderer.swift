import SwiftUI

/// A plugin's View drawn with the app's design system (docs/plugins.md「声明式 UI」): each section a section of the form
/// (a divider starts another one), each item a row — text, a number with what it is, a bar, a list row, a button, a
/// link opened in the app, a field to fill in. Tones are the app's role colors. Buttons that ask first ask; the ones
/// that submit take the page's fields along; what comes back (a new view, a word, a link) is handled here.
struct PluginViewRenderer: View {
    var view: PluginUIView
    var model: PluginViewModel
    /// The first section's title when it has none of its own (a launcher's section: what the plugin calls it).
    var heading: String? = nil
    /// Where links and `open_url` open: the page's link card.
    @Binding var link: LinkTarget?

    /// A section split at its dividers: the title goes with the first part, the footer with the last.
    private struct Part: Identifiable {
        var id: Int
        var title: String?
        var footer: String?
        var items: [PluginUIItem]
    }

    private var groups: [Part] {
        var out: [Part] = []
        for (i, s) in view.sections.enumerated() {
            var parts: [[PluginUIItem]] = [[]]
            for item in s.items {
                if item.type == "divider" { parts.append([]) } else { parts[parts.count - 1].append(item) }
            }
            parts = parts.filter { !$0.isEmpty }
            if parts.isEmpty { parts = [[]] }
            for (j, part) in parts.enumerated() {
                let title = j == 0 ? (s.title?.isEmpty == false ? s.title : (i == 0 ? heading : nil)) : nil
                out.append(Part(id: out.count, title: title, footer: j == parts.count - 1 ? s.footer : nil, items: part))
            }
        }
        return out
    }

    var body: some View {
        ForEach(groups) { g in
            Section {
                ForEach(Array(g.items.enumerated()), id: \.offset) { _, item in
                    PluginItemRow(item: item, model: model, link: $link)
                }
            } header: {
                if let t = g.title, !t.isEmpty { SettingsHeading(t) }
            } footer: {
                if let f = g.footer, !f.isEmpty { Text(f) }
            }
        }
    }
}

/// How a plugin's words map onto the app's look.
enum PluginUIStyle {
    /// neutral | positive | caution | danger → the role colors; anything else reads as neutral.
    static func tone(_ t: String?) -> Color {
        switch t {
        case "positive": Theme.positive
        case "caution": Theme.caution
        case "danger": Theme.danger
        default: .secondary
        }
    }

    /// A metric's number: its tone, or the text's own color when neutral.
    static func ink(_ t: String?) -> Color {
        t == nil || t == "neutral" ? .primary : tone(t)
    }

    /// A bar's fill: neutral takes the accent.
    static func bar(_ t: String?) -> Color {
        t == nil || t == "neutral" ? Theme.accent : tone(t)
    }

    static func font(_ style: String?) -> Font {
        switch style {
        case "secondary": Theme.Font.secondary
        case "caption": Theme.Font.caption
        case "code": Theme.Font.code
        default: Theme.Font.body
        }
    }
}

/// One item of a plugin's View.
private struct PluginItemRow: View {
    var item: PluginUIItem
    @Bindable var model: PluginViewModel
    @Binding var link: LinkTarget?

    var body: some View {
        switch item.type {
        case "text":
            Text(item.text ?? "")
                .font(PluginUIStyle.font(item.style))
                .foregroundStyle(item.style == "secondary" || item.style == "caption" ? Color.secondary : Color.primary)
                .textSelection(.enabled)
        case "metric": metric
        case "progress": progress
        case "row":
            if let action = item.action {
                PluginActionButton(action: action, title: item.title ?? "", model: model, link: $link) { rowLabel(chevron: true) }
                    .foregroundStyle(Color.primary)
            } else {
                rowLabel(chevron: false)
            }
        case "button":
            if let action = item.action {
                PluginActionButton(action: action, title: item.title ?? "", destructive: item.role == "destructive", model: model, link: $link) {
                    HStack(spacing: Theme.Space.s) {
                        Text(item.title ?? "").lineLimit(1).minimumScaleFactor(0.8)
                        if model.running == action.id { ProgressView().controlSize(.small) }
                    }
                }
            }
        case "link":
            if let url = item.url.flatMap(URL.init(string:)), url.scheme == "https" {
                Button { link = LinkTarget(url: url) } label: {
                    Label { Text(item.title ?? url.host() ?? "").lineLimit(1) } icon: { Image(systemName: Theme.Icon.link) }
                }
                .accessibilityIdentifier("plugin-ui-link")
            }
        case "field": PluginFieldItem(item: item, model: model)
        default: EmptyView()
        }
    }

    private var metric: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
            Text(item.label ?? "").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            Text(item.value ?? "").font(Theme.Font.title.monospacedDigit()).foregroundStyle(PluginUIStyle.ink(item.tone)).lineLimit(1)
                .minimumScaleFactor(0.6)
            if let d = item.detail, !d.isEmpty { Text(d).font(Theme.Font.label).foregroundStyle(.secondary) }
        }
        .padding(.vertical, Theme.Space.xxs)
        .accessibilityElement(children: .combine)
    }

    private var progress: some View {
        let ratio = min(max(item.ratio ?? 0, 0), 1)
        return VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(spacing: Theme.Space.sm) {
                Text(item.label ?? "").font(Theme.Font.caption).lineLimit(1)
                Spacer(minLength: Theme.Space.s)
                Text("\(Int((ratio * 100).rounded()))%").font(Theme.Font.caption.monospacedDigit())
                    .foregroundStyle(item.tone == nil || item.tone == "neutral" ? Color.secondary : PluginUIStyle.tone(item.tone)).lineLimit(1)
            }
            ProgressView(value: ratio).tint(PluginUIStyle.bar(item.tone))
            if let d = item.detail, !d.isEmpty { Text(d).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2) }
        }
        .padding(.vertical, Theme.Space.xxs)
        .accessibilityElement(children: .combine)
    }

    private func rowLabel(chevron: Bool) -> some View {
        HStack(spacing: Theme.Space.s) {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(item.title ?? "").font(Theme.Font.body).lineLimit(2)
                if let s = item.subtitle, !s.isEmpty { Text(s).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2) }
            }
            Spacer(minLength: Theme.Space.s)
            if let v = item.value, !v.isEmpty { Text(v).font(Theme.Font.body.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1) }
            if let b = item.badge { Chip(text: b.text, look: b.tone == nil || b.tone == "neutral" ? .neutral : .tint(PluginUIStyle.tone(b.tone))) }
            if chevron {
                if let a = item.action, model.running == a.id {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelStrong).foregroundStyle(.tertiary)
                }
            }
        }
        .contentShape(.rect)
    }
}

/// A button or row that sends its action back to the plugin, asking first when it says so; the answer's word is a
/// toast, its link opens in the app.
private struct PluginActionButton<Content: View>: View {
    @Environment(AppModel.self) private var app
    var action: PluginUIAction
    var title: String
    var destructive = false
    var model: PluginViewModel
    @Binding var link: LinkTarget?
    @ViewBuilder var label: Content
    @State private var asking = false

    var body: some View {
        Button(role: destructive ? .destructive : nil) {
            if action.confirm?.isEmpty == false { asking = true } else { run() }
        } label: {
            label
        }
        .disabled(model.running != nil)
        .accessibilityIdentifier("plugin-ui-action-\(action.id)")
        .confirmationDialog(action.confirm ?? "", isPresented: $asking, titleVisibility: .visible) {
            Button(role: destructive ? .destructive : nil) { run() } label: { Text(title.isEmpty ? "确定" : title).lineLimit(1) }
                .accessibilityIdentifier("plugin-ui-confirm")
            Button("取消", role: .cancel) {}
        }
    }

    private func run() {
        Task {
            let o = await model.perform(action)
            if let e = o.error {
                Haptics.error()
                app.toast = Toast(e, style: .error)
            } else if let t = o.toast {
                app.toast = Toast(t, style: .success)
            }
            if let url = o.url { link = LinkTarget(url: url) }
        }
    }
}

/// A field of a plugin's View: text, a secret (typed in hidden, never filled in for you), a number, a switch, a choice.
private struct PluginFieldItem: View {
    var item: PluginUIItem
    @Bindable var model: PluginViewModel

    private var name: String { item.name ?? "" }
    private var text: Binding<String> {
        Binding(get: { model.values[name] ?? "" }, set: { model.values[name] = $0 })
    }

    var body: some View {
        Group {
            switch item.kind {
            case "toggle":
                Toggle(isOn: Binding(get: { model.values[name] == "true" }, set: { model.values[name] = $0 ? "true" : "false" })) {
                    Text(item.label ?? name).lineLimit(2)
                }
            case "choice":
                Picker(selection: text) {
                    if !(item.choices ?? []).contains(where: { $0.value == (model.values[name] ?? "") }) {
                        Text(item.placeholder ?? "未选").tag(model.values[name] ?? "")
                    }
                    ForEach(item.choices ?? []) { c in Text(c.label).tag(c.value) }
                } label: {
                    Text(item.label ?? name).lineLimit(2)
                }
            case "secret":
                labeled {
                    SecureField(item.placeholder ?? "", text: text)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                }
            case "number":
                labeled {
                    TextField(item.placeholder ?? "", text: text).keyboardType(.decimalPad)
                }
            default:
                labeled {
                    TextField(item.placeholder ?? "", text: text)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                }
            }
        }
        .accessibilityIdentifier("plugin-ui-field-\(name)")
    }

    /// The label above the field.
    private func labeled(@ViewBuilder _ content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Text(item.label ?? name).font(Theme.Font.labelStrong).foregroundStyle(.secondary).lineLimit(2)
            #if os(macOS)
            content().labelsHidden() // a Mac form would put the label beside an empty box (#1221)
            #else
            content()
            #endif
        }
        .padding(.vertical, Theme.Space.xxs)
    }
}

extension View {
    /// Where a plugin's links open: the link card (iPhone) or the panel at the trailing edge (Mac).
    @ViewBuilder func pluginLinks(_ link: Binding<LinkTarget?>) -> some View {
        #if os(macOS)
        macLinkPanel(link)
        #else
        modifier(PluginLinkSheet(link: link))
        #endif
    }
}

#if os(iOS)
private struct PluginLinkSheet: ViewModifier {
    @Environment(AppModel.self) private var model
    @Binding var link: LinkTarget?

    func body(content: Content) -> some View {
        content.sheet(item: $link) { t in LinkSheet(target: t, key: model.assistantMain ?? model.assistantCurrent) }
    }
}
#endif

/// 设置 › 插件 › a plugin › one of its pages (placement plugin_page): its View as a page of its own.
struct PluginPageView: View {
    @State private var page: PluginViewModel
    @State private var link: LinkTarget?
    var title: String

    init(plugin: String, ui: String, title: String) {
        _page = State(initialValue: PluginViewModel(plugin: plugin, ui: ui))
        self.title = title
    }

    var body: some View {
        SettingsForm {
            if let v = page.view {
                PluginViewRenderer(view: v, model: page, link: $link)
            } else if let e = page.error {
                Section {
                    EmptyStateView(symbol: Theme.Icon.error, title: "读不到这一页", message: e,
                                   action: ("重试", { Task { await page.load() } }))
                }
                .listRowBackground(Color.clear)
            } else if page.unsupported {
                Section {
                    EmptyStateView(symbol: Theme.Icon.plugins, title: "这里没有内容", message: "插件现在不在这里显示东西。")
                }
                .listRowBackground(Color.clear)
            }
        }
        .themedPage()
        .overlay { if page.view == nil && page.error == nil && !page.unsupported { ProgressView() } }
        .detailTitle(page.view?.title ?? title)
        .navigationBarTitleDisplayMode(.inline)
        .pluginLinks($link)
        .task { await page.load() }
        .refreshable { await page.load() }
    }
}

/// A launcher's page › what plugins draw for it (placement launcher_section): a section each, after its quota. A plugin
/// that says the launcher's gateway is not its own (404 unsupported) draws nothing, and that is not an error. The page
/// owns the model and loads it (`PluginLauncherModel.load`, with a hub that has the plugin base).
struct PluginLauncherSections: View {
    var page: PluginLauncherModel
    @Binding var link: LinkTarget?

    var body: some View {
        ForEach(page.shown) { entry in
            if let v = entry.model.view {
                PluginViewRenderer(view: v, model: entry.model, heading: entry.place.title, link: $link)
            } else {
                Section {
                    if let e = entry.model.error {
                        Label(e, systemImage: Theme.Icon.error).font(Theme.Font.label).foregroundStyle(Theme.danger)
                        Button { Task { await entry.model.load() } } label: { Text("重试").lineLimit(1) }
                            .accessibilityIdentifier("plugin-ui-retry")
                    } else {
                        HStack(spacing: Theme.Space.s) {
                            ProgressView()
                            Text("正在读取…").font(Theme.Font.label).foregroundStyle(.secondary)
                        }
                    }
                } header: {
                    SettingsHeading(entry.place.title)
                }
            }
        }
    }
}
