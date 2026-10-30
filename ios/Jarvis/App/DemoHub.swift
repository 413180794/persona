import Foundation

/// 体验模式 (docs/ios.md「体验模式」): a made-up hub inside the app, for someone without one (App Review, a friend
/// with the TestFlight link). It sits under the data layer, so no page knows: `APIClient.request` and `LiveConnection`
/// hand the demo config's traffic to it, and it answers with the hub's JSON. The data is scripts/mock_hub.py's, written
/// to Resources/Demo.json by scripts/make_demo_data.py, with times moved to now. Nothing leaves the phone: what would
/// reach a computer, Feishu or mail says 体验模式下不会真的发送 (`onNotSent`); a message sent to an agent is echoed
/// with a canned reply, so the chat behaves like one.
@MainActor
final class DemoHub {
    static let shared = DemoHub()

    /// Routes the demo config's requests and socket here (once, at launch; only the demo config uses them).
    static func install() {
        APIClient.local = { req in try await DemoHub.shared.answer(req) }
        LiveConnection.makeLocal = { DemoSocket() }
    }

    /// An action the demo does not carry out was taken (AppModel shows the toast).
    var onNotSent: (() -> Void)?

    private var data: [String: JSONValue] = [:]
    private var machines: [JSONValue] = []
    private var sessions: [JSONValue] = []
    private var items: [String: [JSONValue]] = [:]
    private var interactions: [JSONValue] = []
    private var inbox: [JSONValue] = []
    private var lark: [String: [JSONValue]] = [:]
    private var routines: [JSONValue] = []
    private var followups: [JSONValue] = []
    private var memory: [JSONValue] = []
    private var prefs: JSONValue = .object([:])
    private var pins: JSONValue = .object([:])
    private var mutes: JSONValue = .object(["spaces": .array([]), "sessions": .array([]), "allowed": .array([])])
    private var tokens: [JSONValue] = []
    /// When the demo started: the metrics' made-up events (a build, a machine away) sit relative to it.
    private var start = Date.now
    private var loaded = false
    private var sockets: [WeakSocket] = []
    private var made = 0

    private static let history = "demo-1"
    static let reply = "这是体验模式：消息没有真的发到电脑上，这里的 Agent 也只是演示数据。连接你自己的 hub 之后，Claude 会在那台电脑上接着做。"

    /// The demo's agents and conversations (drafts typed into them go with the demo).
    var sessionKeys: Set<SessionKey> {
        load()
        return Set(sessions.compactMap { s in
            s["machine_id"]?.string.flatMap { m in s["session_id"]?.string.map { SessionKey(machineID: m, sessionID: $0) } }
        })
    }

    /// The Jarvis tab's main conversation.
    var mainConversation: SessionKey? {
        load()
        return data["assistant"]?["main"]?.string.map { SessionKey(machineID: data["assistant"]?["machine_id"]?.string ?? "server", sessionID: $0) }
    }

    /// Back to the data as shipped (entering 体验模式 again starts over).
    func reset() {
        loaded = false
        sockets = []
    }

    private func load() {
        guard !loaded else { return }
        loaded = true
        start = .now
        guard let url = Bundle.main.url(forResource: "Demo", withExtension: "json"), let raw = try? Data(contentsOf: url),
              let root = try? JSON.decoder.decode(JSONValue.self, from: raw), case .object(let all) = Self.resolve(root, now: start)
        else { return }
        data = all
        machines = all["machines"]?.array ?? []
        sessions = all["sessions"]?.array ?? []
        items = (all["items"]?.object ?? [:]).compactMapValues(\.array)
        interactions = all["interactions"]?.array ?? []
        inbox = all["inbox"]?.array ?? []
        lark = (all["lark"]?.object ?? [:]).compactMapValues(\.array)
        routines = all["routines"]?.array ?? []
        followups = all["followups"]?.array ?? []
        memory = all["memory"]?.array ?? []
        prefs = all["prefs"] ?? .object([:])
        pins = all["pins"] ?? .object([:])
        tokens = all["tokens"]?.array ?? []
    }

