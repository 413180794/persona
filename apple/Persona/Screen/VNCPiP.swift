import AVFoundation
import AVKit
import OSLog
import SwiftUI

#if os(iOS)
/// A Mac's VNC in the system's picture in picture (#1343): it floats over the other apps and the home screen, can be
/// dragged and resized, and only shows (touches are the full page's). Restoring it opens the full page; closing it
/// ends the connection unless the page has it.
///
/// Its source is a layer of the Mac's screen of a normal size (#1839: a 2×2 one in a window that went away never
/// started), in the app's main window behind its content: in a window, not hidden, not seen. It is fed samples on the
/// host clock and has a timebase of its own. The page makes it ready as soon as it shows a Mac, so that 小窗 (a tap)
/// and going home start it at once; from a machine's VNC it starts once there is a picture, or the page opens with why
/// not (VNCPiPLog, 小窗诊断).
@MainActor final class VNCPiP: NSObject, AVPictureInPictureControllerDelegate, AVPictureInPictureSampleBufferPlaybackDelegate {
    static let shared = VNCPiP()
    nonisolated static let log = Logger(subsystem: "jarvis", category: "vnc")
    private(set) var ref: MachineRef?
    private var from: SessionKey?
    private var host: VNCScreenUIView?
    private var controller: AVPictureInPictureController?
    private var watching: NSKeyValueObservation?
    /// The small window is asked for (not just made ready under the page).
    private var wanted = false
    /// The system said it is starting (willStart): no more tries.
    private var starting = false
    private var restoring = false
    private var waiting: Task<Void, Never>?

    static var supported: Bool { AVPictureInPictureController.isPictureInPictureSupported() }
    var active: Bool { controller?.isPictureInPictureActive == true }
    /// How long a machine's VNC waits for the small window before the page instead, and how often it asks the system.
    static let patience: Duration = .seconds(15)
    static let tries = 4

    /// Ready for ref's small window (the page, once it shows the Mac): the source, the controller and the playback
    /// audio session; `automatic`: it starts by itself when the app goes to the background.
    @discardableResult func prepare(_ ref: MachineRef, from: SessionKey?, automatic: Bool) -> Bool {
        if self.ref == ref, let controller {
            controller.canStartPictureInPictureAutomaticallyFromInline = automatic
            if let from { self.from = from }
            return true
        }
        end()
        guard Self.supported else { return note("这台设备不支持画中画") }
        let audio = AVAudioSession.sharedInstance()
        do {
            try audio.setCategory(.playback, mode: .moviePlayback, options: [.mixWithOthers])
            try audio.setActive(true)
        } catch {
            note("音频会话没打开：\(error.localizedDescription)")
        }
        guard let window = Self.mainWindow() else { return note("找不到 App 的主窗口") }
        let session = VNC.hold(ref)
        let size = Self.sourceSize(session.size)
        let v = VNCScreenUIView(frame: CGRect(x: 0, y: window.bounds.maxY - size.height, width: size.width, height: size.height))
        v.session = session
        v.isUserInteractionEnabled = false
        v.accessibilityElementsHidden = true
        Self.giveTimebase(v.display)
        session.show(in: v.display, timed: true)
        window.insertSubview(v, at: 0) // behind the app's content
        let c = AVPictureInPictureController(contentSource: .init(sampleBufferDisplayLayer: v.display, playbackDelegate: self))
        c.delegate = self
        c.requiresLinearPlayback = true
        c.canStartPictureInPictureAutomaticallyFromInline = automatic
        self.ref = ref
        self.from = from
        host = v
        controller = c
        watching = c.observe(\.isPictureInPicturePossible, options: [.new]) { c, _ in
            let possible = c.isPictureInPicturePossible
            Task { @MainActor in VNCPiP.shared.possibleChanged(possible) }
        }
        VNCPiPLog.add("准备好了：源 \(Int(size.width))×\(Int(size.height))pt，\(automatic ? "回桌面时自己开" : "等开")，现在能开：\(VNCPiPLog.yes(c.isPictureInPicturePossible))")
        return true
    }

