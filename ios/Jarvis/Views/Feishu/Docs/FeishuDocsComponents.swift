import SwiftUI
import UIKit

// MARK: - API

extension APIClient {
    /// Like `larkRaw`, but a failure keeps lark-connector's error code (`{"error":{"code":…}}`), which the reader needs to
    /// tell "export forbidden" (→ read the JSON tree) from "format unsupported" (→ open in Feishu). `APIClient.request`
    /// only keeps flat `{"error":"…"}` messages, so this does the GET itself.
    func larkDocs<T: Decodable>(_ path: String, query: [String: String] = [:], as: T.Type = T.self) async throws -> T {
        var req = URLRequest(url: url("/api/v1/lark/raw/" + path, query: query), timeoutInterval: 60)
        req.setValue("Bearer \(config.token)", forHTTPHeaderField: "Authorization")
        let (data, resp) = try await session.data(for: req)
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else { throw FeishuDocsFailure(status: status, code: FeishuDocsFailure.code(from: data)) }
        return try JSON.decoder.decode(T.self, from: data)
    }
}

extension Error {
    /// The message a Feishu docs screen shows, or nil for a cancelled load (leaving the page is not an error).
    var feishuDocsMessage: String? {
        if isCancellation { return nil }
        if let e = self as? URLError {
            switch e.code {
            case .notConnectedToInternet, .networkConnectionLost: return "网络不可用"
            case .timedOut: return "请求超时，请稍后重试"
            case .cannotConnectToHost, .cannotFindHost: return "连不上 hub（手机需要在公司网络 / removed）"
            default: break
            }
        }
        if self is DecodingError { return "飞书返回了无法识别的数据" }
        return localizedDescription
    }
}

// MARK: - Paging

/// A cursor-paged list: first page, pull to refresh, more on reaching the end. Pages are deduplicated by id.
@MainActor @Observable
final class FeishuDocPager<Item: Identifiable & Sendable> where Item.ID: Hashable {
    private(set) var items: [Item] = []
    private(set) var cursor: String?
    private(set) var loaded = false
    private(set) var loading = false
    private(set) var loadingMore = false
    var error: String?
    @ObservationIgnored private var fetch: (String?) async throws -> ([Item], String?)
    @ObservationIgnored private var generation = 0

    init(fetch: @escaping (String?) async throws -> ([Item], String?)) { self.fetch = fetch }

    var hasMore: Bool { cursor?.isEmpty == false }

    /// Swaps what the list reads (another view / sort) and reloads.
    func reset(_ fetch: @escaping (String?) async throws -> ([Item], String?)) async {
        self.fetch = fetch
        items = []
        cursor = nil
        loaded = false
        await refresh()
    }

    func refresh() async {
        generation += 1
        let gen = generation
        loading = true
        defer { if gen == generation { loading = false } }
        do {
            let (page, next) = try await fetch(nil)
            guard gen == generation else { return }
            items = feishuAppendUnique([], page)
            cursor = next
            error = nil
            loaded = true
        } catch {
            guard gen == generation, let msg = error.feishuDocsMessage else { return }
            self.error = msg
        }
    }

    func loadMore() async {
        guard hasMore, !loadingMore, !loading else { return }
        let gen = generation
        loadingMore = true
        defer { loadingMore = false }
        do {
            let (page, next) = try await fetch(cursor)
            guard gen == generation else { return }
            items = feishuAppendUnique(items, page)
            // A cursor that does not move would page forever.
            cursor = (next == cursor || page.isEmpty) ? nil : next
        } catch {
            guard gen == generation, let msg = error.feishuDocsMessage else { return }
            self.error = msg
        }
    }
}

// MARK: - Pieces

/// A document's icon: its custom emoji / image when it has one, else the kind's colored tile.
struct FeishuDocIconView: View {
    var kind: FeishuDocKind
    var icon: FeishuDocIcon?
    var title: String = ""
    var fileExtension: String?
    var size: CGFloat = 36

    var body: some View {
        Group {
            if let icon, icon.type == "emoji", !icon.value.isEmpty {
                Text(icon.value).font(.system(size: size * 0.62))
                    .frame(width: size, height: size)
                    .background(Color.primary.opacity(0.05), in: .rect(cornerRadius: size * 0.24, style: .continuous))
            } else if let icon, icon.type == "image", !icon.value.isEmpty {
                LarkRemoteImage(url: icon.value, maxPixel: size * 3, contentMode: .fit)
                    .frame(width: size, height: size)
                    .clipShape(.rect(cornerRadius: size * 0.24, style: .continuous))
            } else {
                let file = kind == .file ? FeishuFileKind.symbol(ext: fileExtension, title: title) : nil
                FeishuAppIcon(symbol: file?.symbol ?? kind.symbol, hue: file?.hue ?? kind.hue, size: size)
            }
        }
        .accessibilityHidden(true)
    }
}

/// One document in a list: icon, title, "owner · time" and the little tags Feishu shows (外部, 快捷方式, 共享文件夹).
struct FeishuDocRow: View {
    var item: FeishuDocItem
    /// Which time the row shows: when I opened it (最近) or when it was edited.
    var showActivity = false

