import Foundation

/// The menu-bar app and its helper must read the same state root.
nonisolated enum LinkStatePaths {
    static func serviceEnvironment(home: URL, environment: [String: String]) -> [String: String] {
        if environment["PERSONA_HOME"] != nil { return environment }
        let plist = home.appendingPathComponent("Library/LaunchAgents/com.persona.node.plist")
        guard let data = try? Data(contentsOf: plist),
              let value = try? PropertyListSerialization.propertyList(from: data, format: nil) as? [String: Any],
              let stored = value["EnvironmentVariables"] as? [String: String],
              let root = stored["PERSONA_HOME"] else { return environment }
        var result = environment
        result["PERSONA_HOME"] = root
        return result
    }

    static func root(home: URL, environment: [String: String]) -> URL {
        let explicit = environment["PERSONA_HOME"] ?? ""
        guard !explicit.isEmpty else { return home.appendingPathComponent(".persona", isDirectory: true) }
        let path: String
        if explicit == "~" { path = home.path }
        else if explicit.hasPrefix("~/") { path = home.path + "/" + explicit.dropFirst(2) }
        else if explicit.hasPrefix("/") { path = explicit }
        else { path = home.path + "/" + explicit }
        return URL(fileURLWithPath: clean(path), isDirectory: true)
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
