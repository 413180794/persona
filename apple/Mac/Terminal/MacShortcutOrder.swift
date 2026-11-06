#if os(macOS)
import Foundation

/// Terminal Settings › Quick Commands on the Mac: iOS reorders and deletes in the list's edit mode, which a Mac list does not have
/// (`EditButton` is empty there), so each command's context menu moves it up or down, or deletes it.
enum MacShortcutOrder {
    /// The list with the command at `index` moved `by` places (−1 up, +1 down); unchanged at either end.
    static func move(_ list: [String], at index: Int, by step: Int) -> [String] {
        let to = index + step
        guard list.indices.contains(index), list.indices.contains(to) else { return list }
        var out = list
        out.swapAt(index, to)
        return out
    }
}
#endif
