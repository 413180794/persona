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
    /// Match Case / Whole Word / Regex: remembered for the next file while the app runs. A change searches again from the
    /// current match, so it stays where it was when it still matches.
    var options = CodeFindModel.lastOptions {
        didSet {
            guard options != oldValue else { return }
            Self.lastOptions = options
            search(from: currentMatch?.line ?? view?.topLine ?? 0)
        }
    }
    @ObservationIgnored private static var lastOptions = CodeFind.Options()
    /// A regular expression that does not compile: nothing is searched and the bar says so.
    var invalid: Bool { !CodeFind.Matcher.isValid(query, options: options) }
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

    /// Find Next ⌘G / Find Previous ⇧⌘G: the match after (before) the current one; with the bar closed it opens again on
    /// the last query, at the first match from the top of what is shown (as Xcode does).
    func next(forward: Bool) {
        guard visible else { return show() }
        step(forward: forward)
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
        let found = CodeFind.matches(of: query, options: options, lineCount: source.lineCount) { source.findText($0) }
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

/// The bar over the code while finding: the field, how many matches and which one, previous / next, Done.
struct CodeFindBar: View {
    @Bindable var find: CodeFindModel
    @FocusState private var focused: Bool

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            HStack(spacing: Theme.Space.xs) {
                Image(systemName: Theme.Icon.search)
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                TextField(String(localized: "Find in file"), text: $find.query)
                    .textFieldStyle(.plain)
                    .focused($focused)
                    .accessibilityLabel("Find in file")
                if !find.query.isEmpty {
                    Text(count)
                        .font(Theme.Font.caption)
                        .monospacedDigit()
                        .foregroundStyle(find.matches.isEmpty ? Theme.danger : .secondary)
                        .lineLimit(1)
                        .fixedSize()
                }
                option("Aa", String(localized: "Match Case"), on: $find.options.caseSensitive)
                option(String(localized: "Word"), String(localized: "Whole Word"), on: $find.options.wholeWord)
                option(".*", String(localized: "Regular Expression"), on: $find.options.regex)
            }
            .padding(.horizontal, Theme.Space.s)
            .padding(.vertical, Theme.Space.xs)
            .background(Theme.Fill.well, in: RoundedRectangle(cornerRadius: Theme.Radius.badge))
            Button { find.step(forward: false) } label: {
                Image(systemName: Theme.Icon.findPrevious)
            }
            .help("Previous (⇧⏎ or ⇧⌘G)")
            .disabled(find.matches.isEmpty)
            .accessibilityLabel("Previous [Code Find Bar bd4b97]")
            Button { find.step(forward: true) } label: {
                Image(systemName: Theme.Icon.findNext)
            }
            .help("Next (⏎ or ⌘G)")
            .disabled(find.matches.isEmpty)
            .accessibilityLabel("Next [Code Find Bar a898d7]")
            // An icon, not Done: the file viewer's own Done is just below.
            Button { find.close() } label: {
                Image(systemName: Theme.Icon.close)
            }
            .help("Close find (Esc)")
            .accessibilityLabel("Close find")
        }
        .buttonStyle(.borderless)
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.sm)
        .background(.bar)
        .onAppear {
            focused = true
            startKeys()
        }
        .onDisappear(perform: stopKeys)
        .onChange(of: find.focusRequest) { focused = true }
    }

    /// Esc / ⏎ / ⇧⏎ are caught before the window sees them (`CodeFind.barKey`): the file viewer's Done holds Esc as
    /// its shortcut, so the field's own handlers came too late and Esc closed the whole sheet.
    @State private var keys: Any?

    private func startKeys() {
        guard keys == nil else { return }
        keys = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [find] event in
            let shift = event.modifierFlags.intersection(.deviceIndependentFlagsMask) == .shift
            let composing = (event.window?.firstResponder as? NSTextView)?.hasMarkedText() ?? false
            guard let key = CodeFind.barKey(keyCode: event.keyCode, shift: shift, fieldFocused: focused, composing: composing)
            else { return event }
            switch key {
            case .close: find.close()
            case .next: find.step(forward: true)
            case .previous: find.step(forward: false)
            }
            return nil
        }
    }

    private func stopKeys() {
        if let keys { NSEvent.removeMonitor(keys) }
        keys = nil
    }

    /// One of the field's switches: its short label, lit while on.
    private func option(_ label: String, _ name: String, on: Binding<Bool>) -> some View {
        Button { on.wrappedValue.toggle() } label: {
            Text(label)
                .font(Theme.Font.code)
                .lineLimit(1)
                .fixedSize()
                .foregroundStyle(on.wrappedValue ? Theme.accent : .secondary)
                .padding(.horizontal, Theme.Space.xs)
                .padding(.vertical, Theme.Space.xxs)
                .background(on.wrappedValue ? Theme.Fill.macSelected : .clear, in: RoundedRectangle(cornerRadius: Theme.Radius.badge))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(name)
        .accessibilityLabel(name)
        .accessibilityAddTraits(on.wrappedValue ? .isSelected : [])
    }

    private var count: String {
        if find.invalid { return String(localized: "Invalid regex") }
        guard !find.matches.isEmpty else { return String(localized: "No results") }
        return "\((find.current ?? 0) + 1) / \(find.matches.count)"
    }
}
#endif
