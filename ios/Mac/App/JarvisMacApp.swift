import SwiftUI

/// The macOS app (docs/mac.md): one AppModel, as on iOS, behind a Mac shell.
@main
struct JarvisMacApp: App {
    @State private var model = AppModel.shared

    var body: some Scene {
        WindowGroup {
            Text("Jarvis")
                .environment(model)
        }
    }
}
