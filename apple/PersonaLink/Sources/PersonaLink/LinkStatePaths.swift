import Foundation
import Darwin

/// The menu-bar app and its helper must read the same state root.
nonisolated enum LinkStatePaths {
    static func serviceEnvironment(home: URL, environment: [String: String]) -> [String: String] {
        if environment["PERSONA_HOME"] != nil || environment["JARVIS_HOME"] != nil { return environment }
        let plist = home.appendingPathComponent("Library/LaunchAgents/com.jarvis.node.plist")
        guard let data = try? Data(contentsOf: plist),
              let value = try? PropertyListSerialization.propertyList(from: data, format: nil) as? [String: Any],
              let stored = value["EnvironmentVariables"] as? [String: String] else { return environment }
        var result = environment
        for key in ["PERSONA_HOME", "JARVIS_HOME"] {
            if let value = stored[key] { result[key] = value }
        }
        return result
    }

    static func root(home: URL, environment: [String: String]) -> URL {
        let explicit = environment["PERSONA_HOME"] ?? environment["JARVIS_HOME"] ?? ""
        if !explicit.isEmpty {
            let path: String
            if explicit == "~" { path = home.path }
            else if explicit.hasPrefix("~/") { path = home.path + "/" + explicit.dropFirst(2) }
            else if explicit.hasPrefix("/") { path = explicit }
            else { path = home.path + "/" + explicit }
            return URL(fileURLWithPath: clean(path), isDirectory: true)
        }
        let current = home.appendingPathComponent(".persona", isDirectory: true)
        let legacy = home.appendingPathComponent(".jarvis", isDirectory: true)
        var old = stat()
        guard lstat(legacy.path, &old) == 0 else { return current }
        var a = stat(), b = stat()
        if stat(current.path, &a) == 0, stat(legacy.path, &b) == 0,
           a.st_dev == b.st_dev, a.st_ino == b.st_ino { return current }
        return legacy
    }

    /// Match filepath.Clean without resolving symlinks through a custom root.
    private static func clean(_ path: String) -> String {
        var components: [Substring] = []
        for component in path.split(separator: "/") {
            if component == "." { continue }
            if component == ".." { if !components.isEmpty { components.removeLast() } }
            else { components.append(component) }
        }
        return "/" + components.joined(separator: "/")
    }
}
