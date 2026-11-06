import SwiftUI

/// 设置 › 翻译 (docs/ios.md「翻译」): on / off, the target language, and what gets translated: mail (on), and the
/// Feishu chats picked here (none: Feishu has too many messages to translate them all).
struct TranslationSettingsView: View {
    @Environment(AppModel.self) private var model
    @AppStorage(TranslationPrefs.enabledKey) private var enabled = false
    @AppStorage(TranslationPrefs.targetKey) private var target = TranslationPrefs.defaultTarget
    @AppStorage(TranslationPrefs.mailKey) private var mail = true
    @AppStorage(TranslationPrefs.chatsKey) private var chatsRaw = ""
    @State private var picking = false

    private var chats: [TranslationPrefs.Chat] { TranslationPrefs.chats(chatsRaw) }
    private var larkChats: Bool { model.larkFeatures.chats }

    var body: some View {
        SettingsForm {
            Section {
                Toggle(isOn: $enabled) { Label("自动翻译", systemImage: Theme.Icon.translate) }
                ModelConfigurationLink(title: String(localized: "翻译模型"), purposeID: "translate")
            } footer: {
                Text("内容不是目标语言时自动翻译，原文随时可以切回来。翻译由「模型配置」中的翻译模型处理。")
            }
            if enabled {
                Section {
                    Picker("目标语言", selection: $target) {
                        ForEach(TranslationPrefs.languages) { Text($0.name).tag($0.code) }
                    }
                }
                Section {
                    Toggle(isOn: $mail) { Label("邮件", systemImage: Theme.Icon.mail) }
                    // Feishu chats are picked from the hub's (`/api/v1/lark/…`, the Lark extension's).
                    if larkChats {
                        Button { picking = true } label: {
                            LabeledContent {
                                Text(chats.isEmpty ? "不翻译" : "\(chats.count) 个会话").foregroundStyle(.secondary).lineLimit(1)
                            } label: {
                                Label { Text("飞书会话").foregroundStyle(Color.primary) } icon: { Image(systemName: Theme.Icon.conversation) }
                            }
                        }
                        .accessibilityHint("选择要翻译的飞书会话")
                    }
                } header: {
                    Text("翻译范围")
                } footer: {
                    if larkChats { Text("飞书消息很多，默认不翻译；只翻译这里选中的会话里别人发的消息。") }
                }
                if larkChats, !chats.isEmpty {
                    Section("要翻译的飞书会话") {
                        ForEach(chats) { c in
                            Text(c.name).lineLimit(1)
                                #if os(macOS)
                                // A Mac form has no swipe to delete: the row's menu takes it off (#1221).
                                .contextMenu {
                                    Button("不再翻译", role: .destructive) {
                                        chatsRaw = TranslationPrefs.encode(chats.filter { $0.id != c.id })
                                    }
                                }
                                #endif
                        }
                            .onDelete { idx in
                                var list = chats
                                list.remove(atOffsets: idx)
                                chatsRaw = TranslationPrefs.encode(list)
                            }
                    }
                }
            }
        }
        .themedGroupedPage()
        .detailTitle(String(localized: "翻译"))
        .navigationBarTitleDisplayMode(.inline)
        .animation(Theme.Motion.state, value: enabled)
        // The hub translates new mail before it is opened: it needs to know.
        .onChange(of: TranslatePrefsBody(enabled: enabled, target: target, mail: mail)) { _, _ in
            Translator.shared.syncPrefs(hubHas: nil)
        }
        .sheet(isPresented: $picking) {
            TranslationChatPicker(selected: chats) { chatsRaw = TranslationPrefs.encode($0) }
        }
    }
}

/// Picks the Feishu chats to translate: the recent ones (and the ones picked before), searchable by name.
struct TranslationChatPicker: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State var selected: [TranslationPrefs.Chat]
    var save: ([TranslationPrefs.Chat]) -> Void
    @State private var conversations: [LarkConversation] = []
    @State private var query = ""
    @State private var loading = true
    @State private var error: String?
    /// The chats picked as the sheet opened: 取消 asks first once they differ (Mac).
    @State private var opened: Set<String>?

    /// The picked ones first (so a chat no longer in the recent list can still be unpicked), then the recent ones.
    private var rows: [TranslationPrefs.Chat] {
        var seen = Set(selected.map(\.id))
        var out = selected
        for c in conversations where !seen.contains(c.id) {
            seen.insert(c.id)
            out.append(.init(id: c.id, name: c.name))
        }
        return query.isEmpty ? out : out.filter { $0.name.localizedCaseInsensitiveContains(query) }
    }

    var body: some View {
        NavigationStack {
            List {
                if let error { Label(error, systemImage: Theme.Icon.error).font(Theme.Font.caption).foregroundStyle(Theme.danger) }
                ForEach(rows) { c in
                    let on = selected.contains { $0.id == c.id }
                    Button {
                        if on { selected.removeAll { $0.id == c.id } } else { selected.append(c) }
                        Haptics.tap()
                    } label: {
                        HStack(spacing: Theme.Space.m) {
                            let conv = conversations.first { $0.id == c.id }
                            LarkAvatarImage(name: c.name, url: conv?.avatar, isGroup: conv?.isGroup ?? true, size: Theme.Size.wellM)
                            Text(c.name).foregroundStyle(.primary).lineLimit(1)
                            Spacer(minLength: Theme.Space.s)
                            Image(systemName: on ? Theme.Icon.successFilled : Theme.Icon.unchecked)
                                .foregroundStyle(on ? Theme.accent : Color.secondary)
                        }
                    }
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
            }
            .overlay { if loading && conversations.isEmpty { ProgressView() } }
            .pageSearchable(text: $query, prompt: String(localized: "搜索会话"))
            .navigationTitle("要翻译的会话")
            .navigationBarTitleDisplayMode(.inline)
            .sheetCancel(unsaved: opened.map { $0 != Set(selected.map(\.id)) } ?? false) { dismiss() }
            .onAppear { if opened == nil { opened = Set(selected.map(\.id)) } }
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") {
                        save(selected)
                        dismiss()
                    }
                }
            }
            .task { await load() }
        }
        #if os(macOS)
        .macSheet()
        #endif
    }

    private func load() async {
        guard let api = model.api else { return }
        defer { loading = false }
        do {
            // The pinned list and the conversation page are independent GETs; run them together.
            async let pinned = api.larkPinned()
            async let page = api.larkConversations()
            var list = try await pinned
            list += try await page.conversations.filter { c in !list.contains { $0.id == c.id } }
            conversations = list
        } catch {
            self.error = error.shown
        }
    }
}
