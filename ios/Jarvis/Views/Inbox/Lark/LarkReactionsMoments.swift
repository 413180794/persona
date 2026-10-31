import SwiftUI

// MARK: - Reactions

extension EnvironmentValues {
    /// Toggles the user's reaction (key) on a message; set by the chat view. Nil: reactions are read-only.
    @Entry var larkReact: ((LarkMessage, String) -> Void)? = nil
}

/// One emoji from the Feishu catalogue as a picture.
struct LarkEmojiImage: View {
    var key: String
    var size: CGFloat

    var body: some View {
        let media = LarkMedia.shared
        if let url = media.emoji(key)?.url {
            LarkRemoteImage(url: url, maxPixel: size * 3, contentMode: .fit).frame(width: size, height: size)
        } else {
            Text(LarkMedia.labelFallback(key, text: nil)).font(.system(size: size * 0.45)).lineLimit(1).minimumScaleFactor(0.5)
                .frame(width: size, height: size)
        }
    }
}

/// 表情回复: the user's quick reactions on top, then every group of the catalogue (Feishu's picker).
struct LarkReactionPicker: View {
    var message: LarkMessage
    var pick: (String) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        let media = LarkMedia.shared
        let mine = Set((message.reactions ?? []).filter { $0.mine == true }.map { $0.key.lowercased() })
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: Theme.Space.l) {
                    HStack(spacing: 0) {
                        ForEach(Array(media.quickReactions.prefix(7)), id: \.self) { key in
                            cell(key, size: 34, selected: mine.contains(key.lowercased()))
                        }
                    }
                    ForEach(media.emojiGroups, id: \.title) { g in
                        VStack(alignment: .leading, spacing: Theme.Space.s) {
                            Text(g.title).font(Theme.Font.caption.weight(.semibold)).foregroundStyle(.secondary)
                            LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 4), count: 7), spacing: 8) {
                                ForEach(g.keys, id: \.self) { key in cell(key, size: 30, selected: mine.contains(key.lowercased())) }
                            }
                        }
                    }
                    if media.emojiGroups.isEmpty { ProgressView().frame(maxWidth: .infinity) }
                }
                .padding(16)
            }
            .navigationTitle("表情回复")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } } }
            .onAppear { media.loadEmojiCatalog() }
        }
        .presentationDetents([.medium, .large])
    }

    private func cell(_ key: String, size: CGFloat, selected: Bool) -> some View {
        Button {
            pick(key)
            dismiss()
        } label: {
            LarkEmojiImage(key: key, size: size)
                .frame(maxWidth: .infinity, minHeight: size + 12)
                .background(selected ? LarkColor.blue.opacity(0.15) : .clear, in: .rect(cornerRadius: 8))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(LarkMedia.shared.emoji(key)?.label ?? key)
    }
}

// MARK: - removed

nonisolated enum LarkMoments {
    /// The post id of a removed share link (`applink…/client/moments/detail?postId=…`).
    static func postID(_ link: String?) -> String? {
        guard let link, let c = URLComponents(string: link), c.path.hasSuffix("/client/moments/detail") || c.path.hasSuffix("/moments/detail"),
              let id = c.queryItems?.first(where: { $0.name == "postId" })?.value, !id.isEmpty, id.allSatisfy(\.isNumber) else { return nil }
        return id
    }
}

/// A shared removed post in a chat: Feishu's link preview card (title, author, excerpt, counts); tap reads the post.
struct LarkMomentsCard: View {
    var part: LarkPart
    @Environment(\.larkContext) private var context
    @State private var preview: LarkURLPreview?
    @State private var reading = false

    var body: some View {
        let card = preview?.card
        Button { reading = true } label: {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                HStack(spacing: 6) {
                    Image(systemName: "circle.hexagongrid.fill").font(Theme.Font.label).foregroundStyle(LarkColor.blue)
                    Text(preview?.title ?? "removed动态").font(Theme.Font.secondary.weight(.semibold)).lineLimit(1)
                }
                if let author = card?.author {
                    HStack(spacing: 8) {
                        LarkAvatarImage(name: author, url: card?.avatarUrl, size: 28)
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text(author).font(Theme.Font.caption.weight(.medium)).lineLimit(1)
                            if let sub = card?.subtitle { Text(sub).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1) }
                        }
                    }
                }
                if let body = card?.body, !body.isEmpty {
                    Text(body).font(Theme.Font.secondary).foregroundStyle(.primary.opacity(0.85)).lineLimit(5).multilineTextAlignment(.leading)
                } else if preview == nil {
                    Text(part.text ?? "").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                }
                if let footer = card?.footer { Text(footer).font(Theme.Font.label).foregroundStyle(.secondary) }
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(LarkColor.cardBackground, in: .rect(cornerRadius: 12, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(LarkColor.cardStroke))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .task(id: part.url) {
            guard let mid = context.messageID, let url = part.url, let api = AppModel.shared.api else { return }
            preview = try? await api.larkURLPreview(message: mid, url: url)
        }
        .sheet(isPresented: $reading) { if let id = LarkMoments.postID(part.url) { LarkMomentsPostView(postID: id, link: part.url) } }
    }
}

