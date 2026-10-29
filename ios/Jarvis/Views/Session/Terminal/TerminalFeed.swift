import Foundation
import Observation

/// Where the terminal tab's screen comes from (docs/terminal.md): the hub's live stream of the pane — herdr's frames
/// applied to a grid of the pane's size, continued from the last frame after a reconnect — or, with a hub, node or
/// herdr that cannot stream, the colored screen read once a second. Pages of scrollback load above it either way.
@MainActor @Observable
final class TerminalFeed {
    enum Mode { case stream, poll }

    let key: SessionKey
    private(set) var mode = Mode.poll
    /// Scrollback pages above the screen, oldest first.
    private(set) var older: [TermRow] = []
    private(set) var screen: [TermRow] = []
    private(set) var cols = 0
    private(set) var cursor: (row: Int, col: Int)?
    private(set) var hasOlder = true
    private(set) var atLimit = false
    private(set) var loading = false
    private(set) var loaded = false
    private(set) var offline = false
    /// Reading history: the screen stays as it was until 回到最新.
    private(set) var paused = false
    /// A failed page load (shown briefly).
    var error: String?
    /// The link's health for the status bar: when the screen was last known current, why not now, the next try.
    private(set) var lastOK: Date?
    private(set) var failure: String?
    private(set) var nextTry = Date.distantPast
    private var failures = 0

    @ObservationIgnored private let grid = TerminalGrid(cols: 1, rows: 1)
    @ObservationIgnored private var epoch: String?
    @ObservationIgnored private var seq = 0
    @ObservationIgnored private var subscribed = false
    /// Stream unavailable for this session (old node / herdr): stay on polling.
    @ObservationIgnored private var unsupported = false

    static let page = 200
    static let linkDown = "连不上 hub"

    init(key: SessionKey) { self.key = key }

    /// History, then the screen down to its last row with text or the cursor (a shell after `clear` has its prompt
    /// at the top and forty empty rows under it: following the end would show only those).
    var content: TerminalContent {
        var end = screen.count
        while end > 0, end - 1 > (cursor?.row ?? -1), screen[end - 1].allSatisfy(\.isBlank) { end -= 1 }
        var c = TerminalContent(rows: older + screen[..<end], cols: cols)
        if let cursor { c.cursor = (older.count + cursor.row, cursor.col) }
        return c
    }

    // MARK: Driving

    /// Called four times a second while the tab is on screen: keeps the stream subscribed, polls when it cannot
    /// stream, retries after failures and keeps the health current.
    func tick(_ model: AppModel) async {
        if mode == .poll, !unsupported, model.canStreamTerminal, model.isLive {
            mode = .stream // the hub's hello came after the first reads: what they concluded about history is moot
            hasOlder = true
            atLimit = false
            subscribe(model, continuing: false)
            return
        }
        switch mode {
        case .stream:
            guard model.isLive else {
                subscribed = false // the connection's hello subscribes again
                if failure == nil { failure = Self.linkDown }
                return
            }
            if failure == Self.linkDown { failure = nil } // back: an idle pane sends nothing, that is not a failure
            if !subscribed, Date() >= nextTry {
                subscribe(model, continuing: true)
            } else if subscribed, loaded, failure == nil {
                lastOK = Date()
            }
        case .poll:
            if !paused, Date() >= nextTry { await refresh(model) }
        }
    }

    func stop(_ model: AppModel) {
        if mode == .stream { model.terminalUnsubscribe(key) }
        subscribed = false
    }

    func retryNow(_ model: AppModel) {
        nextTry = .distantPast
        if mode == .stream, model.isLive { subscribe(model, continuing: true) }
    }

    private func subscribe(_ model: AppModel, continuing: Bool) {
        subscribed = true
        model.terminalSubscribe(key, epoch: continuing ? epoch : nil, after: seq) { [weak self, weak model] event in
            guard let self, let model else { return }
            self.receive(event, model)
        }
    }

