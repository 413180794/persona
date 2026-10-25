import SwiftUI

/// Cards lark-connector summarises itself (`metadata.variant`): forwarded chats, group / contact cards, meetings,
/// calendar events, votes, tasks, red packets (port of `SummaryCardView`).
struct LarkSummaryCardView: View {
    var meta: JSONValue
    var fallbackTitle: String

    var body: some View {
        let variant = meta["variant"]?.string ?? ""
        Group {
            switch variant {
            case "merge-forward": LarkMergeForwardCard(meta: meta, title: title)
            case "share-group", "share-user": LarkShareCard(meta: meta, title: title, isGroup: variant == "share-group")
            default: LarkGenericSummaryCard(meta: meta, title: title, variant: variant)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(LarkColor.cardBackground)
        .clipShape(.rect(cornerRadius: 12, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(LarkColor.cardStroke))
    }

    private var title: String { meta["title"]?.string ?? fallbackTitle }
}

/// "聊天记录": a few previews; tap opens every forwarded message.
struct LarkMergeForwardCard: View {
    var meta: JSONValue
    var title: String
    @State private var showing = false

    var body: some View {
        let previews = meta["previews"]?.array ?? []
        let count = meta["messageCount"]?.int ?? previews.count
        Button { showing = true } label: {
            VStack(alignment: .leading, spacing: 6) {
                Text(title.isEmpty ? "聊天记录" : title).font(.system(size: 15, weight: .semibold)).lineLimit(2).multilineTextAlignment(.leading)
                ForEach(Array(previews.prefix(4).enumerated()), id: \.offset) { _, p in
                    Text("\(p["senderName"]?.string ?? "")：\(Fmt.plain(p["text"]?.string ?? ""))")
                        .font(.system(size: 13))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Divider().padding(.top, 2)
                HStack {
                    Label("聊天记录", systemImage: "text.bubble").labelStyle(.titleAndIcon)
                    Spacer()
                    Text("\(count) 条")
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            .padding(12)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .sheet(isPresented: $showing) { LarkForwardedView(title: title, meta: meta) }
    }
}

/// Every message of a merge-forward, rendered like the chat.
struct LarkForwardedView: View {
    var title: String
    var meta: JSONValue
    @Environment(\.dismiss) private var dismiss

    private var messages: [LarkForwardedMessage] {
        guard let raw = meta["messages"], let data = try? JSON.encoder.encode(raw) else { return [] }
        return (try? JSON.decoder.decode([LarkForwardedMessage].self, from: data)) ?? []
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 16) {
                    ForEach(messages) { m in
                        HStack(alignment: .top, spacing: 10) {
                            LarkAvatarImage(name: m.senderName ?? "?", url: m.senderAvatarUrl.map(Self.hubURL), size: 32)
                            VStack(alignment: .leading, spacing: 4) {
                                HStack(spacing: 6) {
                                    Text(m.senderName ?? "").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                                    if let d = m.createdAt { Text(d.formatted(date: .abbreviated, time: .shortened)).font(.caption2).foregroundStyle(.tertiary) }
                                }
                                if let parts = m.parts, !parts.isEmpty {
                                    LarkPartsView(parts: parts)
                                        .environment(\.larkContext, LarkRenderContext(messageID: m.id, images: LarkPartsView.imageURLs(parts)))
                                } else {
                                    Text(m.textPreview ?? "").font(.body)
                                }
                            }
                        }
                    }
                }
                .padding(16)
            }
            .navigationTitle(title.isEmpty ? "聊天记录" : title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("完成") { dismiss() } } }
        }
    }

    /// Avatars inside forwarded messages are lark-connector paths; the hub serves them under /api/v1/lark.
    static func hubURL(_ s: String) -> String {
        if s.hasPrefix("/api/media/") { return "/api/v1/lark/media/" + s.dropFirst(11) }
        return s
    }
}

/// Group / contact card: picture, name, eyebrow, facts.
struct LarkShareCard: View {
    var meta: JSONValue
    var title: String
    var isGroup: Bool

    var body: some View {
        let fields = (meta["fields"]?.array ?? []).compactMap { f -> String? in
            guard let l = f["label"]?.string, let v = f["value"]?.string, !v.isEmpty else { return nil }
            return "\(l) \(LarkFormat.fieldValue(v))"
        }
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                LarkAvatarImage(name: title, url: meta["imageUrl"]?.string, isGroup: isGroup, size: 44)
                VStack(alignment: .leading, spacing: 3) {
                    Text(title).font(.system(size: 15, weight: .semibold)).lineLimit(2)
                    if !fields.isEmpty { Text(fields.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                }
            }
            Divider()
            HStack {
                Text(meta["eyebrow"]?.string ?? (isGroup ? "群名片" : "个人名片"))
                Spacer()
                if meta["isDissolved"]?.bool == true { Text("已解散") }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        .padding(12)
    }
}

/// Meetings, events, votes, tasks and anything else: accent bar, glyph, eyebrow, title, status, facts, footer.
struct LarkGenericSummaryCard: View {
    var meta: JSONValue
    var title: String
    var variant: String

    var body: some View {
        let fields = (meta["fields"]?.array ?? []).compactMap { f -> (String, String)? in
            guard let l = f["label"]?.string, let v = f["value"]?.string, !v.isEmpty else { return nil }
            return (l, LarkFormat.fieldValue(v))
        }
        let accent = Self.accent(variant)
        HStack(spacing: 0) {
            Rectangle().fill(accent).frame(width: 4)
            VStack(alignment: .leading, spacing: 8) {
                HStack(alignment: .top, spacing: 10) {
                    Group {
                        if let img = meta["imageUrl"]?.string {
                            LarkRemoteImage(url: img, size: larkThumbEdge(points: 36), maxPixel: 120)
                        } else {
                            Image(systemName: Self.glyph(variant)).font(.system(size: 17, weight: .semibold)).foregroundStyle(accent)
                                .frame(maxWidth: .infinity, maxHeight: .infinity).background(accent.opacity(0.12))
                        }
                    }
                    .frame(width: 36, height: 36)
                    .clipShape(.rect(cornerRadius: 8, style: .continuous))
                    VStack(alignment: .leading, spacing: 2) {
                        if let eyebrow = meta["eyebrow"]?.string { Text(eyebrow).font(.caption).foregroundStyle(.secondary) }
                        Text(title).font(.system(size: 15, weight: .semibold)).lineLimit(3).fixedSize(horizontal: false, vertical: true)
                    }
                    Spacer(minLength: 4)
                    if let status = meta["status"]?.string {
                        Text(status).font(.caption2.weight(.medium)).lineLimit(1).foregroundStyle(.secondary)
                            .padding(.horizontal, 6).padding(.vertical, 2).background(Color.primary.opacity(0.06), in: .capsule).fixedSize()
                    }
                }
                if let desc = meta["description"]?.string, !desc.isEmpty {
                    Text(Self.stripHTML(desc)).font(.system(size: 14)).foregroundStyle(.secondary).lineLimit(4)
                }
                if let vote = meta["vote"], let options = vote["options"]?.array, !options.isEmpty {
                    VStack(alignment: .leading, spacing: 5) {
                        ForEach(Array(options.enumerated()), id: \.offset) { _, o in
                            Label(o["content"]?.string ?? "", systemImage: (vote["maxPick"]?.int ?? 1) > 1 ? "square" : "circle")
                                .font(.system(size: 14))
                        }
                    }
                }
                if !fields.isEmpty {
                    Grid(alignment: .leading, horizontalSpacing: 10, verticalSpacing: 4) {
                        ForEach(Array(fields.enumerated()), id: \.offset) { _, f in
                            GridRow {
                                Text(f.0).foregroundStyle(.secondary).fixedSize()
                                Text(f.1).lineLimit(3)
                            }
                            .font(.system(size: 13))
                        }
                    }
                }
                if let footer = meta["footer"]?.string { Text(LarkFormat.fieldValue(footer)).font(.caption).foregroundStyle(.tertiary) }
                let copy = (meta["actions"]?.array ?? []).first { $0["type"]?.string == "copyMeetingNumber" }
                if let value = copy?["value"]?.string {
                    Button {
                        UIPasteboard.general.string = value
                        Haptics.success()
                        AppModel.shared.toast = Toast("已拷贝会议号", style: .success)
                    } label: { Label("拷贝会议号", systemImage: "doc.on.doc").lineLimit(1).font(.caption.weight(.medium)) }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                }
            }
            .padding(12)
        }
    }

    static func glyph(_ v: String) -> String {
        switch v {
        case "video-chat", "meeting": "video.fill"
        case "calendar": "calendar"
        case "vote": "chart.bar.fill"
        case "hongbao": "gift.fill"
        case "todo", "task": "checklist"
        case "share-user": "person.crop.circle"
        case "location": "mappin.and.ellipse"
        case "mail": "envelope.fill"
        default: "doc.text.fill"
        }
    }

    static func accent(_ v: String) -> Color {
        switch v {
        case "hongbao": LarkColor.hue("red")
        case "calendar": LarkColor.hue("orange")
        case "vote": LarkColor.hue("purple")
        case "todo", "task": LarkColor.hue("green")
        default: LarkColor.blue
        }
    }

    static func stripHTML(_ s: String) -> String {
        guard s.contains("<") else { return LarkFormat.entities(s) }
        let noTags = s.replacingOccurrences(of: "<br\\s*/?>|</p>|</div>", with: "\n", options: .regularExpression)
            .replacingOccurrences(of: "<[^>]+>", with: "", options: .regularExpression)
        return LarkFormat.entities(noTags).trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
