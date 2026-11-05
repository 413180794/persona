import CoreLocation
import Foundation

/// `phone_location_current`: one-shot fix plus a reverse-geocoded address.
enum LocationTool {
    static var tools: [PhoneTool] {
        [
            PhoneTool(
                spec: ToolSpec(
                    name: "phone_location_current",
                    description: """
                    Get the phone's current location. Returns latitude, longitude, horizontal accuracy \
                    in meters, altitude, the reverse-geocoded address (country, province, city, district, \
                    street, formatted address), the device timezone and the timestamp of the fix \
                    (ISO 8601). Waits at most 15 seconds for a fix. Only call this when the user asks \
                    something that needs their location.
                    """,
                    inputSchema: Schema.object([:])
                ),
                source: .location,
                run: { _ in try await current() }
            )
        ]
    }

    static func current() async throws -> [ToolContent] {
        switch LocationAccess.status() {
        case .granted: break
        case .denied: throw ToolFailure(.denied, "位置 / Location") // i18n-ignore
        case .notDetermined: throw ToolFailure(.denied, "位置尚未授权 / Location access has not been requested yet") // i18n-ignore
        case .limited, .notNeeded: break
        }

        let location = try await OneShotLocation.fetch(timeout: 15)
        let address = await reverseGeocode(location)

        var pairs: [(String, JSONValue?)] = [
            ("latitude", .number(location.coordinate.latitude)),
            ("longitude", .number(location.coordinate.longitude)),
            ("accuracy_meters", .number(location.horizontalAccuracy)),
            ("altitude_meters", .number(location.altitude)),
            ("timestamp", .string(ToolDate.string(location.timestamp))),
            ("timezone", .string(TimeZone.current.identifier)),
        ]
        if let address { pairs.append(("address", address)) }
        return Output.json(Output.object(pairs))
    }

    private static func reverseGeocode(_ location: CLLocation) async -> JSONValue? {
        let geocoder = CLGeocoder()
        let places: [CLPlacemark]
        do {
            places = try await geocoder.reverseGeocodeLocation(location)
        } catch {
            // Geocoding failures are common offline; coordinates are the answer.
            return .object(["note": .string("Reverse geocoding failed: \(error.localizedDescription)")])
        }
        guard let place = places.first else { return nil }
        return Output.object([
            ("name", Output.opt(place.name)),
            ("country", Output.opt(place.country)),
            ("province", Output.opt(place.administrativeArea)),
            ("city", Output.opt(place.locality)),
            ("district", Output.opt(place.subLocality)),
            ("street", Output.opt(place.thoroughfare)),
            ("street_number", Output.opt(place.subThoroughfare)),
            ("postal_code", Output.opt(place.postalCode)),
            ("formatted", Output.opt(formatted(place))),
        ])
    }

    /// Chinese addresses read best joined without separators; fall back to the
    /// placemark's own description elsewhere.
    private static func formatted(_ place: CLPlacemark) -> String? {
        let parts = [place.country, place.administrativeArea, place.locality, place.subLocality,
                     place.thoroughfare, place.subThoroughfare].compactMap { $0 }.filter { !$0.isEmpty }
        if !parts.isEmpty {
            let joined = parts.joined()
            // Avoid "中国中国上海..." when the placemark repeats the country.
            return joined
        }
        return place.name
    }
}

/// Bridges `CLLocationManager.requestLocation()` to async/await with a deadline.
/// The manager's delegate callbacks come back on the main thread; the class is
/// kept out of the main actor so a tool call can await it from any context.
nonisolated final class OneShotLocation: NSObject, CLLocationManagerDelegate, @unchecked Sendable {
    private let manager = CLLocationManager()
    private var pending: SingleResume<CLLocation>?

    static func fetch(timeout: TimeInterval) async throws -> CLLocation {
        let helper = OneShotLocation()
        return try await helper.run(timeout: timeout)
    }

    private func run(timeout: TimeInterval) async throws -> CLLocation {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<CLLocation, any Error>) in
            let gate = SingleResume(continuation)
            pending = gate
            manager.delegate = self
            manager.desiredAccuracy = kCLLocationAccuracyHundredMeters
            manager.requestLocation()

            DispatchQueue.main.asyncAfter(deadline: .now() + timeout) { [weak self] in
                guard let self else { return }
                self.pending = nil
                gate.fail(ToolFailure(.unavailable,
                    "15 秒内没拿到定位 / No location fix within 15 seconds. " // i18n-ignore
                        + "The phone may be indoors or location services may be off."))
            }
        }
    }

    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        guard let location = locations.last else {
            pending?.fail(ToolFailure(.unavailable, "定位服务没有返回位置 / Location services returned no location")) // i18n-ignore
            pending = nil
            return
        }
        pending?.succeed(location)
        pending = nil
    }

    func locationManager(_ manager: CLLocationManager, didFailWithError error: any Error) {
        let code = (error as? CLError)?.code
        let message = code == .denied
            ? "定位被拒绝 / Location access was denied" // i18n-ignore
            : "定位失败 / Location failed: \(error.localizedDescription)" // i18n-ignore
        pending?.fail(ToolFailure(code == .denied ? .denied : .unavailable, message))
        pending = nil
    }
}
