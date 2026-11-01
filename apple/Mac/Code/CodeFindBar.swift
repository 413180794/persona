#if os(macOS)
import AppKit
import SwiftUI

/// The code viewer's find (⌘F over an open file): the query, its matches and which one is current. The bar edits
/// it; `CodeLinesView` searches its source with it, draws the matches and scrolls to the current one.
@MainActor @Observable final class CodeFindModel {
    var visible = false
    var query = "" {
        didSet { if query != oldValue { search(from: view?.topLine ?? 0) } }
    }
    private(set) var matches: [CodeFind.Match] = []
    private(set) var current: Int?
    /// A new value each time ⌘F asks for the field (it takes the keyboard again when the bar is already open).
    private(set) var focusRequest = 0
    /// The matches by line, for drawing the lines on screen.
    @ObservationIgnored private(set) var byLine: [Int: [NSRange]] = [:]
    @ObservationIgnored weak var view: CodeLinesView?

    var currentMatch: CodeFind.Match? { current.flatMap { matches.indices.contains($0) ? matches[$0] : nil } }

    func show() {
        let opening = !visible
        visible = true
        focusRequest += 1
        if opening, !query.isEmpty { search(from: view?.topLine ?? 0) }
    }

    func close() {
        guard visible else { return }
        visible = false
        set([], current: nil, scroll: false)
        if let view { view.window?.makeFirstResponder(view) }
    }

    /// ⏎ / ⇧⏎ and the bar's arrows.
    func step(forward: Bool) {
        guard let i = CodeFind.step(from: current, count: matches.count, forward: forward) else { return }
        current = i
        view?.findChanged(scroll: true)
    }

    /// The source's lines changed (a page came in, a JSON node opened): search them again, staying near the
    /// current match, without scrolling.
    func sourceChanged() {
        guard visible, !query.isEmpty else { return }
        search(from: currentMatch?.line ?? view?.topLine ?? 0, scroll: false)
    }

    private func search(from line: Int, scroll: Bool = true) {
        guard visible, let source = view?.source, !query.isEmpty else {
            set([], current: nil, scroll: false)
            return
        }
        let found = CodeFind.matches(of: query, lineCount: source.lineCount) { source.findText($0) }
        set(found, current: CodeFind.first(in: found, atOrAfter: line), scroll: scroll)
    }

    private func set(_ found: [CodeFind.Match], current: Int?, scroll: Bool) {
        matches = found
        self.current = current
        byLine = Dictionary(grouping: found, by: \.line).mapValues { $0.map(\.range) }
        view?.findChanged(scroll: scroll)
    }
}

extension CodeLineSource {
    /// A line's plain text for find: the file's own bytes where there are some (no attributed string made per line).
    func findText(_ i: Int) -> String {
        (self as? CodeFileSource)?.doc.text(i) ?? line(i).string
    }
}

/// The bar over the code while finding: the field, how many matches and which one, previous / next, 完成.
struct CodeFindBar: View {
    @Bindable var find: CodeFindModel
    @FocusState private var focused: Bool

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            HStack(spacing: Theme.Space.xs) {
                Image(systemName: Theme.Icon.search)
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                TextField("在文件中查找", text: $find.query)
                    .textFieldStyle(.plain)
                    .focused($focused)
                    // ⏎ the next match, ⇧⏎ the one before (the field editor sends both as its action).
                    .onSubmit { find.step(forward: !(NSApp.currentEvent?.modifierFlags.contains(.shift) ?? false)) }
                    .onKeyPress(.escape) {
                        find.close()
                        return .handled
                    }
                    .accessibilityLabel("在文件中查找")
                if !find.query.isEmpty {
                    Text(count)
                        .font(Theme.Font.caption)
                        .monospacedDigit()
                        .foregroundStyle(find.matches.isEmpty ? Theme.danger : .secondary)
                        .lineLimit(1)
                        .fixedSize()
                }
            }
            .padding(.horizontal, Theme.Space.s)
            .padding(.vertical, Theme.Space.xs)
            .background(Theme.Fill.well, in: RoundedRectangle(cornerRadius: Theme.Radius.badge))
            Button { find.step(forward: false) } label: {
                Image(systemName: Theme.Icon.findPrevious)
            }
            .help("上一个（⇧⏎）")
            .disabled(find.matches.isEmpty)
            .accessibilityLabel("上一个")
            Button { find.step(forward: true) } label: {
                Image(systemName: Theme.Icon.findNext)
            }
            .help("下一个（⏎）")
            .disabled(find.matches.isEmpty)
            .accessibilityLabel("下一个")
            // An icon, not 完成: the file viewer's own 完成 is just below.
            Button { find.close() } label: {
                Image(systemName: Theme.Icon.close)
            }
            .help("关闭查找（Esc）")
            .accessibilityLabel("关闭查找")
        }
        .buttonStyle(.borderless)
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.sm)
        .background(.bar)
        .onAppear { focused = true }
        .onChange(of: find.focusRequest) { focused = true }
    }

    private var count: String {
        guard !find.matches.isEmpty else { return "没有找到" }
        return "\((find.current ?? 0) + 1) / \(find.matches.count)"
    }
}
#endif
