import SwiftUI

/// One row of a unified diff.
public struct DiffRow: Hashable, Sendable {
    public enum Kind: Sendable { case context, added, removed }

    /// A run of code; `change` marks a word-level edit inside an added or removed line.
    public struct Piece: Hashable, Sendable {
        public var text: String
        public var change: Kind?

        public init(_ text: String, change: Kind? = nil) {
            self.text = text
            self.change = change
        }
    }

    public var oldNumber: Int?
    public var newNumber: Int?
    public var kind: Kind
    public var pieces: [Piece]

    public init(old: Int?, new: Int?, kind: Kind, pieces: [Piece]) {
        self.oldNumber = old
        self.newNumber = new
        self.kind = kind
        self.pieces = pieces
    }

    public init(old: Int?, new: Int?, kind: Kind, _ text: String) {
        self.init(old: old, new: new, kind: kind, pieces: [Piece(text)])
    }
}

/// Light syntax colouring: strings and numbers, keywords, and called functions.
public enum CodeHighlighter {
    public enum Kind: Sendable, Equatable { case plain, literal, keyword, function }

    static let keywords: Set<String> = [
        "import", "from", "export", "default", "async", "function", "const", "let", "var", "await", "return",
        "if", "else", "for", "while", "new", "throw", "try", "catch", "null", "true", "false", "undefined",
    ]

    nonisolated(unsafe) private static let pattern = try! NSRegularExpression(pattern:
        #""(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`[^`]*`|\b\d+(?:\.\d+)?\b|\b(?:"#
        + keywords.sorted().joined(separator: "|")
        + #")\b|[A-Za-z_$][\w$]*(?=\s*\()"#
    )

    /// Splits a line into coloured runs.
    public static func tokens(_ line: String) -> [(text: String, kind: Kind)] {
        let ns = line as NSString
        var out: [(String, Kind)] = []
        var last = 0
        for match in pattern.matches(in: line, range: NSRange(location: 0, length: ns.length)) {
            let r = match.range
            if r.location > last { out.append((ns.substring(with: NSRange(location: last, length: r.location - last)), .plain)) }
            let token = ns.substring(with: r)
            let kind: Kind
            if let first = token.first, first == "\"" || first == "'" || first == "`" || first.isNumber {
                kind = .literal
            } else if keywords.contains(token) {
                kind = .keyword
            } else {
                kind = .function
            }
            out.append((token, kind))
            last = r.location + r.length
        }
        if last < ns.length { out.append((ns.substring(from: last), .plain)) }
        return out
    }
}

struct CodeChangeAttribute: TextAttribute {
    var added: Bool
}

/// A file listing with line numbers, or a unified diff with green/red row tints, an accent
/// bar (hatched for removals) and word-level highlights. Lines wrap.
public struct CodeBlock: View {
    enum Content {
        case code([String])
        case diff([DiffRow])
    }

    var filename: String
    var content: Content
    var copyText: String?
    var copyLabel: String
    var copiedLabel: String
    var onCopy: ((String) -> Void)?
    var counts: (additions: Int, deletions: Int)?
    var maxWidth: CGFloat = 420
    var elevation: BUIElevation = .card
    var onTitleTap: (() -> Void)?

    @State private var copied = false
    @Environment(\.buiTheme) private var theme
    @Environment(\.self) private var environment

    /// A line-numbered listing with a Copy button.
    public init(filename: String, lines: [String], copyText: String? = nil, copyLabel: String = "Copy", copiedLabel: String = "Copied", onCopy: ((String) -> Void)? = nil) {
        self.filename = filename
        self.content = .code(lines)
        self.copyText = copyText ?? lines.joined(separator: "\n")
        self.copyLabel = copyLabel
        self.copiedLabel = copiedLabel
        self.onCopy = onCopy
    }

