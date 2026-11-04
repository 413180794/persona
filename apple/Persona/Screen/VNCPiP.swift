import AVFoundation
import AVKit
import OSLog
import SwiftUI

#if os(iOS)
/// A Mac's VNC in the system's picture in picture (#1343): it floats over the other apps and the home screen, can be
/// dragged and resized, and only shows (touches are the full page's). Its source is a small layer in the app's window
/// fed the session's frames; the playback audio session lets it run in the background (silent, mixed with what else
/// plays). Restoring it opens the full page; closing it ends the connection unless the page has it.
@MainActor final class VNCPiP: NSObject, AVPictureInPictureControllerDelegate, AVPictureInPictureSampleBufferPlaybackDelegate {
    static let shared = VNCPiP()
    nonisolated static let log = Logger(subsystem: "jarvis", category: "vnc")
    private(set) var ref: MachineRef?
    private var from: SessionKey?
    private var host: VNCScreenUIView?
    private var controller: AVPictureInPictureController?
    private var restoring = false

    static var supported: Bool { AVPictureInPictureController.isPictureInPictureSupported() }
    var active: Bool { controller?.isPictureInPictureActive == true }

    /// ref's screen in the small window (another Mac's, if one was up, goes).
    func start(_ ref: MachineRef, from: SessionKey? = nil) {
        if self.ref == ref, active { return }
        stop()
        self.from = from
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback, options: [.mixWithOthers])
        try? AVAudioSession.sharedInstance().setActive(true)
        let session = VNC.hold(ref)
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        guard let window = (scenes.first { $0.activationState == .foregroundActive } ?? scenes.first)?.windows.first(where: { $0.isKeyWindow })
            ?? scenes.first?.windows.first else { VNC.release(ref); return }
        let v = VNCScreenUIView(frame: CGRect(x: window.bounds.maxX - 2, y: window.bounds.maxY - 2, width: 2, height: 2))
        v.session = session
        v.isUserInteractionEnabled = false
        v.accessibilityElementsHidden = true
        session.show(in: v.display)
        window.addSubview(v)
        let source = AVPictureInPictureController.ContentSource(sampleBufferDisplayLayer: v.display, playbackDelegate: self)
        let c = AVPictureInPictureController(contentSource: source)
        c.delegate = self
        c.requiresLinearPlayback = true
        self.ref = ref
        host = v
        controller = c
        AppModel.shared.vncPiP = ref
        // It can start once the layer has a frame.
        Task {
            for _ in 0..<100 {
                if c.isPictureInPicturePossible { c.startPictureInPicture(); return }
                if case .signIn = session.phase { return stopForSignIn() }
                if case .closed = session.phase { return stop() }
                try? await Task.sleep(for: .milliseconds(100))
            }
            Self.log.error("the small window never became possible")
            stop()
        }
    }

    func stop() {
        controller?.stopPictureInPicture()
        end()
    }

    /// The Mac wants a sign-in the phone does not keep: the page asks for it.
    private func stopForSignIn() {
        Self.log.notice("the Mac wants a sign-in: the page")
        let ref = self.ref, from = self.from
        end()
        if let ref { AppModel.shared.openScreen(ref, from: from) }
    }

    /// The whole page took it (the session goes on with the page).
    func handOver() { stop() }

    private func end() {
        if let ref, let host { VNC.current(ref)?.hide(from: host.display) }
        host?.removeFromSuperview()
        host = nil
        controller = nil
        if let ref { VNC.release(ref) }
        ref = nil
        AppModel.shared.vncPiP = nil
    }

    // MARK: AVPictureInPictureControllerDelegate

    nonisolated func pictureInPictureController(_ c: AVPictureInPictureController,
                                                restoreUserInterfaceForPictureInPictureStopWithCompletionHandler done: @escaping (Bool) -> Void) {
        MainActor.assumeIsolated {
            // The small window's 还原 (or a tap on it): the full page, which holds the session before this lets go.
            if let ref { AppModel.shared.openScreen(ref, from: from) }
            restoring = true
        }
        done(true)
    }

    nonisolated func pictureInPictureControllerDidStopPictureInPicture(_ c: AVPictureInPictureController) {
        MainActor.assumeIsolated {
            restoring = false
            end()
        }
    }

    nonisolated func pictureInPictureController(_ c: AVPictureInPictureController, failedToStartPictureInPictureWithError error: Error) {
        Self.log.error("the small window failed to start: \(error.localizedDescription, privacy: .public)")
        MainActor.assumeIsolated {
            let ref = self.ref, from = self.from
            end()
            if let ref { AppModel.shared.openScreen(ref, from: from) } // no small window: the page
        }
    }

    // MARK: AVPictureInPictureSampleBufferPlaybackDelegate (live: no time line, never paused)

    nonisolated func pictureInPictureController(_ c: AVPictureInPictureController, setPlaying playing: Bool) {}
    nonisolated func pictureInPictureControllerTimeRangeForPlayback(_ c: AVPictureInPictureController) -> CMTimeRange {
        CMTimeRange(start: .negativeInfinity, duration: .positiveInfinity)
    }
    nonisolated func pictureInPictureControllerIsPlaybackPaused(_ c: AVPictureInPictureController) -> Bool { false }
    nonisolated func pictureInPictureController(_ c: AVPictureInPictureController, didTransitionToRenderSize size: CMVideoDimensions) {}
    nonisolated func pictureInPictureController(_ c: AVPictureInPictureController, skipByInterval interval: CMTime,
                                                completion: @escaping () -> Void) { completion() }
}
#else
/// A Mac's VNC in a small window that floats over the other windows (#1343): it only shows; a click on it opens the
/// full window; closing it ends the connection unless that window has it.
@MainActor final class VNCPiP: NSObject, NSWindowDelegate {
    static let shared = VNCPiP()
    nonisolated static let log = Logger(subsystem: "jarvis", category: "vnc")
    private(set) var ref: MachineRef?
    private var from: SessionKey?
    private var panel: NSPanel?

