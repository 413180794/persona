import SwiftUI
import UIKit

// MARK: - API

extension APIClient {
    /// Like `larkRaw`, but a failure keeps lark-connector's error code (`{"error":{"code":…}}`), which the reader needs to
    /// tell "export forbidden" (→ read the JSON tree) from "format unsupported" (→ open in Feishu). `APIClient.request`
    /// only keeps flat `{"error":"…"}` messages, so this does the GET itself.
    func feishuDocsGet<T: Decodable>(_ path: String, query: [String: String] = [:], as: T.Type = T.self) async throws -> T {
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
            case .cannotConnectToHost, .cannotFindHost: return "连不上 hub（确认手机能访问 hub）"
            default: break
            }
        }
        if self is DecodingError { return "飞书返回了无法识别的数据" }
        return localizedDescription
    }
}

// MARK: - Pieces


struct FeishuDocsTag: View {
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




enum FeishuDocsLinks {
    /// Opens a document in the Feishu app (universal link) or Safari, bypassing the in-app link handler that would
    /// push the native reader again.
    @MainActor static func openInFeishu(_ url: String) {
        guard let u = URL(string: url) else { return }
        UIApplication.shared.open(u)
    }
}
