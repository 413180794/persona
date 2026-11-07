#if canImport(CarPlay) && os(iOS)
import CarPlay
import Observation
import UIKit

/// The voice states' action buttons (`CPVoiceControlState.actionButtons`, iOS 26.4). Set through the Objective-C
/// runtime, not the Swift property: the property is only in the iOS 26.4+ SDKs, no compiler version marks those SDKs
/// (Xcode 26.4–26.6 are Swift 6.3), and the `#if compiler(>=6.4)` guard it replaced dropped the buttons from every
/// Xcode 26 build. This compiles with any SDK; the runtime has the setter wherever the voice template is used (26.4+).
@MainActor enum CarPlayVoiceButtons {
    private static let key = "actionButtons"
    private static let setter = NSSelectorFromString("setActionButtons:")

    /// Whether this iOS can show buttons on a voice state.
    static var supported: Bool {
        CPVoiceControlState.instancesRespond(to: setter)
    }

    /// Sets the buttons; false (and nothing set) where the state has no buttons. CarPlay ignores buttons past its limit.
    @discardableResult static func set(_ buttons: [CPButton], on state: NSObject) -> Bool {
        guard state.responds(to: setter) else { return false }
        state.setValue(buttons, forKey: key)
        return true
    }

    /// The buttons a state carries, read the same way (for tests).
    static func buttons(of state: NSObject) -> [CPButton] {
        guard state.responds(to: NSSelectorFromString(key)) else { return [] }
        return state.value(forKey: key) as? [CPButton] ?? []
    }
}

/// The CarPlay scene: launches straight into the voice conversation (Apple's voice-based conversation category).
/// Instantiated by name from Info.plist (`$(PRODUCT_MODULE_NAME).CarPlaySceneDelegate`), so keep the name.
/// The call itself is `VoiceConversation(surface: .carPlay)` via `VoiceCalls` (#3789 / docs/assistant/voice.md 6.4).
@MainActor
final class CarPlaySceneDelegate: UIResponder, CPTemplateApplicationSceneDelegate {
    private var controller: CPInterfaceController?
    /// The voice template on iOS 26.4+, kept to switch its states.
    private var voiceTemplate: CPVoiceControlTemplate?
    /// The reason the current voice template's ended state was built with.
    private var builtReason: String?
    /// The current voice template is on screen: CarPlay ignores state changes made before that.
    private var presented = false
    /// Whether the next "became active" starts the conversation (see CarPlayAutoStart).
    private var autoStart = CarPlayAutoStart()
    /// A start that failed before `VoiceCalls` took the call (no hub, no mic…): shown as the ended headline.
    private var startFailure: String?
    /// True while `beginCall` / `resumeCall` is in flight: the template stays on connecting, not Ended.
    private var starting = false
    /// Watches `VoiceCalls` / the car call for phase changes.
    private var watchTask: Task<Void, Never>?

    // MARK: CarPlay connection