    /// "@rel:<seconds>" → that moment as an ISO date, "@unix:<seconds>" → Unix seconds (make_demo_data.py).
    private static func resolve(_ v: JSONValue, now: Date) -> JSONValue {
        switch v {
        case .object(let o): return .object(o.mapValues { resolve($0, now: now) })
        case .array(let a): return .array(a.map { resolve($0, now: now) })
        case .string(let s) where s.hasPrefix("@rel:"):
            return .string(iso(now.addingTimeInterval(Double(s.dropFirst(5)) ?? 0)))
        case .string(let s) where s.hasPrefix("@unix:"):
            return .number((now.timeIntervalSince1970 + (Double(s.dropFirst(6)) ?? 0)).rounded())
        default: return v
        }
    }

    private static func iso(_ d: Date) -> String { d.formatted(.iso8601) }

    // MARK: HTTP

    func answer(_ req: LocalRequest) async throws -> Data {
        load()
        let out = try route(req)
        return try JSON.encoder.encode(out)
    }

    private func body(_ req: LocalRequest) -> JSONValue {
        req.body.flatMap { try? JSON.decoder.decode(JSONValue.self, from: $0) } ?? .object([:])
    }

    private func notFound() -> APIError { APIError(status: 404, message: "体验模式里没有这项数据") }

    private func logMiss(_ req: LocalRequest) {
        #if DEBUG
        NSLog("DemoHub miss: %@ %@", req.method, req.path) // DemoModeTests' walk-through: what no page should ask for
        #endif
    }

    /// An action the demo does not pretend to do: it fails with 体验模式下不会真的发送 (an info toast, not an error:
    /// AppModel.show), so no page claims it worked.
    private func refuse() -> APIError {
        onNotSent?()
        return APIError(status: 0, message: DemoText.notSent, fields: ["code": DemoText.code])
    }

    private func route(_ req: LocalRequest) throws -> JSONValue {
        let p = req.path.split(separator: "/", omittingEmptySubsequences: true).map(String.init)
        guard p.count >= 3, p[0] == "api", p[1] == "v1" else { throw notFound() }
        let rest = Array(p.dropFirst(2))
        do {
            return req.method == "GET" ? try get(rest, req.query) : try change(req.method, rest, body(req))
        } catch let e as APIError where e.status == 404 {
            if req.path != "/api/v1/fleet" { logMiss(req) } // no Live Activity in the demo: nothing there on purpose
            throw e
        }
    }