    var body: some View {
        HStack(spacing: 12) {
            FeishuDocIconView(kind: item.docKind, icon: item.icon, title: item.title, fileExtension: item.extension)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 5) {
                    Text(item.title.isEmpty ? "未命名\(item.docKind.label)" : item.title)
                        .font(.body)
                        .foregroundStyle(item.title.isEmpty ? .secondary : .primary)
                        .lineLimit(1)
                    if item.external == true { FeishuDocTag(text: "外部", hue: "orange") }
                }
                HStack(spacing: 4) {
                    if item.shortcut == true { Image(systemName: "arrowshape.turn.up.right.fill").font(.caption2) }
                    if item.sharedFolder == true { Image(systemName: "person.2.fill").font(.caption2) }
                    Text(subtitle).lineLimit(1)
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            if item.isFolder {
                Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
            } else if item.starred == true {
                Image(systemName: "star.fill").font(.caption).foregroundStyle(LarkColor.hue("yellow"))
            }
        }
        .padding(.vertical, 9)
        .contentShape(.rect)
    }

    private var subtitle: String {
        var parts: [String] = []
        if let o = item.owner, !o.isEmpty { parts.append(item.mine == true ? "我" : o) }
        let when = showActivity ? (item.activity ?? item.updated) : (item.edited ?? item.updated)
        let t = FeishuTime.short(when)
        if !t.isEmpty { parts.append((showActivity ? "打开于 " : "更新于 ") + t) }
        if item.docKind == .file, let s = item.size, s > 0 { parts.append(ByteCountFormatter.string(fromByteCount: Int64(s), countStyle: .file)) }
        return parts.isEmpty ? item.docKind.label : parts.joined(separator: " · ")
    }
}

struct FeishuDocTag: View {
    var text: String
    var hue: String

    var body: some View {
        Text(text)
            .font(.system(size: 10, weight: .medium))
            .foregroundStyle(LarkColor.hue(hue))
            .lineLimit(1)
            .padding(.horizontal, 4)
            .padding(.vertical, 1)
            .background(LarkColor.hue(hue).opacity(0.13), in: .rect(cornerRadius: 3))
            .fixedSize()
    }
}

/// A horizontal row of single-line filter chips (Feishu's list tabs).
struct FeishuDocChips<T: Hashable>: View {
    var options: [T]
    @Binding var selection: T
    var label: (T) -> String

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(options, id: \.self) { o in
                    let on = o == selection
                    Button {
                        withAnimation(.snappy(duration: 0.2)) { selection = o }
                    } label: {
                        Text(label(o))
                            .font(.subheadline.weight(on ? .semibold : .regular))
                            .lineLimit(1)
                            .padding(.horizontal, 14)
                            .frame(height: 32)
                            .foregroundStyle(on ? LarkColor.blue : .primary)
                            .background(on ? LarkColor.blue.opacity(0.12) : Color.primary.opacity(0.05), in: .capsule)
                            .contentShape(.capsule)
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
            }
            .padding(.horizontal, 16)
        }
        .scrollClipDisabled()
    }
}

/// The three states every list shares: first load, failure with retry, nothing there.
struct FeishuDocsListState: View {
    var loading: Bool
    var error: String?
    var empty: (symbol: String, title: String, message: String)
    var retry: () async -> Void

    var body: some View {
        Group {
            if loading {
                ProgressView().controlSize(.large).frame(maxWidth: .infinity, minHeight: 260)
            } else if let error {
                EmptyStateView(symbol: "wifi.exclamationmark", title: "加载失败", message: error, action: ("重试", { Task { await retry() } }))
            } else {
                EmptyStateView(symbol: empty.symbol, title: empty.title, message: empty.message)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 24)
    }
}

/// The row at the end of a paged list: loads the next page when it scrolls in.
struct FeishuDocsMoreRow: View {
    var hasMore: Bool
    var loading: Bool
    var error: String?
    var more: () async -> Void

    var body: some View {
        Group {
            if hasMore {
                HStack {
                    Spacer()
                    if error != nil && !loading {
                        Button("加载失败，点此重试") { Task { await more() } }.font(.footnote).lineLimit(1)
                    } else {
                        ProgressView().controlSize(.small)
                    }
                    Spacer()
                }
                .frame(height: 44)
                .task(id: loading) { if !loading && error == nil { await more() } }
            } else {
                Color.clear.frame(height: 8)
            }
        }
    }
}

/// A non-blocking error line above content that is already shown (refresh failed, cached list stays).
struct FeishuDocsErrorBanner: View {
    var message: String

    var body: some View {
        Label(message, systemImage: "exclamationmark.triangle.fill")
            .font(.footnote)
            .foregroundStyle(Theme.danger)
            .lineLimit(2)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(Theme.danger.opacity(0.1), in: .rect(cornerRadius: 10, style: .continuous))
    }
}

/// Section title inside a docs list.
struct FeishuDocsSectionTitle: View {
    var title: String
    var trailing: String? = nil

    var body: some View {
        HStack {
            Text(title).font(.footnote.weight(.semibold)).foregroundStyle(.secondary)
            Spacer()
            if let trailing { Text(trailing).font(.caption).foregroundStyle(.tertiary) }
        }
        .padding(.top, 14)
        .padding(.bottom, 2)
    }
}

enum FeishuDocLinks {
    /// Opens a document in the Feishu app (universal link) or Safari, bypassing the in-app link handler that would
    /// push the native reader again.
    @MainActor static func openInFeishu(_ url: String) {
        guard let u = URL(string: url) else { return }
        UIApplication.shared.open(u)
    }
}