    func templateApplicationScene(_ templateApplicationScene: CPTemplateApplicationScene,
                                  didConnect interfaceController: CPInterfaceController) {
        controller = interfaceController
        CarPlayVoicePresence.isConnected = true
        autoStart.arm()
        if #available(iOS 26.4, *) {
            present(makeVoiceTemplate(endedReason: nil, initial: currentStateID), reason: nil)
            watchCall()
        } else {
            interfaceController.setRootTemplate(Self.infoTemplate(), animated: false, completion: nil)
        }
    }

    func templateApplicationScene(_ templateApplicationScene: CPTemplateApplicationScene,
                                  didDisconnectInterfaceController interfaceController: CPInterfaceController) {
        watchTask?.cancel()
        watchTask = nil
        CarPlayVoiceSession.endFromBackground()
        CarPlayVoicePresence.isConnected = false
        CarPlayVoicePresence.isForeground = false
        controller = nil
        voiceTemplate = nil
        builtReason = nil
        presented = false
        startFailure = nil
        starting = false
        autoStart = CarPlayAutoStart()
    }

    // MARK: Scene lifecycle

    func sceneWillEnterForeground(_ scene: UIScene) {
        CarPlayVoicePresence.isForeground = true
        autoStart.arm()
    }

    func sceneDidBecomeActive(_ scene: UIScene) {
        // Voice first: the conversation starts as soon as the screen is there, but only once per connection or return
        // to the foreground (CarPlayAutoStart). Recording in CarPlay is only allowed together with the voice control
        // template, so never before 26.4.
        guard #available(iOS 26.4, *) else { return }
        CarPlayVoicePresence.isForeground = true
        if autoStart.takeOnBecomingActive(), !CarPlayVoiceSession.isRunning {
            starting = true
            Task { await self.beginCall() }
        }
    }

    func sceneDidEnterBackground(_ scene: UIScene) {
        // Apple: the voice screen must be on screen while voice is active, so the audio session is given up here.
        CarPlayVoicePresence.isForeground = false
        CarPlayVoiceSession.endFromBackground()
        startFailure = CarPlayVoiceScreen.endedHeadline(for: .left)
        if #available(iOS 26.4, *) { showEndedOrCall() }
    }

    // MARK: Templates

    /// Shown where the voice template is not available (iOS before 26.4). A list is one of the templates this app
    /// category may use; in practice CarPlay only lists the app from 26.4 on, when the category exists.
    private static func infoTemplate() -> CPListTemplate {
        let item = CPListItem(text: "Persona", detailText: String(localized: "Long voice chats with Persona in the car need iOS 26.4 or later"))
        return CPListTemplate(title: "Persona", sections: [CPListSection(items: [item])])
    }

    /// `initial` is the state the template opens on (see `CarPlayVoiceScreen.states`).
    @available(iOS 26.4, *)
    private func makeVoiceTemplate(endedReason: String?, initial: String) -> CPVoiceControlTemplate {
        let buttonsShown = CarPlayVoiceButtons.supported
        let specs = CarPlayVoiceScreen.states(endedReason: endedReason, initial: initial, buttonsShown: buttonsShown)
        let states = specs.map { spec -> CPVoiceControlState in
            let image = Self.symbol(spec.icon, pointSize: Theme.Size.carPlayVoiceImage)
            let state = CPVoiceControlState(identifier: spec.id, titleVariants: spec.titles, image: image, repeats: spec.repeats)
            if buttonsShown {
                CarPlayVoiceButtons.set(spec.buttons.map(Self.button), on: state)
            }
            return state
        }
        return CPVoiceControlTemplate(voiceControlStates: states)
    }

    private static func button(_ b: CarPlayVoiceButton) -> CPButton {
        let button = CPButton(image: symbol(CarPlayVoiceScreen.icon(b), pointSize: Theme.Size.carPlayVoiceImage),
                              handler: { @Sendable _ in
                                  Task { @MainActor in CarPlaySceneDelegate.handle(b) }
                              })
        button.title = CarPlayVoiceScreen.title(b)
        return button
    }

    /// Starts the shared engine and mirrors its phase on the template.
    private func beginCall() async {
        starting = true
        startFailure = nil
        defer { starting = false }
        if let failure = await CarPlayVoiceSession.start() {
            startFailure = failure
        }
        if #available(iOS 26.4, *) { showEndedOrCall() }
    }

    private func resumeCall() async {
        starting = true
        startFailure = nil
        defer { starting = false }
        if let failure = await CarPlayVoiceSession.resume() {
            startFailure = failure
        }
        if #available(iOS 26.4, *) { showEndedOrCall() }
    }

    /// Makes the template show the call's state; the ended state is rebuilt when its sentence changes.
    @available(iOS 26.4, *)
    private func showEndedOrCall() {
        let reason = endedReasonNow
        let ends = currentStateID == "ended"
        guard controller != nil else { return }
        if ends, reason != builtReason || voiceTemplate == nil {
            present(makeVoiceTemplate(endedReason: reason, initial: "ended"), reason: reason)
        } else {
            activateCurrentState()
        }
    }

    private var endedReasonNow: String? {
        if let startFailure { return startFailure }
        if let call = CarPlayVoiceSession.call, case .ended(let end) = call.phase {
            return CarPlayVoiceScreen.endedHeadline(for: end)
        }
        return nil
    }

    /// The state to show now: the car call's phase, or connecting while it is about to start by itself.
    private var currentStateID: String {
        let call = CarPlayVoiceSession.call
        let busy = call.map { c in
            guard let state = AppModel.shared.latest(c.key)?.state else { return false }
            return state == .working || state == .needsYou
        } ?? false
        return CarPlayVoiceScreen.openingStateID(
            for: call?.phase,
            startingNow: starting && startFailure == nil,
            tools: call?.tools.count ?? 0,
            asking: call?.askingID != nil,
            targetBusy: busy,
            link: call?.link ?? .live
        )
    }

    private func watchCall() {
        watchTask?.cancel()
        watchTask = Task { @MainActor [weak self] in
            while let self, !Task.isCancelled {
                if #available(iOS 26.4, *) { self.showEndedOrCall() }
                await withCheckedContinuation { (cont: CheckedContinuation<Void, Never>) in
                    withObservationTracking {
                        _ = VoiceCalls.shared.current?.phase
                        _ = VoiceCalls.shared.current?.tools.count
                        _ = VoiceCalls.shared.current?.askingID
                        _ = VoiceCalls.shared.current?.link
                        _ = VoiceCalls.shared.current?.surface
                    } onChange: {
                        cont.resume()
                    }
                }
            }
        }
    }

    /// Makes `template` the root. Until CarPlay says it is on screen, state changes wait; then the phase at that moment
    /// is shown (it may have moved on while the template was being presented).
    @available(iOS 26.4, *)
    private func present(_ template: CPVoiceControlTemplate, reason: String?) {
        guard let controller else { return }
        voiceTemplate = template
        builtReason = reason
        presented = false
        controller.setRootTemplate(template, animated: false) { [weak self] _, _ in
            Task { @MainActor [weak self] in
                guard let self, self.voiceTemplate === template else { return }
                self.presented = true
                self.activateCurrentState()
            }
        }
    }

    /// Switches the presented template to the session's phase; nothing while it is not on screen yet.
    @available(iOS 26.4, *)
    private func activateCurrentState() {
        guard presented, let voiceTemplate else { return }
        let id = currentStateID
        if voiceTemplate.activeStateIdentifier != id {
            voiceTemplate.activateVoiceControlState(withIdentifier: id)
        }
    }

    /// A symbol image for the CarPlay voice template; an empty image if the name is not a symbol on this OS.
    private static func symbol(_ name: String, pointSize: CGFloat) -> UIImage {
        UIImage(systemName: name, withConfiguration: UIImage.SymbolConfiguration(pointSize: pointSize)) ?? UIImage()
    }

    // MARK: Buttons

    /// A voice screen button pressed in the car.
    static func handle(_ b: CarPlayVoiceButton) {
        switch b {
        case .end:
            CarPlayVoiceSession.endFromUser()
        case .interrupt:
            CarPlayVoiceSession.interrupt()
        case .resume:
            guard let delegate = Self.activeDelegate else {
                Task { _ = await CarPlayVoiceSession.resume() }
                return
            }
            Task { await delegate.resumeCall() }
        }
    }

    /// The connected scene's delegate, when CarPlay is up (button handlers have no `self`).
    private static var activeDelegate: CarPlaySceneDelegate? {
        UIApplication.shared.connectedScenes
            .compactMap { $0.delegate as? CarPlaySceneDelegate }
            .first
    }
}
#endif
