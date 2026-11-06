import Foundation

/// MarkdownUI shows raw HTML as source text. READMEs use a little of it (image grids in `<table>`, centered `<p>`,
/// `<img width=…>`), so turn the common tags into markdown and drop the rest, leaving their text.
public nonisolated enum MarkdownHTML {
    public static func simplify(_ md: String) -> String {
        guard md.contains("<") else { return md }
        var out: [String] = []
        var fence: MarkdownFence?
        var table: [String]?
        for line in md.components(separatedBy: "\n") {
            let t = line.trimmingCharacters(in: .whitespaces)
            if let f = fence {
                if f.isClosed(by: t) { fence = nil }
                out.append(line)
                continue
            }
            if let f = MarkdownFence.opening(t) {
                fence = f
                out.append(line)
                continue
            }
            if table != nil {
                table!.append(line)
                if t.range(of: "</table>", options: .caseInsensitive) != nil {
                    out.append(contentsOf: Self.table(table!.joined(separator: "\n")))
                    table = nil
                }
                continue
            }
            if t.range(of: "^<table\\b", options: [.regularExpression, .caseInsensitive]) != nil {
                if t.range(of: "</table>", options: .caseInsensitive) != nil {
                    out.append(contentsOf: Self.table(t))
                } else {
                    table = [line]
                }
                continue
            }
            if t.hasPrefix("<") && !t.hasPrefix("<http") {
                let s = inline(t)
                if !s.isEmpty { out.append(s) }
                continue
            }
            out.append(line)
        }
        if let table { out.append(contentsOf: table) }
        return out.joined(separator: "\n")
    }

    /// `<table>` → a markdown table, the first row as the header.
    private static func table(_ html: String) -> [String] {
        let rows = matches("<tr\\b[^>]*>(.*?)</tr>", in: html).map { row in
            matches("<t[dh]\\b[^>]*>(.*?)</t[dh]>", in: row).map { inline($0).replacingOccurrences(of: "|", with: "\\|") }
        }.filter { !$0.isEmpty }
        guard let width = rows.map(\.count).max() else { return [] }
        func line(_ cells: [String]) -> String {
            "| " + (cells + Array(repeating: "", count: width - cells.count)).joined(separator: " | ") + " |"
        }
        return [""] + [line(rows[0]), line(Array(repeating: "---", count: width))] + rows.dropFirst().map(line) + [""]
    }

    /// Images and links into markdown, line breaks into spaces, other tags dropped.
    public static func inline(_ html: String) -> String {
        var s = html
        s = replace("<img\\b[^>]*>", in: s) { tag in
            guard let src = attr("src", in: tag) else { return "" }
            return "![\(attr("alt", in: tag) ?? "")](\(src))"
        }
        s = replace("<a\\b[^>]*>(.*?)</a>", in: s) { tag in
            let text = replace("<[^>]+>", in: matches("<a\\b[^>]*>(.*?)</a>", in: tag).first ?? "") { _ in "" }
            guard let href = attr("href", in: tag) else { return text }
            return "[\(text)](\(href))"
        }
        s = replace("<br\\s*/?>", in: s) { _ in " " }
        s = replace("<[^>]+>", in: s) { _ in "" }
        for (k, v) in [("&nbsp;", " "), ("&lt;", "<"), ("&gt;", ">"), ("&quot;", "\""), ("&amp;", "&")] {
            s = s.replacingOccurrences(of: k, with: v)
        }
        return s.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    private static func attr(_ name: String, in tag: String) -> String? {
        matches("\\b\(name)\\s*=\\s*[\"']([^\"']*)[\"']", in: tag).first
    }

    /// The first capture group of each match (dot matches newlines, case-insensitive).
    private static func matches(_ pattern: String, in s: String) -> [String] {
        guard let re = try? NSRegularExpression(pattern: pattern, options: [.caseInsensitive, .dotMatchesLineSeparators]) else { return [] }
        let ns = s as NSString
        return re.matches(in: s, range: NSRange(location: 0, length: ns.length)).map { ns.substring(with: $0.range(at: 1)) }
    }

    private static func replace(_ pattern: String, in s: String, with f: (String) -> String) -> String {
        guard let re = try? NSRegularExpression(pattern: pattern, options: [.caseInsensitive, .dotMatchesLineSeparators]) else { return s }
        let ns = s as NSString
        var out = ""
        var last = 0
        for m in re.matches(in: s, range: NSRange(location: 0, length: ns.length)) {
            out += ns.substring(with: NSRange(location: last, length: m.range.location - last))
            out += f(ns.substring(with: m.range))
            last = m.range.location + m.range.length
        }
        return out + ns.substring(from: last)
    }
}