    /// A unified diff with +/- counts in the header.
    /// - Parameters:
    ///   - additions / deletions: the header's counts when `diff` is only an excerpt of the change.
    ///   - elevation: `.hairline` drops the shadows (a block inside another card).
    ///   - onTitleTap: makes the file name a button (open the file).
    public init(
        filename: String,
        diff: [DiffRow],
        additions: Int? = nil,
        deletions: Int? = nil,
        maxWidth: CGFloat = 420,
        elevation: BUIElevation = .card,
        onTitleTap: (() -> Void)? = nil
    ) {
        self.filename = filename
        self.content = .diff(diff)
        self.copyText = nil
        self.copyLabel = "Copy"
        self.copiedLabel = "Copied"
        self.onCopy = nil
        if let additions, let deletions { self.counts = (additions, deletions) }
        self.maxWidth = maxWidth
        self.elevation = elevation
        self.onTitleTap = onTitleTap
    }

    /// Diffs without line numbers (an edit's old and new text) drop the number column and its rule.
    private var numbered: Bool {
        if case .diff(let rows) = content { return rows.contains { $0.oldNumber != nil || $0.newNumber != nil } }
        return true
    }

    public var body: some View {
        let numbered = numbered
        VStack(spacing: 0) {
            header
            VStack(spacing: 0) {
                switch content {
                case .code(let lines):
                    ForEach(Array(lines.enumerated()), id: \.offset) { index, line in
                        row(number: index + 1, kind: .context, pieces: [DiffRow.Piece(line)], numbered: true)
                    }
                case .diff(let rows):
                    ForEach(Array(rows.enumerated()), id: \.offset) { _, r in
                        row(number: r.kind == .removed ? r.oldNumber : r.newNumber, kind: r.kind, pieces: r.pieces, numbered: numbered)
                    }
                }
            }
            .background(alignment: .leading) {
                if numbered { Rectangle().fill(theme.colors.line).frame(width: 1).offset(x: 20) }
            }
            .padding(.vertical, 12)
        }
        .frame(maxWidth: maxWidth)
        .buiSurface(radius: theme.radii.card, elevation: elevation)
    }

    private var header: some View {
        HStack(spacing: 8) {
            if let onTitleTap {
                Button(action: onTitleTap) { title.contentShape(.rect) }
                    .buttonStyle(.plain)
            } else {
                title
            }
            Spacer(minLength: 8)
            switch content {
            case .diff(let rows):
                HStack(spacing: 8) {
                    Text("+\(counts?.additions ?? rows.filter { $0.kind == .added }.count)").foregroundStyle(theme.colors.green)
                    Text("-\(counts?.deletions ?? rows.filter { $0.kind == .removed }.count)").foregroundStyle(theme.colors.red)
                }
                .buiFont(BUITextStyle(size: 12, design: .monospaced, lineHeight: 1))
                .monospacedDigit()
            case .code:
                Button(action: copy) {
                    HStack(spacing: 4) {
                        BUIIcon(copied ? .check : .copy, size: 11, lineWidth: copied ? 3 : 2)
                        Text(copied ? copiedLabel : copyLabel)
                            .buiFont(BUITextStyle(size: 12, weight: .medium, lineHeight: 1))
                    }
                    .foregroundStyle(copied ? theme.colors.green : theme.colors.ink3)
                    .padding(.horizontal, 6)
                    .frame(height: 24)
                    .contentShape(.rect(cornerRadius: 6))
                }
                .buttonStyle(PressHighlightStyle(radius: 6, color: theme.colors.hover))
                .padding(.trailing, -4)
                .accessibilityLabel("Copy code")
            }
        }
        .padding(.horizontal, 16)
        .frame(height: 44 * theme.typeScale)
        .overlay(alignment: .bottom) {
            Rectangle().fill(theme.colors.line).frame(height: 1)
        }
    }

    private var title: some View {
        HStack(spacing: 7) {
            BUIIcon(BUIGlyph("M17.25 6.75 22.5 12l-5.25 5.25m-10.5 0L1.5 12l5.25-5.25m7.5-3-4.5 16.5"), size: 15, lineWidth: 1.8)
                .foregroundStyle(theme.colors.ink3)
            Text(filename)
                .buiFont(BUITextStyle(size: 12.5, design: .monospaced, lineHeight: 1))
                .foregroundStyle(theme.colors.ink)
                .lineLimit(1)
                .truncationMode(.head)
        }
    }