    private func receive(_ event: TerminalEvent, _ model: AppModel) {
        switch event {
        case .frame(let epoch, let seq, let full, let cols, let rows, let data):
            switch apply(epoch: epoch, seq: seq, full: full, cols: cols, rows: rows, data: data) {
            case .applied:
                healthy()
                offline = false
                loaded = true
            case .duplicate:
                break
            case .gap:
                subscribe(model, continuing: false)
            }
        case .ended(let why, let unsupported):
            subscribed = false
            epoch = nil
            if unsupported {
                self.unsupported = true
                mode = .poll
                nextTry = .distantPast
                return
            }
            offline = why.contains("not running")
            failed(offline ? "会话没有在 herdr 里运行" : why)
            loaded = true
        case .reconnected:
            if mode == .stream { subscribe(model, continuing: true) }
        }
    }

    enum FrameResult { case applied, duplicate, gap }

    /// Applies a frame in order: a whole one always, a diff only right after the frame before it. A diff that does
    /// not follow (one went missing, or a new stream started without its whole frame) is a gap: start again from a
    /// whole frame.
    func apply(epoch: String, seq: Int, full: Bool, cols: Int, rows: Int, data: Data) -> FrameResult {
        if full {
            grid.reset(cols: cols, rows: rows)
        } else if epoch == self.epoch, seq <= self.seq {
            return .duplicate // a replay overlapping what arrived live
        } else if epoch != self.epoch || seq != self.seq + 1 {
            self.epoch = nil
            return .gap
        }
        grid.feed(data)
        self.epoch = epoch
        self.seq = seq
        if !paused { publish() }
        return .applied
    }

    private func publish() {
        screen = grid.lines
        cols = grid.cols
        cursor = grid.cursorVisible ? (grid.cursorRow, grid.cursorCol) : nil
    }

    private func healthy() {
        lastOK = Date()
        failure = nil
        failures = 0
    }

    private func failed(_ why: String) {
        failure = why
        failures += 1
        nextTry = Date().addingTimeInterval(Self.backoff(failures))
    }

    /// 1, 2, 4, 8 and then every 15 seconds.
    static func backoff(_ failures: Int) -> TimeInterval { min(15, pow(2, Double(max(0, failures - 1)))) }

    // MARK: Polling (no stream)

    private func refresh(_ model: AppModel) async {
        guard let api = model.api else { return }
        do {
            let r = try await api.screen(key, lines: Self.page, recent: true, ansi: true)
            guard mode == .poll else { return }
            offline = r.status == .offline
            screen = TerminalGrid.rows(fromANSI: r.text)
            cols = 0
            cursor = nil
            if screen.count < Self.page - 1 { hasOlder = false }
            healthy()
            nextTry = Date().addingTimeInterval(1)
        } catch {
            guard !error.isCancellation else { return }
            failed(error.shown ?? "读取终端失败")
        }
        loaded = true
    }

    /// Ask for the screen again right away (after sending keys, so their echo shows).
    func nudge() {
        if mode == .poll { nextTry = .distantPast }
    }

    // MARK: History

    /// The page of scrollback before the first rows on screen; the screen stops following the pane meanwhile.
    func loadOlder(_ model: AppModel) async {
        guard !loading, hasOlder, loaded, let api = model.api else { return }
        loading = true
        error = nil
        let wasPaused = paused
        paused = true // the screen must stay put while a page is placed above it
        defer {
            loading = false
            if older.isEmpty, !wasPaused { resume() } // nothing above (a full-screen TUI): keep following
        }
        var all = (older + screen).map(TerminalGrid.plain)
        while all.last?.isEmpty == true { all.removeLast() } // herdr's reads end at the last row with text
        let anchor = Array(all.prefix(20))
        do {
            let r = try await api.screen(key, before: anchor, loaded: all.count, lines: Self.page, ansi: true)
            if !r.text.isEmpty { older = TerminalGrid.rows(fromANSI: r.text) + older }
            hasOlder = r.hasMore
            atLimit = r.atLimit
        } catch {
            if !error.isCancellation { self.error = error.shown }
        }
    }

    /// Back to the live screen: history goes, the screen follows the pane again.
    func resume() {
        older = []
        hasOlder = true
        atLimit = false
        paused = false
        if mode == .stream { publish() } else { nextTry = .distantPast }
    }
}
