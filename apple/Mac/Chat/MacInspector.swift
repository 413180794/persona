import SwiftUI

extension View {
    /// An inspector column on the trailing edge (the chat's Session Info, ⌥⌘I). SwiftUI's `.inspector` comes up empty
    /// inside the Agents split view's detail column, so this is the link panel's arrangement: a sibling in an HStack
    /// with a draggable divider, the width remembered; too narrow for both, it covers the page.
    func macInspector<Panel: View>(isPresented: Binding<Bool>, @ViewBuilder content: @escaping () -> Panel) -> some View {
        modifier(MacInspectorHost(shown: isPresented, panel: content))
    }
}

private struct MacInspectorHost<Panel: View>: ViewModifier {
    @Binding var shown: Bool
    let panel: () -> Panel
    @StoredValue("mac.inspector.width") private var width: Double = Theme.Size.macInspector
    @State private var dragging: Double?

    func body(content: Content) -> some View {
        GeometryReader { geo in
            let beside = geo.size.width >= Theme.Size.macInspectorMin + Theme.Size.macLinkPanelPageMin
            HStack(spacing: 0) {
                content
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                if shown, beside {
                    MacPanelResizeHandle { delta in
                        dragging = clamp((dragging ?? width) - delta, in: geo.size.width)
                    } ended: {
                        if let dragging { width = dragging }
                        dragging = nil
                    }
                    column.frame(width: clamp(dragging ?? width, in: geo.size.width))
                }
            }
            .overlay(alignment: .trailing) {
                if shown, !beside { column }
            }
        }
        .animation(Theme.Motion.layout, value: shown)
    }

    private var column: some View {
        panel()
            .background(Theme.Fill.macPanel)
            .transition(.move(edge: .trailing).combined(with: .opacity))
    }

    private func clamp(_ w: Double, in total: CGFloat) -> CGFloat {
        let most = max(Theme.Size.macInspectorMin, min(Theme.Size.macInspectorMax, total - Theme.Size.macLinkPanelPageMin))
        return min(max(w, Theme.Size.macInspectorMin), most)
    }
}