    static var supported: Bool { true }
    var active: Bool { panel != nil }

    func start(_ ref: MachineRef, from: SessionKey? = nil) {
        if self.ref == ref, panel != nil { panel?.orderFront(nil); return }
        stop()
        self.from = from
        let session = VNC.hold(ref)
        let p = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 480, height: 300),
                        styleMask: [.titled, .closable, .resizable, .utilityWindow, .nonactivatingPanel], backing: .buffered, defer: false)
        p.title = "VNC · " + (AppModel.shared.machine(ref)?.name ?? ref.id)
        p.level = .floating
        p.isFloatingPanel = true
        p.hidesOnDeactivate = false
        p.isReleasedWhenClosed = false
        p.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        p.contentView = NSHostingView(rootView: VNCScreen(session: session, onClick: { [weak self] in self?.restore() }))
        p.delegate = self
        if let screen = NSScreen.main?.visibleFrame {
            p.setFrameOrigin(NSPoint(x: screen.maxX - p.frame.width - Theme.Space.xl, y: screen.maxY - p.frame.height - Theme.Space.xl))
        }
        p.orderFront(nil)
        self.ref = ref
        panel = p
        AppModel.shared.vncPiP = ref
    }

    /// The full window, then the small one goes.
    func restore() {
        guard let ref else { return }
        AppModel.shared.openScreen(ref, from: from)
    }

    /// The full window took it.
    func handOver() { stop() }

    func stop() {
        let p = panel
        panel = nil
        p?.close()
        if let ref { VNC.release(ref) }
        ref = nil
        AppModel.shared.vncPiP = nil
    }

    func windowWillClose(_ notification: Notification) {
        guard panel != nil else { return }
        panel = nil
        if let ref { VNC.release(ref) }
        ref = nil
        AppModel.shared.vncPiP = nil
    }
}
#endif
