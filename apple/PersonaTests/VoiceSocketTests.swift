import Foundation
import Network
import Testing
@testable import Persona

/// `HubVoiceSocket` against a loopback server that refuses the WebSocket handshake (docs/assistant/voice.md 1.3, 6.5). A call
/// sends `start` the moment it opens, so the failed send, the ping and the receive all race to report the close: the
/// hub's status must come out whichever wins, else a revoked token or a missing extension looks like a passing drop and
/// is tried three more times.
@MainActor
struct VoiceSocketTests {
    @Test(arguments: [401, 403, 404])
    func aRefusedHandshakeReportsItsStatus(status: Int) async throws {
        let server = try RefusingServer(status: status)
        try await server.start()
        defer { server.stop() }
        let session = URLSession(configuration: .ephemeral)
        defer { session.invalidateAndCancel() }
        // A few in a row: which completion reports first is up to URLSession.
        for _ in 0..<5 {
            var request = URLRequest(url: URL(string: "ws://127.0.0.1:\(server.port)/api/v1/extensions/voice/ws")!, timeoutInterval: 5)
            request.setValue("Bearer t", forHTTPHeaderField: "Authorization")
            let socket = HubVoiceSocket(request: request, session: session)
            let seen = SocketEvents()
            socket.open { event in seen.events.append(event) }
            socket.send(text: #"{"type":"start","protocol":2}"#)
            let deadline = Date.now.addingTimeInterval(5)
            while seen.close == nil, Date.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
            #expect(seen.close?.status == status)
            #expect(seen.events.count == 1, "closed once, and nothing before it")
            socket.close()
        }
    }
}

@MainActor private final class SocketEvents {
    var events: [VoiceSocketEvent] = []

    var close: VoiceSocketClose? {
        for case .closed(let close) in events { return close }
        return nil
    }
}

/// Answers every request with `status` and an empty body, then closes: a hub refusing the upgrade.
private final class RefusingServer: @unchecked Sendable {
    private let listener: NWListener
    private let queue = DispatchQueue(label: "voice-refusing-server")
    private let status: Int
    private var connections: [NWConnection] = []

    init(status: Int) throws {
        self.status = status
        let params = NWParameters.tcp
        params.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
        listener = try NWListener(using: params)
        listener.newConnectionHandler = { [weak self] c in self?.accept(c) }
    }

    var port: UInt16 { listener.port?.rawValue ?? 0 }

    func start() async throws {
        listener.start(queue: queue)
        for _ in 0..<200 where port == 0 { try await Task.sleep(for: .milliseconds(10)) }
    }

    func stop() {
        listener.cancel()
        queue.sync { connections.forEach { $0.cancel() } }
    }

    private func accept(_ c: NWConnection) {
        connections.append(c)
        c.start(queue: queue)
        c.receive(minimumIncompleteLength: 1, maximumLength: 64 << 10) { [status] _, _, _, _ in
            let reply = "HTTP/1.1 \(status) Refused\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
            c.send(content: Data(reply.utf8), contentContext: .finalMessage, isComplete: true, completion: .contentProcessed { _ in })
        }
    }
}
