#if DEBUG
import AppKit
import SwiftUI

/// Screenshots without the screen (docs/mac.md「截图」): this Mac is often locked, and `screencapture` then only gets
/// the lock screen. `-macSnapshot <png>` renders the main window's own views into a PNG after a while
/// (`-macSnapshotDelay <s>`, default 4), then quits with `-macSnapshotQuit YES`. `-macWindowSize 1280x820` sizes the
/// window first; `-macAppearance dark|light` picks the appearance (`-AppleInterfaceStyle Dark` also works).
/// `scripts/mac_shot.sh` drives it.
@MainActor enum MacSnapshot {
    /// Before any window exists.
    static func applyAppearanceIfAsked() { applyAppearance(UserDefaults.standard.string(forKey: "macAppearance")) }

    /// `-macAppearance` for SwiftUI too: `NSApp.appearance` alone left List rows in the light scheme's colors.
    static var colorScheme: ColorScheme? {
        switch UserDefaults.standard.string(forKey: "macAppearance")?.lowercased() {
        case "dark": .dark
        case "light": .light
        default: nil
        }
    }

    static func scheduleIfAsked() {
        let d = UserDefaults.standard
        let size = d.string(forKey: "macWindowSize").flatMap(parseSize)
        if let size {
            // The window exists a moment after launch; size it as soon as it does.
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { resize(to: size) }
        }
        guard let path = d.string(forKey: "macSnapshot"), !path.isEmpty else { return }
        let delay = d.object(forKey: "macSnapshotDelay") != nil ? d.double(forKey: "macSnapshotDelay") : 4
        DispatchQueue.main.asyncAfter(deadline: .now() + max(delay, 0.5)) {
            if let size { resize(to: size) }
            // One more turn so a resize has laid out and drawn.
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) {
                let ok = capture(to: URL(fileURLWithPath: path))
                FileHandle.standardError.write(Data("macSnapshot \(ok ? "wrote" : "FAILED") \(path)\n".utf8))
                // A sheet is a window of its own, not drawn with the main one: it goes next to it as <name>-sheet.png.
                if let sheet = mainWindow?.attachedSheet {
                    let url = URL(fileURLWithPath: path)
                    let sheetURL = url.deletingPathExtension().appendingPathExtension("sheet").appendingPathExtension(url.pathExtension)
                    let wrote = capture(to: sheetURL, window: sheet)
                    FileHandle.standardError.write(Data("macSnapshot \(wrote ? "wrote" : "FAILED") \(sheetURL.path)\n".utf8))
                }
                // A conversation opened in its own window (-openChatWindow): next to it as <name>.chat.png.
                if let chat = NSApp.windows.first(where: { $0.isVisible && $0 !== mainWindow && $0.identifier?.rawValue.hasPrefix(MacChatWindow.id) == true }) {
                    let url = URL(fileURLWithPath: path)
                    let chatURL = url.deletingPathExtension().appendingPathExtension("chat").appendingPathExtension(url.pathExtension)
                    let wrote = capture(to: chatURL, window: chat)
                    FileHandle.standardError.write(Data("macSnapshot \(wrote ? "wrote" : "FAILED") \(chatURL.path)\n".utf8))
                }
                if d.bool(forKey: "macSnapshotQuit") {
                    NSApp.terminate(nil)
                    // A sheet with a text editor in it holds termination off: leave anyway.
                    DispatchQueue.main.asyncAfter(deadline: .now() + 1) { exit(0) }
                }
            }
        }
    }

    private static func applyAppearance(_ name: String?) {
        switch name?.lowercased() {
        case "dark": NSApp.appearance = NSAppearance(named: .darkAqua)
        case "light": NSApp.appearance = NSAppearance(named: .aqua)
        default: break
        }
    }

    private static func parseSize(_ s: String) -> CGSize? {
        let parts = s.lowercased().split(separator: "x").compactMap { Double($0) }
        guard parts.count == 2, parts[0] > 100, parts[1] > 100 else { return nil }
        return CGSize(width: parts[0], height: parts[1])
    }

    private static var mainWindow: NSWindow? {
        NSApp.windows.first { $0.isVisible && $0.contentView != nil && !($0 is NSPanel) && $0.className != "NSStatusBarWindow" }
    }

    private static func resize(to size: CGSize) {
        guard let w = mainWindow else { return }
        let frame = w.frameRect(forContentRect: NSRect(origin: .zero, size: size))
        w.setFrame(NSRect(origin: w.frame.origin, size: frame.size), display: true)
    }

    /// The window's frame view (title bar and toolbar included) drawn into a bitmap at the screen's scale. Views
    /// draw themselves through `cacheDisplay`, which does not need the window server's picture of the screen.
    /// Materials (NSVisualEffectView, glass) render as the window server would composite them only on screen: here
    /// they draw their fallback fill, so the window background is painted underneath first.
    private static func isShown(_ v: NSView) -> Bool {
        var cur: NSView? = v
        while let c = cur { if c.isHidden || c.alphaValue < 0.01 { return false }; cur = c.superview }
        return v.bounds.width > 0 && v.bounds.height > 0
    }

    private static func paint(_ rep: NSBitmapImageRep, appearance: NSAppearance? = nil, _ draw: () -> Void) {
        guard let ctx = NSGraphicsContext(bitmapImageRep: rep) else { return }
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = ctx
        (appearance ?? NSApp.effectiveAppearance).performAsCurrentDrawingAppearance(draw)
        NSGraphicsContext.restoreGraphicsState()
    }

    private static func filteredLayers(_ l: CALayer) -> [CALayer] {
        (l.compositingFilter != nil || l.filters?.isEmpty == false ? [l] : []) + (l.sublayers ?? []).flatMap(filteredLayers)
    }

    private static func descendants(of v: NSView) -> [NSView] { v.subviews + v.subviews.flatMap(descendants) }

    /// The window drawn into a bitmap at the screen's scale, title bar and toolbar included, through `cacheDisplay`:
    /// views draw themselves and nothing needs the window server's picture of the screen, so a locked Mac works.
    ///
    /// What that misses, found by trying (2026-10-31, docs/mac.md「截图」): the rows of AppKit-backed lists
    /// (SwiftUI `List` is an NSTableView) come out empty when drawn as part of the window, so every row is drawn again
    /// on its own and laid over its place; a selected row's material draws black off screen, so it is painted in the
    /// inactive selection color. Glass and other materials draw no blur: the window background shows through.
    /// `layer.render(in:)` and `dataWithPDF` were worse (upside-down, or most text missing).
    static func capture(to url: URL, window: NSWindow? = nil) -> Bool {
        guard let window = window ?? mainWindow, let frameView = window.contentView?.superview ?? window.contentView else { return false }
        frameView.layoutSubtreeIfNeeded()
        frameView.displayIfNeeded()
        let bounds = frameView.bounds
        guard let rep = frameView.bitmapImageRepForCachingDisplay(in: bounds) else { return false }
        paint(rep) { NSColor.windowBackgroundColor.setFill(); bounds.fill() }
        // Vibrant text (in sidebars, over glass) is drawn with a blend mode or filter against what is behind it; off
        // screen there is nothing behind and it comes out dark. Drawn plainly for the shot, and put back.
        let blended = frameView.layer.map(filteredLayers) ?? []
        let saved = blended.map { ($0.compositingFilter, $0.filters) }
        blended.forEach { $0.compositingFilter = nil; $0.filters = nil }
        defer { zip(blended, saved).forEach { $0.compositingFilter = $1.0; $0.filters = $1.1 } }
        window.effectiveAppearance.performAsCurrentDrawingAppearance { frameView.cacheDisplay(in: bounds, to: rep) }
        let rows = descendants(of: frameView).compactMap { $0 as? NSTableRowView }
        for row in rows where isShown(row) {
            guard let sub = row.bitmapImageRepForCachingDisplay(in: row.bounds) else { continue }
            let materials = descendants(of: row).compactMap { $0 as? NSVisualEffectView }.filter { !$0.isHidden }
            materials.forEach { $0.isHidden = true }
            if row.isSelected {
                paint(sub, appearance: row.effectiveAppearance) {
                    for m in materials {
                        let r = m.convert(m.bounds, to: row)
                        let box = row.isFlipped ? NSRect(x: r.minX, y: row.bounds.height - r.maxY, width: r.width, height: r.height) : r
                        let radius = m.layer?.cornerRadius ?? 0
                        NSColor.unemphasizedSelectedContentBackgroundColor.setFill()
                        NSBezierPath(roundedRect: box, xRadius: radius, yRadius: radius).fill()
                    }
                }
            }
            // Vibrant text (sidebar lists) is drawn with a blend mode against the material behind it; off screen
            // there is nothing behind and it comes out dark. Drawn plainly for the shot.
            row.effectiveAppearance.performAsCurrentDrawingAppearance { row.cacheDisplay(in: row.bounds, to: sub) }
            materials.forEach { $0.isHidden = false }
            // The window bitmap's context is in the frame view's points, origin at the bottom left, as `convert` gives;
            // clipped to what its scroll view shows.
            var r = row.convert(row.bounds, to: frameView)
            if let clip = row.enclosingScrollView?.contentView { r = r.intersection(clip.convert(clip.bounds, to: frameView)) }
            guard !r.isEmpty, let cg = sub.cgImage else { continue }
            let full = row.convert(row.bounds, to: frameView)
            if let ctx = NSGraphicsContext(bitmapImageRep: rep) {
                ctx.cgContext.saveGState()
                ctx.cgContext.clip(to: r)
                ctx.cgContext.draw(cg, in: full)
                ctx.cgContext.restoreGState()
                ctx.flushGraphics()
            }
        }
        guard let png = rep.representation(using: .png, properties: [:]) else { return false }
        do {
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try png.write(to: url)
            return true
        } catch {
            return false
        }
    }
}
#endif
