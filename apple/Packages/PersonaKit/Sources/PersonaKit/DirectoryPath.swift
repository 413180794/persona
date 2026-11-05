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
        if m.contains("permission denied") { return "没有权限列出「\(shown)」里的目录。知道下一级的名字，可以在上面直接输入完整路径" }
        if m.contains("no such file") { return "「\(path)」不存在" }
        if m.contains("not a directory") { return "「\(path)」不是目录" }
        return message.isEmpty ? "打不开「\(path)」" : message
    }
}