    private func get(_ p: [String], _ q: [String: String]) throws -> JSONValue {
        switch p {
        case ["health"]: return ["ok": true, "version": "demo"]
        case ["machines"]: return ["machines": .array(machines)]
        case ["metrics"]: return ["machines": .array(machines.compactMap { $0["id"]?.string }.map { metrics($0, span: 3600, step: 60) }), "step": 60]
        case ["sessions"]: return ["sessions": .array(sessions)]
        case ["interactions"]: return ["interactions": .array(interactions)]
        case ["interactions", "history"]: return ["interactions": .array([])]
        case ["auth", "tokens"]: return ["tokens": .array(tokens), "current": "dev_phone"]
        case ["pins"]: return pins
        case ["mutes"]: return mutes
        case ["assistant"]:
            var a = data["assistant"]?.object ?? [:]
            a["conversations"] = .array(sessions.filter { $0["assistant"]?.bool == true })
            return .object(a)
        case ["assistant", "routines"]: return ["routines": .array(routines)]
        case ["assistant", "followups"]: return ["followups": .array(followups)]
        case ["assistant", "runs"]:
            let runs = data["runs"]?.array ?? []
            return ["runs": .array(q["ref"].map { ref in runs.filter { $0["ref_id"]?.string == ref } } ?? runs)]
        case ["assistant", "memory"]: return ["files": .array(memory), "dir": "/home/me/.jarvis/assistant/memory"]
        case ["inbox"]: return ["items": .array(inbox)]
        case ["mail", "accounts"]: return ["accounts": data["mail_accounts"] ?? .array([])]
        case ["mail", "messages"]: return ["messages": .array([]), "has_more": false]
        case ["lark", "unread-count"]: return ["count": 4]
        case ["lark", "conversations"], ["lark", "pinned"]: return ["conversations": .array([]), "next_cursor": ""]
        case ["lark", "labels"]: return ["labels": .array([])]
        case ["lark", "flags"]: return ["conversations": .array([]), "messages": .array([])]
        case ["lark", "emojis"]: return ["emojis": .object([:])]
        case ["kb-search"]: return ["hits": .array([])]
        case ["lark", "reactions", "quick"]: return ["keys": .array([])]
        default: break
        }
        switch (p.first ?? "", p.count) {
        case ("machines", 3):
            let id = p[1]
            switch p[2] {
            case "metrics":
                let (span, step) = ["6h": (6 * 3600, 60), "24h": (86400, 240)][q["range"] ?? ""] ?? (3600, 10)
                return ["metrics": metrics(id, span: span, step: step), "range": .string(q["range"] ?? "1h"), "step": .number(Double(step))]
            case "launchers":
                guard online(id) else { throw APIError(status: 503, message: "\(id): machine is offline") }
                return ["launchers": data["launchers"] ?? .array([])]
            case "dirs":
                guard online(id) else { throw APIError(status: 503, message: "\(id): machine is offline") }
                return data["dirs"] ?? .object([:])
            case "claude-settings": return ["settings": .array([])]
            default: throw notFound()
            }
        case ("machines", 4) where p[2] == "git" && p[3] == "status": // no repositories in the demo: the chat shows no git card
            return ["is_repo": false, "ahead": 0, "behind": 0, "files": .array([]), "stashes": 0, "commits": .array([])]
        case ("devices", 3) where p[2] == "prefs": return prefs
        case ("inbox", 2):
            guard let it = inbox.first(where: { $0["id"]?.string == p[1] }), case .object(var o) = it else { throw notFound() }
            o["context"] = o["preview"]
            return .object(o)
        case ("mail", 4) where p[1] == "messages":
            guard let m = data["mail"]?[p[3]] else { throw notFound() }
            return ["message": m]
        case ("lark", 4) where p[1] == "conversations" && p[3] == "messages":
            return ["messages": .array(lark[p[2]] ?? []), "has_older": false, "has_newer": false]
        case ("lark", 4) where p[1] == "conversations" && p[3] == "labels":
            return ["label_ids": .array([]), "labels": .array([])]
        case ("kb", 2):
            return ["items": data["kb"]?[p[1]] ?? .array([]), "tags": .array([])]
        case ("kb", 3):
            guard let e = data["kb"]?[p[1]]?.array?.first(where: { $0["id"]?.string == p[2] }) else { throw notFound() }
            return ["detail": ["kind": .string(p[1]), "id": .string(p[2]), "path": .string("\(p[1])/\(p[2]).md"),
                               "summary": e["one_line"] ?? "", "log": .array([]), "links": .array([])], "agents": .array([])]
        case ("sessions", 4):
            return try sessionGet(SessionKey(machineID: p[1], sessionID: p[2]), p[3], q)
        case ("sessions", 6) where p[3] == "items" && p[5] == "full":
            guard let it = data["full"]?[p[4]] else { throw notFound() }
            return it
        default: throw notFound()
        }
    }

    private func sessionGet(_ k: SessionKey, _ what: String, _ q: [String: String]) throws -> JSONValue {
        let list = items[k.sessionID] ?? []
        let last = list.last?["seq"]?.int ?? 0
        switch what {
        case "items":
            if let a = q["after_seq"].flatMap(Int.init) {
                return ["items": .array(list.filter { ($0["seq"]?.int ?? 0) > a }), "more_after": false, "history": .string(Self.history),
                        "from_seq": .number(Double(a)), "to_seq": .number(Double(max(a, last)))]
            }
            if let through = q["through_seq"].flatMap(Int.init) {
                let from = q["from_seq"].flatMap(Int.init) ?? 0
                return ["items": .array(list.filter { (from...through).contains($0["seq"]?.int ?? -1) }), "more": false]
            }
            let before = q["before_seq"].flatMap(Int.init)
            let page = before.map { b in list.filter { ($0["seq"]?.int ?? 0) < b } } ?? list
            return ["items": .array(q["fold_work"] == "1" ? Self.foldWork(page) : page), "has_more": false,
                    "history": .string(Self.history), "to_seq": .number(Double(before.map { $0 - 1 } ?? last))]
        case "info":
            let s = session(k)
            return ["session_id": .string(k.sessionID), "cwd": s?["cwd"] ?? .null,
                    "model": .string(s?["agent"]?.string == "codex" ? "gpt-5.5" : "claude-opus-5"), "launcher": s?["launcher"] ?? .null,
                    "git_branch": "main", "version": "2.1.320", "permission_mode": s?["permission_mode"] ?? .null,
                    "effort": s?["effort"] ?? .null, "context_tokens": 48200, "output_tokens": 12800, "compactions": 0, "turns": 6,
                    "started_at": .string(Self.iso(.now.addingTimeInterval(-2400))), "last_activity": .string(Self.iso(.now))]
        case "screen": return ["text": data["screen"] ?? "", "status": "idle"]
        default: throw notFound()
        }
    }

