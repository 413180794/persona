import Foundation

/// The directory browser's typed paths and failures, apart from the view (DirectoryPathTests).
public nonisolated enum DirectoryPath {
    /// What to ask the hub for: `~`, `~/…` and absolute paths as typed (the node expands `~`); anything else is
    /// under `current` (under the home directory when there is none yet). nil for nothing typed.
    public static func resolve(_ typed: String, from current: String) -> String? {
        let t = typed.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return nil }
        if t.hasPrefix("/") || t == "~" || t.hasPrefix("~/") { return t }
        guard !current.isEmpty else { return "~/" + t }
        return (current as NSString).appendingPathComponent(t)
    }

    /// Why the hub couldn't list `path`, in words: its errors are Go's os errors ("open /x: permission denied").
    public static func problem(_ message: String, path: String) -> String {
        let m = message.lowercased()
        let name = (path as NSString).lastPathComponent
        let shown = name.isEmpty ? path : name
        if m.contains("permission denied") { return String(localized: "No permission to list the folders in “\(shown)”. If you know the next folder's name, type the full path above") }
        if m.contains("no such file") { return String(localized: "“\(path)” doesn't exist") }
        if m.contains("not a directory") { return String(localized: "“\(path)” isn't a folder") }
        return message.isEmpty ? String(localized: "Can't open “\(path)”") : message
    }
}