    /// ref's screen in the small window: at once when it can be (in the tap that asked), else once there is a picture;
    /// failing that, the page, saying why.
    func start(_ ref: MachineRef, from: SessionKey? = nil) {
        if self.ref == ref, active { return }
        VNCPiPLog.add("要小窗")
        guard prepare(ref, from: from, automatic: false) else { return fallBack(ref, from, why: VNCPiPLog.last ?? "这台设备不支持画中画") }
        wanted = true
        starting = false
        // With the source still empty (connecting) the system takes the call and does nothing, not even failing
        // (TestFlight 161's 小窗诊断): in the tap only once there is a picture, else as soon as there is one.
        if let c = controller, c.isPictureInPicturePossible, VNC.current(ref)?.pictured == true {
            VNCPiPLog.add("在点按里开")
            c.startPictureInPicture()
        } else {
            VNCPiPLog.add("画面还没到：到了再开")
        }
        waiting?.cancel()
        waiting = Task { [weak self] in
            let deadline = ContinuousClock.now + Self.patience
            var tries = 0, lastTry = ContinuousClock.now, pictured = false
            while ContinuousClock.now < deadline {
                try? await Task.sleep(for: .milliseconds(200))
                guard let self, !Task.isCancelled, self.wanted, !self.active, self.ref == ref else { return }
                let session = VNC.current(ref)
                switch session?.phase {
                case .signIn: return self.fallBack(ref, self.from, why: nil) // the page asks for the sign-in
                case .closed(let why): return self.fallBack(ref, self.from, why: why ?? "连接断了")
                default: break
                }
                guard session?.pictured == true, !self.starting, let c = self.controller else { continue }
                if !pictured {
                    pictured = true
                    VNCPiPLog.add("画面到了")
                }
                // Again every 1.5 s while the system has not started it, a few times.
                guard c.isPictureInPicturePossible, tries < Self.tries,
                      tries == 0 || ContinuousClock.now - lastTry >= .milliseconds(1500) else { continue }
                tries += 1
                lastTry = ContinuousClock.now
                VNCPiPLog.add(tries == 1 ? "开（不在点按里）" : "系统没反应，再开一次（第 \(tries) 次）")
                c.startPictureInPicture()
            }
            guard let self, !Task.isCancelled, self.wanted, !self.active, self.ref == ref else { return }
            let why = !pictured ? "画面没到（\(Int(Self.patience.components.seconds)) 秒内）"
                : tries == 0 ? "系统说现在不能开" : "开了 \(tries) 次，系统都没反应"
            self.fallBack(ref, self.from, why: why)
        }
    }

    /// Said in 小窗诊断; the start waits for a picture (start's loop), not for this.
    private func possibleChanged(_ possible: Bool) {
        guard let c = controller, c.isPictureInPicturePossible == possible else { return }
        VNCPiPLog.add("现在能开：\(VNCPiPLog.yes(possible))")
    }

    /// The whole page took it: the small window goes (the source stays ready for the page).
    func handOver() {
        wanted = false
        waiting?.cancel()
        if active, !restoring { controller?.stopPictureInPicture() }
    }

    /// The page went: nothing is left to show the small window for unless it is up or asked for.
    func pageGone(_ ref: MachineRef) {
        if self.ref == ref, !wanted, !active { end() }
    }

    func stop() {
        wanted = false
        if active { controller?.stopPictureInPicture() }
        end()
    }

    private func fallBack(_ ref: MachineRef, _ from: SessionKey?, why: String?) {
        let pageUp = AppModel.shared.screenOpen?.machine == ref
        end()
        if let why {
            VNCPiPLog.add("改用整页：\(why)")
            AppModel.shared.toast = Toast("小窗打不开：\(why)，已改用整页", style: .error)
        }
        if !pageUp { AppModel.shared.openScreen(ref, from: from) }
    }

    private func end() {
        waiting?.cancel()
        waiting = nil
        watching = nil
        wanted = false
        if let ref, let host { VNC.current(ref)?.hide(from: host.display) }
        host?.removeFromSuperview()
        host = nil
        controller = nil
        if let ref { VNC.release(ref) }
        ref = nil
        AppModel.shared.vncPiP = nil
    }

    @discardableResult private func note(_ why: String) -> Bool {
        VNCPiPLog.add(why)
        return false
    }