    private func change(_ method: String, _ p: [String], _ body: JSONValue) throws -> JSONValue {
        switch p {
        case ["sessions"]:
            throw APIError(status: 403, message: "体验模式下不能新建会话：连接你自己的 hub 后再试")
        case ["assistant", "conversations"]:
            return ["session": startConversation(body["prompt"]?.string ?? "")]
        case ["assistant", "routines"]:
            routines = body["routines"]?.array ?? routines
            return ["routines": .array(routines)]
        case ["inbox", "refresh"]: return ["items": .array(inbox)]
        case ["pins"]:
            pins = body
            return pins
        case ["mutes"]:
            mutes = body
            return mutes
        case ["assistant", "warm"], ["devices"]: return ["ok": true]
        default: break
        }
        switch (p.first ?? "", p.count) {
        case ("sessions", 4):
            let k = SessionKey(machineID: p[1], sessionID: p[2])
            switch p[3] {
            case "messages":
                echo(k, body["text"]?.string ?? "")
                return ["ok": true]
            case "seen":
                markSeen(k.sessionID)
                return ["ok": true]
            case "modes":
                edit(k.sessionID) { s in
                    for f in ["permission_mode", "effort"] { if let v = body[f], v != .null { s[f] = v } }
                }
                broadcastSessions(k.machineID)
                return ["ok": true]
            default: throw refuse()
            }
        case ("interactions", 3) where p[2] == "resolve":
            resolve(p[1])
            onNotSent?()
            return ["ok": true]
        case ("devices", 3) where p[2] == "prefs":
            if case .object(var o) = prefs, case .object(let new) = body {
                for (k, v) in new where k != "island" { o[k] = v }
                prefs = .object(o)
            }
            return prefs
        case ("devices", 3): return ["ok": true] // live-activity reports
        case ("assistant", 4) where p[1] == "routines" || p[1] == "followups": throw refuse() // 立即运行
        case ("assistant", 3) where p[1] == "followups" && method == "DELETE":
            followups.removeAll { $0["id"]?.string == p[2] }
            return ["ok": true]
        case ("assistant", 3) where p[1] == "memory":
            memory.removeAll { $0["name"]?.string == p[2] }
            if method != "DELETE" {
                memory.append(["name": .string(p[2]), "content": body["content"] ?? "", "updated_at": .string(Self.iso(.now))])
            }
            return ["ok": true]
        case ("inbox", 3) where p[2] == "done":
            editInbox(p[1]) { $0["done_at"] = body["done"]?.bool == true ? .string(Self.iso(.now)) : .null }
            return ["ok": true]
        case ("inbox", 3) where p[2] == "draft":
            let it = inbox.first { $0["id"]?.string == p[1] }
            return ["text": it?["agent"]?["draft"] ?? "好的，我看一下，晚点回复你。"]
        case ("inbox", 3) where p[2] == "reply": throw refuse()
        case ("lark", 4) where p[1] == "conversations" && p[3] == "read": return ["ok": true]
        case ("mail", 5) where p[1] == "messages" && p[4] == "flags": return ["ok": true] // opening a mail marks it read
        case ("lark", 4) where p[1] == "conversations" && p[3] == "messages":
            let n = (lark[p[2]]?.compactMap { $0["position"]?.string.flatMap(Int.init) }.max() ?? 0) + 1
            let text = body["text"]?.string ?? ""
            lark[p[2], default: []].append(["id": .string("om_demo_\(n)"), "position": .string(String(n)), "sender_name": "我",
                                             "is_mine": true, "type": "TEXT", "text": .string(text), "recalled": false, "reply_count": 0,
                                             "created_at": .string(Self.iso(.now)), "parts": [["kind": "text", "text": .string(text)]]])
            onNotSent?()
            return ["ok": true]
        case ("auth", 3) where method == "DELETE":
            tokens = tokens.map { t in
                guard t["id"]?.string == p[2], case .object(var o) = t else { return t }
                o["revoked_at"] = .string(Self.iso(.now))
                return .object(o)
            }
            return ["ok": true]
        case ("machines", 3) where p[2] == "reconnect": return ["ok": true]
        case ("kb", _): return ["ok": true]
        default: throw refuse()
        }
    }

