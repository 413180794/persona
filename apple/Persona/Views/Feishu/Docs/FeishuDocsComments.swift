import SwiftUI

// MARK: - Comments

/// Every comment thread of a document: the quoted text and its replies; open threads first, resolved ones on their own
/// tab. Where the hub's Feishu plugin can write comments (`LarkFeatures.docComments`, #1290) each thread has 回复 and
/// 解决 / 重新打开, and the box at the bottom comments on the whole document, answers the thread picked, or comments
/// on the passage the reader sent here; typing @ finds people to mention.
struct FeishuDocsCommentsView: View {
    var url: String
    var title: String
    /// Opened to comment on a passage of the document (the reader's 评论): the box starts there.
    var passage: FeishuDocsCommentsModel.Target? = nil
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var model: FeishuDocsCommentsModel?
    @State private var resolved = false
    @FocusState private var focused: Bool

    private var writes: Bool { app.larkFeatures.docComments }

    var body: some View {
        let model = model
        ScrollView {
            LazyVStack(alignment: .leading, spacing: Theme.Space.m) {
                Picker("状态", selection: $resolved) {
                    Text("未解决 \(model?.threads(resolved: false).count ?? 0)").tag(false)
                    Text("已解决 \(model?.threads(resolved: true).count ?? 0)").tag(true)
                }
                .pickerStyle(.segmented)
                .padding(.bottom, Theme.Space.xs)
                if let model {
                    let shown = model.threads(resolved: resolved)
                    if model.threads == nil {
                        FeishuDocsListState(loading: model.loadError == nil, error: model.loadError, empty: ("text.bubble", "", ""),
                                            retry: { await model.load() })
                    } else if shown.isEmpty {
                        EmptyStateView(symbol: Theme.Icon.conversation, title: resolved ? "没有已解决的评论" : "没有未解决的评论",
                                       message: resolved ? "解决后的评论会收在这里" : (writes ? "在下面写一条，或者划选正文评论" : "在飞书里对文档发表的评论会出现在这里"))
                            .padding(.top, Theme.Space.xxl)
                    } else {
                        ForEach(shown) { t in
                            FeishuDocsThreadCard(thread: t, actions: writes ? actions(model, t) : nil)
                        }
                    }
                }
            }
            .padding(Theme.Space.l)
        }
        .scrollDismissesKeyboard(.interactively)
        .themedPage()
        .safeAreaInset(edge: .bottom) {
            if writes, let model { composer(model) }
        }
        .navigationTitle("评论")
        .navigationBarTitleDisplayMode(.inline)
        #if os(macOS)
        .macSheetDone { dismiss() }
        #else
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Button { dismiss() } label: { Label("关闭", systemImage: Theme.Icon.close) }
            }
        }
        #endif
        .refreshable { await model?.load() }
        .onChange(of: model?.writeError) { _, message in
            guard let message else { return }
            Haptics.error()
            app.toast = Toast(message, style: .error)
        }
        .task {
            LarkMedia.shared.loadEmojiCatalog()
            guard self.model == nil, let api = app.api else { return }
            let m = FeishuDocsCommentsModel(url: url, api: api, target: passage ?? .document)
            #if DEBUG
            // Screenshots: `-showDocCommentDraft <text>` is in the box when it appears (not focused: its @ is looked up at the end);
            // `-showDocCommentReply <thread id>` answers that thread.
            let d = UserDefaults.standard
            if let draft = d.string(forKey: "showDocCommentDraft") { m.draft = draft }
            #endif
            self.model = m
            if passage != nil { focused = true }
            await m.load()
            #if DEBUG
            if let id = d.string(forKey: "showDocCommentReply"), let t = m.threads?.first(where: { $0.id == id }) { m.reply(to: t); focused = true }
            #endif
        }
    }

    private func actions(_ model: FeishuDocsCommentsModel, _ t: FeishuDocsComments.Thread) -> FeishuDocsThreadCard.Actions {
        FeishuDocsThreadCard.Actions(
            replying: { if case .thread(let id, _, _) = model.target { id == t.id } else { false } }(),
            resolving: model.resolving.contains(t.id),
            reply: {
                model.reply(to: t)
                focused = true
            },
            setResolved: { r in Task { await model.setResolved(t, r) } })
    }

    @ViewBuilder private func composer(_ model: FeishuDocsCommentsModel) -> some View {
        @Bindable var model = model
        LarkComposer(placeholder: placeholder(model.target), text: $model.draft, focused: $focused,
                     replyTo: replyBar(model.target), cancelReply: { model.target = .document },
                     quote: passageBar(model), boxID: "docComments.box", sendID: "docComments.send",
                     findPeople: { [model] q in try await model.people(q) }, mentions: $model.mentions) {
            Task { await model.send() }
        }
        .disabled(model.sending)
        .padding(.top, Theme.Space.s)
    }

    private func placeholder(_ t: FeishuDocsCommentsModel.Target) -> String {
        switch t {
        case .document: "评论全文"
        case .thread: "回复"
        case .passage: "评论这段"
        }
    }

    /// 评论这段 over the box while it comments on a passage; × goes back to commenting on the document.
    private func passageBar(_ model: FeishuDocsCommentsModel) -> (heading: String, summary: String, removeLabel: String, remove: () -> Void)? {
        guard case .passage(let quote, _) = model.target else { return nil }
        return ("评论这段", quote, "不评论这段", { model.target = .document })
    }

    private func replyBar(_ t: FeishuDocsCommentsModel.Target) -> LarkReplyTo? {
        guard case .thread(let id, let name, let text) = t else { return nil }
        return LarkReplyTo(id: id, senderName: name, text: text)
    }
}