/// One removed post: author, text with Feishu emoji, pictures, reactions, comments.
struct LarkMomentsPostView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    var postID: String
    var link: String?
    @State private var post: LarkMomentsPost?
    @State private var comments: [LarkMomentsComment] = []
    @State private var error: String?
    @State private var gallery: Int?
    @State private var person: LarkPersonRef?

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: Theme.Space.l) {
                    if let error { Label(error, systemImage: "exclamationmark.triangle").font(Theme.Font.caption).foregroundStyle(Theme.danger) }
                    if let p = post {
                        author(name: p.userName ?? "", avatar: p.userAvatarUrl, id: p.userId, time: p.createTime)
                        LarkInlineText(runs: Self.runs(p.content), base: LarkTextBase(size: 17)).textSelection(.enabled)
                        images(p.images ?? [])
                        if let groups = p.reactionGroups, !groups.isEmpty {
                            LarkFlowLayout(spacing: 6, lineSpacing: 6) {
                                ForEach(groups, id: \.type) { g in
                                    HStack(spacing: 4) {
                                        LarkEmojiImage(key: g.type, size: 17)
                                        Text("\(g.count)").font(Theme.Font.label).foregroundStyle(g.selfInvolved == true ? LarkColor.blue : .secondary)
                                    }
                                    .padding(.horizontal, 8).padding(.vertical, 4)
                                    .background(Color.primary.opacity(0.05), in: .capsule)
                                }
                            }
                        }
                        Divider()
                        Text("\(p.commentCount ?? comments.count) 条评论").font(Theme.Font.secondary.weight(.semibold))
                        ForEach(comments) { c in
                            VStack(alignment: .leading, spacing: Theme.Space.sm) {
                                author(name: c.userName ?? "", avatar: c.userAvatarUrl, id: c.userId, time: c.createTime, small: true)
                                LarkInlineText(runs: Self.runs(c.content), base: LarkTextBase(size: 15)).padding(.leading, 40)
                            }
                            .padding(.vertical, 4)
                        }
                    } else if error == nil {
                        ProgressView().frame(maxWidth: .infinity, minHeight: 200)
                    }
                }
                .padding(16)
            }
            .navigationTitle("removed")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("完成") { dismiss() }.tint(LarkColor.blue) }
                if let link, let u = URL(string: link) {
                    ToolbarItem(placement: .primaryAction) {
                        Button { openURL(u) } label: { Label("在飞书中打开", systemImage: "arrow.up.forward.app") }
                    }
                }
            }
            .task {
                guard let api = model.api else { return }
                LarkMedia.shared.loadEmojiCatalog()
                async let c = api.larkMomentsComments(postID)
                do { post = try await api.larkMomentsPost(postID) } catch { self.error = error.localizedDescription }
                comments = (try? await c) ?? []
            }
            .fullScreenCover(item: Binding(get: { gallery.map { GalleryIndex(index: $0) } }, set: { gallery = $0?.index })) { g in
                LarkImageGallery(urls: (post?.images ?? []).compactMap(\.url), index: g.index)
            }
            .sheet(item: $person) { LarkProfileView(person: $0) }
        }
    }

    private struct GalleryIndex: Identifiable { var index: Int; var id: Int { index } }

    private func author(name: String, avatar: String?, id: String?, time: Double?, small: Bool = false) -> some View {
        Button { if let id { person = LarkPersonRef(id: id, name: name, avatar: avatar) } } label: {
            HStack(spacing: 10) {
                LarkAvatarImage(name: name, url: avatar, size: small ? 30 : 40)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(name).font(small ? Theme.Font.caption.weight(.semibold) : Theme.Font.secondary.weight(.semibold)).lineLimit(1)
                    if let time { Text(Date(timeIntervalSince1970: time / 1000).formatted(date: .abbreviated, time: .shortened)).font(Theme.Font.label).foregroundStyle(.secondary) }
                }
            }
        }
        .buttonStyle(.plain)
    }

    @ViewBuilder private func images(_ imgs: [LarkMomentsPost.Image]) -> some View {
        let urls = imgs.compactMap(\.url)
        if urls.count == 1, let img = imgs.first, let u = img.url {
            let size = LarkImagePart.displaySize(width: img.width ?? 0, height: img.height ?? 0, maxW: 300, maxH: 360)
            LarkRemoteImage(url: u, maxPixel: 1200).frame(width: size.width, height: size.height)
                .clipShape(.rect(cornerRadius: 10, style: .continuous)).onTapGesture { gallery = 0 }
        } else if !urls.isEmpty {
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 4), count: 3), spacing: 4) {
                ForEach(Array(urls.enumerated()), id: \.offset) { i, u in
                    Color.clear.aspectRatio(1, contentMode: .fit)
                        .overlay { LarkRemoteImage(url: u, maxPixel: 600) }
                        .clipShape(.rect(cornerRadius: 6, style: .continuous))
                        .onTapGesture { gallery = i }
                }
            }
        }
    }

    /// Post text: plain runs and Feishu emoji.
    static func runs(_ content: LarkMomentsPost.Content?) -> [LarkInline] {
        guard let segs = content?.segments, !segs.isEmpty else { return [.text(content?.text ?? "", LarkRunStyle())] }
        return segs.map { s in
            if s.kind == "emoji", let key = s.key ?? s.emojiKey { return .emoji(key: key, fallback: s.text ?? "[\(key)]") }
            return .text(s.text ?? "", LarkRunStyle())
        }
    }
}