    // MARK: State

    private func session(_ k: SessionKey) -> JSONValue? {
        sessions.first { $0["session_id"]?.string == k.sessionID && $0["machine_id"]?.string == k.machineID }
    }

    private func online(_ machine: String) -> Bool {
        machines.contains { $0["id"]?.string == machine && $0["online"]?.bool == true }
    }

    private func edit(_ sid: String, _ change: (inout [String: JSONValue]) -> Void) {
        for i in sessions.indices where sessions[i]["session_id"]?.string == sid {
            guard case .object(var o) = sessions[i] else { continue }
            change(&o)
            sessions[i] = .object(o)
        }
    }

    private func editInbox(_ id: String, _ change: (inout [String: JSONValue]) -> Void) {
        for i in inbox.indices where inbox[i]["id"]?.string == id {
            guard case .object(var o) = inbox[i] else { continue }
            change(&o)
            inbox[i] = .object(o)
        }
    }

    private func markSeen(_ sid: String) {
        edit(sid) { s in
            guard s["state"]?.string == "unread" else { return }
            s["state"] = "idle"
            s["status"] = "idle"
            s["seen_at"] = .string(Self.iso(.now))
        }
    }

    private func append(_ sid: String, _ new: [String: JSONValue]) -> (item: JSONValue, from: Int) {
        let from = items[sid]?.last?["seq"]?.int ?? 0
        made += 1
        var o = new
        o["id"] = .string("demo-\(made)#0")
        o["seq"] = .number(Double(from + 1))
        o["ts"] = .string(Self.iso(.now))
        let item = JSONValue.object(o)
        items[sid, default: []].append(item)
        return (item, from)
    }

    /// A message to an agent: it shows as sent, the agent "works" a moment and answers that this is a demo.
    private func echo(_ k: SessionKey, _ text: String) {
        let (mine, from) = append(k.sessionID, ["role": "user", "kind": "text", "text": .string(text)])
        broadcastItems(k, [mine], from: from)
        edit(k.sessionID) { s in
            s["state"] = "working"
            s["status"] = "working"
            s["updated_at"] = .string(Self.iso(.now))
        }
        broadcastSessions(k.machineID)
        Task {
            try? await Task.sleep(for: .seconds(1.5))
            respond(k, Self.reply)
        }
    }

    private func respond(_ k: SessionKey, _ text: String) {
        let (reply, from) = append(k.sessionID, ["role": "assistant", "kind": "text", "text": .string(text)])
        broadcastItems(k, [reply], from: from)
        edit(k.sessionID) { s in
            let assistant = s["assistant"]?.bool == true
            s["state"] = assistant ? "idle" : "unread"
            s["status"] = assistant ? "idle" : "done"
            s["last_text"] = .string(text)
            s["updated_at"] = .string(Self.iso(.now))
        }
        broadcastSessions(k.machineID)
    }

    /// Answering a question / plan card: the card goes, the agent picks up and comes back with the demo's reply.
    private func resolve(_ id: String) {
        guard let ix = interactions.first(where: { $0["id"]?.string == id }),
              let m = ix["machine_id"]?.string, let sid = ix["session_id"]?.string else { return }
        interactions.removeAll { $0["id"]?.string == id }
        broadcast(["type": "interaction_resolved", "id": .string(id)])
        let k = SessionKey(machineID: m, sessionID: sid)
        edit(sid) { s in
            s["pending_interactions"] = 0
            s["state"] = "working"
            s["status"] = "working"
        }
        broadcastSessions(m)
        Task {
            try? await Task.sleep(for: .seconds(1.5))
            respond(k, "收到，按你的选择接着做。\n\n" + Self.reply)
        }
    }

    private func startConversation(_ prompt: String) -> JSONValue {
        made += 1
        let sid = String(format: "d0e0a0b0-0000-4000-8000-%012d", made)
        let title = String(prompt.prefix(20))
        sessions.append(["machine_id": "server", "session_id": .string(sid), "title": .string(title.isEmpty ? "新对话" : title),
                         "cwd": "/home/me/.jarvis/assistant", "status": "working", "state": "working", "agent": "claude",
                         "launcher": "claude", "model": "opus", "started_by_jarvis": true, "assistant": true,
                         "updated_at": .string(Self.iso(.now)), "last_text": "", "permission_mode": "default", "effort": "high",
                         "pending_interactions": 0, "rev": 1000])
        _ = append(sid, ["role": "user", "kind": "text", "text": .string(prompt)])
        broadcastSessions("server")
        let k = SessionKey(machineID: "server", sessionID: sid)
        Task {
            try? await Task.sleep(for: .seconds(1.5))
            respond(k, Self.reply)
        }
        return session(k) ?? .null
    }

