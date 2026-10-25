import SwiftUI

/// Someone to show a profile card for (from a sender avatar / name, a mention, a leader link).
struct LarkPersonRef: Identifiable, Hashable {
    var id: String
    var name: String
    var avatar: String?
    /// The chat the person was tapped in: the card then shows their group nickname.
    var feedID: String?
}

extension EnvironmentValues {
    /// Opens a person's profile card; set by the chat view.
    @Entry var larkOpenProfile: ((LarkPersonRef) -> Void)? = nil
}

/// A person's profile card, laid out like lark-connector's (profile-dialog): cover, avatar, names, status, signature,
/// 消息, then the info rows (first five, 查看更多信息 for the rest). Leader rows open the leader's card.
struct LarkProfileView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.feishuOpen) private var feishuOpen
    var person: LarkPersonRef
    /// Pushed inside an existing navigation stack (a route) rather than presented as its own sheet.
    var embedded = false
    @State private var profile: LarkProfile?
    @State private var error: String?
    @State private var showAll = false
    @State private var path = NavigationPath()
    @State private var opening = false

    var body: some View {
        if embedded {
            card
        } else {
            NavigationStack(path: $path) {
                card
                    .toolbar { ToolbarItem(placement: .topBarTrailing) { Button("完成") { dismiss() }.tint(LarkColor.blue) } }
                    .feishuRoutes()
            }
            .environment(\.feishuOpen) { path.append($0) }
        }
    }

    /// Opens a chat / another profile in whichever stack shows this card.
    private func push(_ route: FeishuRoute) {
        if embedded, let feishuOpen { feishuOpen(route) } else { path.append(route) }
    }

    private var card: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                header
                if let error { Label(error, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger) }
                if let sig = profile?.description, !sig.isEmpty {
                    Text(sig).font(.subheadline).foregroundStyle(.secondary).lineLimit(6)
                        .padding(12).frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color.primary.opacity(0.04), in: .rect(cornerRadius: 12, style: .continuous))
                }
                if profile?.userType != "bot" { messageButton }
                fields
            }
            .padding(.horizontal, 16)
            .padding(.bottom, 24)
        }
        .ignoresSafeArea(edges: .top)
        .task {
            guard let api = model.api else { return }
            do { profile = try await api.larkProfile(person.id, feedID: person.feedID) } catch { self.error = error.localizedDescription }
        }
    }

    private var displayName: String { profile?.alias.flatMap { $0.isEmpty ? nil : $0 } ?? profile?.displayName ?? person.name }

    private var header: some View {
        VStack(alignment: .leading, spacing: 10) {
            ZStack(alignment: .bottomLeading) {
                Group {
                    if let cover = profile?.coverUrl, !cover.isEmpty {
                        LarkRemoteImage(url: cover, maxPixel: 1200)
                    } else {
                        LinearGradient(colors: [LarkColor.blue.opacity(0.35), LarkColor.blue.opacity(0.12)], startPoint: .topLeading, endPoint: .bottomTrailing)
                    }
                }
                .frame(height: 150)
                .frame(maxWidth: .infinity)
                .clipped()
                .padding(.horizontal, -16)
                LarkAvatarImage(name: displayName, url: profile?.avatarUrl ?? person.avatar, size: 80)
                    .overlay(Circle().strokeBorder(Color(.systemBackground), lineWidth: 3))
                    .offset(y: 40)
            }
            .padding(.bottom, 40)
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    Text(displayName).font(.title2.weight(.bold)).lineLimit(2)
                    if profile?.userType == "bot" { LarkNameTag(text: "机器人") }
                    if profile?.relation?.external == true { LarkNameTag(text: "外部") }
                }
                let subtitle = [profile?.enName, profile?.alias?.isEmpty == false ? profile?.displayName : nil, profile?.groupNickname.map { "群昵称：\($0)" }]
                    .compactMap { $0 }.filter { !$0.isEmpty && $0 != displayName }
                if !subtitle.isEmpty { Text(subtitle.joined(separator: " · ")).font(.subheadline).foregroundStyle(.secondary) }
                HStack(spacing: 6) {
                    if let s = profile?.workStatus?.status, s == 1 || s == 2 { chip(s == 1 ? "请假中" : "会议中", color: .orange) }
                    if let t = profile?.customStatus?.title, !t.isEmpty { chip(t, color: LarkColor.blue) }
                    if let m = profile?.medalName, !m.isEmpty { chip(m, color: .purple) }
                    if let tz = profile?.timezone, let zone = TimeZone(identifier: tz), zone.secondsFromGMT() != TimeZone.current.secondsFromGMT() {
                        chip("当地时间 " + Date.now.formatted(Date.FormatStyle(date: .omitted, time: .shortened, timeZone: zone)), color: .gray)
                    }
                }
                if profile?.relation?.external == true, let t = profile?.relation?.tenantName { Text(t).font(.footnote).foregroundStyle(.secondary) }
            }
        }
    }

    private func chip(_ text: String, color: Color) -> some View {
        Text(text).font(.caption.weight(.medium)).lineLimit(1).foregroundStyle(color)
            .padding(.horizontal, 8).padding(.vertical, 3).background(color.opacity(0.12), in: .capsule)
    }

    private var messageButton: some View {
        Button {
            Task {
                guard let api = model.api else { return }
                opening = true
                defer { opening = false }
                do {
                    let feed = try await api.larkDirectChat(person.id)
                    push(.chat(LarkConversation(id: feed, name: displayName, avatar: profile?.avatarUrl ?? person.avatar, isGroup: false)))
                } catch {
                    model.show(error)
                }
            }
        } label: {
            Group { if opening { ProgressView() } else { Label("发消息", systemImage: "bubble.left.fill").lineLimit(1) } }
                .font(.subheadline.weight(.semibold))
                .frame(maxWidth: .infinity, minHeight: 44)
        }
        .buttonStyle(.borderedProminent)
        .tint(LarkColor.blue)
        .disabled(opening)
    }

    /// Rows with a value; relation / alias rows (shown in the header) and empty ones are skipped.
    private var rows: [LarkProfile.Field] {
        (profile?.fields ?? []).filter { f in
            guard let v = f.value?.string, !v.isEmpty else { return false }
            return !["B-ALIAS", "B-ALIAS-AND-MEMO", "B-CHATGROUPNICKNAME", "B-MORE"].contains(f.key)
        }
    }

    @ViewBuilder private var fields: some View {
        let all = rows
        let shown = showAll ? all : Array(all.prefix(5))
        if !all.isEmpty {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(shown.enumerated()), id: \.offset) { i, f in
                    if i > 0 { Divider() }
                    row(f)
                }
                if all.count > 5 {
                    Divider()
                    Button { withAnimation(.snappy) { showAll.toggle() } } label: {
                        Text(showAll ? "收起" : "查看更多信息").font(.subheadline).foregroundStyle(LarkColor.blue).lineLimit(1)
                            .frame(maxWidth: .infinity, minHeight: 44)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 14)
            .background(Color.primary.opacity(0.04), in: .rect(cornerRadius: 12, style: .continuous))
        }
    }

    private func row(_ f: LarkProfile.Field) -> some View {
        let value = f.value?.string ?? ""
        let label = switch f.key {
        case "B-DEPARTMENT": "部门"
        case "B-LEADER": "直属上级"
        case "B-EMPLOYEETYPE": "人员类型"
        default: f.label ?? f.key
        }
        let leader = f.url.flatMap { u in u.hasPrefix("lark://client/profile") ? URLComponents(string: u)?.queryItems?.first { $0.name == "uid" }?.value : nil }
        let link = f.url.flatMap { $0.hasPrefix("http") ? URL(string: $0) : nil }
        return HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(label).font(.subheadline).foregroundStyle(.secondary).frame(width: 76, alignment: .leading).lineLimit(2)
            Group {
                if let leader {
                    Button { push(.profile(LarkPersonRef(id: leader, name: value))) } label: { Text(value).foregroundStyle(LarkColor.blue) }.buttonStyle(.plain)
                } else if let link {
                    Link(value, destination: link).foregroundStyle(LarkColor.blue)
                } else {
                    Text(value).textSelection(.enabled)
                }
            }
            .font(.subheadline)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 11)
        .contextMenu {
            Button { UIPasteboard.general.string = value } label: { Label("拷贝", systemImage: "doc.on.doc") }
        }
    }
}