    /// The app's own window (not the page's, ScreenWindow, nor the toasts'): it stays while the small window is up.
    private static func mainWindow() -> UIWindow? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let scene = scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
        return scene?.windows.first { $0.windowLevel == .normal && !$0.isHidden && $0.rootViewController != nil }
    }

    /// A normal size in the Mac's proportions (16:10 before it is known).
    private static func sourceSize(_ mac: CGSize) -> CGSize {
        let width = Theme.Size.pictureInPictureSource
        let ratio = mac.width > 1 && mac.height > 1 ? mac.height / mac.width : 10.0 / 16
        return CGSize(width: width, height: (width * ratio).rounded())
    }

    /// The layer's own clock, the host's, running: the samples' times (RFBFrameSink) are shown as they come.
    private static func giveTimebase(_ layer: AVSampleBufferDisplayLayer) {
        var timebase: CMTimebase?
        guard CMTimebaseCreateWithSourceClock(allocator: kCFAllocatorDefault, sourceClock: CMClockGetHostTimeClock(), timebaseOut: &timebase) == noErr,
              let timebase else { return }
        CMTimebaseSetTime(timebase, time: CMClockGetTime(CMClockGetHostTimeClock()))
        CMTimebaseSetRate(timebase, rate: 1)
        layer.controlTimebase = timebase
    }

    // MARK: AVPictureInPictureControllerDelegate

    nonisolated func pictureInPictureControllerWillStartPictureInPicture(_ c: AVPictureInPictureController) {
        MainActor.assumeIsolated {
            wanted = true // also when it starts by itself, the app going to the background with the page up
            starting = true
            VNCPiPLog.add("小窗要开了")
        }
    }

    nonisolated func pictureInPictureControllerDidStartPictureInPicture(_ c: AVPictureInPictureController) {
        MainActor.assumeIsolated {
            waiting?.cancel()
            VNCPiPLog.add("小窗开了")
            // The small window has it now: the page closes as its × does (ScreenWindowRoot), its connection held here.
            AppModel.shared.vncPiP = ref
        }
    }

    nonisolated func pictureInPictureController(_ c: AVPictureInPictureController,
                                                restoreUserInterfaceForPictureInPictureStopWithCompletionHandler done: @escaping (Bool) -> Void) {
        MainActor.assumeIsolated {
            // 还原 (or a tap on it): the full page, which holds the session before this lets go.
            VNCPiPLog.add("还原成整页")
            restoring = true
            if let ref { AppModel.shared.openScreen(ref, from: from) }
            restoring = false
        }
        done(true)
    }

    nonisolated func pictureInPictureControllerDidStopPictureInPicture(_ c: AVPictureInPictureController) {
        MainActor.assumeIsolated {
            VNCPiPLog.add("小窗关了")
            wanted = false
            starting = false
            AppModel.shared.vncPiP = nil
            // Ready again under the page when it is up (and by itself again when the app goes to the background);
            // else the connection goes.
            if let ref, AppModel.shared.screenOpen?.machine == ref {
                controller?.canStartPictureInPictureAutomaticallyFromInline = true
                return
            }
            end()
        }
    }

    nonisolated func pictureInPictureController(_ c: AVPictureInPictureController, failedToStartPictureInPictureWithError error: Error) {
        Self.log.error("the small window failed to start: \(error.localizedDescription, privacy: .public)")
        MainActor.assumeIsolated {
            starting = false
            guard let ref else { return }
            fallBack(ref, from, why: "系统没让开（\(error.localizedDescription)）")
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

/// What the small window did, step by step (#1839): the last few dozen lines, for 小窗诊断 on the page and the
/// runtime CLI's state; each also goes to the system log (category vnc). No names or passwords.
@MainActor enum VNCPiPLog {
    private(set) static var lines: [String] = []
    static var last: String? { lines.last.map { String($0.drop { $0 != " " }.dropFirst()) } }

    static func yes(_ on: Bool) -> String { on ? "是" : "否" }

    static func add(_ line: String) {
        lines.append(Date.now.formatted(.dateTime.hour(.twoDigits(amPM: .omitted)).minute(.twoDigits).second(.twoDigits)) + " " + line)
        if lines.count > 60 { lines.removeFirst(lines.count - 60) }
        VNCPiP.log.notice("pip: \(line, privacy: .public)")
    }
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