    // MARK: Socket

    fileprivate func attach(_ s: DemoSocket) {
        load()
        sockets.removeAll { $0.socket == nil }
        sockets.append(WeakSocket(socket: s))
        s.send(["type": "hello", "server_version": "demo", "client_id": "demo",
                "features": ["file_upload", "terminal_stream"]])
        s.send(["type": "machines", "machines": .array(machines)])
        for m in Set(sessions.compactMap { $0["machine_id"]?.string }).sorted() {
            s.send(["type": "sessions", "machine_id": .string(m), "sessions": .array(sessions.filter { $0["machine_id"]?.string == m })])
        }
    }

    fileprivate func received(_ msg: JSONValue, from s: DemoSocket) {
        guard let type = msg["type"]?.string else { return }
        let key = msg["machine_id"]?.string.flatMap { m in msg["session_id"]?.string.map { SessionKey(machineID: m, sessionID: $0) } }
        switch type {
        case "subscribe":
            guard let key else { return }
            let list = items[key.sessionID] ?? []
            let last = list.last?["seq"]?.int ?? 0
            var out: [String: JSONValue] = ["type": "items", "machine_id": .string(key.machineID), "session_id": .string(key.sessionID),
                                            "backlog": true, "history": .string(Self.history)]
            if let since = msg["since_seq"]?.int, since > 0 {
                out["items"] = .array(list.filter { ($0["seq"]?.int ?? 0) > since })
                out["from_seq"] = .number(Double(since))
                out["to_seq"] = .number(Double(max(since, last)))
            } else {
                out["items"] = .array(msg["fold_work"]?.bool == true ? Self.foldWork(list) : list)
                out["has_more"] = false
                out["to_seq"] = .number(Double(last))
            }
            s.send(.object(out))
        case "seen":
            if let key { markSeen(key.sessionID) }
        case "terminal_subscribe":
            if let key { s.play(key, frames: data["terminal_frames"]?.array ?? []) }
        case "terminal_unsubscribe":
            s.stopPlaying()
        case "terminal_input":
            onNotSent?()
        default: break
        }
    }

    private func broadcast(_ msg: JSONValue) {
        for w in sockets { w.socket?.send(msg) }
    }

    private func broadcastSessions(_ machine: String) {
        broadcast(["type": "sessions", "machine_id": .string(machine), "sessions": .array(sessions.filter { $0["machine_id"]?.string == machine })])
    }

    private func broadcastItems(_ k: SessionKey, _ new: [JSONValue], from: Int) {
        broadcast(["type": "items", "machine_id": .string(k.machineID), "session_id": .string(k.sessionID), "items": .array(new),
                   "history": .string(Self.history), "from_seq": .number(Double(from)), "to_seq": .number(Double(from + new.count))])
    }

    // MARK: Folding (transcript.FoldWork, like mock_hub.fold_work)