    private func row(number: Int?, kind: DiffRow.Kind, pieces: [DiffRow.Piece], numbered: Bool) -> some View {
        let tone: Color? = kind == .added ? theme.colors.green : kind == .removed ? theme.colors.red : nil
        return HStack(alignment: .firstTextBaseline, spacing: 0) {
            if numbered {
                Text(number.map(String.init) ?? "")
                    .buiFont(BUITextStyle(size: 11, design: .monospaced, lineHeight: 12.5 * 1.65 / 11))
                    .foregroundStyle(tone ?? theme.colors.ink3)
                    .frame(width: 20)
            }
            highlighted(pieces)
                .buiFont(BUITextStyle(size: 12.5, design: .monospaced, lineHeight: 1.65))
                .foregroundStyle(theme.colors.ink2)
                .textRenderer(CodeChangeRenderer(
                    added: Color(theme.colors.green.opacity(0.18).resolve(in: environment)),
                    removed: Color(theme.colors.red.opacity(0.18).resolve(in: environment))
                ))
                .fixedSize(horizontal: false, vertical: true)
                .padding(.leading, numbered ? 4 : 12)
                .padding(.trailing, 12)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .background(kind == .added ? theme.colors.greenTint : kind == .removed ? theme.colors.redTint : .clear)
        .overlay(alignment: .leading) {
            switch kind {
            case .added: Rectangle().fill(theme.colors.green).frame(width: 3)
            case .removed: Hatch(color: theme.colors.red).frame(width: 3)
            case .context: EmptyView()
            }
        }
    }

    private func highlighted(_ pieces: [DiffRow.Piece]) -> Text {
        var text = Text("")
        for piece in pieces {
            var run = Text("")
            for token in CodeHighlighter.tokens(piece.text) {
                var t = Text(token.text)
                switch token.kind {
                case .plain: break
                case .literal: t = t.foregroundStyle(theme.colors.orange)
                case .keyword: t = t.foregroundStyle(theme.colors.accentInk)
                case .function: t = t.foregroundStyle(theme.colors.ink).fontWeight(.medium)
                }
                run = Text("\(run)\(t)")
            }
            if let change = piece.change, change != .context {
                run = run.customAttribute(CodeChangeAttribute(added: change == .added))
            }
            text = Text("\(text)\(run)")
        }
        return text
    }

    private func copy() {
        guard let text = copyText else { return }
        #if canImport(UIKit)
        UIPasteboard.general.string = text
        #else
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        #endif
        onCopy?(text)
        withAnimation(.easeOut(duration: 0.1)) { copied = true }
        Task {
            try? await Task.sleep(for: .seconds(1.5))
            withAnimation(.easeOut(duration: 0.1)) { copied = false }
        }
    }
}

/// Rounded 18 % tints behind word-level edits (`rounded-[3px] px-[2px] -mx-px`).
struct CodeChangeRenderer: TextRenderer {
    var added: Color
    var removed: Color

    func draw(layout: Text.Layout, in ctx: inout GraphicsContext) {
        for line in layout {
            for run in line {
                if let change = run[CodeChangeAttribute.self] {
                    let r = run.typographicBounds.rect.insetBy(dx: -1, dy: 0)
                    ctx.fill(Path(roundedRect: r, cornerRadius: 3), with: .color(change.added ? added : removed))
                }
            }
            for run in line { ctx.draw(run) }
        }
    }
}

/// 45° red hatch: 1.5 pt stripes every 3 pt, as on the removed-line accent bar.
private struct Hatch: View {
    var color: Color

    var body: some View {
        Canvas { gc, size in
            var path = Path()
            var x: CGFloat = -size.height
            while x < size.width + size.height {
                path.move(to: CGPoint(x: x, y: size.height))
                path.addLine(to: CGPoint(x: x + size.height, y: 0))
                x += 3 * 2.squareRoot()
            }
            gc.stroke(path, with: .color(color), lineWidth: 1.5 / 2.squareRoot() * 1.5)
        }
        .clipped()
    }
}
