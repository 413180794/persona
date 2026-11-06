import SwiftUI

/// How much of the model's window the agent's latest reply used (#3289, the web's #2430 `lib/context-usage.ts`): the
/// node reads it off the transcript (`Session.contextTokens` / `contextWindow`), the app works out the share.
nonisolated struct ContextUsage: Equatable, Sendable {
    /// At or past this share of the window used, the bar turns to the warning colour (the web's `CONTEXT_WARN_USED`).
    static let warnUsed = 80

    /// Whole percent of the window used, 0–100.
    var used: Int
    var tokens: Int
    var window: Int

    /// Whole percent left.
    var left: Int { 100 - used }
    var warns: Bool { used >= Self.warnUsed }
    /// 0…1 for the bar: what is left, like the web's header.
    var fractionLeft: Double { Double(left) / 100 }

    init?(tokens: Int?, window: Int?) {
        guard let tokens, let window, tokens > 0, window > 0 else { return nil }
        self.tokens = tokens
        self.window = window
        used = min(100, max(0, Int((Double(tokens) / Double(window) * 100).rounded())))
    }

    init?(_ session: Session?) { self.init(tokens: session?.contextTokens, window: session?.contextWindow) }

    /// 「还剩 61%」.
    var label: String { String(localized: "\(left)% left") }
    /// 「172K / 200K」 (for VoiceOver).
    var detail: String { "\(Self.short(tokens)) / \(Self.short(window))" }

    /// 172000 → "172K", 1000000 → "1M", 1260000 → "1.3M".
    static func short(_ n: Int) -> String {
        // A unit ends where its number would round to 1000: 999_600 is "1M", not "1000K". The M figure is rounded to
        // one decimal before asking whether it is whole, so 1_960_000 is "2M", not "2.0M".
        if n >= 999_500 {
            let m = (Double(n) / 100_000).rounded() / 10
            return (m == m.rounded() ? String(Int(m)) : String(format: "%.1f", m)) + "M"
        }
        if n >= 1000 { return "\(Int((Double(n) / 1000).rounded()))K" }
        return "\(n)"
    }
}

/// Session Info's context row, under the tokens: the share left in words and a thin bar; the warning colour from 80% used.
struct ContextUsageRow: View {
    let usage: ContextUsage

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            LabeledContent {
                Text(usage.label)
                    .font(Theme.Font.secondaryMedium)
                    .monospacedDigit()
                    .foregroundStyle(usage.warns ? Theme.caution : Color.secondary)
            } label: {
                Label("Context Remaining", systemImage: Theme.Icon.context).lineLimit(1)
            }
            ProgressView(value: usage.fractionLeft)
                .tint(usage.warns ? Theme.caution : Theme.accent)
                .accessibilityHidden(true)
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Context \(usage.label), \(usage.used)% used, window \(usage.detail)")
    }
}