    /// Each run of tool calls (with their results) and thoughts becomes one fold item.
    static func foldWork(_ list: [JSONValue]) -> [JSONValue] {
        let uses = Set(list.filter { $0["kind"]?.string == "tool_use" }.compactMap { $0["tool_use_id"]?.string })
        let results = Set(list.filter { $0["kind"]?.string == "tool_result" }.compactMap { $0["tool_use_id"]?.string })
        func inside(_ x: JSONValue) -> Bool {
            let kind = x["kind"]?.string ?? ""
            if kind == "thinking" { return true }
            guard let t = x["tool_use_id"]?.string else { return false }
            return ["tool_use", "tool_result", "image"].contains(kind) && uses.contains(t) && results.contains(t)
        }
        var out: [JSONValue] = []
        var i = 0
        while i < list.count {
            guard inside(list[i]) else {
                out.append(list[i])
                i += 1
                continue
            }
            var j = i
            while j < list.count && inside(list[j]) { j += 1 }
            let run = Array(list[i..<j])
            let size = run.reduce(0) { $0 + ($1["text"]?.string?.count ?? 0) + ($1["tool_input"]?.pretty.count ?? 0) }
            if run.count < 3 && size < 4096 {
                out += run
            } else {
                let first = run[0], last = run[run.count - 1]
                out.append(["id": .string("fold:" + (first["id"]?.string ?? "")), "seq": first["seq"] ?? 0, "ts": first["ts"] ?? .null,
                            "role": "assistant", "kind": "fold",
                            "fold": ["items": .number(Double(run.count)),
                                     "tools": .number(Double(run.filter { $0["kind"]?.string == "tool_use" }.count)),
                                     "thoughts": .number(Double(run.filter { $0["kind"]?.string == "thinking" && !($0["text"]?.string ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }.count)),
                                     "until": last["ts"] ?? .null, "through_seq": last["seq"] ?? 0, "through_id": last["id"] ?? ""]])
            }
            i = j
        }
        return out
    }

    // MARK: Metrics (mock_hub.metric_at: made-up but smooth curves)

    private struct Profile { var cpu: Double; var mem: Double; var rx: Double; var tx: Double; var swap: Double; var disk: Double; var diskTotal: Double }
    private static let profiles: [String: Profile] = [
        "server": Profile(cpu: 38, mem: 0.62, rx: 2.4e6, tx: 0.35, swap: 0, disk: 0.64, diskTotal: 1000),
        "macbook": Profile(cpu: 18, mem: 0.55, rx: 4.5e5, tx: 0.2, swap: 1.2, disk: 0.71, diskTotal: 1000),
        "studio": Profile(cpu: 27, mem: 0.31, rx: 1.2e6, tx: 0.6, swap: 0, disk: 0.38, diskTotal: 2000),
        "nas": Profile(cpu: 6, mem: 0.42, rx: 3e5, tx: 1.4, swap: 0.4, disk: 0.93, diskTotal: 4000),
    ]
    private static let gb = Double(1 << 30)

    private func system(_ mid: String) -> JSONValue? { machines.first { $0["id"]?.string == mid }?["system"] }

    /// Whether the machine was sending at t: the Mac Studio was away for two hours this morning, the NAS is offline.
    private func present(_ mid: String, _ t: Int) -> Bool {
        let t0 = Int(start.timeIntervalSince1970)
        if mid == "studio" && (t0 - 7 * 3600..<t0 - 5 * 3600).contains(t) { return false }
        if mid == "nas" && t >= t0 - 3 * 3600 { return false }
        return Self.profiles[mid] != nil
    }

    private func reading(_ mid: String, _ t: Int) -> (cpu: Double, load: Double, mem: Double, swap: Double, rx: Double, tx: Double, disk: Double) {
        let p = Self.profiles[mid]!
        let cores = system(mid)?["cores"]?.double ?? 8
        let memory = system(mid)?["memory"]?.double ?? 16 * Self.gb
        var x = UInt64(t / 10) &* 0x9E37_79B9_7F4A_7C15 ^ UInt64(mid.utf8.reduce(0) { $0 &* 31 &+ Int($1) })
        x ^= x >> 29
        x = x &* 0xBF58_476D_1CE4_E5B9
        x ^= x >> 32
        let h = Double(x % 1_000_000) / 1_000_000 // jitter 0…1
        let day = sin(2 * .pi * Double(t % 86400) / 86400)
        let wave = sin(Double(t) / 1900) * 0.5 + sin(Double(t) / 530 + Double(mid.count)) * 0.3
        let building = mid == "server" && t > Int(start.timeIntervalSince1970) - 25 * 60 // a build running when the demo opened
        let cpu = min(99.5, max(0.5, building ? 92 + 6 * h : p.cpu + 10 * day + 12 * wave + 8 * h))
        let memFrac = building ? 0.88 + 0.02 * h : p.mem + 0.05 * day + 0.04 * wave
        let rx = max(0, p.rx * (1 + 0.6 * wave + 0.5 * h))
        return (cpu: (cpu * 10).rounded() / 10, load: ((cores * cpu / 100 * (0.9 + 0.3 * h)) * 100).rounded() / 100,
                mem: (memFrac * memory).rounded(), swap: (p.swap * Self.gb).rounded(), rx: rx.rounded(), tx: (rx * p.tx * (0.7 + 0.6 * h)).rounded(),
                disk: (p.disk * p.diskTotal * Self.gb).rounded())
    }

    private func metrics(_ mid: String, span: Int, step: Int) -> JSONValue {
        let now = Int(Date.now.timeIntervalSince1970)
        var points: [JSONValue] = []
        var missing = 0
        if Self.profiles[mid] != nil {
            var t = (now - span) / step * step
            while t <= now {
                defer { t += step }
                guard present(mid, t) else {
                    missing += 1
                    if missing > 3, points.last?["gap"]?.bool != true { points.append(["t": .number(Double(t - (missing - 1) * step)), "gap": true]) }
                    continue
                }
                missing = 0
                let v = reading(mid, t)
                points.append(["t": .number(Double(t)), "cpu": .number(v.cpu), "load": .number(v.load), "mem": .number(v.mem),
                               "swap": .number(v.swap), "rx": .number(v.rx), "tx": .number(v.tx), "disk": .number(v.disk)])
            }
        }
        var latest: JSONValue = .null
        let t = now / 10 * 10
        if let p = Self.profiles[mid], present(mid, t) {
            let v = reading(mid, t)
            var o: [String: JSONValue] = ["t": .number(Double(t)), "cpu": .number(v.cpu), "load": .number(v.load), "mem_used": .number(v.mem),
                                          "mem_total": system(mid)?["memory"] ?? 0, "swap_used": .number(v.swap),
                                          "swap_total": .number(v.swap > 0 ? 4 * Self.gb : 0), "net_rx": .number(v.rx), "net_tx": .number(v.tx),
                                          "disk_used": .number(v.disk), "disk_total": .number(p.diskTotal * Self.gb)]
            if mid == "server" {
                let cores = Int(system(mid)?["cores"]?.double ?? 16)
                o["cores"] = .array((0..<cores).map { i in .number((min(100, max(1, v.cpu + 9 * Double((i * 37) % 7 - 3))) * 10).rounded() / 10) })
            }
            latest = .object(o)
        }
        let m = machines.first { $0["id"]?.string == mid }
        return ["machine_id": .string(mid), "online": m?["online"] ?? false, "system": m?["system"] ?? .null, "latest": latest,
                "points": .array(points)]
    }
}

private struct WeakSocket {
    weak var socket: DemoSocket?
}

/// The demo hub's end of the app's socket: one per `LiveConnection.open`.
@MainActor
final class DemoSocket: LocalSocket {
    private var deliver: ((Data) -> Void)?
    private var playing: Task<Void, Never>?

