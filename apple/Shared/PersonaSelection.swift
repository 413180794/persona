import Foundation

/// The only supported personas. Resolve legacy and unknown IDs without erasing preferences needed by older builds.
nonisolated enum PersonaSelection {
    static let builtinID = "persona"
    static let packageID = "nwverse"

    static func resolve(_ id: String?) -> String {
        id == packageID ? packageID : builtinID
    }

    static func supports(_ id: String) -> Bool {
        id == builtinID || id == packageID
    }
}