struct FeishuDocsThreadCard: View {
    /// What the card can do where comments can be written; nil: read only.
    struct Actions {
        var replying: Bool
        var resolving: Bool
        var reply: () -> Void
        var setResolved: (Bool) -> Void
    }

    var thread: FeishuDocsComments.Thread
    var actions: Actions? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            HStack(spacing: Theme.Space.sm) {
                if thread.whole {
                    Text("全文评论").font(Theme.Font.label).foregroundStyle(.secondary)
                } else if !thread.quote.isEmpty {
                    Text(thread.quote)
                        .font(Theme.Font.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .padding(.leading, Theme.Space.s)
                        .overlay(alignment: .leading) { Capsule().fill(LarkColor.hue("yellow")).frame(width: Theme.Size.bar) }
                }
                Spacer(minLength: 0)
                if thread.resolved { FeishuDocsTag(text: "已解决", hue: "green") }
                if thread.visibility != nil { FeishuDocsTag(text: "部分可见", hue: "grey") }
            }
            ForEach(thread.replies) { r in
                HStack(alignment: .top, spacing: Theme.Space.s) {
                    LarkAvatarImage(name: r.author.name, url: r.author.avatarUrl, size: Theme.Size.wellXS)
                    VStack(alignment: .leading, spacing: Theme.Space.xs) {
                        HStack(spacing: Theme.Space.sm) {
                            Text(r.author.name).font(Theme.Font.captionStrong).lineLimit(1)
                            Text(FeishuTime.short(r.created)).font(Theme.Font.label).foregroundStyle(.tertiary).lineLimit(1)
                            if r.edited == true { Text("已编辑").font(Theme.Font.label).foregroundStyle(.tertiary) }
                        }
                        Text(replyText(r)).font(Theme.Font.secondary).fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                        if let n = r.imageCount, n > 0 {
                            Label("\(n) 张图片", systemImage: Theme.Icon.photo).font(Theme.Font.label).foregroundStyle(.secondary)
                        }
                        if let rs = r.reactions, !rs.isEmpty {
                            HStack(spacing: Theme.Space.sm) {
                                ForEach(rs, id: \.key) { re in
                                    HStack(spacing: Theme.Space.xs) {
                                        LarkEmojiImage(key: re.key, size: 16)
                                        Text("\(re.count)").font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary)
                                    }
                                    .padding(.horizontal, Theme.Space.sm).padding(.vertical, Theme.Space.xxs)
                                    .background(Color.primary.opacity(0.05), in: .capsule)
                                }
                            }
                        }
                    }
                }
            }
            if let actions { footer(actions) }
        }
        .padding(Theme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(LarkColor.cardBackground, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
                .strokeBorder(actions?.replying == true ? LarkColor.blue : LarkColor.cardStroke,
                              lineWidth: actions?.replying == true ? Theme.Size.line : Theme.Size.hairline)
        }
        .animation(Theme.Motion.state, value: actions?.replying)
    }

    /// 回复 and 解决 / 重新打开, under the replies (Feishu's thread footer).
    private func footer(_ a: Actions) -> some View {
        HStack(spacing: Theme.Space.l) {
            if !thread.resolved {
                Button(action: a.reply) {
                    Label("回复", systemImage: Theme.Icon.reply).lineLimit(1)
                }
                .accessibilityIdentifier("docComments.reply." + thread.id)
            }
            Button { a.setResolved(!thread.resolved) } label: {
                if a.resolving {
                    ProgressView().controlSize(.small)
                } else {
                    Label(thread.resolved ? "重新打开" : "解决", systemImage: thread.resolved ? Theme.Icon.undo : Theme.Icon.done).lineLimit(1)
                }
            }
            .disabled(a.resolving)
            .accessibilityIdentifier("docComments.resolve." + thread.id)
            Spacer(minLength: 0)
        }
        .font(Theme.Font.captionStrong)
        .foregroundStyle(.secondary)
        .buttonStyle(.plain)
        .padding(.leading, Theme.Size.wellXS + Theme.Space.s) // under the replies' text, past the avatars
    }

    private func replyText(_ r: FeishuDocsComments.Reply) -> AttributedString {
        var out = AttributedString()
        for seg in r.content {
            var s = AttributedString(seg.text)
            switch seg.kind {
            case "mention": s.foregroundColor = LarkColor.blue
            case "link":
                s.foregroundColor = LarkColor.blue
                if let h = seg.href { s.link = URL(string: h) }
            default: break
            }
            out += s
        }
        return out
    }
}

