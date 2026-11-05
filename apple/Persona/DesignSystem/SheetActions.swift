import SwiftUI

// The way out of a sheet on a Mac (#1248). A Mac sheet has no navigation bar: only its bottom row of actions is drawn,
// so a 关闭 at the leading edge (`.topBarLeading`, `.navigation` there) never shows (#743). It cannot be swiped down
// and a click outside it only beeps, so that row's 取消 / 完成 (Esc) is the only way out, and Esc is easy to hit.

extension View {
    /// 取消 in an edit sheet's bar (Esc on a Mac). On a Mac, with unsaved edits it asks before throwing them away;
    /// iOS cancels at once, as it always has.
    /// `when`: only in a sheet, for a page that is also pushed.
    func sheetCancel(_ title: LocalizedStringKey = "取消", unsaved: Bool, when active: Bool = true,
                     cancel: @escaping () -> Void) -> some View {
        modifier(SheetCancel(title: title, unsaved: unsaved, active: active, cancel: cancel))
    }

    /// 完成 for a sheet that only shows things, in a Mac sheet's bottom row (Esc too). Nothing on iOS: there the page
    /// keeps its own 关闭 or the swipe down.
    func macSheetDone(_ done: @escaping () -> Void) -> some View {
        #if os(macOS)
        toolbar { ToolbarItem(placement: .cancellationAction) { Button(action: done) { Text("完成").lineLimit(1) } } }
        #else
        self
        #endif
    }
}

private struct SheetCancel: ViewModifier {
    var title: LocalizedStringKey
    var unsaved: Bool
    var active: Bool
    var cancel: () -> Void
    @State private var asking = false

    func body(content: Content) -> some View {
        if active { guarded(content) } else { content }
    }

    private func guarded(_ content: Content) -> some View {
        content
            .pageToolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button {
                        #if os(macOS)
                        if unsaved { asking = true; return }
                        #endif
                        cancel()
                    } label: {
                        Text(title).lineLimit(1)
                    }
                }
            }
            // On the content, not on the bar's button: the bar is hosted apart from the page.
            .confirmationDialog("放弃未保存的修改？", isPresented: $asking, titleVisibility: .visible) {
                Button("放弃修改", role: .destructive, action: cancel)
                Button("继续编辑", role: .cancel) {}
            } message: {
                Text("关掉后，这次改的内容不会保存。")
            }
            #if DEBUG && os(macOS)
            // `-showDiscardConfirm YES`: the question up as the sheet opens, for screenshots.
            .task {
                guard UserDefaults.standard.bool(forKey: "showDiscardConfirm") else { return }
                try? await Task.sleep(for: .seconds(1))
                asking = true
            }
            #endif
    }
}
