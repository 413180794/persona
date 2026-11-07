import Foundation

// Meta glasses through Meta's Wearables Device Access Toolkit (docs/apple/meta-glasses.md). The SDK is compiled in only when
// the build config enables it (PERSONA_META_WEARABLES=YES, apple/meta-wearables.yml). Otherwise `isBuiltIn` is false and
// the calls below are no-ops or throw, so the rest of the app never needs to know which build it is.

/// An error to show in the UI as it is (Chinese text, or the SDK's own description).
nonisolated struct GlassesError: LocalizedError, Sendable {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

#if os(iOS) && canImport(MWDATCore) && canImport(MWDATCamera)
import MWDATCamera
import MWDATCore
import Observation
import UIKit

/// The glasses' registration, camera permission, preview and photos. One session at a time.
@Observable
@MainActor
final class MetaGlasses {
    static let shared = MetaGlasses()
    /// True only when the Meta Wearables SDK is compiled in.
    static let isBuiltIn = true

    /// The glasses are registered with this app through the Meta AI app.
    private(set) var registered = false
    /// The latest preview frame while a session runs; nil otherwise.
    private(set) var preview: UIImage?
    /// The last error to show in the UI.
    private(set) var error: String?

    @ObservationIgnored private var configured = false
    @ObservationIgnored private var sdkReady = false
    @ObservationIgnored private var registrationTask: Task<Void, Never>?
    @ObservationIgnored private var session: DeviceSession?
    @ObservationIgnored private var camera: MWDATCamera.Camera?
    @ObservationIgnored private let sessionTokens = ListenerTokenBag()
    @ObservationIgnored private let streamTokens = ListenerTokenBag()
    @ObservationIgnored private var photoContinuation: CheckedContinuation<Data, Error>?
    @ObservationIgnored private var photoTimeout: Task<Void, Never>?

    private init() {}

    /// Called once at launch. Does nothing after the first call.
    func configure() {
        guard !configured else { return }
        configured = true
        do {
            try Wearables.configure()
            sdkReady = true
        } catch {
            self.error = String(localized: "The Meta glasses are not ready: \(error.localizedDescription)")
            return
        }
        registered = Wearables.shared.registrationState == .registered
        registrationTask = Task { [weak self] in
            for await state in Wearables.shared.registrationStateStream() {
                self?.registered = state == .registered
            }
        }
    }

    /// Takes a Meta AI callback URL. False when the URL is not one of the SDK's.
    func handle(_ url: URL) -> Bool {
        let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        guard components?.queryItems?.contains(where: { $0.name == "metaWearablesAction" }) == true else { return false }
        guard sdkReady else { return true }
        Task { @MainActor [weak self] in
            do {
                _ = try await Wearables.shared.handleUrl(url)
                self?.registered = Wearables.shared.registrationState == .registered
            } catch {
                self?.error = error.localizedDescription
            }
        }
        return true
    }

    /// Switches to the Meta AI app to register the glasses. Call only after the user tapped a button that says so.
    func connect() async throws {
        error = nil
        guard sdkReady else { throw GlassesError(String(localized: "The Meta glasses are not ready yet")) }
        guard Wearables.shared.registrationState != .registering else { return }
        do {
            try await Wearables.shared.startRegistration()
        } catch {
            self.error = error.localizedDescription
            throw error
        }
    }

    func cameraAllowed() async -> Bool {
        guard sdkReady else { return false }
        return (try? await Wearables.shared.checkPermissionStatus(.camera)) == .granted
    }

    /// Switches to the Meta AI app to allow the camera. Call only after the user tapped a button that says so.
    func requestCamera() async throws -> Bool {
        error = nil
        guard sdkReady else { throw GlassesError(String(localized: "The Meta glasses are not ready yet")) }
        do {
            return try await Wearables.shared.requestPermission(.camera) == .granted
        } catch {
            self.error = error.localizedDescription
            throw error
        }
    }

    /// Starts the session and the camera's preview stream. Does nothing when a session already runs.
    func startPreview() async throws {
        guard session == nil else { return }
        guard sdkReady else { throw GlassesError(String(localized: "The Meta glasses are not ready yet")) }
        error = nil
        let newSession: DeviceSession
        do {
            newSession = try Wearables.shared.createSession(deviceSelector: AutoDeviceSelector(wearables: Wearables.shared))
        } catch {
            self.error = error.localizedDescription
            throw error
        }
        session = newSession
        do {
            try await openPreview(newSession)
        } catch {
            if !(error is CancellationError) {
                self.error = error.localizedDescription
            }
            if self.session === newSession {
                stop()
            } else {
                newSession.stop()
            }
            throw error
        }
    }

    /// Takes one photo from the glasses. Falls back to the latest preview frame when the glasses refuse the shutter.
    func capture() async throws -> UIImage {
        error = nil
        do {
            return try await takePhoto()
        } catch {
            self.error = error.localizedDescription
            throw error
        }
    }

    /// Ends the camera and the session. Safe to call any number of times.
    func stop() {
        let camera = self.camera
        let session = self.session
        release()
        camera?.stop()
        session?.stop()
    }

    // MARK: - Session

    private func openPreview(_ newSession: DeviceSession) async throws {
        newSession.statePublisher.listen { @Sendable [weak self, weak newSession] state in
            Task { @MainActor in
                guard let self, let newSession, self.session === newSession else { return }
                self.sessionChanged(state)
            }
        }.store(in: sessionTokens)
        newSession.errorPublisher.listen { @Sendable [weak self, weak newSession] sessionError in
            let message = sessionError.localizedDescription
            Task { @MainActor in
                guard let self, let newSession, self.session === newSession else { return }
                self.error = message
            }
        }.store(in: sessionTokens)

        try newSession.start()
        try await waitUntilStarted(newSession)
        guard self.session === newSession else { throw CancellationError() }
        try addCamera(to: newSession)
    }

    /// The session's state is polled, not streamed: the wait has a deadline and the state is read on the main actor.
    private func waitUntilStarted(_ newSession: DeviceSession) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(15))
        while newSession.state != .started {
            guard self.session === newSession else { throw CancellationError() }
            if newSession.state == .stopped {
                throw GlassesError(String(localized: "The glasses are not connected. Make sure you are wearing them and they are connected to Meta AI"))
            }
            guard ContinuousClock.now < deadline else {
                throw GlassesError(String(localized: "The glasses are not connected. Make sure you are wearing them and they are connected to Meta AI"))
            }
            try await Task.sleep(for: .milliseconds(200))
        }
    }

    private func addCamera(to newSession: DeviceSession) throws {
        let config = StreamConfiguration(videoCodec: .raw, resolution: .medium, frameRate: 24)
        guard let newCamera = try newSession.addCamera(config: config) else {
            throw GlassesError(String(localized: "Couldn't open the glasses' camera. Try again"))
        }
        camera = newCamera
        newCamera.stream.videoFramePublisher.listen { @Sendable [weak self, weak newSession] frame in
            let image: UIImage? = frame.makeUIImage()
            Task { @MainActor in
                guard let self, let newSession, self.session === newSession else { return }
                self.preview = image
            }
        }.store(in: streamTokens)
        newCamera.stream.errorPublisher.listen { @Sendable [weak self, weak newSession] streamError in
            let message = streamError.localizedDescription
            Task { @MainActor in
                guard let self, let newSession, self.session === newSession else { return }
                self.error = message
            }
        }.store(in: streamTokens)
        newCamera.stream.photoDataPublisher.listen { @Sendable [weak self, weak newSession] photo in
            let bytes: Data = photo.data
            Task { @MainActor in
                guard let self, let newSession, self.session === newSession else { return }
                self.finishPhoto(.success(bytes))
            }
        }.store(in: streamTokens)
        newCamera.stream.start()
    }

    private func sessionChanged(_ state: DeviceSessionState) {
        // The SDK ended the session (glasses off, Meta AI closed): drop our references, the SDK has stopped it already.
        if state == .stopped { release() }
    }

    /// Clears every reference and listener. Does not stop the SDK objects; `stop()` does that.
    private func release() {
        streamTokens.clear()
        sessionTokens.clear()
        camera = nil
        session = nil
        preview = nil
        finishPhoto(.failure(GlassesError(String(localized: "The glasses disconnected; no photo was taken"))))
    }

    // MARK: - Photo

    private func takePhoto() async throws -> UIImage {
        guard let camera = self.camera, session != nil else { throw GlassesError(String(localized: "The glasses aren't connected yet. Wait for the preview first")) }
        guard photoContinuation == nil else { throw GlassesError(String(localized: "Taking a photo, one moment")) }
        guard camera.stream.capturePhoto(format: .jpeg) else {
            if let preview { return preview }
            throw GlassesError(String(localized: "The photo didn't work. Try again"))
        }
        let bytes = try await nextPhotoData()
        guard let image = UIImage(data: bytes) else { throw GlassesError(String(localized: "Couldn't read the photo. Take it again")) }
        return image
    }

    /// Waits for the next photo data. The continuation is stored before this function suspends, so a photo that
    /// arrives right after the shutter cannot be missed.
    private func nextPhotoData() async throws -> Data {
        photoTimeout?.cancel()
        photoTimeout = Task { @MainActor [weak self] in
            do {
                try await Task.sleep(for: .seconds(10))
            } catch {
                return
            }
            self?.finishPhoto(.failure(GlassesError(String(localized: "Taking the photo timed out. Check the glasses are still connected and try again"))))
        }
        return try await withCheckedThrowingContinuation { continuation in
            photoContinuation = continuation
        }
    }

    private func finishPhoto(_ result: Result<Data, Error>) {
        photoTimeout?.cancel()
        photoTimeout = nil
        guard let continuation = photoContinuation else { return }
        photoContinuation = nil
        switch result {
        case .success(let bytes): continuation.resume(returning: bytes)
        case .failure(let failure): continuation.resume(throwing: failure)
        }
    }
}

#else

import Observation
#if os(iOS)
import UIKit
#endif

/// Stub for builds without the Meta Wearables SDK: the same surface, nothing to connect.
@Observable
@MainActor
final class MetaGlasses {
    static let shared = MetaGlasses()
    /// True only when the Meta Wearables SDK is compiled in.
    static let isBuiltIn = false

    private(set) var registered = false
    private(set) var error: String?

    private init() {}

    func configure() {}

    func handle(_ url: URL) -> Bool { false }

    func connect() async throws {
        throw GlassesError(String(localized: "This build has no Meta glasses support"))
    }

    func cameraAllowed() async -> Bool { false }

    func requestCamera() async throws -> Bool {
        throw GlassesError(String(localized: "This build has no Meta glasses support"))
    }

    func stop() {}

    #if os(iOS)
    private(set) var preview: UIImage?

    func startPreview() async throws {
        throw GlassesError(String(localized: "This build has no Meta glasses support"))
    }

    func capture() async throws -> UIImage {
        throw GlassesError(String(localized: "This build has no Meta glasses support"))
    }
    #endif
}

#endif