// MARK: - Commenting on a passage

extension EnvironmentValues {
    /// The reader's 评论这段 (#1290): opens the comments with the box on that passage (its text, the block it is in when
    /// the reader knows). Nil where comments cannot be written.
    @Entry var feishuDocComment: ((String, String?) -> Void)? = nil
}

/// A paragraph of a document the user can comment on: a long press offers 拷贝 (when the document lets its text be
/// copied), 评论这段 and 选一段评论… (the paragraph in a text view, where 评论 comments on the selected words). It takes
/// the place of the text selection, whose menu takes no items of ours.
struct FeishuDocsCommentable: ViewModifier {
    var text: String
    var blockID: String?
    var copyable: Bool
    var comment: (String, String?) -> Void
    @State private var picking = false

    func body(content: Content) -> some View {
        content
            .contextMenu {
                if copyable {
                    Button { UIPasteboard.general.string = text; Haptics.success() } label: { Label("拷贝", systemImage: Theme.Icon.copy) }
                }
                Button { comment(text, blockID) } label: { Label("评论这段", systemImage: Theme.Icon.conversation) }
                Button { picking = true } label: { Label("选一段评论…", systemImage: Theme.Icon.select) }
            }
            .sheet(isPresented: $picking) {
                PickTextSheet(text: text, actions: [PickAction(title: "评论", icon: Theme.Icon.conversation, kind: "comment")]) { picked, _ in
                    comment(picked, blockID)
                }
            }
    }
}
