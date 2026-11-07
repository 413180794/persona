import Foundation

/// Token counts, the same everywhere (#4973): K / M / B with three significant digits and no trailing zeros
/// (1,250 → 1.25K, 2,960,000 → 2.96M, 178,000,000 → 178M); under 1,000 the number itself. Rounding that reaches
/// 1,000 of a unit moves up a unit (999,500 → 1M, not 1000K). The web has the same rules (web/src/lib/tokens.ts).
nonisolated enum TokenFormat {
    private static let units: [(Double, String)] = [(1e3, "K"), (1e6, "M"), (1e9, "B")]

    static func count(_ n: Int64) -> String {
        let v = Double(n.magnitude)
        let sign = n < 0 ? "-" : ""
        guard v >= 1000 else { return sign + String(n.magnitude) }
        var i = units.lastIndex { v >= $0.0 } ?? 0
        while true {
            let x = v / units[i].0
            let digits = x >= 100 ? 0 : x >= 10 ? 1 : 2
            let scale = pow(10, Double(digits))
            let rounded = (x * scale).rounded() / scale
            if rounded >= 1000, i + 1 < units.count {
                i += 1
                continue
            }
            return sign + trimmed(String(format: "%.\(digits)f", rounded)) + units[i].1
        }
    }

    static func count(_ n: Int) -> String { count(Int64(n)) }

    /// "31K/min": a rate of tokens per minute.
    static func perMinute(_ n: Int64) -> String { String(localized: "\(count(n))/min", comment: "Tokens per minute, as in 31K/min") }

    private static func trimmed(_ s: String) -> String {
        guard s.contains(".") else { return s }
        var t = s
        while t.hasSuffix("0") { t.removeLast() }
        if t.hasSuffix(".") { t.removeLast() }
        return t
    }
}

/// The four kinds of tokens, in the order a stacked chart puts them from the bottom.
nonisolated enum UsageKind: String, CaseIterable, Identifiable, Sendable {
    case input, output, cacheWrite, cacheRead

    var id: String { rawValue }

    var title: String {
        switch self {
        case .input: String(localized: "Input [Token Usage]", comment: "Token usage: input tokens not from the cache")
        case .output: String(localized: "Output [Token Usage]", comment: "Token usage: output tokens")
        case .cacheWrite: String(localized: "Cache Write", comment: "Token usage: tokens written to the prompt cache")
        case .cacheRead: String(localized: "Cache Read", comment: "Token usage: tokens read from the prompt cache")
        }
    }
}

/// The four kinds of tokens and the replies, summed over some time (docs/server/machines.md "Token usage").
nonisolated struct UsageTokens: Codable, Hashable, Sendable {
    var input: Int64 = 0
    var cacheWrite: Int64 = 0
    var cacheRead: Int64 = 0
    var output: Int64 = 0
    var requests: Int = 0

    enum CodingKeys: String, CodingKey {
        case input = "in", cacheWrite = "cache_write", cacheRead = "cache_read", output = "out", requests = "req"
    }

    init(input: Int64 = 0, cacheWrite: Int64 = 0, cacheRead: Int64 = 0, output: Int64 = 0, requests: Int = 0) {
        self.input = input
        self.cacheWrite = cacheWrite
        self.cacheRead = cacheRead
        self.output = output
        self.requests = requests
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        input = try c.decodeIfPresent(Int64.self, forKey: .input) ?? 0
        cacheWrite = try c.decodeIfPresent(Int64.self, forKey: .cacheWrite) ?? 0
        cacheRead = try c.decodeIfPresent(Int64.self, forKey: .cacheRead) ?? 0
        output = try c.decodeIfPresent(Int64.self, forKey: .output) ?? 0
        requests = try c.decodeIfPresent(Int.self, forKey: .requests) ?? 0
    }

    /// Every token, cache reads included.
    var total: Int64 { input + cacheWrite + cacheRead + output }

    /// Cache read / (input + cache write + cache read); nil without any input.
    var hitRate: Double? {
        let prompt = input + cacheWrite + cacheRead
        return prompt > 0 ? Double(cacheRead) / Double(prompt) : nil
    }

    func value(_ kind: UsageKind) -> Int64 {
        switch kind {
        case .input: input
        case .output: output
        case .cacheWrite: cacheWrite
        case .cacheRead: cacheRead
        }
    }

    static func + (a: Self, b: Self) -> Self {
        Self(input: a.input + b.input, cacheWrite: a.cacheWrite + b.cacheWrite, cacheRead: a.cacheRead + b.cacheRead,
             output: a.output + b.output, requests: a.requests + b.requests)
    }
}

/// One step of a usage chart: the step's sums, or a break where the machine was offline.
nonisolated struct UsagePoint: Decodable, Hashable, Sendable {
    var t: Int64
    var tokens: UsageTokens
    var gap: Bool

    enum CodingKeys: String, CodingKey { case t, gap }

    init(t: Int64, tokens: UsageTokens, gap: Bool = false) {
        self.t = t
        self.tokens = tokens
        self.gap = gap
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        t = try c.decode(Int64.self, forKey: .t)
        gap = try c.decodeIfPresent(Bool.self, forKey: .gap) ?? false
        tokens = try UsageTokens(from: decoder)
    }

    var date: Date { Date(timeIntervalSince1970: TimeInterval(t)) }
}

/// A model's or launcher's sums over the range; a launcher's empty name is the unknown one.
nonisolated struct UsageShare: Decodable, Hashable, Sendable, Identifiable {
    var name: String
    var tokens: UsageTokens

    enum CodingKeys: String, CodingKey { case name }

    init(name: String, tokens: UsageTokens) {
        self.name = name
        self.tokens = tokens
    }

    init(from decoder: Decoder) throws {
        name = try decoder.container(keyedBy: CodingKeys.self).decode(String.self, forKey: .name)
        tokens = try UsageTokens(from: decoder)
    }

    var id: String { name }
}

/// GET /api/v1/machines/{id}/usage.
nonisolated struct MachineUsageSeries: Decodable, Hashable, Sendable {
    var machineID: String
    var online: Bool
    var range: String
    var step: Int
    var points: [UsagePoint]
    var total: UsageTokens
    var models: [UsageShare]
    var launchers: [UsageShare]
    /// False for an older node: its usage names no launcher, so there is nothing to filter by.
    var launchersKnown: Bool

    enum CodingKeys: String, CodingKey {
        case machineID = "machine_id", online, range, step, points, total, models, launchers
        case launchersKnown = "launchers_known"
    }

    /// Minutes in a step: a point's sums over this are per minute.
    var minutesPerStep: Double { max(Double(step) / 60, 1) }
}

/// A machine's tokens on its card (GET /api/v1/metrics): today, all tokens per minute now, the last hour by minute.
nonisolated struct UsageCard: Codable, Hashable, Sendable {
    var today: UsageTokens
    var perMinute: Int64
    /// The last 60 minutes, oldest first; the last is the minute now.
    var recent: [Int64]

    enum CodingKeys: String, CodingKey { case today, perMinute = "per_minute", recent }
}

nonisolated enum UsageRange: String, CaseIterable, Identifiable, Sendable {
    case hour = "1h", day = "24h", week = "7d"

    var id: String { rawValue }

    var title: String {
        switch self {
        case .hour: String(localized: "1 Hour")
        case .day: String(localized: "24 Hours")
        case .week: String(localized: "7 Days", comment: "Token usage chart range")
        }
    }

    var seconds: TimeInterval {
        switch self {
        case .hour: 3600
        case .day: 24 * 3600
        case .week: 7 * 24 * 3600
        }
    }
}
