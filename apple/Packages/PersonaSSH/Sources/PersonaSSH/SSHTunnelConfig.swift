import Foundation

/// Where the tunnel goes: the SSH server, who logs in there, and the hub's address as the server sees it.
public struct SSHTunnelConfig: Sendable, Hashable {
    public var host: String
    public var port: Int
    /// As typed: a Warpgate target login (`user:target`) is just a user name to SSH.
    public var user: String
    public var targetHost: String
    public var targetPort: Int

    public init(host: String, port: Int = 22, user: String, targetHost: String = "127.0.0.1", targetPort: Int = 18660) {
        self.host = host
        self.port = port
        self.user = user
        self.targetHost = targetHost
        self.targetPort = targetPort
    }
}

/// Why the tunnel is not up, sorted by what the user does about it.
public enum SSHTunnelError: Error, Sendable, Equatable, LocalizedError {
    /// The SSH server did not answer (no route, refused, DNS, timed out).
    case unreachable(String)
    /// First connection: the server's key is not known yet. Shown to the user to confirm, never trusted silently.
    case hostKeyUnknown(fingerprint: String, algorithm: String)
    /// The server's key is not the one pinned: refused (someone in between, or the server was reinstalled).
    case hostKeyChanged(expected: String, actual: String)
    /// The server did not take this device's key (not in authorized_keys, or the wrong user).
    case notAuthorized
    /// The SSH handshake failed for another reason (no algorithm in common, the server closed the connection).
    case handshake(String)
    /// Logged in, but the server would not or could not open the hub's address (permitopen, or the port is closed).
    case targetRefused(String)
    case timedOut
    case stopped

    public var errorDescription: String? {
        switch self {
        case .unreachable(let why): "连不上 SSH 服务器（\(why)）"
        case .hostKeyUnknown: "第一次连这台 SSH 服务器，需要先确认它的指纹"
        case .hostKeyChanged: "SSH 服务器的指纹变了，已拒绝连接"
        case .notAuthorized: "SSH 服务器没有接受这台设备的公钥"
        case .handshake(let why): "SSH 握手失败（\(why)）"
        case .targetRefused(let why): "SSH 服务器打不开到 hub 的转发（\(why)）"
        case .timedOut: "SSH 连接超时"
        case .stopped: "SSH 隧道已关闭"
        }
    }

    /// Trying again without the user changing something does not help: the tunnel waits for a new configuration.
    public var needsUser: Bool {
        switch self {
        case .hostKeyUnknown, .hostKeyChanged, .notAuthorized: true
        default: false
        }
    }
}

/// How long things may take, and how the tunnel backs off after failing to connect (shorter in the tests).
public struct SSHTunnelTimings: Sendable {
    /// TCP connect to the SSH server.
    public var connect: TimeInterval = 10
    /// From TCP connected to logged in.
    public var handshake: TimeInterval = 15
    /// Opening one forwarded connection on a logged-in session: a session that takes longer is taken for dead.
    public var open: TimeInterval = 10
    /// After a failed connect, the next one waits backoffBase × 2^(failures-1), at most backoffCap; requests that
    /// come meanwhile fail at once with the last error.
    public var backoffBase: TimeInterval = 1
    public var backoffCap: TimeInterval = 30

    public init() {}

    func backoff(after failures: Int) -> TimeInterval {
        min(backoffCap, backoffBase * pow(2, Double(max(0, failures - 1))))
    }
}
