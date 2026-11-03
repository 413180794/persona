import Foundation
import os

/// The push relay this build registers with (docs/push-relay.md): it holds the app's APNs key and pushes for hubs that
/// have none. The phone registers its device token here and hands the grant it gets, with its public key, to its own hub;
/// the relay never sees the key, so it cannot read what the hub seals.
nonisolated enum PushRelay {
    static let log = Logger(subsystem: "jarvis", category: "push-relay")

    /// From the build config (`app.push_relay`, Info.plist `JarvisPushRelay`); nil: this build uses no relay.
    static var url: URL? {
        guard let s = Bundle.main.object(forInfoDictionaryKey: "JarvisPushRelay") as? String,
              !s.isEmpty, let u = URL(string: s), u.scheme == "https" else { return nil } // the grant must not cross in the clear
        return u
    }

    private struct Cached: Codable { var relay: String; var token: String; var env: String; var topic: String; var grant: String }
    private static let account = "push-relay-grant"

    /// The grant for this device token: the cached one while relay, token, environment and app are the same, else a new one.
    static func grant(relay: URL, token: String, env: String, topic: String, session: URLSession = .shared) async throws -> String {
        if let data = Keychain.load(account: account), let c = try? JSONDecoder().decode(Cached.self, from: data),
           c.relay == relay.absoluteString, c.token == token, c.env == env, c.topic == topic {
            return c.grant
        }
        var req = URLRequest(url: relay.appending(path: "v1/register"), timeoutInterval: 15)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(["apns_token": token, "env": env, "topic": topic])
        let (data, resp) = try await session.data(for: req)
        struct Answer: Decodable { var grant: String?; var error: String? }
        let answer = try? JSONDecoder().decode(Answer.self, from: data)
        guard (resp as? HTTPURLResponse)?.statusCode == 200, let grant = answer?.grant, !grant.isEmpty else {
            throw RelayError(status: (resp as? HTTPURLResponse)?.statusCode ?? 0, message: answer?.error ?? "")
        }
        if let data = try? JSONEncoder().encode(Cached(relay: relay.absoluteString, token: token, env: env, topic: topic, grant: grant)) {
            Keychain.save(data, account: account)
        }
        log.notice("registered with the push relay")
        return grant
    }

    struct RelayError: LocalizedError {
        var status: Int
        var message: String
        var errorDescription: String? { "push relay \(status) \(message)" }
    }
}