    func open(deliver: @escaping (Data) -> Void) {
        self.deliver = deliver
        DemoHub.shared.attach(self)
    }

    func receive(_ message: [String: Any]) {
        // Through JSON, not JSONValue.from: a boxed 0 or 1 would come out a Bool.
        guard let data = try? JSONSerialization.data(withJSONObject: message),
              let msg = try? JSON.decoder.decode(JSONValue.self, from: data) else { return }
        DemoHub.shared.received(msg, from: self)
    }

    func close() {
        deliver = nil
        stopPlaying()
    }

    /// Like a socket, later: never inside the call that sent the message (the app is still in the middle of it).
    func send(_ msg: JSONValue) {
        guard let data = try? JSON.encoder.encode(msg) else { return }
        DispatchQueue.main.async { [weak self] in self?.deliver?(data) }
    }

    /// The terminal: a made-up Claude screen, its spinner ticking once a second (mock_hub.terminal_frames).
    func play(_ k: SessionKey, frames: [JSONValue]) {
        stopPlaying()
        playing = Task { [weak self] in
            for f in frames {
                guard let self, !Task.isCancelled, case .object(var o) = f else { return }
                o["type"] = "terminal"
                o["machine_id"] = .string(k.machineID)
                o["session_id"] = .string(k.sessionID)
                o["epoch"] = "demo"
                self.send(.object(o))
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    func stopPlaying() {
        playing?.cancel()
        playing = nil
    }
}

nonisolated extension JSONValue: ExpressibleByStringLiteral, ExpressibleByBooleanLiteral, ExpressibleByIntegerLiteral,
    ExpressibleByDictionaryLiteral, ExpressibleByArrayLiteral {
    init(stringLiteral value: String) { self = .string(value) }
    init(booleanLiteral value: Bool) { self = .bool(value) }
    init(integerLiteral value: Int) { self = .number(Double(value)) }
    init(dictionaryLiteral elements: (String, JSONValue)...) { self = .object(Dictionary(elements, uniquingKeysWith: { $1 })) }
    init(arrayLiteral elements: JSONValue...) { self = .array(elements) }
}
