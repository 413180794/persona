import BeautifulUI
import SwiftUI

/// A model API error in a chat, read from the text Claude Code writes when it gives up ("API Error: 502 …", an
/// assistant message) or from the hub's notice for a retry ("API 错误：Overloaded（正在重试）"). What a person needs
/// first is in front: why, in words, the status code, and whether it is likely to pass by itself; the request id,
/// session and the raw text are folded under it.
nonisolated struct APIErrorInfo: Hashable, Sendable {
    var status: Int?
    /// One line, in words.
    var reason: String
    /// Likely to pass if tried again later (5xx, rate limits, timeouts); false when someone has to act (login,
    /// a bad request); nil when the text does not say.
    var transient: Bool?
    var requestID: String?
    var session: String?
    var raw: String

    /// Two errors are "the same" for folding when these match.
    var signature: String { "\(status ?? 0)|\(reason)" }

    nonisolated(unsafe) private static let statusRe = /^\s*(\d{3})\b/
    nonisolated(unsafe) private static let requestRe = /(?i)\b(?:req(?:uest)?(?:[ _-]?id)?)[:=\s"]+([A-Za-z0-9_-]{8,})/
    nonisolated(unsafe) private static let sessionRe = /(?i)\bsession(?:[ _-]?id)?[:=\s"]+([A-Za-z0-9_:-]{6,})/

    static func parse(_ text: String?) -> APIErrorInfo? {
        guard let t = text?.trimmingCharacters(in: .whitespacesAndNewlines) else { return nil }
        let body: String
        if t.hasPrefix("API Error:") {
            body = String(t.dropFirst("API Error:".count))
        } else if t.hasPrefix("API 错误：") {
            body = String(t.dropFirst("API 错误：".count))
        } else {
            return nil
        }
        let status = (try? statusRe.firstMatch(in: body)).flatMap { Int($0.1) }
        let (reason, transient) = explain(body.lowercased(), status: status)
        return APIErrorInfo(
            status: status,
            reason: reason,
            transient: transient,
            requestID: (try? requestRe.firstMatch(in: body)).map { String($0.1) },
            session: (try? sessionRe.firstMatch(in: body)).map { String($0.1) },
            raw: t
        )
    }

    /// What went wrong, as a person would say it, and whether it is likely to pass.
    private static func explain(_ s: String, status: Int?) -> (String, Bool?) {
        if s.contains("no available gpu") { return ("推理服务暂时没有可用的 GPU", true) }
        if s.contains("overloaded") || status == 529 { return ("模型服务太忙，过载了", true) }
        if status == 401 || status == 403 || s.contains("authentication") || s.contains("/login") || s.contains("login expire") {
            return ("登录过期了，要在电脑上重新 /login", false)
        }
        if status == 429 || s.contains("rate limit") || s.contains("rate_limit") { return ("请求太多，被限流了", true) }
        if s.contains("timed out") || s.contains("timeout") { return ("请求超时了", true) }
        if s.contains("connection") { return ("连不上模型服务", true) }
        if status == 400 { return ("请求被拒绝：参数或上下文有问题", false) }
        if let status, (500..<600).contains(status) { return ("模型服务暂时不可用", true) }
        let temporary = s.contains("temporary") || s.contains("try again")
        return ("模型请求失败", temporary ? true : nil)
    }
}

/// The card for an `APIErrorInfo`: the reason with a warning mark, BeautifulUI's chips (the status in the error
/// style, 临时 / 要处理, how many times in a row), then — folded — the ids to copy, the raw text, and 重试 when the
/// error is the last thing in an idle chat and likely to pass (a login error is not fixed by trying again).
struct APIErrorCard: View {
    var info: APIErrorInfo
    var count: Int
    var at: Date?
    var retry: (() -> Void)?
    @Environment(AppModel.self) private var model
    #if DEBUG
    /// `-showErrorDetails YES`: cards start open, for screenshots.
    @State private var open = UserDefaults.standard.bool(forKey: "showErrorDetails")
    #else
    @State private var open = false
    #endif

    private var chips: [ToolChipGroup.Chip] {
        var out: [ToolChipGroup.Chip] = []
        if let s = info.status {
            out.append(ToolChipGroup.Chip(symbol: "exclamationmark.circle", label: "\(s)", isError: true))
        }
        switch info.transient {
        case true?: out.append(ToolChipGroup.Chip(symbol: "clock.arrow.circlepath", label: "临时故障"))
        case false?: out.append(ToolChipGroup.Chip(symbol: "hand.raised", label: "要处理"))
        case nil: break
        }
        if count > 1 { out.append(ToolChipGroup.Chip(symbol: "repeat", label: "连续 \(count) 次")) }
        return out
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            Button {
                Haptics.tap()
                withAnimation(.snappy(duration: 0.25)) { open.toggle() }
            } label: {
                VStack(alignment: .leading, spacing: Theme.Space.s) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Image(systemName: "exclamationmark.triangle.fill").font(Theme.Font.heading).foregroundStyle(Theme.danger)
                        Text(info.reason).font(Theme.Font.heading).foregroundStyle(Color.primary).multilineTextAlignment(.leading)
                    }
                    ToolChipGroup(chips, isOpen: open)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("API 错误：\(info.reason)")
            .accessibilityValue(open ? "已展开" : "已折叠")

            if open {
                VStack(alignment: .leading, spacing: Theme.Space.s) {
                    if let id = info.requestID { field("请求 ID", id) }
                    if let s = info.session { field("Session", s) }
                    if let at { field("时间", at.formatted(.dateTime.month().day().hour().minute().second()), copyable: false) }
                    // Wrapped, not a sideways-scrolling block: the raw text is usually one long line.
                    Text(info.raw)
                        .font(Theme.Font.code)
                        .lineHeight(.code)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Theme.codeBackground, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
                    Button {
                        copy(info.raw)
                    } label: {
                        Label("复制原文", systemImage: "doc.on.doc").font(Theme.Font.label.weight(.medium)).lineLimit(1)
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(Theme.accent)
                }
                .transition(.opacity)
            }

            if let retry {
                Button {
                    Haptics.tap()
                    retry()
                } label: {
                    Label("重试", systemImage: "arrow.clockwise").font(Theme.Font.secondary.weight(.semibold)).lineLimit(1)
                        .padding(.horizontal, 4)
                }
                .themedButton()
            }
        }
        .padding(Theme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.danger.opacity(0.06), in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous).strokeBorder(Theme.danger.opacity(0.22), lineWidth: 1))
    }

    private func field(_ name: String, _ value: String, copyable: Bool = true) -> some View {
        HStack(spacing: 8) {
            Text(name).font(Theme.Font.caption).foregroundStyle(.secondary).frame(width: 64, alignment: .leading)
            Text(value).font(Theme.Font.code).lineLimit(1).truncationMode(.middle).textSelection(.enabled)
            Spacer(minLength: 0)
            if copyable {
                Button { copy(value) } label: {
                    Image(systemName: "doc.on.doc").font(Theme.Font.caption)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
                .accessibilityLabel("复制\(name)")
            }
        }
    }

    private func copy(_ s: String) {
        UIPasteboard.general.string = s
        Haptics.tap()
        model.toast = Toast("已复制")
    }
}
